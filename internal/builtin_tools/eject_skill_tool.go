package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type EjectSkillTool struct{}

func NewEjectSkillTool() *EjectSkillTool {
	return &EjectSkillTool{}
}

func (t *EjectSkillTool) Name() string { return EjectSkillToolName }

func (t *EjectSkillTool) Description() string {
	return "卸载一个已加载的 skill：停止在后续轮次向上下文注入其指令，回收上下文空间。" +
		"当某个先前通过 `skill` 工具加载的 skill 已完成使命、与当前及后续 step 不再相关时调用。" +
		"非破坏性操作——不删除 skill 文件或 catalog，需要时可再次用 `skill` 工具重新加载。"
}

func (t *EjectSkillTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "要卸载的 skill 名称（应为当前已加载/已注入的 skill）。",
			},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
}

func (t *EjectSkillTool) Execute(_ context.Context, args map[string]any) (string, error) {
	name, _ := args["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}

	out, err := json.Marshal(map[string]any{
		"ok":   true,
		"name": name,
	})
	if err != nil {
		return "", fmt.Errorf("marshal result failed: %w", err)
	}
	return string(out), nil
}
