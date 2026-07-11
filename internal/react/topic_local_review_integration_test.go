package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// 本文件钉住第四阶段 per-topic 局部 review 的三缺陷修复（C1/M2/M3）。
// C1 用 state 层确定性复现「planner LLM 窗口内他 topic peer 完成」的 lost-update：
// 对照旧式「陈旧快照 merge」与新的「活盘持锁原子 merge」，证明缺陷真实且被修复。
// findItem / depsOf 定义在 runtime_scheduler_mergeplan_test.go（同 package）。

// TestMergeReplanIntoPlan_PreservesConcurrentPeerCompletion 钉住 C1：
// 局部 review 回流 Plan 期间他 topic（topic-b）的 peer 完成活盘写入，
// 经 MergeReplanIntoPlan（以活盘为 prev、持锁原子）后【不被 clobber】；
// 而旧式 mergeReplannedPlan(陈旧快照, ...) 会把 b1 退回 InProgress（丢失完成态）。
func TestMergeReplanIntoPlan_PreservesConcurrentPeerCompletion(t *testing.T) {
	tracker := NewStateTracker()
	tracker.SetTopics([]*builtin_tools.AnalysisTopic{
		{ID: "topic-a", Status: builtin_tools.AnalysisTopicPending},
		{ID: "topic-b", Status: builtin_tools.AnalysisTopicPending},
	})
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a1", TopicID: "topic-a", Status: builtin_tools.PlanStepCompleted, Step: "topic-a 侦察"},
		{ID: "a2", TopicID: "topic-a", Status: builtin_tools.PlanStepPending, Step: "topic-a 旧 pending"}, // 将被 replan 替换
		{ID: "b1", TopicID: "topic-b", Status: builtin_tools.PlanStepInProgress, Step: "topic-b 在跑"},
		{ID: "b2", TopicID: "topic-b", Status: builtin_tools.PlanStepPending, Step: "topic-b 未来"},
	}, "seed", true)

	// 模拟 runPlanPhase 在 planner LLM 调用【之前】捕获的陈旧快照（此刻 b1 仍 InProgress）。
	staleSnapshot := tracker.Snapshot()

	// LLM 窗口内：topic-b 的 peer 完成 b1（活盘写入，落在陈旧快照视野外）。
	tracker.UpdateInlineStep("b1", builtin_tools.CurrentStepUpdate{
		Status:  builtin_tools.PlanStepCompleted,
		Summary: "peer done",
	})

	// planner 局部 review 回流：只产 topic-a 的新深 step。
	next := []*builtin_tools.PlanItem{
		{ID: "a3", TopicID: "topic-a", Status: builtin_tools.PlanStepPending, Step: "topic-a 深化"},
	}

	// 新路径（C1 修复）：以活盘为 prev、持锁原子 merge + 归一 + 写回。
	merged, err := tracker.MergeReplanIntoPlan(next, "topic-a", "replan", true)
	if err != nil {
		t.Fatalf("MergeReplanIntoPlan 失败: %v", err)
	}

	if b1 := findItem(merged.Plan, "b1"); b1 == nil || b1.Status != builtin_tools.PlanStepCompleted {
		t.Fatalf("C1 违反：topic-b 的 b1 完成态被 clobber，got %+v（应保持 completed）", b1)
	}
	if findItem(merged.Plan, "b2") == nil {
		t.Fatalf("C1 违反：topic-b 的 pending b2 在局部回流后丢失")
	}
	if findItem(merged.Plan, "a3") == nil {
		t.Fatalf("topic-a 局部深化 step a3 未入盘")
	}
	if findItem(merged.Plan, "a2") != nil {
		t.Fatalf("topic-a 旧 pending a2 应被 a3 替换，却仍在盘")
	}

	// 对照：旧式「陈旧快照 merge」把 b1 保留为陈旧的 InProgress——它没看见 peer 完成，
	// 后续整盘覆盖即把完成态丢弃。这正是 C1 的 lost-update；证明缺陷真实、修复有效。
	stale := mergeReplannedPlan(staleSnapshot.Plan, next, "topic-a")
	if b := findItem(stale, "b1"); b == nil || b.Status != builtin_tools.PlanStepInProgress {
		t.Fatalf("对照前提失效：陈旧 merge 的 b1 应保留旧 InProgress（证明其未见 peer 完成），got %+v", b)
	}
}

