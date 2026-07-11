package builtin_tools

import (
	"testing"
)

func TestNormalizeAnalysisTopics_Valid(t *testing.T) {
	phases, err := NormalizeAnalysisTopics([]*AnalysisTopic{
		{ID: "Phase-A", Name: " scheduler 分析 "},
		{ID: "phase-b", DependsOn: []string{"Phase-A"}, Status: AnalysisTopicPending},
	}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(phases))
	}
	if phases[0].ID != "phase-a" || phases[0].Name != "scheduler 分析" || phases[0].Status != AnalysisTopicPending {
		t.Fatalf("unexpected phase[0]: %+v", phases[0])
	}
	if len(phases[1].DependsOn) != 1 || phases[1].DependsOn[0] != "phase-a" {
		t.Fatalf("expected canonicalized dependency [phase-a], got %v", phases[1].DependsOn)
	}
}

func TestNormalizeAnalysisTopics_Rejections(t *testing.T) {
	cases := []struct {
		name   string
		phases []*AnalysisTopic
	}{
		{"missing id", []*AnalysisTopic{{Name: "x"}}},
		{"duplicate id", []*AnalysisTopic{{ID: "a"}, {ID: "a"}}},
		{"reserved synthetic id", []*AnalysisTopic{{ID: SyntheticTopicID}}},
		{"unknown dependency", []*AnalysisTopic{{ID: "a", DependsOn: []string{"ghost"}}}},
		{"cycle", []*AnalysisTopic{{ID: "a", DependsOn: []string{"b"}}, {ID: "b", DependsOn: []string{"a"}}}},
		{"invalid status", []*AnalysisTopic{{ID: "a", Status: "running"}}},
	}
	for _, tc := range cases {
		if _, err := NormalizeAnalysisTopics(tc.phases, true); err == nil {
			t.Fatalf("%s: expected error, got nil", tc.name)
		}
	}
}

func TestReadyFrontierPlanStepIDs_CrossLane(t *testing.T) {
	phases := []*AnalysisTopic{
		{ID: "phase-a", Status: AnalysisTopicPending},
		{ID: "phase-b", Status: AnalysisTopicPending},
	}
	plan := []*PlanItem{
		{ID: "a1", Status: PlanStepPending, TopicID: "phase-a"},
		{ID: "a2", Status: PlanStepPending, TopicID: "phase-a", DependsOn: []string{"a1"}},
		{ID: "b1", Status: PlanStepPending, TopicID: "phase-b"},
	}
	got := ReadyFrontierPlanStepIDs(plan, phases)
	if len(got) != 2 || got[0] != "a1" || got[1] != "b1" {
		t.Fatalf("expected cross-lane frontier [a1 b1], got %v", got)
	}
}

func TestReadyFrontierPlanStepIDs_PhaseLocked(t *testing.T) {
	phases := []*AnalysisTopic{
		{ID: "phase-a", Status: AnalysisTopicPending},
		{ID: "phase-b", Status: AnalysisTopicPending, DependsOn: []string{"phase-a"}},
	}
	plan := []*PlanItem{
		{ID: "a1", Status: PlanStepPending, TopicID: "phase-a"},
		{ID: "b1", Status: PlanStepPending, TopicID: "phase-b"},
	}
	got := ReadyFrontierPlanStepIDs(plan, phases)
	if len(got) != 1 || got[0] != "a1" {
		t.Fatalf("expected phase-b locked, frontier [a1], got %v", got)
	}

	// phase-a completed 解锁 phase-b；blocked 同样视同 terminal 解锁。
	for _, status := range []AnalysisTopicStatus{AnalysisTopicCompleted, AnalysisTopicBlocked} {
		phases[0].Status = status
		got = ReadyFrontierPlanStepIDs(plan, phases)
		if len(got) != 1 || got[0] != "b1" {
			t.Fatalf("phase-a %s: expected frontier [b1], got %v", status, got)
		}
	}
}

