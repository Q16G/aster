package builtin_tools

import "testing"

// TestTopicQuiesced 验证 topic 局部静默点判定：全 terminal + ≥1 step → 静默；
// 有 pending/in_progress → 未静默；0-step → 未静默（0-step 守卫）。
func TestTopicQuiesced(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a1", TopicID: "topic-a", Status: PlanStepCompleted},
		{ID: "a2", TopicID: "topic-a", Status: PlanStepFailed},
		{ID: "b1", TopicID: "topic-b", Status: PlanStepCompleted},
		{ID: "b2", TopicID: "topic-b", Status: PlanStepInProgress},
		{ID: "c1", TopicID: "topic-c", Status: PlanStepPending},
	}

	if !TopicQuiesced(plan, "topic-a") {
		t.Error("topic-a 全 terminal，应静默")
	}
	if TopicQuiesced(plan, "topic-b") {
		t.Error("topic-b 有 in_progress，不应静默")
	}
	if TopicQuiesced(plan, "topic-c") {
		t.Error("topic-c 有 pending，不应静默")
	}
	if TopicQuiesced(plan, "topic-none") {
		t.Error("0-step topic 不应静默（0-step 守卫）")
	}
	if TopicQuiesced(plan, "") {
		t.Error("空 topicID 不应静默")
	}
}

// TestQuiescedActiveTopics 验证只返回「active（非终态+已解锁）且已静默」的 topic。
func TestQuiescedActiveTopics(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a1", TopicID: "topic-a", Status: PlanStepCompleted}, // a 静默
		{ID: "b1", TopicID: "topic-b", Status: PlanStepInProgress}, // b 未静默
		{ID: "c1", TopicID: "topic-c", Status: PlanStepCompleted}, // c 静默但 c 已 completed（终态）
	}
	phases := []*AnalysisTopic{
		{ID: "topic-a", Status: AnalysisTopicPending},
		{ID: "topic-b", Status: AnalysisTopicPending},
		{ID: "topic-c", Status: AnalysisTopicCompleted}, // 终态 → 不 active
		{ID: "topic-d", Status: AnalysisTopicPending, DependsOn: []string{"topic-b"}}, // 前置未终态 → 未解锁
	}

	got := QuiescedActiveTopics(plan, phases)
	if len(got) != 1 || got[0].ID != "topic-a" {
		ids := make([]string, len(got))
		for i, p := range got {
			ids[i] = p.ID
		}
		t.Fatalf("应只返回已静默的 active topic-a，got %v", ids)
	}
}
