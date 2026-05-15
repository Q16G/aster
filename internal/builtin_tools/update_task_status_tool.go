package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type UpdateTaskStatusTool struct {
	ctx ToolContext
}

func NewUpdateTaskStatusTool(ctx ToolContext) *UpdateTaskStatusTool {
	return &UpdateTaskStatusTool{ctx: ctx}
}

func (t *UpdateTaskStatusTool) Name() string { return UpdateTaskStatusToolName }

func (t *UpdateTaskStatusTool) Description() string {
	return "设置当前任务状态，并记录结果或错误。"
}

func (t *UpdateTaskStatusTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type": "string",
				"enum": []any{
					string(TaskStatusCompleted),
					string(TaskStatusFailed),
					string(TaskStatusCanceled),
				},
				"description": "终态：completed/failed/canceled",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "可选：状态说明/备注",
			},
			"progress": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"maximum":     100,
				"description": "可选：整体进度（0-100）",
			},
			"result": map[string]any{
				"type":        []any{"string", "object", "array"},
				"description": "status=completed 时必填：最终输出（推荐直接传 JSON 对象/数组，系统会自动序列化）",
				// OpenAI function schema validator requires "items" when "array" is allowed.
				"items": map[string]any{},
			},
			"error": map[string]any{
				"type":        "string",
				"description": "status=failed 时必填：失败原因",
			},
		},
		"required":             []string{"status"},
		"additionalProperties": false,
	}
}

func (t *UpdateTaskStatusTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.ctx == nil {
		return "", fmt.Errorf("tool context is nil")
	}

	statusRaw := ""
	if v, ok := args["status"]; ok {
		statusRaw = ToolRuntimeValue(v)
	}
	if statusRaw == "" {
		return softToolValidationOutput(
			"missing_status",
			"status is required",
			map[string]any{
				"allowed_status": []any{
					string(TaskStatusCompleted),
					string(TaskStatusFailed),
					string(TaskStatusCanceled),
				},
			},
		)
	}
	status, ok := normalizeTaskStatusInput(statusRaw)
	if !ok {
		return softToolValidationOutput(
			"invalid_status",
			fmt.Sprintf("invalid status: %s", statusRaw),
			map[string]any{
				"allowed_status": []any{
					string(TaskStatusCompleted),
					string(TaskStatusFailed),
					string(TaskStatusCanceled),
				},
			},
		)
	}

	message := ToolRuntimeValue(args["message"])
	result, err := normalizeToolTextOrJSON(args["result"])
	if err != nil {
		return softToolValidationOutput(
			"invalid_result",
			err.Error(),
			nil,
		)
	}
	errText := ToolRuntimeValue(args["error"])

	progress := -1
	if v, ok := args["progress"]; ok {
		switch n := v.(type) {
		case int:
			progress = n
		case int32:
			progress = int(n)
		case int64:
			progress = int(n)
		case float32:
			progress = int(n)
		case float64:
			progress = int(n)
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
				progress = parsed
			}
		default:
			progress = int64ToIntSafe(n)
		}
	}

	if status == TaskStatusCompleted && result == "" {
		return softToolValidationOutput(
			"missing_result",
			"result is required when status=completed",
			map[string]any{
				"next_action": "provide result and call update_task_status again",
			},
		)
	}
	if status == TaskStatusFailed && errText == "" {
		return softToolValidationOutput(
			"missing_error",
			"error is required when status=failed",
			map[string]any{
				"next_action": "provide error and call update_task_status again",
			},
		)
	}
	if status == TaskStatusCompleted {
		snap := t.ctx.Snapshot()
		if !allPlanStepsCompleted(snap.Plan) {
			step, st := firstUnfinishedPlanStep(snap.Plan)
			extra := map[string]any{
				"unfinished_status": st,
				"next_action":       "complete remaining plan steps first (update_current_step), then call update_task_status with completed",
			}
			msg := "plan has unfinished steps"
			if step != "" {
				extra["unfinished_step"] = step
				msg = fmt.Sprintf("plan has unfinished step: %s (%s)", step, st)
			}
			return softToolValidationOutput("unfinished_plan", msg, extra)
		}
	}

	task := ""
	switch status {
	case TaskStatusCompleted:
		task = "任务完成"
	case TaskStatusFailed:
		task = "任务失败"
	default:
		task = "任务结束"
	}

	snapshot := t.ctx.UpdateTaskStatus(TaskStatusUpdate{
		Task:     task,
		Status:   status,
		Message:  message,
		Progress: progress,
		Result:   result,
		Error:    errText,
	})
	t.ctx.GetEmitter().EmitStateChange(snapshot)

	out, _ := json.Marshal(map[string]any{
		"ok":      true,
		"task":    task,
		"status":  status,
		"message": message,
	})
	return string(out), nil
}

func int64ToIntSafe(v any) int {
	s := ToolRuntimeValue(v)
	if s == "" {
		return -1
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return -1
	}
	return int(n)
}

func allPlanStepsCompleted(plan []*PlanItem) bool {
	if len(plan) == 0 {
		return true
	}
	for _, it := range plan {
		if it == nil {
			continue
		}
		if it.Status != PlanStepCompleted {
			return false
		}
	}
	return true
}

func firstUnfinishedPlanStep(plan []*PlanItem) (step string, status PlanStepStatus) {
	if len(plan) == 0 {
		return "", ""
	}
	for _, it := range plan {
		if it == nil {
			continue
		}
		if it.Status != PlanStepCompleted {
			return strings.TrimSpace(it.Step), it.Status
		}
	}
	return "", ""
}

func normalizeTaskStatusInput(raw string) (TaskStatus, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", false
	}
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	switch s {
	case "completed", "complete", "done", "success", "succeeded", "ok":
		return TaskStatusCompleted, true
	case "failed", "fail", "error", "errored":
		return TaskStatusFailed, true
	case "canceled", "cancelled", "cancel", "aborted", "stopped", "stop":
		return TaskStatusCanceled, true
	default:
		return "", false
	}
}

func softToolValidationOutput(reason string, message string, extra map[string]any) (string, error) {
	out := map[string]any{
		"ok":      false,
		"reason":  strings.TrimSpace(reason),
		"message": strings.TrimSpace(message),
	}
	for k, v := range extra {
		if strings.TrimSpace(k) == "" {
			continue
		}
		out[k] = v
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

type TaskStatusQueryTool struct {
	ctx ToolContext
}

func NewTaskStatusQueryTool(ctx ToolContext) *TaskStatusQueryTool {
	return &TaskStatusQueryTool{ctx: ctx}
}

func (t *TaskStatusQueryTool) Name() string { return TaskStatusQueryToolName }

func (t *TaskStatusQueryTool) Description() string {
	return "查询当前任务状态快照。"
}

func (t *TaskStatusQueryTool) Parameters() any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func (t *TaskStatusQueryTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.ctx == nil {
		return "", fmt.Errorf("tool context is nil")
	}
	_ = ctx
	_ = args

	snap := t.ctx.Snapshot()
	out, _ := json.Marshal(map[string]any{
		"ok":       true,
		"snapshot": snap,
	})
	return string(out), nil
}
