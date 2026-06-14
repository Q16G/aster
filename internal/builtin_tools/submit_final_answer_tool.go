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
		"required": []string{"is_complete", "status", "reason", "should_replan", "next_goal", "incomplete_items", "depth_gaps", "new_surfaces", "warnings", "user_message", "references"},
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
				"description": "是否应回流 plan。仅当 agent 还能继续执行补齐缺口时为 true。",
			},
			"next_goal": map[string]any{
				"type":        "string",
				"description": "下一轮明确目标。仅在确实需要 agent 继续执行时填写；不要写「等待用户输入」。",
			},
			"incomplete_items": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "轴①存在性/完成度：当前用户诉求范围内、根本没做的要点。默认受意图半径约束，意图外维度不计入；显式聚焦时仅列聚焦方向内的未完成项。不含'做了但不扎实'（属 depth_gaps）。",
			},
			"depth_gaps": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "轴②深度/质量：跨 step 来看做了但不扎实的项 —— " + DepthSmellsEnumeration + "。即使轴①为空也须独立判定。",
			},
			"new_surfaces": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "轴③泛化：对照意图半径内的诉求全集、尚未被任何已完成工作覆盖的面（任务覆盖完整性视角；范围是整个任务而非某个 step）。意图外/明确不做项及聚焦方向外的面填此字段但不单独驱动 should_replan。",
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
