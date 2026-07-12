package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// TestNextSchedulerPhase_NoFinalAnswerWhileBackgroundSubAgentRunning 复现并守卫一个终态判定闸门缺口：
//
// 现象（session 59b04114）：主 agent 把整片漏洞维度派给 run_in_background 的后台 sub_agent 后，
// spawning step 完成 → 其所属 topic 被标 terminal → AllTopicsSettled=true → nextSchedulerPhase
// 返回 FinalAnswer，而后台 sub_agent 仍在跑、结果从未折回 topics。结果：run 假成功、topics 全 pending、
// 后台维度工作被丢弃。
//
// 根因：nextSchedulerPhase 的 AllTopicsSettled→FinalAnswer 分支只看 topics，不查
// asyncRegistry.HasRunningSubAgent()。本测试要求：全终态 topic + 后台 sub_agent 在跑时，
// 不得判 FinalAnswer——应留在调度循环（StepReplan）让后台结果被 drain / 整合后再收尾。
func TestNextSchedulerPhase_NoFinalAnswerWhileBackgroundSubAgentRunning(t *testing.T) {
	a := &Agent{asyncRegistry: NewAsyncAgentRegistry()}
	// 一个仍在跑的后台 sub_agent（Kind 默认空 = sub_agent，非 inline_step）。
	a.asyncRegistry.Register("bg-xxe", "对 6 个子系统做 XXE 测试", "/tmp/ws")
	if !a.asyncRegistry.HasRunningSubAgent() {
		t.Fatal("前置：应有一个在跑的后台 sub_agent")
	}

	// 全终态 topic（spawning step 已完成，topic 被标 completed），但后台 sub_agent 仍在跑。
	snap := builtin_tools.StateSnapshot{
		Phase: builtin_tools.AgentPhaseStep,
		Plan: []*builtin_tools.PlanItem{
			{ID: "a1", TopicID: "topic-a", Status: builtin_tools.PlanStepCompleted},
		},
		Topics: []*builtin_tools.AnalysisTopic{
			{ID: "topic-a", Status: builtin_tools.AnalysisTopicCompleted},
		},
	}

	got := a.nextSchedulerPhase(snap)
	if got == builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("闸门缺口复现：后台 sub_agent 仍在跑时判了 FinalAnswer（假成功）；期望留在 StepReplan 等待+整合，got %v", got)
	}
	if got != builtin_tools.AgentPhaseStepReplan {
		t.Fatalf("期望 StepReplan（挂起等待后台 sub_agent），got %v", got)
	}
}