// TestMergeReplannedPlan_CrossTopicTextCollisionKeepsNewStep 钉住 M3：
// per-topic 回流（replaceTopicID=topic-a）时，planner 为 topic-a 产的新 step 与他 topic
// pending 归一文案撞车（同「输出报告」），新 step【不被误去重丢弃】、其下游依赖【不被
// remap 到他 topic】。
func TestMergeReplannedPlan_CrossTopicTextCollisionKeepsNewStep(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "a1", TopicID: "topic-a", Status: builtin_tools.PlanStepCompleted, Step: "topic-a 侦察"},
		{ID: "a2", TopicID: "topic-a", Status: builtin_tools.PlanStepPending, Step: "topic-a 旧 pending"},
		{ID: "b1", TopicID: "topic-b", Status: builtin_tools.PlanStepPending, Step: "输出报告"}, // 他 topic pending，文案将撞车
	}
	next := []*builtin_tools.PlanItem{
		{ID: "a3", TopicID: "topic-a", Status: builtin_tools.PlanStepPending, Step: "输出报告"},                                     // 与 b1 归一文案撞车
		{ID: "a4", TopicID: "topic-a", Status: builtin_tools.PlanStepPending, Step: "复核", DependsOn: []string{"a3"}}, // 依赖 a3
	}

	merged := mergeReplannedPlan(prev, next, "topic-a")

	if findItem(merged, "a3") == nil {
		t.Fatalf("M3 违反：topic-a 新 step a3 被他 topic pending 文案误去重丢弃")
	}
	if deps := depsOf(merged, "a4"); len(deps) != 1 || deps[0] != "a3" {
		t.Fatalf("M3 违反：a4 依赖应指 topic-a 的 a3，got %v（remap 到他 topic id 即跨 topic 错连）", deps)
	}
	if findItem(merged, "b1") == nil {
		t.Fatalf("M3 违反：他 topic pending b1 被丢失（红线：不 clobber 他 topic）")
	}
}

// TestApplyTopicAssessmentsScoped_LocalBlockedStaysInTopic 钉住 M2：
// 局部 review（scope=topic-a）判 topic-a blocked，只 skip 属 topic-a 的 pending；
// 依赖 topic-a 的他 topic（topic-b）pending【不被跨 topic 传播 skip】。
func TestApplyTopicAssessmentsScoped_LocalBlockedStaysInTopic(t *testing.T) {
	tracker := newBlockedPropagationTracker(t)

	changed, snap := tracker.ApplyTopicAssessmentsScoped(
		[]*builtin_tools.TopicAssessment{{TopicID: "topic-a", Status: builtin_tools.TopicAssessBlocked}},
		"topic-a",
	)
	if len(changed) != 1 {
		t.Fatalf("topic-a 应发生 blocked 状态变化，got %d", len(changed))
	}
	if a1 := findItem(snap.Plan, "a1"); a1 == nil || a1.Status != builtin_tools.PlanStepSkipped {
		t.Fatalf("M2：topic-a 的 blocked-phase pending a1 应被 skip，got %+v", a1)
	}
	if b1 := findItem(snap.Plan, "b1"); b1 == nil || b1.Status != builtin_tools.PlanStepPending {
		t.Fatalf("M2 违反：局部 blocked 越权 skip 了他 topic 的 pending b1（应保持 pending，跨 topic 传播延到全局 reducer），got %+v", b1)
	}
}

// TestApplyTopicAssessments_GlobalBlockedPropagatesCrossTopic 是 M2 的对照：
// 全局 reducer（scope=""）判 blocked 时，跨 topic 下游 skip 仍完整传播——证明「延后」不丢失传播。
func TestApplyTopicAssessments_GlobalBlockedPropagatesCrossTopic(t *testing.T) {
	tracker := newBlockedPropagationTracker(t)

	changed, snap := tracker.ApplyTopicAssessmentsScoped(
		[]*builtin_tools.TopicAssessment{{TopicID: "topic-a", Status: builtin_tools.TopicAssessBlocked}},
		"",
	)
	if len(changed) != 1 {
		t.Fatalf("topic-a 应发生 blocked 状态变化，got %d", len(changed))
	}
	if a1 := findItem(snap.Plan, "a1"); a1 == nil || a1.Status != builtin_tools.PlanStepSkipped {
		t.Fatalf("全局：topic-a blocked-phase pending a1 应被 skip，got %+v", a1)
	}
	if b1 := findItem(snap.Plan, "b1"); b1 == nil || b1.Status != builtin_tools.PlanStepSkipped {
		t.Fatalf("全局 reducer 应跨 topic 传播 skip 到依赖 a1 的 b1（延后不丢失传播），got %+v", b1)
	}
}

// newBlockedPropagationTracker 构造 M2 用例的共享盘：topic-a 一个 pending a1；
// topic-b 一个 pending b1 跨 topic 依赖 a1。
func newBlockedPropagationTracker(t *testing.T) *StateTracker {
	t.Helper()
	tracker := NewStateTracker()
	tracker.SetTopics([]*builtin_tools.AnalysisTopic{
		{ID: "topic-a", Status: builtin_tools.AnalysisTopicPending},
		{ID: "topic-b", Status: builtin_tools.AnalysisTopicPending},
	})
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a1", TopicID: "topic-a", Status: builtin_tools.PlanStepPending, Step: "topic-a 步骤"},
		{ID: "b1", TopicID: "topic-b", Status: builtin_tools.PlanStepPending, Step: "topic-b 步骤", DependsOn: []string{"a1"}},
	}, "seed", true)
	return tracker
}
