package react

import (
	"aster/internal/builtin_tools"
	"testing"
)

func TestStateTracker_SetTopicsAndSynthesize(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "s1", Step: "step one", Status: builtin_tools.PlanStepPending},
	}, "", false)

	// UpdatePlan 无 phases 时自动合成 synthetic phase 并挂靠
	snap := tracker.Snapshot()
	if len(snap.Topics) != 1 || snap.Topics[0].ID != builtin_tools.SyntheticTopicID {
		t.Fatalf("expected synthetic phase after UpdatePlan, got %+v", snap.Topics)
	}
	if snap.Plan[0].TopicID != builtin_tools.SyntheticTopicID {
		t.Fatalf("plan item not attached: %q", snap.Plan[0].TopicID)
	}

	// SetTopics 替换 lane 清单；未挂靠 item 由 Synthesize 兜底
	tracker.SetTopics([]*builtin_tools.AnalysisTopic{{ID: "phase-a", Status: builtin_tools.AnalysisTopicPending}})
	snap = tracker.Snapshot()
	if len(snap.Topics) != 2 {
		t.Fatalf("expected [phase-a synthetic], got %+v", snap.Topics)
	}

	// 快照隔离：外部改动不影响内部状态
	snap.Topics[0].Status = builtin_tools.AnalysisTopicBlocked
	if tracker.Snapshot().Topics[0].Status != builtin_tools.AnalysisTopicPending {
		t.Fatal("snapshot phases must be isolated")
	}
}

func TestStateTracker_ApplyTopicAssessments(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a1", Step: "a1", Status: builtin_tools.PlanStepCompleted, TopicID: "phase-a"},
		{ID: "b1", Step: "b1", Status: builtin_tools.PlanStepPending, TopicID: "phase-b"},
		{ID: "b2", Step: "b2", Status: builtin_tools.PlanStepPending, TopicID: "phase-b", DependsOn: []string{"b1"}},
	}, "", false)
	tracker.SetTopics([]*builtin_tools.AnalysisTopic{
		{ID: "phase-a", Status: builtin_tools.AnalysisTopicPending},
		{ID: "phase-b", Status: builtin_tools.AnalysisTopicPending},
	})

	changed, snap := tracker.ApplyTopicAssessments([]*builtin_tools.TopicAssessment{
		{TopicID: "phase-a", Status: builtin_tools.TopicAssessCompleted},
		{TopicID: "phase-b", Status: builtin_tools.TopicAssessBlocked},
		{TopicID: "phase-ghost", Status: builtin_tools.TopicAssessCompleted},
	})
	if len(changed) != 2 {
		t.Fatalf("expected 2 changed phases, got %d", len(changed))
	}
	byID := map[string]builtin_tools.AnalysisTopicStatus{}
	for _, phase := range snap.Topics {
		byID[phase.ID] = phase.Status
	}
	if byID["phase-a"] != builtin_tools.AnalysisTopicCompleted || byID["phase-b"] != builtin_tools.AnalysisTopicBlocked {
		t.Fatalf("unexpected phase statuses: %+v", byID)
	}
	// blocked 联动：phase-b 下 pending step 全部 skipped（含依赖传播）
	for _, item := range snap.Plan {
		if item.TopicID == "phase-b" && item.Status != builtin_tools.PlanStepSkipped {
			t.Fatalf("step %s of blocked phase should be skipped, got %s", item.ID, item.Status)
		}
	}
	// continue 与重复评估为 no-op
	changed, _ = tracker.ApplyTopicAssessments([]*builtin_tools.TopicAssessment{
		{TopicID: "phase-a", Status: builtin_tools.TopicAssessCompleted},
		{TopicID: "phase-b", Status: builtin_tools.TopicAssessContinue},
	})
	if len(changed) != 0 {
		t.Fatalf("expected idempotent no-op, got %d changed", len(changed))
	}
}