func TestReadyFrontierPlanStepIDs_TerminalPhaseAdmitsNothing(t *testing.T) {
	phases := []*AnalysisTopic{{ID: "phase-a", Status: AnalysisTopicCompleted}}
	plan := []*PlanItem{{ID: "a1", Status: PlanStepPending, TopicID: "phase-a"}}
	if got := ReadyFrontierPlanStepIDs(plan, phases); got != nil {
		t.Fatalf("completed phase must not release steps, got %v", got)
	}
}

func TestReadyFrontierPlanStepIDs_NoPhaseContextDegrades(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a", Status: PlanStepCompleted},
		{ID: "b", Status: PlanStepPending, DependsOn: []string{"a"}},
	}
	got := ReadyFrontierPlanStepIDs(plan, nil)
	want := ReadyRunnablePlanStepIDs(plan)
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("empty phases must degrade to ReadyRunnablePlanStepIDs: got %v want %v", got, want)
	}
}

func TestReadyFrontierPlanStepIDs_DanglingTopicIDAdmitted(t *testing.T) {
	phases := []*AnalysisTopic{{ID: "phase-a", Status: AnalysisTopicPending}}
	plan := []*PlanItem{{ID: "x1", Status: PlanStepPending, TopicID: "phase-ghost"}}
	got := ReadyFrontierPlanStepIDs(plan, phases)
	if len(got) != 1 || got[0] != "x1" {
		t.Fatalf("dangling phase_id must be admitted (submit-time validation owns rejection), got %v", got)
	}
}

