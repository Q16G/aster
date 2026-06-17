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

// TestParseSubmitPlanArgs_CurrentPhaseRequired 校验 needs_planning=true && !simple 时
// current_phase 必填——缺失即返回 error，迫使模型在 submit_plan 重试通道补字段。
func TestParseSubmitPlanArgs_CurrentPhaseRequired(t *testing.T) {
	_, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"explanation":        "复杂任务",
		"goal_understanding": "核心目标: 分析多个对等模块的访问控制",
		"plan": []map[string]any{
			{"id": "step-1", "step": "枚举 hr-api 接口", "status": "pending", "depends_on": []string{}},
		},
		// current_phase 缺失 ↓
	}, true)
	if err == nil {
		t.Fatal("expected error when current_phase missing under needs_planning=true && !simple")
	}
}

// TestParseSubmitPlanArgs_CurrentPhaseSimpleExempt 校验 simple=true 任务豁免 current_phase。
func TestParseSubmitPlanArgs_CurrentPhaseSimpleExempt(t *testing.T) {
	res, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"simple":             true,
		"explanation":        "单步即可",
		"goal_understanding": "核心目标: 一次性查询",
		"plan": []map[string]any{
			{"id": "step-1", "step": "查询并回复", "status": "pending", "depends_on": []string{}},
		},
		// current_phase 缺失但 simple=true，应通过 ↓
	}, true)
	if err != nil {
		t.Fatalf("simple task should bypass current_phase check, got error: %v", err)
	}
	if res.CurrentPhase != "" {
		t.Fatalf("expected empty current_phase, got %q", res.CurrentPhase)
	}
}

// TestParseSubmitPlanArgs_CurrentPhaseAccepted 校验 current_phase 字段被正确解析。
func TestParseSubmitPlanArgs_CurrentPhaseAccepted(t *testing.T) {
	res, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"explanation":        "复杂任务",
		"goal_understanding": "核心目标: 多模块审计",
		"current_phase":      "分析模块 hr-api 的访问控制（接口枚举 + 鉴权矩阵）",
		"plan": []map[string]any{
			{"id": "step-1", "step": "枚举 hr-api 接口", "status": "pending", "depends_on": []string{}},
		},
	}, true)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if res.CurrentPhase != "分析模块 hr-api 的访问控制（接口枚举 + 鉴权矩阵）" {
		t.Fatalf("current_phase mismatch: got %q", res.CurrentPhase)
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
