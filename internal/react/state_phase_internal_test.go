package react

import (
	"aster/internal/builtin_tools"
	"testing"
)

func TestStateTracker_SetPhasesAndSynthesize(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "s1", Step: "step one", Status: builtin_tools.PlanStepPending},
	}, "", false)

	// UpdatePlan 无 phases 时自动合成 synthetic phase 并挂靠
	snap := tracker.Snapshot()
	if len(snap.Phases) != 1 || snap.Phases[0].ID != builtin_tools.SyntheticPhaseID {
		t.Fatalf("expected synthetic phase after UpdatePlan, got %+v", snap.Phases)
	}
	if snap.Plan[0].PhaseID != builtin_tools.SyntheticPhaseID {
		t.Fatalf("plan item not attached: %q", snap.Plan[0].PhaseID)
	}

	// SetPhases 替换 lane 清单；未挂靠 item 由 Synthesize 兜底
	tracker.SetPhases([]*builtin_tools.PlanPhase{{ID: "phase-a", Status: builtin_tools.PlanPhasePending}})
	snap = tracker.Snapshot()
	if len(snap.Phases) != 2 {
		t.Fatalf("expected [phase-a synthetic], got %+v", snap.Phases)
	}

	// 快照隔离：外部改动不影响内部状态
	snap.Phases[0].Status = builtin_tools.PlanPhaseBlocked
	if tracker.Snapshot().Phases[0].Status != builtin_tools.PlanPhasePending {
		t.Fatal("snapshot phases must be isolated")
	}
}

func TestStateTracker_ApplyPhaseAssessments(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a1", Step: "a1", Status: builtin_tools.PlanStepCompleted, PhaseID: "phase-a"},
		{ID: "b1", Step: "b1", Status: builtin_tools.PlanStepPending, PhaseID: "phase-b"},
		{ID: "b2", Step: "b2", Status: builtin_tools.PlanStepPending, PhaseID: "phase-b", DependsOn: []string{"b1"}},
	}, "", false)
	tracker.SetPhases([]*builtin_tools.PlanPhase{
		{ID: "phase-a", Status: builtin_tools.PlanPhasePending},
		{ID: "phase-b", Status: builtin_tools.PlanPhasePending},
	})

	changed, snap := tracker.ApplyPhaseAssessments([]*builtin_tools.PhaseAssessment{
		{PhaseID: "phase-a", Status: builtin_tools.PhaseAssessCompleted},
		{PhaseID: "phase-b", Status: builtin_tools.PhaseAssessBlocked},
		{PhaseID: "phase-ghost", Status: builtin_tools.PhaseAssessCompleted},
	})
	if len(changed) != 2 {
		t.Fatalf("expected 2 changed phases, got %d", len(changed))
	}
	byID := map[string]builtin_tools.PlanPhaseStatus{}
	for _, phase := range snap.Phases {
		byID[phase.ID] = phase.Status
	}
	if byID["phase-a"] != builtin_tools.PlanPhaseCompleted || byID["phase-b"] != builtin_tools.PlanPhaseBlocked {
		t.Fatalf("unexpected phase statuses: %+v", byID)
	}
	// blocked 联动：phase-b 下 pending step 全部 skipped（含依赖传播）
	for _, item := range snap.Plan {
		if item.PhaseID == "phase-b" && item.Status != builtin_tools.PlanStepSkipped {
			t.Fatalf("step %s of blocked phase should be skipped, got %s", item.ID, item.Status)
		}
	}
	// continue 与重复评估为 no-op
	changed, _ = tracker.ApplyPhaseAssessments([]*builtin_tools.PhaseAssessment{
		{PhaseID: "phase-a", Status: builtin_tools.PhaseAssessCompleted},
		{PhaseID: "phase-b", Status: builtin_tools.PhaseAssessContinue},
	})
	if len(changed) != 0 {
		t.Fatalf("expected idempotent no-op, got %d changed", len(changed))
	}
}
