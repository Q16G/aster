package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

func seedPlannerJournal(t *testing.T, rootDir string) {
	t.Helper()
	if err := AppendPlannerJournalRecords(rootDir, []*builtin_tools.PlannerJournalRecord{
		{Kind: builtin_tools.PlannerJournalKindPlan, PlanVersion: 2, Item: &builtin_tools.PlanItem{ID: "step-1", Step: "A", Status: builtin_tools.PlanStepCompleted, ShortSummary: "done"}},
		{Kind: builtin_tools.PlannerJournalKindPlan, PlanVersion: 2, Item: &builtin_tools.PlanItem{ID: "step-2", Step: "B", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}}},
	}); err != nil {
		t.Fatalf("seed journal: %v", err)
	}
}

// TestSynthesizeResumeSnapshot_PlannerJournalIsPlanTruthSource 校验恢复时 plan 以
// planner.jsonl 重放为权威来源，assessed_state 的内联 plan 仅作 journal 缺失回退。
func TestSynthesizeResumeSnapshot_PlannerJournalIsPlanTruthSource(t *testing.T) {
	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	writer, err := newArtifactWriter(runtime)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	seedPlannerJournal(t, rootDir)

	// assessed_state 带着过期的内联 plan（旧快照），不应覆盖 journal。
	stale := &FinalAssessmentArtifact{
		AssessedState: assessedStatePayload{
			Status:      builtin_tools.TaskStatusRunning,
			Plan:        []*builtin_tools.PlanItem{{ID: "step-1", Step: "A", Status: builtin_tools.PlanStepInProgress}},
			PlanVersion: 1,
		},
	}

	snapshot, planValid := synthesizeResumeSnapshot(writer, nil, nil, stale, 0)
	if !planValid {
		t.Fatal("expected plan valid from journal")
	}
	if snapshot.PlanVersion != 2 {
		t.Fatalf("expected journal plan_version 2, got %d", snapshot.PlanVersion)
	}
	if len(snapshot.Plan) != 2 || snapshot.Plan[0].Status != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected journal plan replayed, got %+v", snapshot.Plan)
	}
	if snapshot.Plan[0].ShortSummary != "done" {
		t.Fatalf("expected baked产出字段 from journal, got %+v", snapshot.Plan[0])
	}
}

// TestAlignPlanWithJournal_OverridesBlobPlan 校验 v2 blob 恢复后 journal 校准 plan。
func TestAlignPlanWithJournal_OverridesBlobPlan(t *testing.T) {
	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	seedPlannerJournal(t, rootDir)

	a := &Agent{workspaceRuntime: runtime}
	st := builtin_tools.StateSnapshot{
		Plan:        []*builtin_tools.PlanItem{{ID: "step-1", Step: "A", Status: builtin_tools.PlanStepInProgress}},
		PlanVersion: 1,
	}
	a.alignPlanWithJournal(&st)
	if st.PlanVersion != 2 || len(st.Plan) != 2 {
		t.Fatalf("expected journal alignment, got version=%d plan=%+v", st.PlanVersion, st.Plan)
	}

	// journal 缺失（旧 session）：blob 原样保留。
	a2 := &Agent{workspaceRuntime: mustRuntime(t, t.TempDir())}
	st2 := builtin_tools.StateSnapshot{
		Plan:        []*builtin_tools.PlanItem{{ID: "x", Step: "X", Status: builtin_tools.PlanStepPending}},
		PlanVersion: 1,
	}
	a2.alignPlanWithJournal(&st2)
	if len(st2.Plan) != 1 || st2.PlanVersion != 1 {
		t.Fatalf("expected blob plan preserved without journal, got %+v", st2)
	}
}

