package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// SubmitFinalAnswerTool 是 final_answer 阶段的提交工具：模型在完成性评估结束、
// 准备交付最终答复时调用它，把结构化决策（三轴盘点 + 终报文本 + 引用）作为参数提交。
//
// 注意：该工具的入参由 final_answer 阶段循环按工具名直接拦截并解析消费，
// 不经普通工具注册表 / executeToolCall 派发，因此 Execute 仅为满足 Tool 接口而存在，
// 回显已提交的参数，正常路径下不会被调用。
type SubmitFinalAnswerTool struct{}

func NewSubmitFinalAnswerTool() *SubmitFinalAnswerTool { return &SubmitFinalAnswerTool{} }

func (t *SubmitFinalAnswerTool) Name() string { return SubmitFinalAnswerToolName }

func (t *SubmitFinalAnswerTool) Description() string {
	return "完成性评估结束、准备交付最终答复时调用，提交结构化决策（参数语义见 schema）；调用即视为提交，不要把决策写成普通文本或代码块。"
}

func (t *SubmitFinalAnswerTool) Parameters() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"is_complete", "status", "reason", "should_replan", "warnings", "user_message", "references"},
		"properties": map[string]any{
			"is_complete": map[string]any{
				"type":        "boolean",
				"description": "当前 INPUT_TIMELINE 对应的用户诉求是否已被满足性响应。",
			},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"completed", "failed", "canceled", "running"},
				"description": "当前任务状态。",
			},
			"reason": map[string]any{
				"type":        "string",
				"description": "完成或未完成的核心依据，应围绕用户输入是否已被响应。",
			},
			"should_replan": map[string]any{
				"type":        "boolean",
				"description": "是否应回流 plan。仅当 agent 还能继续执行补齐缺口时为 true。三轴缺口的盘点与承接归复核阶段（step_replan）与账本，本阶段只做终态裁决，不产缺口清单。",
			},
			"warnings": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "最终产出的不可解局限与风险事项清单：主要来自账本 ## 不可解局限，runtime 内部告警中有重要的也可一并纳入。每条须在 user_message 中有对应归置，不得静默丢弃。",
			},
			"user_message": map[string]any{
				"type":        "string",
				"description": "最终给用户看的答复文本。若当前输入已被响应完成，应输出可直接交付的完整响应（高密度，不主动压缩）。",
			},
			"references": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "支撑最终结论的关键引用列表。所有文件路径必须使用绝对路径，禁止使用相对路径。",
			},
		},
	}
}

func (t *SubmitFinalAnswerTool) Execute(_ context.Context, args map[string]any) (string, error) {
	out, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("submit_final_answer: marshal args failed: %w", err)
	}
	return string(out), nil
}
