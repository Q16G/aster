package react

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"aster/internal/builtin_tools"
	"aster/internal/runtimelog"
	"aster/internal/utils/argx"
)

const (
	defaultSubAgentMaxIter = 1000
)

type SubAgentTool struct {
	parentAgent *Agent
	factory     *AgentFactory
}

var _ AgentTool = (*SubAgentTool)(nil)

func NewSubAgentTool(parent *Agent, factory *AgentFactory) *SubAgentTool {
	return &SubAgentTool{parentAgent: parent, factory: factory}
}

func (t *SubAgentTool) Name() string  { return builtin_tools.SubAgentToolName }
func (t *SubAgentTool) IsAgent() bool { return true }

func (t *SubAgentTool) Description() string {
	return "派生一个子 Agent 独立执行委派任务。遇到相互独立、可并行、专业性强或耗时较长的子任务时，应优先考虑委派给 sub_agent，而不是全部自己串行完成。"
}

func (t *SubAgentTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"instruction": map[string]any{
				"type":        "string",
				"description": "子 Agent 的完整指令，包含角色定义、任务目标、执行约束和输出要求。",
			},
			"tools": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "可选：工具白名单。非空时子 Agent 只能使用指定工具；为空则继承父 Agent 工具集。",
			},
			"context": map[string]any{
				"type":        "string",
				"description": "可选：传递给子 Agent 的显式上下文信息，优先于系统自动注入的交接上下文。",
			},
			"run_in_background": map[string]any{
				"type":        "boolean",
				"description": "可选：异步执行子 Agent，立即返回 agent_id。适合长耗时或可并行子任务。完成后结果会自动推送到上下文；启动后调用 await_subagents 让出执行权等待完成，不要紧密轮询 sub_agent_status。",
			},
		},
		"required":             []string{"instruction"},
		"additionalProperties": false,
	}
}

type childAgentSetup struct {
	childName    string
	childAgent   *Agent
	childRootDir string
	execOpts     []ExecuteOption
	instruction  string
	runtime      builtin_tools.ToolRuntimeInfo
}

func (t *SubAgentTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.parentAgent == nil || t.factory == nil {
		return "", fmt.Errorf("sub_agent tool not initialized")
	}

	runtime, _ := builtin_tools.GetToolRuntime(ctx)
	if runtime.StackDepth > 0 {
		return "", fmt.Errorf("sub_agent is disabled inside sub-agents (stack_depth=%d)", runtime.StackDepth)
	}

	setup, err := t.buildChild(ctx, args, runtime)
	if err != nil {
		return "", err
	}

	runInBackground, _ := args["run_in_background"].(bool)
	if runInBackground {
		return t.executeAsync(ctx, setup)
	}
	return t.executeSync(ctx, setup)
}

func (t *SubAgentTool) buildChild(ctx context.Context, args map[string]any, runtime builtin_tools.ToolRuntimeInfo) (*childAgentSetup, error) {
	instruction, err := argx.RequiredText(args, "instruction")
	if err != nil {
		return nil, fmt.Errorf("instruction is required")
	}
	toolNames := argx.StringSlice(args["tools"])
	explicitContext := argx.OptionalText(args, "context")
	handoffContext := argx.OptionalText(args, "__handoff_context__")

	callID := runtime.CallID
	if callID == "" {
		callID = generateRandomString(8)
	}
	childName := fmt.Sprintf("sub-%s", truncateID(callID, 8))

	childDef := AgentDefinition{
		Name:        childName,
		Instruction: instruction,
		ToolNames:   t.resolveChildToolNames(toolNames),
		IsSubAgent:  true,
		Policies: AgentPolicies{
			MaxIterations: t.childMaxIterations(),
		},
	}

	if contextEntries := buildSubAgentContextEntries(explicitContext, handoffContext); len(contextEntries) > 0 {
		childDef.Context = contextEntries
	}

	if t.parentAgent.cfg != nil && t.parentAgent.cfg.BashTool != nil {
		childDef.Policies.AllowBash = true
		childDef.Policies.BashPermissionContext = t.parentAgent.cfg.BashTool
	}

	childRootDir := filepath.Join(t.parentAgent.workspaceRootDir, "sub_agents", childName)
	childRuntime, err := newLocalWorkspaceRuntime(
		t.parentAgent.workspaceSessionID,
		childRootDir,
		"root",
	)
	if err != nil {
		return nil, fmt.Errorf("create child workspace: %w", err)
	}

	childAgent, err := t.factory.Build(childDef)
	if err != nil {
		return nil, fmt.Errorf("build sub agent: %w", err)
	}

	t.preRegisterChildAgent(runtime, childName, childRootDir)

	execOpts := []ExecuteOption{
		WithWorkspaceRuntime(childRuntime),
		WithParentWorkspace(t.parentAgent.workspaceRootDir),
		WithSkipIntentPrelude(),
	}
	if tc := childDef.BuildTaskContext(); tc != nil {
		execOpts = append(execOpts, WithTaskContext(tc))
	}

	return &childAgentSetup{
		childName:    childName,
		childAgent:   childAgent,
		childRootDir: childRootDir,
		execOpts:     execOpts,
		instruction:  instruction,
		runtime:      runtime,
	}, nil
}

