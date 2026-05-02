package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type TaskPlannerTool struct {
	ctx ToolContext
}

func NewTaskPlannerTool(ctx ToolContext) *TaskPlannerTool {
	return &TaskPlannerTool{ctx: ctx}
}

func (t *TaskPlannerTool) Name() string { return TaskPlannerToolName }

func (t *TaskPlannerTool) Description() string {
	return "生成任务计划（plan）。"
}

func (t *TaskPlannerTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"overwrite": map[string]any{
				"type":        "boolean",
				"description": "可选：是否覆盖已有 plan（默认 false）",
			},
		},
		"additionalProperties": false,
	}
}

func (t *TaskPlannerTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.ctx == nil {
		return "", fmt.Errorf("tool context is nil")
	}

	overwrite := false
	if v, ok := args["overwrite"]; ok {
		if b, ok := v.(bool); ok {
			overwrite = b
		}
	}

	input := plannerInputFromTimeline(t.ctx.Snapshot().InputTimeline)
	if input == "" {
		return "", fmt.Errorf("input is required")
	}

	prev := t.ctx.Snapshot()
	if len(prev.Plan) > 0 && !overwrite {
		out, _ := json.Marshal(map[string]any{
			"ok":              true,
			"updated":         false,
			"message":         "plan already exists; set overwrite=true to regenerate",
			"plan":            prev.Plan,
			"current_step_id": prev.CurrentStepID,
		})
		return string(out), nil
	}

	planner := t.ctx.GetTaskPlanner()
	if planner == nil {
		return "", fmt.Errorf("task planner not configured")
	}

	res, err := planner.Plan(ctx, input)
	if err != nil {
		return "", err
	}
	if res == nil || len(res.Plan) == 0 {
		out, _ := json.Marshal(map[string]any{
			"ok":             true,
			"updated":        false,
			"needs_planning": false,
			"message":        "planner returned empty plan",
		})
		return string(out), nil
	}

	items, err := normalizePlanItems(res.Plan, true)
	if err != nil {
		return "", fmt.Errorf("planner returned invalid dependency plan: %w", err)
	}
	if len(items) == 0 {
		out, _ := json.Marshal(map[string]any{
			"ok":             true,
			"updated":        false,
			"needs_planning": false,
			"message":        "planner plan contains no valid step",
		})
		return string(out), nil
	}
	snapshot := t.ctx.ApplyPlanAndEmit(ctx, items, res.Explanation, res.NeedsPlanning)

	out, _ := json.Marshal(map[string]any{
		"ok":              true,
		"updated":         true,
		"needs_planning":  res.NeedsPlanning,
		"explanation":     strings.TrimSpace(res.Explanation),
		"plan":            items,
		"current_step_id": snapshot.CurrentStepID,
	})
	return string(out), nil
}

func plannerInputFromTimeline(timeline []*TimelineInput) string {
	if len(timeline) == 0 {
		return ""
	}
	lines := make([]string, 0, len(timeline))
	for _, item := range timeline {
		if item == nil {
			continue
		}
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		if item.CreatedAt.IsZero() {
			lines = append(lines, "- "+content)
			continue
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", item.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), content))
	}
	if len(lines) == 0 {
		return ""
	}
	return "用户输入时间线：\n" + strings.Join(lines, "\n")
}