func mustRuntime(t *testing.T, rootDir string) WorkspaceRuntime {
	t.Helper()
	rt, err := newLocalWorkspaceRuntime("s", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	return rt
}

// TestSynthesizeResumeSnapshot_LegacyDataGetsSyntheticPhase 校验旧数据（journal 无
// phase 行、item 无 phase_id）恢复时合成 synthetic phase 并完成挂靠。
func TestSynthesizeResumeSnapshot_LegacyDataGetsSyntheticPhase(t *testing.T) {
	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	writer, err := newArtifactWriter(runtime)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	seedPlannerJournal(t, rootDir)

	snapshot, planValid := synthesizeResumeSnapshot(writer, nil, nil, nil, 0)
	if !planValid {
		t.Fatal("expected plan valid from journal")
	}
	if len(snapshot.Topics) != 1 || snapshot.Topics[0].ID != builtin_tools.SyntheticTopicID {
		t.Fatalf("expected synthetic phase for legacy data, got %+v", snapshot.Topics)
	}
	for _, item := range snapshot.Plan {
		if item.TopicID != builtin_tools.SyntheticTopicID {
			t.Fatalf("item %s not attached to synthetic phase: %q", item.ID, item.TopicID)
		}
	}
}

// TestSynthesizeResumeSnapshot_JournalTopicsRestored 校验 journal 含 phase 行时恢复
// phases 并保持 item 挂靠。
func TestSynthesizeResumeSnapshot_JournalTopicsRestored(t *testing.T) {
	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	writer, err := newArtifactWriter(runtime)
	if err != nil {
		t.Fatalf("writer: %v", err)
	}
	if err := AppendPlannerJournalRecords(rootDir, []*builtin_tools.PlannerJournalRecord{
		{Kind: builtin_tools.PlannerJournalKindPlan, PlanVersion: 1, Item: &builtin_tools.PlanItem{ID: "a1", Step: "A", Status: builtin_tools.PlanStepCompleted, TopicID: "phase-a"}},
		{Kind: builtin_tools.PlannerJournalKindPlan, PlanVersion: 1, Item: &builtin_tools.PlanItem{ID: "b1", Step: "B", Status: builtin_tools.PlanStepPending, TopicID: "phase-b"}},
		{Kind: builtin_tools.PlannerJournalKindTopic, PlanVersion: 1, Phase: &builtin_tools.AnalysisTopic{ID: "phase-a", Status: builtin_tools.AnalysisTopicCompleted}},
		{Kind: builtin_tools.PlannerJournalKindTopic, PlanVersion: 1, Phase: &builtin_tools.AnalysisTopic{ID: "phase-b", Status: builtin_tools.AnalysisTopicPending, DependsOn: []string{"phase-a"}}},
	}); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	snapshot, planValid := synthesizeResumeSnapshot(writer, nil, nil, nil, 0)
	if !planValid {
		t.Fatal("expected plan valid")
	}
	if len(snapshot.Topics) != 2 || snapshot.Topics[0].Status != builtin_tools.AnalysisTopicCompleted {
		t.Fatalf("expected journal phases restored, got %+v", snapshot.Topics)
	}
	if snapshot.Plan[1].TopicID != "phase-b" {
		t.Fatalf("item attachment lost: %+v", snapshot.Plan[1])
	}
}

// TestResolveResumeCurrentStepID_RespectsPhaseGate 校验 review #2：resume 选步用 frontier
// 判定（带 phase 门），不选被 blocked / 依赖未解锁 lane 下的 step。
func TestResolveResumeCurrentStepID_RespectsPhaseGate(t *testing.T) {
	phases := []*builtin_tools.AnalysisTopic{
		{ID: "phase-a", Status: builtin_tools.AnalysisTopicBlocked},
		{ID: "phase-b", Status: builtin_tools.AnalysisTopicPending, DependsOn: []string{"phase-a"}},
	}
	plan := []*builtin_tools.PlanItem{
		// phase-a 被 blocked，其下 a1 应被 SkipStepsOfBlockedTopics 收敛，不应被选中
		{ID: "a1", Status: builtin_tools.PlanStepPending, TopicID: "phase-a"},
		// phase-b 依赖 phase-a（blocked=terminal 解锁）→ b1 可选
		{ID: "b1", Status: builtin_tools.PlanStepPending, TopicID: "phase-b"},
	}
	builtin_tools.SkipStepsOfBlockedTopics(plan, phases)
	builtin_tools.HydratePlanRelations(plan)
	got := resolveResumeCurrentStepID(plan, phases, "a1")
	if got != "b1" {
		t.Fatalf("resume must skip blocked lane step and pick frontier b1, got %q", got)
	}
}
