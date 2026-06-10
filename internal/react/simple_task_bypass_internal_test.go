package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// TestParseSubmitPlanArgs_SimpleFlag 校验 simple 标记从 submit_plan 参数解析。
func TestParseSubmitPlanArgs_SimpleFlag(t *testing.T) {
	res, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"simple":             true,
		"explanation":        "单步即可",
		"goal_understanding": "核心目标: 回答一个简单问题",
		"plan": []map[string]any{
			{"id": "step-1", "step": "直接回答", "status": "pending", "depends_on": []string{}},
		},
	}, true)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !res.Simple {
		t.Fatal("expected simple=true parsed")
	}
}

// TestSetSimpleTask_ResetOnReplan 校验直通标记随重规划提交复位。
func TestSetSimpleTask_ResetOnReplan(t *testing.T) {
	tracker := NewStateTracker()
	tracker.SetSimpleTask(true)
	if !tracker.Snapshot().SimpleTask {
		t.Fatal("expected simple_task set")
	}
	tracker.SetSimpleTask(false)
	if tracker.Snapshot().SimpleTask {
		t.Fatal("expected simple_task reset")
	}
}

// TestSimpleBypassCondition 校验直通判定条件：simple 且单步；多步计划不直通。
func TestSimpleBypassCondition(t *testing.T) {
	single := builtin_tools.StateSnapshot{
		SimpleTask: true,
		Plan:       []*builtin_tools.PlanItem{{ID: "s1", Step: "x", Status: builtin_tools.PlanStepCompleted}},
	}
	if !(single.SimpleTask && len(single.Plan) == 1) {
		t.Fatal("expected single-step simple snapshot to bypass")
	}
	multi := builtin_tools.StateSnapshot{
		SimpleTask: true,
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "x", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "y", Status: builtin_tools.PlanStepPending},
		},
	}
	if multi.SimpleTask && len(multi.Plan) == 1 {
		t.Fatal("multi-step plan must not bypass")
	}
}
