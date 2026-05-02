package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"context"
	"testing"
	"time"
)

type staticTaskPlanner struct {
	result *TaskPlannerResult
	err    error
}

func (p *staticTaskPlanner) Plan(ctx context.Context, input string) (*TaskPlannerResult, error) {
	_ = ctx
	_ = input
	return p.result, p.err
}

func TestParsePlanItemsSupportsDependencies(t *testing.T) {
	items, err := ParsePlanItems([]any{
		map[string]any{
			"id":         "inspect-events",
			"step":       "盘点事件类型",
			"status":     "in_progress",
			"depends_on": []any{},
		},
		map[string]any{
			"id":         "map-backend",
			"step":       "补齐后端映射",
			"status":     "pending",
			"depends_on": []any{"inspect-events"},
		},
		map[string]any{
			"id":         "render-ui",
			"step":       "前端展示 task 面板",
			"status":     "pending",
			"depends_on": []any{"map-backend"},
		},
	})
	if err != nil {
		t.Fatalf("parsePlanItems returned error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[1].ID != "map-backend" {
		t.Fatalf("expected item id map-backend, got %q", items[1].ID)
	}
	if len(items[2].DependsOn) != 1 || items[2].DependsOn[0] != "map-backend" {
		t.Fatalf("expected render-ui depends_on=[map-backend], got %+v", items[2].DependsOn)
	}
}

func TestUpdateCurrentStepDoesNotAutoPromoteNextItem(t *testing.T) {
	ctx := newFakeToolContext()
	ctx.snapshot.Plan = []*PlanItem{
		{ID: "inspect-events", Step: "盘点事件类型", Status: PlanStepInProgress},
		{ID: "map-backend", Step: "补齐后端映射", Status: PlanStepPending, DependsOn: []string{"inspect-events"}},
		{ID: "render-ui", Step: "前端展示 task 面板", Status: PlanStepPending, DependsOn: []string{"map-backend"}},
	}
	ctx.snapshot.CurrentStepID = "inspect-events"

	tool := NewUpdateCurrentStepTool(ctx)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"status": "completed",
	}); err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if ctx.snapshot.Plan[0].Status != PlanStepCompleted {
		t.Fatalf("expected first step completed, got %s", ctx.snapshot.Plan[0].Status)
	}
	if ctx.snapshot.Plan[1].Status != PlanStepPending {
		t.Fatalf("expected dependency-ready step to remain pending, got %s", ctx.snapshot.Plan[1].Status)
	}
	if ctx.snapshot.Plan[2].Status != PlanStepPending {
		t.Fatalf("expected blocked dependent step to remain pending, got %s", ctx.snapshot.Plan[2].Status)
	}
	// 新 runtime：terminal update 后进入 step_summary phase，保留 current_step_id 供总结阶段消费。
	if ctx.snapshot.CurrentStepID != "inspect-events" {
		t.Fatalf("expected current step kept for step_summary, got %q", ctx.snapshot.CurrentStepID)
	}
}

