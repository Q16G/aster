package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/builtin_tools"
	"aster/internal/utils/argx"
)

const (
	maxSubAgentDepth       = 3
	defaultSubAgentMaxIter = 500
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
	return "派生一个子 Agent 独立执行委派任务。由你编写 instruction 来定义子 Agent 的角色、任务和约束。"
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
				"description": "可选：传递给子 Agent 的额外上下文信息。",
			},
		},
		"required":             []string{"instruction"},
		"additionalProperties": false,
	}
}

func (t *SubAgentTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.parentAgent == nil || t.factory == nil {
		return "", fmt.Errorf("sub_agent tool not initialized")
	}

	runtime, _ := builtin_tools.GetToolRuntime(ctx)
	if runtime.StackDepth >= maxSubAgentDepth {
		return "", fmt.Errorf("sub_agent recursion depth exceeded: current=%d, max=%d", runtime.StackDepth, maxSubAgentDepth)
	}

	instruction, err := argx.RequiredText(args, "instruction")
	if err != nil {
		return "", fmt.Errorf("instruction is required")
	}
	toolNames := argx.StringSlice(args["tools"])
	extraContext := argx.OptionalText(args, "context")

	callID := runtime.CallID
	if callID == "" {
		callID = generateRandomString(8)
	}
	childName := fmt.Sprintf("sub-%s", truncateID(callID, 8))

	childDef := AgentDefinition{
		Name:        childName,
		Instruction: instruction,
		ToolNames:   t.resolveChildToolNames(toolNames),
		Policies: AgentPolicies{
			MaxIterations: t.childMaxIterations(),
		},
	}

	if extraContext != "" {
		childDef.Context = []TaskContextEntry{{
			Label:       "委派上下文",
			Value:       extraContext,
			Description: "父 Agent 传递的额外上下文",
		}}
	}

	if t.parentAgent.cfg != nil && t.parentAgent.cfg.BashTool != nil {
		childDef.Policies.AllowBash = true
		childDef.Policies.BashPermissionContext = t.parentAgent.cfg.BashTool
	}

	namespace := fmt.Sprintf("agents/%s", childName)
	childRuntime, err := newLocalWorkspaceRuntime(
		t.parentAgent.workspaceSessionID,
		t.parentAgent.workspaceRootDir,
		namespace,
	)
	if err != nil {
		return "", fmt.Errorf("create child workspace: %w", err)
	}

	childAgent, err := t.factory.Build(childDef)
	if err != nil {
		return "", fmt.Errorf("build sub agent: %w", err)
	}

	result, err := childAgent.Execute(ctx, instruction,
		WithWorkspaceRuntime(childRuntime),
		WithResultSource(ResultSourceLatestStepResult),
		WithSkipIntentPrelude(),
	)
	if err != nil {
		return "", fmt.Errorf("sub agent execution: %w", err)
	}

	return formatSubAgentResult(childName, namespace, result), nil
}

func (t *SubAgentTool) childMaxIterations() int {
	return defaultSubAgentMaxIter
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
			if parentSet[name] || (t.factory.toolRegistry != nil && t.factory.toolRegistry.Has(name)) {
				result = append(result, name)
			}
		}
		return result
	}
	return t.parentDomainToolNames()
}

func (t *SubAgentTool) parentDomainToolNames() []string {
	if t.parentAgent == nil {
		return nil
	}
	platformTools := map[string]bool{
		builtin_tools.UpdateCurrentStepToolName: true,
		builtin_tools.TaskStatusQueryToolName:   true,
		builtin_tools.HumanConfirmToolName:      true,
		builtin_tools.SubAgentToolName:          true,
		builtin_tools.BashToolName:              true,
		builtin_tools.LoadSkillsToolName:        true,
		builtin_tools.ListSkillsToolName:        true,
		builtin_tools.DeleteSkillToolName:       true,
		builtin_tools.SkillToolName:             true,
	}
	var names []string
	for _, name := range t.parentAgent.tools.Keys() {
		if platformTools[name] {
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

func formatSubAgentResult(agentName, namespace string, result *builtin_tools.RunResult) string {
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
	out, _ := json.Marshal(map[string]any{
		"ok":         ok,
		"agent_name": agentName,
		"namespace":  namespace,
		"status":     status,
		"summary":    summary,
		"error":      errText,
	})
	return string(out)
}
