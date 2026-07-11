package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// TestNextSchedulerPhase_PerTopicRouting（Inc3）验证 per-topic 双触发路由四分支（未接线，纯逻辑）：
// ① 静默可 review topic → StepReplan(local) + reviewTopicID 置位；② 有 ready → Step；
// ③ 无可 review 无 ready 未全终态 → StepReplan(全局 reducer, reviewTopicID="")；④ 全终态 → FinalAnswer。
func TestNextSchedulerPhase_PerTopicRouting(t *testing.T) {
	a := &Agent{}
	mk := func(phase builtin_tools.AgentPhase, plan []*builtin_tools.PlanItem, phases []*builtin_tools.AnalysisTopic) builtin_tools.StateSnapshot {
		return builtin_tools.StateSnapshot{Phase: phase, Plan: plan, Topics: phases}
	}

	// ① 静默 topic-a（a1 completed，边界空）→ 局部 review。
	snap := mk(builtin_tools.AgentPhaseStepReplan,
		[]*builtin_tools.PlanItem{{ID: "a1", TopicID: "topic-a", Status: builtin_tools.PlanStepCompleted}},
		[]*builtin_tools.AnalysisTopic{{ID: "topic-a", Status: builtin_tools.AnalysisTopicPending}})
	if got := a.nextSchedulerPhase(snap); got != builtin_tools.AgentPhaseStepReplan || a.reviewTopicID != "topic-a" {
		t.Fatalf("① 静默 topic 应 StepReplan(local, topic-a)，got phase=%v reviewTopicID=%q", got, a.reviewTopicID)
	}

	// ② 有 ready step → Step，reviewTopicID 清空。
	snap = mk(builtin_tools.AgentPhaseStep,
		[]*builtin_tools.PlanItem{{ID: "b1", TopicID: "topic-b", Status: builtin_tools.PlanStepPending}},
		[]*builtin_tools.AnalysisTopic{{ID: "topic-b", Status: builtin_tools.AnalysisTopicPending}})
	if got := a.nextSchedulerPhase(snap); got != builtin_tools.AgentPhaseStep || a.reviewTopicID != "" {
		t.Fatalf("② 有 ready 应 Step，got %v reviewTopicID=%q", got, a.reviewTopicID)
	}

	// ③ topic-a 已 review 过（边界=a1=latest）、无 pending、未全终态 → 全局 reducer。
	a.lastReplanBoundaryByTopic = map[string]string{"topic-a": "a1"}
	snap = mk(builtin_tools.AgentPhaseStepReplan,
		[]*builtin_tools.PlanItem{{ID: "a1", TopicID: "topic-a", Status: builtin_tools.PlanStepCompleted}},
		[]*builtin_tools.AnalysisTopic{{ID: "topic-a", Status: builtin_tools.AnalysisTopicPending}})
	if got := a.nextSchedulerPhase(snap); got != builtin_tools.AgentPhaseStepReplan || a.reviewTopicID != "" {
		t.Fatalf("③ 已 review + 未终态应全局 reducer，got %v reviewTopicID=%q", got, a.reviewTopicID)
	}

	// ④ 全终态 → FinalAnswer。
	snap = mk(builtin_tools.AgentPhaseStep,
		[]*builtin_tools.PlanItem{{ID: "a1", TopicID: "topic-a", Status: builtin_tools.PlanStepCompleted}},
		[]*builtin_tools.AnalysisTopic{{ID: "topic-a", Status: builtin_tools.AnalysisTopicCompleted}})
	if got := a.nextSchedulerPhase(snap); got != builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("④ 全终态应 FinalAnswer，got %v", got)
	}
}
