package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// completeStep 把某 step 标 completed（模拟 step 阶段收尾），返回新快照。
func completeStep(t *testing.T, tracker *StateTracker, stepID string) builtin_tools.StateSnapshot {
	t.Helper()
	snap := tracker.Snapshot()
	plan := make([]*builtin_tools.PlanItem, len(snap.Plan))
	for i, item := range snap.Plan {
		clone := *item
		if clone.ID == stepID {
			clone.Status = builtin_tools.PlanStepCompleted
		}
		plan[i] = &clone
	}
	return tracker.Replace(builtin_tools.StateSnapshot{
		Phase:       builtin_tools.AgentPhaseStep,
		Plan:        plan,
		Topics:      snap.Topics,
		PlanVersion: snap.PlanVersion,
		CurrentGoal: snap.CurrentGoal,
	})
}

// TestFrontier_TwoLaneLifecycle 端到端验证 Parallel Frontier 的核心生命周期（纯状态机，
// 无 goroutine，确定性）：两条 lane A/B，B 的 phase depends_on A，max_parallel=3。
//
//	phase-a: a1 → a2 (a2 depends a1)
//	phase-b: b1        (phase-b depends_on phase-a)
//
// 断言：初始 frontier 只放 phase-a → a1/a2 顺次完成后 phase-a 未 settled 时 b1 仍被锁 →
// barrier 处 currentPhase 路由到 StepReplan → assessment 判 phase-a completed 解锁 phase-b →
// frontier 放 b1 → b1 完成 + phase-b completed → AllTopicsSettled → currentPhase 路由 FinalAnswer。
func TestFrontier_TwoLaneLifecycle(t *testing.T) {
	const maxParallel = 3
	tracker := NewStateTracker()
	tracker.SetTopics([]*builtin_tools.AnalysisTopic{
		{ID: "phase-a", Name: "lane A", Status: builtin_tools.AnalysisTopicPending},
		{ID: "phase-b", Name: "lane B", DependsOn: []string{"phase-a"}, Status: builtin_tools.AnalysisTopicPending},
	})
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a1", Step: "A1", Status: builtin_tools.PlanStepPending, TopicID: "phase-a"},
		{ID: "a2", Step: "A2", Status: builtin_tools.PlanStepPending, TopicID: "phase-a", DependsOn: []string{"a1"}},
		{ID: "b1", Step: "B1", Status: builtin_tools.PlanStepPending, TopicID: "phase-b"},
	}, "two-lane", true)

	snap := tracker.Snapshot()

	// 1) 初始 frontier：只放 phase-a 的 a1（a2 依赖 a1 未满足；b1 被 phase-b 锁）。
	if got := builtin_tools.ReadyFrontierPlanStepIDs(snap.Plan, snap.Topics); len(got) != 1 || got[0] != "a1" {
		t.Fatalf("initial frontier should release only a1, got %v", got)
	}

	// 2) a1 完成 → frontier 放 a2；b1 仍被锁。
	snap = completeStep(t, tracker, "a1")
	if got := builtin_tools.ReadyFrontierPlanStepIDs(snap.Plan, snap.Topics); len(got) != 1 || got[0] != "a2" {
		t.Fatalf("after a1 done, frontier should release a2, got %v", got)
	}

	// 3) a2 完成 → phase-a 的 step 全 terminal，但 phase-a 状态仍 pending（未 settled），
	//    b1 仍被 phase-b 锁 → frontier 空。
	snap = completeStep(t, tracker, "a2")
	if got := builtin_tools.ReadyFrontierPlanStepIDs(snap.Plan, snap.Topics); got != nil {
		t.Fatalf("phase-a steps done but phase-a unsettled: b1 must stay locked, got %v", got)
	}
	// currentPhase 在 Step 阶段、frontier 空、非全 plan terminal（b1 pending）→ 不是终态防卫；
	// 主路径完成翻 StepReplan 后 frontier 空 → 真正进 step_replan（barrier）。
	replanSnap := snap
	replanSnap.Phase = builtin_tools.AgentPhaseStepReplan
	if got := currentPhase(replanSnap, maxParallel); got != builtin_tools.AgentPhaseStepReplan {
		t.Fatalf("at barrier with locked b1, should stay StepReplan, got %q", got)
	}

	// 4) step_replan 判 phase-a completed → 解锁 phase-b → frontier 放 b1。
	changed, snap := tracker.ApplyTopicAssessments([]*builtin_tools.TopicAssessment{
		{TopicID: "phase-a", Status: builtin_tools.TopicAssessCompleted},
	})
	if len(changed) != 1 {
		t.Fatalf("expected phase-a status change, got %d", len(changed))
	}
	if got := builtin_tools.ReadyFrontierPlanStepIDs(snap.Plan, snap.Topics); len(got) != 1 || got[0] != "b1" {
		t.Fatalf("after phase-a completed, frontier should release b1, got %v", got)
	}

	// 5) b1 完成 → 全 plan terminal；phase-b 仍 pending → 未 settled → barrier 路由 StepReplan。
	snap = completeStep(t, tracker, "b1")
	if !builtin_tools.AllPlanStepsTerminal(snap.Plan) {
		t.Fatal("all steps should be terminal after b1")
	}
	stepSnap := snap
	stepSnap.Phase = builtin_tools.AgentPhaseStep
	if got := currentPhase(stepSnap, maxParallel); got != builtin_tools.AgentPhaseStepReplan {
		t.Fatalf("all-terminal but phase-b unsettled should route StepReplan, got %q", got)
	}

	// 6) step_replan 判 phase-b completed → AllTopicsSettled → currentPhase 路由 FinalAnswer。
	_, snap = tracker.ApplyTopicAssessments([]*builtin_tools.TopicAssessment{
		{TopicID: "phase-b", Status: builtin_tools.TopicAssessCompleted},
	})
	if !builtin_tools.AllTopicsSettled(snap.Topics) {
		t.Fatal("all phases should be settled")
	}
	finalSnap := snap
	finalSnap.Phase = builtin_tools.AgentPhaseStep
	if got := currentPhase(finalSnap, maxParallel); got != builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("all-terminal + all-settled should route FinalAnswer, got %q", got)
	}
}

