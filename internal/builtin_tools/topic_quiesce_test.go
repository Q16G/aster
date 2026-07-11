package builtin_tools

import "testing"

// TestPhaseQuiesced 验证 topic 局部静默点判定：全 terminal + ≥1 step → 静默；
// 有 pending/in_progress → 未静默；0-step → 未静默（0-step 守卫）。
func TestPhaseQuiesced(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a1", PhaseID: "topic-a", Status: PlanStepCompleted},
		{ID: "a2", PhaseID: "topic-a", Status: PlanStepFailed},
		{ID: "b1", PhaseID: "topic-b", Status: PlanStepCompleted},
		{ID: "b2", PhaseID: "topic-b", Status: PlanStepInProgress},
		{ID: "c1", PhaseID: "topic-c", Status: PlanStepPending},
	}

	if !PhaseQuiesced(plan, "topic-a") {
		t.Error("topic-a 全 terminal，应静默")
	}
	if PhaseQuiesced(plan, "topic-b") {
		t.Error("topic-b 有 in_progress，不应静默")
	}
	if PhaseQuiesced(plan, "topic-c") {
		t.Error("topic-c 有 pending，不应静默")
	}
	if PhaseQuiesced(plan, "topic-none") {
		t.Error("0-step topic 不应静默（0-step 守卫）")
	}
	if PhaseQuiesced(plan, "") {
		t.Error("空 phaseID 不应静默")
	}
}

// TestQuiescedActivePhases 验证只返回「active（非终态+已解锁）且已静默」的 topic。
func TestQuiescedActivePhases(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a1", PhaseID: "topic-a", Status: PlanStepCompleted}, // a 静默
		{ID: "b1", PhaseID: "topic-b", Status: PlanStepInProgress}, // b 未静默
		{ID: "c1", PhaseID: "topic-c", Status: PlanStepCompleted}, // c 静默但 c 已 completed（终态）
	}
	phases := []*PlanPhase{
		{ID: "topic-a", Status: PlanPhasePending},
		{ID: "topic-b", Status: PlanPhasePending},
		{ID: "topic-c", Status: PlanPhaseCompleted}, // 终态 → 不 active
		{ID: "topic-d", Status: PlanPhasePending, DependsOn: []string{"topic-b"}}, // 前置未终态 → 未解锁
	}

	got := QuiescedActivePhases(plan, phases)
	if len(got) != 1 || got[0].ID != "topic-a" {
		ids := make([]string, len(got))
		for i, p := range got {
			ids[i] = p.ID
		}
		t.Fatalf("应只返回已静默的 active topic-a，got %v", ids)
	}
}