func TestNextFrontierPlanStepID(t *testing.T) {
	phases := []*AnalysisTopic{
		{ID: "phase-a", Status: AnalysisTopicCompleted},
		{ID: "phase-b", Status: AnalysisTopicPending, DependsOn: []string{"phase-a"}},
	}
	plan := []*PlanItem{
		{ID: "a1", Status: PlanStepCompleted, TopicID: "phase-a"},
		{ID: "b1", Status: PlanStepPending, TopicID: "phase-b"},
	}
	if got := NextFrontierPlanStepID(plan, phases); got != "b1" {
		t.Fatalf("expected b1, got %q", got)
	}
	plan[1].Status = PlanStepCompleted
	if got := NextFrontierPlanStepID(plan, phases); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestAllTopicsSettled(t *testing.T) {
	if !AllTopicsSettled(nil) {
		t.Fatal("empty phases must be settled")
	}
	phases := []*AnalysisTopic{
		{ID: "a", Status: AnalysisTopicCompleted},
		{ID: "b", Status: AnalysisTopicBlocked},
	}
	if !AllTopicsSettled(phases) {
		t.Fatal("completed+blocked must be settled")
	}
	phases = append(phases, &AnalysisTopic{ID: "c", Status: AnalysisTopicPending})
	if AllTopicsSettled(phases) {
		t.Fatal("pending phase must not be settled")
	}
}

func TestSynthesizeTopicsIfMissing_LegacyPlan(t *testing.T) {
	plan := []*PlanItem{
		{ID: "s1", Status: PlanStepCompleted},
		{ID: "s2", Status: PlanStepPending},
	}
	phases := SynthesizeTopicsIfMissing(plan, nil, "接口 A 的深度测试\n第二行")
	if len(phases) != 1 || phases[0].ID != SyntheticTopicID {
		t.Fatalf("expected single synthetic phase, got %+v", phases)
	}
	if phases[0].Name != "接口 A 的深度测试" {
		t.Fatalf("expected first line as name, got %q", phases[0].Name)
	}
	for _, item := range plan {
		if item.TopicID != SyntheticTopicID {
			t.Fatalf("item %s not attached to synthetic phase: %q", item.ID, item.TopicID)
		}
	}
}

func TestSynthesizeTopicsIfMissing_DanglingAttached(t *testing.T) {
	plan := []*PlanItem{
		{ID: "s1", Status: PlanStepPending, TopicID: "phase-a"},
		{ID: "s2", Status: PlanStepPending, TopicID: "phase-ghost"},
	}
	in := []*AnalysisTopic{{ID: "phase-a", Status: AnalysisTopicPending}}
	phases := SynthesizeTopicsIfMissing(plan, in, "")
	if len(phases) != 2 || phases[1].ID != SyntheticTopicID {
		t.Fatalf("expected [phase-a synthetic], got %+v", phases)
	}
	if plan[0].TopicID != "phase-a" || plan[1].TopicID != SyntheticTopicID {
		t.Fatalf("unexpected attachment: %q %q", plan[0].TopicID, plan[1].TopicID)
	}
	// 入参 phases 不被共享底座
	phases[0].Status = AnalysisTopicBlocked
	if in[0].Status != AnalysisTopicPending {
		t.Fatal("input phases mutated: expected clone isolation")
	}
}

func TestSynthesizeTopicsIfMissing_NoopWhenClosed(t *testing.T) {
	plan := []*PlanItem{{ID: "s1", Status: PlanStepPending, TopicID: "phase-a"}}
	in := []*AnalysisTopic{{ID: "phase-a", Status: AnalysisTopicPending}}
	phases := SynthesizeTopicsIfMissing(plan, in, "goal")
	if len(phases) != 1 || phases[0].ID != "phase-a" {
		t.Fatalf("expected unchanged single phase, got %+v", phases)
	}
}

func TestSkipStepsOfBlockedTopics(t *testing.T) {
	phases := []*AnalysisTopic{
		{ID: "phase-a", Status: AnalysisTopicBlocked},
		{ID: "phase-b", Status: AnalysisTopicPending},
	}
	plan := []*PlanItem{
		{ID: "a1", Status: PlanStepPending, TopicID: "phase-a"},
		{ID: "a2", Status: PlanStepCompleted, TopicID: "phase-a"},
		{ID: "b1", Status: PlanStepPending, TopicID: "phase-b", DependsOn: []string{"a1"}},
		{ID: "b2", Status: PlanStepPending, TopicID: "phase-b"},
	}
	HydratePlanRelations(plan)
	if !SkipStepsOfBlockedTopics(plan, phases) {
		t.Fatal("expected changed=true")
	}
	if plan[0].Status != PlanStepSkipped {
		t.Fatalf("a1 should be skipped, got %s", plan[0].Status)
	}
	if plan[1].Status != PlanStepCompleted {
		t.Fatalf("completed a2 must not change, got %s", plan[1].Status)
	}
	if plan[2].Status != PlanStepSkipped {
		t.Fatalf("cross-phase downstream b1 should be skipped transitively, got %s", plan[2].Status)
	}
	if plan[3].Status != PlanStepPending {
		t.Fatalf("independent b2 must stay pending, got %s", plan[3].Status)
	}
	if SkipStepsOfBlockedTopics(plan, phases) {
		t.Fatal("second call must be idempotent (changed=false)")
	}
}

func TestNormalizePlanItems_PreservesTopicID(t *testing.T) {
	items, err := NormalizePlanItems([]*PlanItem{
		{ID: "s1", Step: "do something", TopicID: "Phase-A"},
	}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if items[0].TopicID != "phase-a" {
		t.Fatalf("expected canonicalized phase_id phase-a, got %q", items[0].TopicID)
	}
}

func TestCloneReplanContext_ClonesTopicAssessments(t *testing.T) {
	in := &ReplanContext{
		TopicAssessments: []*TopicAssessment{
			{TopicID: "phase-a", Status: TopicAssessContinue, DepthGaps: []string{"gap"}},
		},
	}
	out := CloneReplanContext(in)
	if len(out.TopicAssessments) != 1 {
		t.Fatalf("expected 1 assessment, got %d", len(out.TopicAssessments))
	}
	out.TopicAssessments[0].DepthGaps[0] = "mutated"
	if in.TopicAssessments[0].DepthGaps[0] != "gap" {
		t.Fatal("assessments not deep-cloned")
	}
}
