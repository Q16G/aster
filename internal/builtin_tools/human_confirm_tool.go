package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aster/internal/utils/argx"

	"github.com/google/uuid"
)

type HumanConfirmTool struct {
	ctx ToolContext
}

func NewHumanConfirmTool(ctx ToolContext) *HumanConfirmTool {
	return &HumanConfirmTool{ctx: ctx}
}

func (t *HumanConfirmTool) Name() string { return HumanConfirmToolName }

func (t *HumanConfirmTool) Description() string {
	return "请求人工确认。"
}

func (t *HumanConfirmTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": "需要人工确认的问题（自然语言）",
			},
			"input_type": map[string]any{
				"type":        "string",
				"description": "输入类型：text（文本）、single_choice（单选）、multi_choice（多选）、structured（结构化）",
				"enum":        []string{"text", "single_choice", "multi_choice", "structured"},
				"default":     "text",
			},
			"options": map[string]any{
				"type":        "array",
				"description": "选项列表（用于 single_choice 或 multi_choice）",
				"items": map[string]any{
					"type": "string",
				},
			},
			"context": map[string]any{
				"type":                 "object",
				"description":          "可选：补充上下文（结构化信息）",
				"additionalProperties": true,
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "可选：等待人工输入的超时（毫秒）",
				"minimum":     0,
			},
		},
		"required":             []string{"question"},
		"additionalProperties": false,
	}
}

func (t *HumanConfirmTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.ctx == nil {
		return "", fmt.Errorf("tool context is nil")
	}

	onHumanInput := t.ctx.GetOnHumanInput()
	if onHumanInput == nil {
		return "", fmt.Errorf("human input callback not configured")
	}

	question, err := argx.RequiredText(args, "question")
	if err != nil {
		return "", err
	}

	timeoutMS := int64(0)
	if v, ok := args["timeout_ms"]; ok && v != nil {
		if ms, ok := asInt64Any(v); ok && ms >= 0 {
			timeoutMS = ms
		}
	}
	if timeoutMS > 0 {
		ctxWithTimeout, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
		ctx = ctxWithTimeout
	}

	var ctxMap map[string]any
	if raw := args["context"]; raw != nil {
		if m, ok := raw.(map[string]any); ok {
			ctxMap = CloneAnyMap(m)
		}
	}
	if ctxMap == nil {
		ctxMap = make(map[string]any)
	}

	inputType := "text"
	if v := argx.OptionalText(args, "input_type"); v != "" {
		inputType = v
	}
	switch inputType {
	case "text", "single_choice", "multi_choice", "structured":
	default:
		inputType = "text"
	}

	options := argx.StringSlice(args["options"])

	requestID := uuid.NewString()
	iteration := t.ctx.Snapshot().Iteration

	humanInputRequest := map[string]any{
		"request_id": requestID,
		"question":   question,
		"input_type": inputType,
		"options":    options,
		"context":    CloneAnyMap(ctxMap),
	}

	t.ctx.GetEmitter().EmitHumanRequest(iteration, requestID, question, humanInputRequest)

	snap := t.ctx.UpdateTaskStatus(TaskStatusUpdate{
		Task:     "等待人工确认",
		Status:   TaskStatusPaused,
		Message:  question,
		Progress: -1,
	})
	t.ctx.GetEmitter().EmitStateChange(snap)

	ctxMap["request_id"] = requestID
	ctxMap["input_type"] = inputType
	ctxMap["options"] = options
	answer, err := onHumanInput(ctx, question, ctxMap)
	if err != nil {
		snap := t.ctx.UpdateTaskStatus(TaskStatusUpdate{
			Task:     "人工确认失败",
			Status:   TaskStatusRunning,
			Message:  err.Error(),
			Progress: -1,
		})
		t.ctx.GetEmitter().EmitStateChange(snap)
		return "", err
	}

	answer = strings.TrimSpace(answer)

	snap = t.ctx.UpdateTaskStatus(TaskStatusUpdate{
		Task:     "收到人工确认",
		Status:   TaskStatusRunning,
		Message:  "已收到人工输入",
		Progress: -1,
	})
	t.ctx.GetEmitter().EmitStateChange(snap)

	var value any = answer
	switch inputType {
	case "multi_choice", "structured":
		var parsed any
		if err := json.Unmarshal([]byte(answer), &parsed); err == nil {
			value = parsed
		}
	}

	out, _ := json.Marshal(map[string]any{
		"ok":         true,
		"request_id": requestID,
		"type":       inputType,
		"answer":     answer,
		"value":      value,
	})
	return string(out), nil
}
