package react

import (
	"context"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// 这批测试补 L3「委派归队屏障」的端到端覆盖（review 缺口 G1/G2/G4/G6）：
// 只有 T1（next_scheduler_phase_subagent_gate_internal_test.go）测了 nextSchedulerPhase 的纯决策，
// 而屏障那段循环逻辑（block-await → drain → 重算 phase）此前零覆盖。

// newBarrierTestAgent 构造一个「全 topic settled」的 root agent，可直接跑 runSchedulerLoop。
func newBarrierTestAgent(t *testing.T, client ai.ChatClient) *Agent {
	t.Helper()
	agent, err := NewReActAgent("test-barrier", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	runtime, err := newLocalWorkspaceRuntime("barrier-sess", t.TempDir(), "root")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	agent.workspaceRuntime = runtime
	agent.workspaceSessionID = "barrier-sess"
	agent.ensureAsyncRegistry()
	// 全 topic settled（completed）、plan 全 completed —— AllTopicsSettled=true。
	agent.state.Replace(builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhaseStep,
		Status:        builtin_tools.TaskStatusRunning,
		NeedsPlanning: true,
		CurrentGoal:   "审计目标系统",
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", TopicID: "topic-a", Step: "step one", Status: builtin_tools.PlanStepCompleted},
		},
		Topics: []*builtin_tools.AnalysisTopic{
			{ID: "topic-a", Status: builtin_tools.AnalysisTopicCompleted},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "done one"},
		},
	})
	return agent
}

func finalAnswerReply() intentTestReply {
	return intentTestReply{content: `{"is_complete":true,"status":"completed","reason":"done",` +
		`"should_replan":false,"next_goal":"","incomplete_items":[],"depth_gaps":[],"new_surfaces":[],` +
		`"warnings":[],"user_message":"最终答复","references":[]}`}
}

// G1+G2：全 topic settled + 后台 sub_agent 在跑 → 调度器走 block-await 屏障等它归队 + drain，
// 再进 FinalAnswer 真终态；**期间不反复空跑 step_replan LLM**（只在 final_answer 调一次模型）。
func TestSchedulerLoop_AwaitsDelegatedSubAgentBeforeTerminal(t *testing.T) {
	client := &intentTestClient{replies: []intentTestReply{finalAnswerReply()}}
	agent := newBarrierTestAgent(t, client)

	agent.asyncRegistry.Register("bg-1", "对 6 个子系统做 XXE 测试", t.TempDir())

	// 后台子 agent 短延时后完成（模拟真实后台跑完），让 awaitAllBackgroundSubAgents 解除阻塞。
	go func() {
		time.Sleep(80 * time.Millisecond)
		agent.asyncRegistry.Complete("bg-1", &builtin_tools.RunResult{Success: true, Result: "子 agent 战果：发现 XXE"})
	}()

	ctx := context.Background()
	result, err := agent.runSchedulerLoop(ctx, client, "", nil, 0)
	if err != nil {
		t.Fatalf("runSchedulerLoop: %v", err)
	}

	// G2：不空转——屏障用 block-await 替代 step_replan 空转，全程只有 final_answer 一次模型调用。
	if client.calls != 1 {
		t.Fatalf("G2 不空转失败：期望仅 final_answer 一次模型调用，实际 %d 次（>1 说明屏障没接住、step_replan 空转了）", client.calls)
	}

	// 真终态。
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	snap := agent.state.Snapshot()
	if snap.Phase != builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("expected FinalAnswer terminal, got phase=%v", snap.Phase)
	}

	// G1：屏障 block-await 到子 agent 归队后才收尾——收尾时后台子 agent 必已归队（不再 running）。
	// 对照 L3 之前：主队列一空就 FinalAnswer，此时 HasRunningSubAgent 仍为 true（假成功）。
	if agent.asyncRegistry.HasRunningSubAgent() {
		t.Fatal("G1 归队失败：收尾时后台子 agent 仍在跑（HasRunningSubAgent=true）——屏障没 block-await 就收尾了")
	}
}

