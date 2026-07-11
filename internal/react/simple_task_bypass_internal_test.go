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
	}, true, nil, nil)
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

// TestParseSubmitPlanArgs_TopicsRequired 校验 needs_planning=true && !simple 时
// phases 必填——缺失即返回 error，迫使模型在 submit_plan 重试通道补字段。
func TestParseSubmitPlanArgs_TopicsRequired(t *testing.T) {
	_, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"explanation":        "复杂任务",
		"goal_understanding": "核心目标: 分析多个对等模块的访问控制",
		"plan": []map[string]any{
			{"id": "step-1", "step": "枚举模块 A 接口", "status": "pending", "phase_id": "phase-a", "depends_on": []string{}},
		},
		// phases 缺失 ↓
	}, true, nil, nil)
	if err == nil {
		t.Fatal("expected error when phases missing under needs_planning=true && !simple")
	}
}

// TestParseSubmitPlanArgs_TopicsSimpleExempt 校验 simple=true 任务豁免 phases。
func TestParseSubmitPlanArgs_TopicsSimpleExempt(t *testing.T) {
	res, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"simple":             true,
		"explanation":        "单步即可",
		"goal_understanding": "核心目标: 一次性查询",
		"plan": []map[string]any{
			{"id": "step-1", "step": "查询并回复", "status": "pending", "depends_on": []string{}},
		},
		// phases 缺失但 simple=true，应通过（runtime synthetic phase 兜底）↓
	}, true, nil, nil)
	if err != nil {
		t.Fatalf("simple task should bypass phases check, got error: %v", err)
	}
	if len(res.Topics) != 0 {
		t.Fatalf("expected empty phases, got %+v", res.Topics)
	}
}

// TestParseSubmitPlanArgs_TopicsAccepted 校验 phases 与 phase_id 被正确解析归一。
func TestParseSubmitPlanArgs_TopicsAccepted(t *testing.T) {
	res, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"explanation":        "复杂任务",
		"goal_understanding": "核心目标: 多模块审计",
		"phases": []map[string]any{
			{"id": "phase-a", "name": "模块 A 的访问控制分析", "depends_on": []string{}},
			{"id": "phase-b", "name": "模块 B 的访问控制分析", "depends_on": []string{}},
		},
		"plan": []map[string]any{
			{"id": "step-1", "step": "枚举模块 A 接口", "status": "pending", "phase_id": "phase-a", "depends_on": []string{}},
			{"id": "step-2", "step": "枚举模块 B 接口", "status": "pending", "phase_id": "phase-b", "depends_on": []string{}},
		},
	}, true, nil, nil)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(res.Topics) != 2 || res.Topics[0].ID != "phase-a" || res.Topics[0].Status != builtin_tools.AnalysisTopicPending {
		t.Fatalf("phases mismatch: %+v", res.Topics)
	}
}

// TestParseSubmitPlanArgs_DanglingTopicIDRejected 校验 plan×phases 引用闭包：
// step 引用未知 phase / 缺 phase_id 时拒绝提交（防被错挂 synthetic phase）。
func TestParseSubmitPlanArgs_DanglingTopicIDRejected(t *testing.T) {
	_, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"explanation":        "复杂任务",
		"goal_understanding": "核心目标: 多模块审计",
		"phases": []map[string]any{
			{"id": "phase-a", "name": "模块 A 的访问控制分析", "depends_on": []string{}},
		},
		"plan": []map[string]any{
			{"id": "step-1", "step": "枚举模块 A 接口", "status": "pending", "phase_id": "phase-ghost", "depends_on": []string{}},
		},
	}, true, nil, nil)
	if err == nil {
		t.Fatal("expected error for dangling phase_id")
	}
}

// TestParseSubmitPlanArgs_MergePreservesTerminalTopics 校验重规划合并：被省略的既有
// completed/blocked phase 保留，其下承接的 completed step 引用不悬空；被省略的既有
// pending phase 不保留。
func TestParseSubmitPlanArgs_MergePreservesTerminalTopics(t *testing.T) {
	prior := []*builtin_tools.AnalysisTopic{
		{ID: "phase-done", Name: "已收束 lane", Status: builtin_tools.AnalysisTopicCompleted},
		{ID: "phase-old-pending", Name: "被放弃 lane", Status: builtin_tools.AnalysisTopicPending},
	}
	res, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"explanation":        "重规划",
		"goal_understanding": "核心目标: 继续推进",
		"phases": []map[string]any{
			{"id": "phase-next", "name": "下一个 lane", "depends_on": []string{}},
		},
		"plan": []map[string]any{
			{"id": "step-old", "step": "已完成的旧步骤", "status": "completed", "phase_id": "phase-done", "depends_on": []string{}},
			{"id": "step-new", "step": "新步骤", "status": "pending", "phase_id": "phase-next", "depends_on": []string{}},
		},
	}, false, prior, nil)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	ids := map[string]bool{}
	for _, phase := range res.Topics {
		ids[phase.ID] = true
	}
	if !ids["phase-done"] || !ids["phase-next"] {
		t.Fatalf("expected terminal phase preserved and new phase adopted, got %+v", res.Topics)
	}
	if ids["phase-old-pending"] {
		t.Fatalf("omitted pending phase must not be preserved, got %+v", res.Topics)
	}
}

// TestParseSubmitPlanArgs_MergePreservesPhaseReferencedByTerminalStep 校验 review #4：
// 被省略的既有 pending phase 若被前轮某 terminal step 引用（该 step 会被 mergeReplannedPlan
// 保留），该 phase 也必须保留，避免保留 step 的 phase_id 悬空被错挂 synthetic。
func TestParseSubmitPlanArgs_MergePreservesPhaseReferencedByTerminalStep(t *testing.T) {
	prior := []*builtin_tools.AnalysisTopic{
		// phase-mixed 仍是 pending（其下有 completed a1 + 本轮不再提交），但被 terminal step 引用
		{ID: "phase-mixed", Name: "混合 lane", Status: builtin_tools.AnalysisTopicPending},
	}
	priorPlan := []*builtin_tools.PlanItem{
		{ID: "a1", Step: "已完成步骤", Status: builtin_tools.PlanStepCompleted, TopicID: "phase-mixed"},
	}
	res, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"explanation":        "重规划",
		"goal_understanding": "核心目标: 继续推进",
		"phases": []map[string]any{
			{"id": "phase-next", "name": "下一个 lane", "depends_on": []string{}},
		},
		"plan": []map[string]any{
			{"id": "b1", "step": "新步骤", "status": "pending", "phase_id": "phase-next", "depends_on": []string{}},
		},
	}, false, prior, priorPlan)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	found := false
	for _, phase := range res.Topics {
		if phase.ID == "phase-mixed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("pending phase referenced by terminal step must be preserved, got %+v", res.Topics)
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