func (t *SubAgentTool) executeSync(ctx context.Context, setup *childAgentSetup) (string, error) {
	result, err := setup.childAgent.Execute(ctx, setup.instruction, setup.execOpts...)
	t.finalizeChildAgent(setup.runtime, setup.childName, setup.childRootDir, result)
	if err != nil {
		return "", fmt.Errorf("sub agent execution: %w", err)
	}
	return formatSubAgentResult(setup.childName, setup.childRootDir, result), nil
}

func (t *SubAgentTool) executeAsync(ctx context.Context, setup *childAgentSetup) (string, error) {
	registry := t.parentAgent.ensureAsyncRegistry()

	instrSummary := setup.instruction
	if len([]rune(instrSummary)) > 200 {
		instrSummary = string([]rune(instrSummary)[:200]) + "..."
	}
	registry.Register(setup.childName, instrSummary, setup.childRootDir)

	// Bridge the background lifecycle to the TUI: the launcher tool's own
	// tool_start/tool_end collapse to near-zero here (executeAsync returns
	// immediately), so the right-side sub-agent panel would never see a running
	// card. Emit a durable start event keyed by agent_id; the matching end event
	// is emitted from drainAsyncAgentNotifications when the child completes.
	t.parentAgent.emitter.EmitJSON(EventTypeSubAgentBgStart, setup.childName, map[string]any{
		"agent_id":    setup.childName,
		"tool_name":   builtin_tools.SubAgentToolName,
		"instruction": instrSummary,
		"workspace":   setup.childRootDir,
	})

	// ctx is the parent scheduler's context: child is cancelled when parent finishes or is aborted.
	go func() {
		var result *builtin_tools.RunResult
		defer func() {
			if r := recover(); r != nil {
				result = &builtin_tools.RunResult{
					Success: false,
					Error:   fmt.Sprintf("sub agent panicked: %v", r),
				}
			}
			t.finalizeChildAgent(setup.runtime, setup.childName, setup.childRootDir, result)
			registry.Complete(setup.childName, result)
		}()

		var err error
		result, err = setup.childAgent.Execute(ctx, setup.instruction, setup.execOpts...)
		if err != nil && result == nil {
			result = &builtin_tools.RunResult{
				Success: false,
				Error:   err.Error(),
			}
		}
	}()

	out, _ := json.Marshal(map[string]any{
		"status":    "async_launched",
		"agent_id":  setup.childName,
		"workspace": setup.childRootDir,
	})
	return string(out), nil
}

func buildSubAgentContextEntries(explicitContext, handoffContext string) []TaskContextEntry {
	explicitContext = strings.TrimSpace(explicitContext)
	handoffContext = strings.TrimSpace(handoffContext)

	entries := make([]TaskContextEntry, 0, 2)
	if explicitContext != "" {
		entries = append(entries, TaskContextEntry{
			Label:       "委派上下文",
			Value:       explicitContext,
			Description: "父 Agent 传递的显式上下文；若与交接上下文冲突，以此为准",
		})
	}
	if handoffContext != "" && handoffContext != explicitContext {
		entries = append(entries, TaskContextEntry{
			Label:       "交接上下文",
			Value:       handoffContext,
			Description: "父 Agent 自动注入的已完成步骤与摘要上下文，仅作补充；若与显式上下文冲突，以显式上下文为准",
		})
	}
	return entries
}

func (t *SubAgentTool) childMaxIterations() int {
	return defaultSubAgentMaxIter
}

func (t *SubAgentTool) preRegisterChildAgent(runtime builtin_tools.ToolRuntimeInfo, childName, childRootDir string) {
	if t.parentAgent == nil || t.parentAgent.workspaceRuntime == nil {
		return
	}
	parentState, err := t.parentAgent.workspaceRuntime.LoadWorkspaceState()
	if err != nil || parentState == nil {
		return
	}
	parentState.ChildAgents[childName] = &builtin_tools.WorkspaceChildAgentPointer{
		Status:          "running",
		ParentStepKey:   strings.TrimSpace(runtime.CurrentStepID),
		ArtifactRootDir: childRootDir,
	}
	if err := t.parentAgent.workspaceRuntime.SaveWorkspaceState(parentState); err != nil {
		runtimelog.LogJSON("warning", map[string]any{
			"event": "pre_register_child_agent_save_failed",
			"child": childName,
			"error": err.Error(),
		})
	}
}