// TestFrontier_BlockedLaneSkipsAndSettles 验证 blocked lane 联动 skip 其下 pending step，
// 并使全 plan 达 terminal + 全 phase settled，正常收尾到 FinalAnswer（不卡死）。
func TestFrontier_BlockedLaneSkipsAndSettles(t *testing.T) {
	tracker := NewStateTracker()
	tracker.SetTopics([]*builtin_tools.AnalysisTopic{
		{ID: "phase-a", Status: builtin_tools.AnalysisTopicPending},
	})
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a1", Step: "A1", Status: builtin_tools.PlanStepCompleted, TopicID: "phase-a"},
		{ID: "a2", Step: "A2", Status: builtin_tools.PlanStepPending, TopicID: "phase-a"},
	}, "blocked-lane", true)

	_, snap := tracker.ApplyTopicAssessments([]*builtin_tools.TopicAssessment{
		{TopicID: "phase-a", Status: builtin_tools.TopicAssessBlocked},
	})
	// blocked lane 的 pending a2 被收敛 skipped。
	for _, item := range snap.Plan {
		if item.ID == "a2" && item.Status != builtin_tools.PlanStepSkipped {
			t.Fatalf("blocked lane pending step should be skipped, got %s", item.Status)
		}
	}
	if !builtin_tools.AllPlanStepsTerminal(snap.Plan) || !builtin_tools.AllTopicsSettled(snap.Topics) {
		t.Fatal("blocked lane should make plan terminal and phases settled")
	}
	finalSnap := snap
	finalSnap.Phase = builtin_tools.AgentPhaseStep
	if got := currentPhase(finalSnap, 3); got != builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("blocked+settled should route FinalAnswer, got %q", got)
	}
}