// G4：屏障 block-await 中途 ctx 被取消（子 agent 永不完成）→ 不死锁，最终收尾（canceled），
// 且 cancelRunningSubAgents 被触发（不泄漏）。守卫「屏障不自己收尾、靠下一轮循环顶 ctx 检查兜底」的隐式契约。
func TestSchedulerLoop_BarrierCtxCancelDoesNotDeadlock(t *testing.T) {
	client := &intentTestClient{replies: []intentTestReply{finalAnswerReply()}}
	agent := newBarrierTestAgent(t, client)

	// 后台子 agent 永不完成。
	agent.asyncRegistry.Register("bg-stuck", "永不完成的委派", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	// 进屏障 block-await 后取消 ctx，让 awaitAllBackgroundSubAgents 经 ctx.Done() 退出。
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()

	done := make(chan struct{})
	var result *builtin_tools.RunResult
	var loopErr error
	go func() {
		result, loopErr = agent.runSchedulerLoop(ctx, client, "", nil, 0)
		close(done)
	}()

	select {
	case <-done:
		// 未死锁——通过。
	case <-time.After(5 * time.Second):
		t.Fatal("G4 死锁：屏障 block-await 中途 ctx 取消后 runSchedulerLoop 未返回（5s 超时）")
	}
	_ = result
	_ = loopErr
	// 取消收尾走终态（canceled 分支返回 nil error + finalize）。
	snap := agent.state.Snapshot()
	if snap.Phase != builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("期望取消后进 FinalAnswer 收尾，got phase=%v", snap.Phase)
	}
}

// G7：多个后台子 agent，其一硬失败 → 屏障 await 后所有子 agent 都归队、主任务正常收尾（真终态），
// 失败子不连坐兄弟、不把主任务判 canceled。回归锁：防将来改用 errgroup/共享 cancel 倒退成"一个失败全体取消"。
func TestSchedulerLoop_OneSubAgentFailureDoesNotConnectCancelSiblings(t *testing.T) {
	client := &intentTestClient{replies: []intentTestReply{finalAnswerReply()}}
	agent := newBarrierTestAgent(t, client)

	agent.asyncRegistry.Register("bg-ok-1", "维度一", t.TempDir())
	agent.asyncRegistry.Register("bg-fail", "维度二", t.TempDir())
	agent.asyncRegistry.Register("bg-ok-2", "维度三", t.TempDir())

	go func() {
		time.Sleep(40 * time.Millisecond)
		agent.asyncRegistry.Complete("bg-fail", &builtin_tools.RunResult{Success: false, Error: "该维度探测失败"})
		time.Sleep(40 * time.Millisecond)
		agent.asyncRegistry.Complete("bg-ok-1", &builtin_tools.RunResult{Success: true, Result: "维度一战果"})
		agent.asyncRegistry.Complete("bg-ok-2", &builtin_tools.RunResult{Success: true, Result: "维度三战果"})
	}()

	result, err := agent.runSchedulerLoop(context.Background(), client, "", nil, 0)
	if err != nil {
		t.Fatalf("runSchedulerLoop: %v（一个子失败不应让整个 run 出错）", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// 主任务未被连坐取消——不空转、进真终态。
	if client.calls != 1 {
		t.Fatalf("期望仅 final_answer 一次模型调用，实际 %d", client.calls)
	}
	snap := agent.state.Snapshot()
	if snap.Phase != builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("期望 FinalAnswer 真终态（非 canceled），got phase=%v", snap.Phase)
	}
	if snap.Status == builtin_tools.TaskStatusCanceled {
		t.Fatal("一个子失败把主任务连坐判 canceled——连坐倒退")
	}
	// 三个子 agent 全归队。
	if agent.asyncRegistry.HasRunningSubAgent() {
		t.Fatal("期望三个后台子 agent 全归队")
	}
}

// G6：子 agent 自身（asyncRegistry==nil）时，nextSchedulerPhase 全 settled 直接判 FinalAnswer，
// 不进屏障、不 panic、不误判 StepReplan。
func TestNextSchedulerPhase_NilRegistryFinalizes(t *testing.T) {
	a := &Agent{} // asyncRegistry == nil
	snap := builtin_tools.StateSnapshot{
		Phase: builtin_tools.AgentPhaseStep,
		Plan: []*builtin_tools.PlanItem{
			{ID: "a1", TopicID: "topic-a", Status: builtin_tools.PlanStepCompleted},
		},
		Topics: []*builtin_tools.AnalysisTopic{
			{ID: "topic-a", Status: builtin_tools.AnalysisTopicCompleted},
		},
	}
	if got := a.nextSchedulerPhase(snap); got != builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("nil registry 全终态应 FinalAnswer，got %v", got)
	}
}
