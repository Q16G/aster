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
	"aster/internal/workspacefs"
)

const (
	defaultSubAgentMaxIter = 0 // <=0 表示子 Agent 默认不限制迭代次数
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
	return "派生子 Agent 独立执行任务。当前阶段需处理同手段/同产物口径独立可验收的子任务集（已落清单）时是默认动作；自己串行处理是反模式。"
}

func (t *SubAgentTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"role": map[string]any{
				"type":        "string",
				"description": "可选：子 Agent 身份/职业定位，一句话画出是什么角色，限定其判断的语义维度（例如某专业域的资深从业者）；不要把规矩/输出要求写进来。",
			},
			"background": map[string]any{
				"type":        "string",
				"description": "可选：子 Agent 背景知识与熟悉度，描述这位角色懂什么、做过什么、对什么有偏好；不要把任务上下文、路径等具体值写进来。",
			},
			"instruction": map[string]any{
				"type":        "string",
				"description": "子 Agent 的指令性约束：按什么规矩干、偏好、强约定与输出要求；身份和背景请走 role / background 字段，不要混进此处。",
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
			"subagent_type": map[string]any{
				"type":        "string",
				"enum":        []string{subAgentTypeExplore, subAgentTypeGeneralPurpose},
				"description": "可选：子 Agent 类型。explore=只读检索取证（查找/定位/取证，不改被审对象）；general-purpose=通用执行（可动手改动型动作）。缺省 general-purpose。类型只影响职责描述，不改变可用工具。",
			},
			"run_in_background": map[string]any{
				"type":        "boolean",
				"description": "可选：异步执行子 Agent，立即返回 agent_id。完成后结果会自动推送到上下文。",
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
	// taskContext 是拼给子 Agent 首条 user 消息的随附上下文（B8：不再走 TaskContextEntry）。
	taskContext string
	// spec 是本次委派的子 Agent 类型（描述注入 prompt `## 本次委派类型`，U1 类型只差描述）。
	spec    SubAgentType
	runtime builtin_tools.ToolRuntimeInfo
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
	role := strings.TrimSpace(argx.OptionalText(args, "role"))
	background := strings.TrimSpace(argx.OptionalText(args, "background"))
	toolNames := argx.StringSlice(args["tools"])
	explicitContext := argx.OptionalText(args, "context")
	handoffContext := argx.OptionalText(args, "__handoff_context__")
	// 类型只影响 prompt 职责描述（U1）；工具集统一走 resolveChildToolNames，不按类型裁剪。
	spec := resolveSubAgentType(argx.OptionalText(args, "subagent_type"))
	// B8：委派上下文拼成单串进子 Agent 首条 user 消息（不再走 TaskContextEntry / identity env）。
	taskContext := mergeSubAgentContext(explicitContext, handoffContext)

	callID := runtime.CallID
	if callID == "" {
		callID = generateRandomString(8)
	}
	childName := fmt.Sprintf("sub-%s", childAgentToken(callID))

	// 无 childDef.Policies.MaxIterations（U2：子 loop 无 MaxTurns，由 parent ctx 兜底）；
	// AgentPolicies.MaxIterations 字段本身保留（主 Execute 仍用，B9）。
	childDef := AgentDefinition{
		Name:        childName,
		Role:        role,
		Background:  background,
		Instruction: instruction,
		ToolNames:   t.resolveChildToolNames(toolNames),
		IsSubAgent:  true,
	}

	if t.parentAgent.cfg != nil && t.parentAgent.cfg.BashTool != nil {
		childDef.Policies.AllowBash = true
		childDef.Policies.BashPermissionContext = t.parentAgent.cfg.BashTool
	}

	// 经 Join 而非 Layout.SubAgentDir：父 root 为空（未 Execute 的 agent）时保持
	// 旧有相对路径语义 sub_agents/<child>，路径知识仍来自 Layout 的 rel 原语。
	childRootDir := filepath.Join(t.parentAgent.workspaceRootDir, filepath.FromSlash(workspacefs.Layout{}.SubAgentDirRel(childName)))
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
		WithSourceWorkingDir(t.parentAgent.sourceWorkingDir),
	}

	return &childAgentSetup{
		childName:    childName,
		childAgent:   childAgent,
		childRootDir: childRootDir,
		execOpts:     execOpts,
		instruction:  instruction,
		taskContext:  taskContext,
		spec:         spec,
		runtime:      runtime,
	}, nil
}

// mergeSubAgentContext 把显式上下文与自动交接上下文拼成子 Agent 首条 user 消息的随附上下文串。
// 显式优先；两者都在且不同则并列（显式在前）。
func mergeSubAgentContext(explicitContext, handoffContext string) string {
	explicitContext = strings.TrimSpace(explicitContext)
	handoffContext = strings.TrimSpace(handoffContext)
	var b strings.Builder
	if explicitContext != "" {
		b.WriteString("委派上下文：\n")
		b.WriteString(explicitContext)
	}
	if handoffContext != "" && handoffContext != explicitContext {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("交接上下文（补充，若与委派上下文冲突以委派上下文为准）：\n")
		b.WriteString(handoffContext)
	}
	return b.String()
}

func (t *SubAgentTool) executeSync(ctx context.Context, setup *childAgentSetup) (string, error) {
	result, _, err := setup.childAgent.RunSubAgentLoop(ctx, setup.instruction, setup.taskContext, setup.spec, setup.execOpts...)
	t.finalizeChildAgent(setup.runtime, setup.childName, setup.childRootDir, result)
	if err != nil {
		return "", fmt.Errorf("sub agent execution: %w", err)
	}
	return buildSubAgentEnvelope(setup.childName, setup.childRootDir, result), nil
}

// buildSubAgentEnvelope 是子 Agent 同步返回的统一信封：{status, summary, result|result_file,
// error, usage, workspace}。长 result 落 result_file 只回指针（与 async 通知同口径，避免撑爆父
// 上下文）；短 result 内联。删旧字段 ok/agent_name/plan_summary（P6）。
func buildSubAgentEnvelope(agentName, workspaceRoot string, result *builtin_tools.RunResult) string {
	status := "completed"
	fullResult := ""
	errText := ""
	if result != nil {
		fullResult = result.Result
		errText = result.Error
		if !result.Success {
			status = "failed"
		}
	} else {
		status = "failed"
		errText = "no result"
	}

	payload := map[string]any{
		"status":    status,
		"workspace": workspaceRoot,
		"summary":   truncateRuneString(fullResult, maxAsyncNotificationRunes),
	}
	if len([]rune(fullResult)) > maxAsyncNotificationRunes {
		if rf := writeSubAgentResultFile(workspaceRoot, agentName, status, result); rf != "" {
			payload["result_file"] = rf
		} else {
			payload["result"] = fullResult // 落盘失败兜底内联全文
		}
	} else if fullResult != "" {
		payload["result"] = fullResult
	}
	if errText != "" {
		payload["error"] = errText
	}
	if result != nil && result.Usage != nil {
		payload["usage"] = result.Usage
	}
	out, _ := json.Marshal(payload)
	return string(out)
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
		// step_id 让 TUI 把后台子 agent 卡片归到派生它的那个 peer step——并发 step 下
		// 不能依赖 activeStepByAgent 单槽（会被其它 peer 的 in_progress 覆盖）。
		// setup.runtime.CurrentStepID 即 effectiveStepID(runCtx)（见 buildChild 捕获）。
		"step_id": strings.TrimSpace(setup.runtime.CurrentStepID),
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
			// D1 单写者：child goroutine 不再直写 workspace state（消除并发 finalize 的
			// RMW 竞态）。ChildAgents 终态写移到 drain（scheduler 单写者），由 Complete 携带
			// result 经通知落盘。此处只 Complete + 关卡片。
			registry.Complete(setup.childName, result)
			// Close the panel card the instant the child settles, independent of
			// when the parent scheduler next drains. Coupling this to drain leaves
			// the card stuck on "running" whenever the parent is parked
			// (awaitBackgroundRequested), mid model call, or cancelled. stepHistory
			// injection stays in drainAsyncAgentNotifications (parent-only writer).
			t.parentAgent.emitSubAgentCardEnd(setup.childName, result)
		}()

		var err error
		result, _, err = setup.childAgent.RunSubAgentLoop(ctx, setup.instruction, setup.taskContext, setup.spec, setup.execOpts...)
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

func (t *SubAgentTool) preRegisterChildAgent(runtime builtin_tools.ToolRuntimeInfo, childName, childRootDir string) {
	if t.parentAgent == nil || t.parentAgent.workspaceRuntime == nil {
		return
	}
	// 走 MutateWorkspaceState 持锁 RMW：preRegister 可在 inline peer goroutine 发生，
	// 与 drain（scheduler）终态写、skill 写并发——统一锁串行化消除跨区丢更新。
	err := t.parentAgent.workspaceRuntime.MutateWorkspaceState(func(state *builtin_tools.WorkspaceState) error {
		state.ChildAgents[childName] = &builtin_tools.WorkspaceChildAgentPointer{
			Status:          "running",
			ParentStepKey:   strings.TrimSpace(runtime.CurrentStepID),
			ArtifactRootDir: childRootDir,
		}
		return nil
	})
	if err != nil {
		runtimelog.LogJSON("warning", map[string]any{
			"event": "pre_register_child_agent_save_failed",
			"child": childName,
			"error": err.Error(),
		})
	}
}

// finalizeChildAgent 只由 sync 路径调用把 ChildAgents 指针翻终态（async 路径的终态写移到
// drain 单写者，见 async_agent_drain.go writeChildAgentTerminalState）。走 MutateWorkspaceState
// 持锁 RMW——sync 子 Agent 可在 inline peer goroutine 执行，与其它写者并发。
func (t *SubAgentTool) finalizeChildAgent(runtime builtin_tools.ToolRuntimeInfo, childName, childRootDir string, result *builtin_tools.RunResult) {
	if t.parentAgent == nil || t.parentAgent.workspaceRuntime == nil {
		return
	}
	status := "completed"
	if result == nil || !result.Success {
		status = "failed"
	}
	err := t.parentAgent.workspaceRuntime.MutateWorkspaceState(func(state *builtin_tools.WorkspaceState) error {
		ptr := state.ChildAgents[childName]
		if ptr == nil {
			ptr = &builtin_tools.WorkspaceChildAgentPointer{ParentStepKey: strings.TrimSpace(runtime.CurrentStepID)}
		}
		ptr.Status = status
		ptr.ArtifactRootDir = childRootDir
		if result != nil {
			ptr.PlanSummary = result.PlanSummary
		}
		state.ChildAgents[childName] = ptr
		return nil
	})
	if err != nil {
		runtimelog.LogJSON("warning", map[string]any{
			"event": "finalize_child_agent_save_failed",
			"child": childName,
			"error": err.Error(),
		})
	}
}

func (t *SubAgentTool) resolveChildToolNames(requested []string) []string {
	var result []string
	if len(requested) > 0 {
		for _, name := range requested {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if policyManagedTools[name] {
				continue
			}
			// Only registry-resolvable names are kept: Build realizes
			// def.ToolNames via toolRegistry.Resolve, so agent-resident tools
			// (MCP adapters, bash, skill, orchestration) must not enter the
			// list. They reach the child via their own registration paths.
			if t.factory.toolRegistry != nil && t.factory.toolRegistry.Has(name) {
				result = append(result, name)
			}
		}
	} else {
		result = t.parentDomainToolNames()
	}
	// Always guarantee the read-only builtin domain tools regardless of what the
	// parent requested. plan/replan/final phases structurally rely on them (their
	// prompts advertise read_file/list_files/rg), so a child must have them even
	// when the parent passed an unrelated or policy-only tools list (e.g.
	// tools:["bash"] strips down to empty and previously dropped these entirely).
	return t.withBaselineDomainTools(result)
}

// withBaselineDomainTools unions the builtin file/search tools into names,
// guarded by registry.Has and de-duplicated, preserving the original order.
func (t *SubAgentTool) withBaselineDomainTools(names []string) []string {
	seen := make(map[string]struct{}, len(names)+len(baselineDomainToolNames))
	for _, name := range names {
		seen[name] = struct{}{}
	}
	for _, name := range baselineDomainToolNames {
		if _, ok := seen[name]; ok {
			continue
		}
		if t.factory == nil || t.factory.toolRegistry == nil || !t.factory.toolRegistry.Has(name) {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// baselineDomainToolNames are the builtin file/search tools every agent
// (including sub-agents) must always have. They are registry-resident "common
// utilities" (see NewDefaultToolRegistry) and plan/replan/final prompts assume
// their presence, so they are unioned into every child's ToolNames regardless
// of what the parent requested.
var baselineDomainToolNames = []string{
	builtin_tools.ReadFileToolName,
	builtin_tools.ListFilesToolName,
	builtin_tools.WriteToolName,
	builtin_tools.EditToolName,
	builtin_tools.NotebookEditToolName,
	builtin_tools.RgToolName,
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
	builtin_tools.EjectSkillToolName:        true,
}

// excludeFromInheritance is the full set of platform / orchestration tools
// that should NOT be auto-inherited when the child agent uses the default
// (no explicit tools) path. It is a superset of policyManagedTools: registry-
// resident skill-management tools are also excluded because sub-agents should
// not manage skills by default.
//
// Agent-resident tools that live outside the ToolRegistry (e.g. MCP adapters
// registered directly on the agent in AgentFactory.Build) are intentionally
// NOT listed here: both inheritance paths gate on toolRegistry.Has, which
// already drops them. They are re-registered on the child by Build's MCP
// block, so excluding their names from ToolNames loses no capability.
var excludeFromInheritance = map[string]bool{
	builtin_tools.UpdateCurrentStepToolName: true,
	builtin_tools.TaskStatusQueryToolName:   true,
	builtin_tools.HumanConfirmToolName:      true,
	builtin_tools.SubAgentToolName:          true,
	builtin_tools.SubAgentStatusToolName:    true,
	builtin_tools.AwaitSubAgentsToolName:    true,
	builtin_tools.BashToolName:              true,
	builtin_tools.SkillToolName:             true,
	builtin_tools.ListSkillsToolName:        true,
	builtin_tools.EjectSkillToolName:        true,
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
		// Only inherit registry-resolvable names. Agent-resident tools (MCP
		// adapters, bash, etc.) are not in the registry and would fail
		// Build's resolveTools; the child re-acquires them via their own
		// registration paths.
		if t.factory == nil || t.factory.toolRegistry == nil || !t.factory.toolRegistry.Has(name) {
			continue
		}
		names = append(names, name)
	}
	return names
}

// childAgentToken derives the unique, name-safe token a child agent embeds in
// its name from the spawning tool's call_id. The full call_id is kept (not
// truncated): provider call ids like "call_00_<rand>" share a non-unique
// "call_00_" prefix, so truncating to a fixed length collapses distinct calls
// into one childName (shared workspace dir / registry slot / durable pointer).
// Non [a-z0-9_] runes are folded to '_' so the token is filesystem-safe and
// free of '-' (skill children split their token on the last '-').
func childAgentToken(callID string) string {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return generateRandomString(8)
	}
	var b strings.Builder
	b.Grow(len(callID))
	for _, r := range callID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
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
