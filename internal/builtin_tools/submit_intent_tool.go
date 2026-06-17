package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// SubmitIntentTool 是 intent_classification 阶段的提交工具：模型在判定本回合最新用户
// 输入相对既有任务的关系后，调用它提交结构化结论（action + reason）。
//
// 注意：该工具的入参由 intent_classification 阶段循环按工具名直接拦截并解析消费，
// 不经普通工具注册表 / executeToolCall 派发，因此 Execute 仅为满足 Tool 接口而存在，
// 回显已提交的参数，正常路径下不会被调用。
type SubmitIntentTool struct{}

func NewSubmitIntentTool() *SubmitIntentTool { return &SubmitIntentTool{} }

func (t *SubmitIntentTool) Name() string { return SubmitIntentToolName }

func (t *SubmitIntentTool) Description() string {
	return "完成意图判定后调用此工具提交结论。action 取值：" +
		"carry=基于既有目标继续/深入/追问（含补充能力、环境、工具等仅为达成既有目标的手段）；" +
		"replan=仍在同一任务域内但改变方向/策略/范围，需要重做规划；" +
		"cold_start=与既有工作完全无关的全新任务。" +
		"reason 为一句话判定依据。调用即视为提交，不要把结论写成普通文本或代码块。"
}

func (t *SubmitIntentTool) Parameters() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"action", "reason"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"carry", "replan", "cold_start"},
				"description": "执行策略判定。补充的能力/环境/工具属达成既有目标的手段，不得僭越为新意图，应判 carry。",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "一句话判定依据，应说明最新输入与既有目标/范围的关系。",
			},
		},
	}
}

func (t *SubmitIntentTool) Execute(_ context.Context, args map[string]any) (string, error) {
	out, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("submit_intent: marshal args failed: %w", err)
	}
	return string(out), nil
}
