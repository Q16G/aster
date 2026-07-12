package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// SubmitResultTool 是子 Agent（sub_agent 单循环）的专属终止工具：子 Agent 完成本次委派、
// 准备把结论交回父 Agent 时调用它，把「结论 + 关键事实 + 产物路径指针」作为参数提交。
//
// 注意：该工具的入参由子 Agent 单循环内核（RunSubAgentLoop）按工具名直接拦截并解析消费，
// 不进普通工具注册表 / childDef.ToolNames、不经 executeToolCall 派发，因此 Execute 仅为满足
// Tool 接口而存在、回显已提交参数，正常路径下不会被调用（与 SubmitFinalAnswerTool 同机制）。
type SubmitResultTool struct{}

func NewSubmitResultTool() *SubmitResultTool { return &SubmitResultTool{} }

func (t *SubmitResultTool) Name() string { return SubmitResultToolName }

func (t *SubmitResultTool) Description() string {
	return "本次委派达成、准备把结论交回父 Agent 时调用，提交结论与关键事实（参数语义见 schema）；调用即视为提交并结束，不要把结论写成普通文本代替提交。未达成也调用它并把 status 标为未完成。"
}

func (t *SubmitResultTool) Parameters() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"result"},
		"properties": map[string]any{
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"completed", "failed"},
				"description": "本次委派是否达成；未达成填 failed。缺省视为 completed。",
			},
			"result": map[string]any{
				"type":        "string",
				"description": "给父 Agent 消费的结论 + 关键事实（每条带具体值）+ 产物绝对路径指针；大块原始数据落盘后只给路径，不内联大段原文。",
			},
		},
	}
}

func (t *SubmitResultTool) Execute(_ context.Context, args map[string]any) (string, error) {
	out, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("submit_result: marshal args failed: %w", err)
	}
	return string(out), nil
}