func (t *SubAgentTool) finalizeChildAgent(runtime builtin_tools.ToolRuntimeInfo, childName, childRootDir string, result *builtin_tools.RunResult) {
	if t.parentAgent == nil || t.parentAgent.workspaceRuntime == nil {
		return
	}
	parentState, err := t.parentAgent.workspaceRuntime.LoadWorkspaceState()
	if err != nil || parentState == nil {
		return
	}
	status := "completed"
	if result == nil || !result.Success {
		status = "failed"
	}
	ptr := &builtin_tools.WorkspaceChildAgentPointer{
		Status:          status,
		ParentStepKey:   strings.TrimSpace(runtime.CurrentStepID),
		ArtifactRootDir: childRootDir,
	}
	if result != nil {
		ptr.PlanSummary = result.PlanSummary
	}
	parentState.ChildAgents[childName] = ptr
	if err := t.parentAgent.workspaceRuntime.SaveWorkspaceState(parentState); err != nil {
		runtimelog.LogJSON("warning", map[string]any{
			"event": "finalize_child_agent_save_failed",
			"child": childName,
			"error": err.Error(),
		})
	}
}

func (t *SubAgentTool) resolveChildToolNames(requested []string) []string {
	if len(requested) > 0 {
		parentSet := make(map[string]bool)
		if t.parentAgent != nil && t.parentAgent.tools != nil {
			for _, name := range t.parentAgent.tools.Keys() {
				parentSet[name] = true
			}
		}
		var result []string
		for _, name := range requested {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if policyManagedTools[name] {
				continue
			}
			if parentSet[name] || (t.factory.toolRegistry != nil && t.factory.toolRegistry.Has(name)) {
				result = append(result, name)
			}
		}
		return result
	}
	return t.parentDomainToolNames()
}

// policyManagedTools lists tools hard-wired by NewReActAgent / AgentFactory.Build
// outside the ToolRegistry. They must be stripped from ToolNames so that
// resolveChildToolNames never passes them to factory.resolveTools (which would
// fail with "tool not registered").
var policyManagedTools = map[string]bool{
	builtin_tools.UpdateCurrentStepToolName: true,
	builtin_tools.TaskStatusQueryToolName:   true,
	builtin_tools.HumanConfirmToolName:      true,
	builtin_tools.SubAgentToolName:          true,
	builtin_tools.SubAgentStatusToolName:    true,
	builtin_tools.AwaitSubAgentsToolName:    true,
	builtin_tools.BashToolName:              true,
	builtin_tools.SkillToolName:             true,
}

// excludeFromInheritance is the full set of platform / orchestration tools
// that should NOT be auto-inherited when the child agent uses the default
// (no explicit tools) path. It is a superset of policyManagedTools: registry-
// resident skill-management tools are also excluded because sub-agents should
// not manage skills by default.
var excludeFromInheritance = map[string]bool{
	builtin_tools.UpdateCurrentStepToolName: true,
	builtin_tools.TaskStatusQueryToolName:   true,
	builtin_tools.HumanConfirmToolName:      true,
	builtin_tools.SubAgentToolName:          true,
	builtin_tools.SubAgentStatusToolName:    true,
	builtin_tools.AwaitSubAgentsToolName:    true,
	builtin_tools.BashToolName:              true,
	builtin_tools.SkillToolName:             true,
	builtin_tools.LoadSkillsToolName:        true,
	builtin_tools.ListSkillsToolName:        true,
	builtin_tools.DeleteSkillToolName:       true,
}

func (t *SubAgentTool) parentDomainToolNames() []string {
	if t.parentAgent == nil {
		return nil
	}
	var names []string
	for _, name := range t.parentAgent.tools.Keys() {
		if excludeFromInheritance[name] {
			continue
		}
		names = append(names, name)
	}
	return names
}

func truncateID(id string, maxLen int) string {
	id = strings.TrimSpace(id)
	if len(id) > maxLen {
		return id[:maxLen]
	}
	if id == "" {
		return generateRandomString(maxLen)
	}
	return id
}

func formatSubAgentResult(agentName, workspaceRoot string, result *builtin_tools.RunResult) string {
	status := "completed"
	summary := ""
	errText := ""
	ok := false
	if result != nil {
		ok = result.Success
		if result.Success {
			summary = result.Result
		} else {
			status = "failed"
			errText = result.Error
			summary = result.Result
		}
	} else {
		status = "failed"
		errText = "no result"
	}
	payload := map[string]any{
		"ok":             ok,
		"agent_name":     agentName,
		"workspace_root": workspaceRoot,
		"status":         status,
		"summary":        summary,
		"error":          errText,
	}
	if result != nil && result.PlanSummary != nil {
		payload["plan_summary"] = result.PlanSummary
	}
	out, _ := json.Marshal(payload)
	return string(out)
}
