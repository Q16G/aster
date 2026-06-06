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
		"每个 step 收尾时都应主动评估已注入的 skill 是否仍相关——某个先前通过 `skill` 工具加载的 skill 一旦其指令对当前 step 已无进一步用处（产出已落盘/已回写），就立即卸载，避免其指令在后续每一轮被重复注入、持续占用上下文。" +
		"非破坏性操作——不删除 skill 文件或 catalog，卸载后该 skill 回到可用状态，后续真正需要时可再次用 `skill` 工具重新加载；误卸代价远低于持续堆积。"
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