func TestTaskPlannerToolBuildsDependencyAwarePlan(t *testing.T) {
	ctx := newFakeToolContext()
	ctx.snapshot.InputTimeline = []*TimelineInput{
		{Content: "优化任务规划并建立依赖关系", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
	}
	ctx.planner = &staticTaskPlanner{
		result: &TaskPlannerResult{
			NeedsPlanning: true,
			Plan: []*PlanItem{
				{ID: "verify", Step: "补充验证", Status: PlanStepPending, DependsOn: []string{"implement"}},
				{ID: "inspect", Step: "梳理现状", Status: PlanStepPending, DependsOn: []string{}},
				{ID: "implement", Step: "实现改动", Status: PlanStepPending, DependsOn: []string{"inspect"}},
			},
			Explanation: "需要先梳理，再实现，最后验证。",
		},
	}

	tool := NewTaskPlannerTool(ctx)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if len(ctx.snapshot.Plan) != 3 {
		t.Fatalf("expected 3 plan items, got %d", len(ctx.snapshot.Plan))
	}
	if ctx.snapshot.Plan[0].Status != PlanStepPending {
		t.Fatalf("expected blocked verify step to remain pending, got %s", ctx.snapshot.Plan[0].Status)
	}
	if ctx.snapshot.Plan[1].Status != PlanStepPending {
		t.Fatalf("expected first dependency-ready step to stay pending, got %s", ctx.snapshot.Plan[1].Status)
	}
	if len(ctx.snapshot.Plan[2].DependsOn) != 1 || ctx.snapshot.Plan[2].DependsOn[0] != "inspect" {
		t.Fatalf("expected implement depends_on=[inspect], got %+v", ctx.snapshot.Plan[2].DependsOn)
	}
	if len(ctx.snapshot.Plan[2].ResolvedDependsOn) != 1 || ctx.snapshot.Plan[2].ResolvedDependsOn[0] != ctx.snapshot.Plan[1] {
		t.Fatalf("expected implement resolved dependency to point inspect item")
	}
	if ctx.snapshot.CurrentStepID != "inspect" {
		t.Fatalf("expected current step id inspect, got %q", ctx.snapshot.CurrentStepID)
	}
}

func TestTaskPlannerToolRejectsMissingTimelineInput(t *testing.T) {
	ctx := newFakeToolContext()
	ctx.planner = &staticTaskPlanner{
		result: &TaskPlannerResult{
			NeedsPlanning: true,
			Plan: []*PlanItem{
				{ID: "inspect", Step: "梳理现状", Status: PlanStepPending},
			},
		},
	}

	tool := NewTaskPlannerTool(ctx)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatalf("expected missing timeline input error, got nil")
	}
}

func TestTaskPlannerToolRejectsPlanWithoutStatus(t *testing.T) {
	ctx := newFakeToolContext()
	ctx.snapshot.InputTimeline = []*TimelineInput{
		{Content: "优化任务规划并建立依赖关系", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
	}
	ctx.planner = &staticTaskPlanner{
		result: &TaskPlannerResult{
			NeedsPlanning: true,
			Plan: []*PlanItem{
				{ID: "inspect", Step: "梳理现状"},
			},
		},
	}

	tool := NewTaskPlannerTool(ctx)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Fatalf("expected missing status error, got nil")
	}
}

func TestPropagateSkippedPlanSteps_MarksDownstreamPendingSkipped(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a", Step: "A", Status: PlanStepFailed},
		{ID: "b", Step: "B", Status: PlanStepPending, DependsOn: []string{"a"}},
		{ID: "c", Step: "C", Status: PlanStepPending, DependsOn: []string{"b"}},
		{ID: "d", Step: "D", Status: PlanStepPending},
	}

	changed := PropagateSkippedPlanSteps(plan)
	if !changed {
		t.Fatalf("expected changed=true")
	}
	if plan[1].Status != PlanStepSkipped {
		t.Fatalf("expected b skipped, got %s", plan[1].Status)
	}
	if plan[2].Status != PlanStepSkipped {
		t.Fatalf("expected c skipped, got %s", plan[2].Status)
	}
	if plan[3].Status != PlanStepPending {
		t.Fatalf("expected unrelated d stay pending, got %s", plan[3].Status)
	}
}

func TestNormalizePlanItems_HydratesResolvedDependsOn(t *testing.T) {
	items, err := NormalizePlanItems([]*PlanItem{
		{ID: "inspect", Step: "梳理现状", Status: PlanStepPending},
		{ID: "implement", Step: "实现改动", Status: PlanStepPending, DependsOn: []string{"inspect"}},
	}, true)
	if err != nil {
		t.Fatalf("normalize returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if len(items[1].ResolvedDependsOn) != 1 {
		t.Fatalf("expected one resolved dependency, got %+v", items[1].ResolvedDependsOn)
	}
	if items[1].ResolvedDependsOn[0] != items[0] {
		t.Fatalf("expected resolved dependency to point first item")
	}
}
