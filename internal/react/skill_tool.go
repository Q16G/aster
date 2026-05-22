package react

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"aster/internal/builtin_tools"
	"aster/internal/utils/argx"
)

type SkillInfo struct {
	Name          string
	Description   string
	Instructions  string
	Agent         string
	WhenToUse     string
	Arguments     []string
	AllowedTools  []string
	MCP           []string
	Context       string
	SkillDir      string
	UserInvocable bool
}

type SkillLookup interface {
	LookupSkill(ctx context.Context, name string) (*SkillInfo, error)
}

type SkillTool struct {
	parentAgent *Agent
	factory     *AgentFactory
	lookup      SkillLookup
}

func NewSkillTool(parent *Agent, factory *AgentFactory, lookup SkillLookup) *SkillTool {
	return &SkillTool{parentAgent: parent, factory: factory, lookup: lookup}
}

func (t *SkillTool) Name() string  { return builtin_tools.SkillToolName }
func (t *SkillTool) IsAgent() bool { return false }

func (t *SkillTool) Description() string {
	return "调用一个 Skill。inline 模式下 Skill 内容注入当前上下文；fork 模式下在子 Agent 中独立执行。"
}

func (t *SkillTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": map[string]any{
				"type":        "string",
				"description": "要调用的 Skill 名称。",
			},
			"args": map[string]any{
				"type":        "string",
				"description": "可选：传递给 Skill 的参数字符串。",
			},
		},
		"required":             []string{"skill"},
		"additionalProperties": false,
	}
}

func (t *SkillTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.lookup == nil {
		return "", fmt.Errorf("skill tool not initialized")
	}

	skillName, err := argx.RequiredText(args, "skill")
	if err != nil {
		return "", fmt.Errorf("skill name is required")
	}
	rawArgs := argx.OptionalText(args, "args")

	info, err := t.lookup.LookupSkill(ctx, skillName)
	if err != nil {
		return "", fmt.Errorf("lookup skill %q: %w", skillName, err)
	}

	if strings.EqualFold(info.Context, "fork") {
		return t.executeFork(ctx, info, rawArgs)
	}
	return t.executeInline(ctx, info, rawArgs)
}

func (t *SkillTool) executeInline(_ context.Context, info *SkillInfo, rawArgs string) (string, error) {
	body := substituteParams(info, rawArgs, t.sessionID())
	out, _ := json.Marshal(map[string]any{
		"ok":    true,
		"count": 1,
		"skills": []map[string]any{
			{
				"name":         info.Name,
				"description":  info.Description,
				"context":      info.Context,
				"instructions": body,
			},
		},
	})
	return string(out), nil
}

func (t *SkillTool) executeFork(ctx context.Context, info *SkillInfo, rawArgs string) (string, error) {
	if t.parentAgent == nil || t.factory == nil {
		return "", fmt.Errorf("fork mode requires agent and factory")
	}

	runtime, _ := builtin_tools.GetToolRuntime(ctx)
	if runtime.StackDepth > 0 {
		return "", fmt.Errorf("skill fork is disabled inside sub-agents (stack_depth=%d)", runtime.StackDepth)
	}

	body := substituteParams(info, rawArgs, t.sessionID())

	callID := runtime.CallID
	if callID == "" {
		callID = generateRandomString(8)
	}
	childName := fmt.Sprintf("skill-%s-%s", info.Name, truncateID(callID, 6))

	childDef := AgentDefinition{
		Name:        childName,
		Instruction: body,
		ToolNames:   t.resolveToolNames(info.AllowedTools),
		Policies: AgentPolicies{
			MaxIterations: defaultSubAgentMaxIter,
		},
	}

	if len(info.MCP) > 0 && t.factory != nil && t.factory.mcpManager != nil {
		childDef.MCPServers = t.factory.mcpManager.LookupConfigs(info.MCP)
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
		return "", fmt.Errorf("create child workspace: %w", err)
	}

	childAgent, err := t.factory.Build(childDef)
	if err != nil {
		return "", fmt.Errorf("build skill fork agent: %w", err)
	}

	result, err := childAgent.Execute(ctx, body,
		WithWorkspaceRuntime(childRuntime),
		WithResultSource(ResultSourceLatestStepResult),
		WithSkipIntentPrelude(),
	)
	if err != nil {
		return "", fmt.Errorf("skill fork execution: %w", err)
	}

	return formatSubAgentResult(childName, childRootDir, result), nil
}

func (t *SkillTool) resolveToolNames(allowedTools []string) []string {
	if len(allowedTools) > 0 {
		parentSet := make(map[string]bool)
		if t.parentAgent != nil && t.parentAgent.tools != nil {
			for _, name := range t.parentAgent.tools.Keys() {
				parentSet[name] = true
			}
		}
		var result []string
		for _, name := range allowedTools {
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
	return nil
}

func (t *SkillTool) sessionID() string {
	if t.parentAgent != nil {
		return t.parentAgent.workspaceSessionID
	}
	return ""
}

func substituteParams(info *SkillInfo, rawArgs string, sessionID string) string {
	body := info.Instructions
	if body == "" {
		return body
	}

	rawArgs = strings.TrimSpace(rawArgs)
	args := parseSkillArgs(rawArgs)

	body = strings.ReplaceAll(body, "${SKILL_DIR}", info.SkillDir)
	body = strings.ReplaceAll(body, "${SESSION_ID}", sessionID)

	for i := len(args) - 1; i >= 0; i-- {
		body = strings.ReplaceAll(body, fmt.Sprintf("$ARGUMENTS[%d]", i), args[i])
	}
	body = strings.ReplaceAll(body, "$ARGUMENTS", rawArgs)

	for i := len(args) - 1; i >= 0; i-- {
		body = strings.ReplaceAll(body, fmt.Sprintf("$%d", i), args[i])
	}

	for i, paramName := range info.Arguments {
		paramName = strings.TrimSpace(paramName)
		if paramName == "" {
			continue
		}
		val := ""
		if i < len(args) {
			val = args[i]
		}
		body = strings.ReplaceAll(body, "$"+paramName, val)
	}

	return body
}

func parseSkillArgs(argsString string) []string {
	argsString = strings.TrimSpace(argsString)
	if argsString == "" {
		return nil
	}

	var result []string
	var current strings.Builder
	inQuote := false

	for i := 0; i < len(argsString); i++ {
		ch := argsString[i]
		switch {
		case ch == '"':
			inQuote = !inQuote
		case ch == ' ' && !inQuote:
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}
