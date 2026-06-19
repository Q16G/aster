package react

import (
	"context"
	"strings"
	"testing"
	"time"

	"aster/internal/builtin_tools"
)

// drainAgent 构造最小 Agent 实例供 drain 单测使用：注册表 + state tracker（含 fan-out plan）。
func drainAgent(t *testing.T) *Agent {
	t.Helper()
	r := NewAsyncAgentRegistry()
	state := NewStateTracker()
	state.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "前置", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Step: "主路径", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "c", Step: "远程", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
	}, "init", true)
	state.EnsureCurrentStep()
	return &Agent{asyncRegistry: r, state: state}
}

func TestDrain_RemoteStepCallsUpdateRemotePlanItem(t *testing.T) {
	a := drainAgent(t)

	// 注册远程 step c 并完成（success）
	a.asyncRegistry.RegisterInlineStep("c", "")
	a.asyncRegistry.Complete("c", &builtin_tools.RunResult{
		Success: true,
		Result:  "远程完成摘要",
	})

	// 等待 notification 到位
	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout: notification not enqueued")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	beforeStepHistoryLen := len(a.stepHistory)
	a.drainAsyncAgentNotifications(context.Background())

	// 1. PlanItem c 应该被回写为 Completed
	snap := a.state.Snapshot()
	var c *builtin_tools.PlanItem
	for _, it := range snap.Plan {
		if it != nil && it.ID == "c" {
			c = it
		}
	}
	if c == nil || c.Status != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected c Completed via UpdateRemotePlanItem, got status=%q", c.Status)
	}

	// 2. Phase 不应该被翻 StepReplan（远程 step 不动 Phase）
	if snap.Phase != builtin_tools.AgentPhaseStep {
		t.Fatalf("expected Phase unchanged (Step), got %q", snap.Phase)
	}

	// 3. stepHistory 不应该被注入（远程 step 不污染主 transcript）
	if len(a.stepHistory) != beforeStepHistoryLen {
		t.Fatalf("expected stepHistory unchanged (got %d -> %d)", beforeStepHistoryLen, len(a.stepHistory))
	}

	// 4. StepOutcome 应该包含 Summary
	var found bool
	for _, oc := range snap.StepOutcomes {
		if oc != nil && oc.StepID == "c" {
			found = true
			if oc.Summary != "远程完成摘要" {
				t.Fatalf("summary not propagated, got %q", oc.Summary)
			}
		}
	}
	if !found {
		t.Fatal("expected StepOutcome for c")
	}
}

func TestDrain_RemoteStepFailedMapsToPlanStepFailed(t *testing.T) {
	a := drainAgent(t)

	a.asyncRegistry.RegisterInlineStep("c", "")
	a.asyncRegistry.Complete("c", &builtin_tools.RunResult{
		Success: false,
		Error:   "remote crash",
	})

	// 等待 notification
	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	a.drainAsyncAgentNotifications(context.Background())
	snap := a.state.Snapshot()
	var c *builtin_tools.PlanItem
	for _, it := range snap.Plan {
		if it != nil && it.ID == "c" {
			c = it
		}
	}
	if c.Status != builtin_tools.PlanStepFailed {
		t.Fatalf("expected c Failed, got %q", c.Status)
	}
	// Error 也应该传播到 outcome
	var found bool
	for _, oc := range snap.StepOutcomes {
		if oc != nil && oc.StepID == "c" {
			found = true
			if !strings.Contains(oc.Error, "remote crash") {
				t.Fatalf("expected error to contain 'remote crash', got %q", oc.Error)
			}
		}
	}
	if !found {
		t.Fatal("expected StepOutcome for failed c")
	}
}

func TestDrain_SubAgentStillInjectsStepHistory(t *testing.T) {
	// 回归测试：sub_agent 路径行为不变——注入 stepHistory，不调 UpdateRemotePlanItem
	a := drainAgent(t)
	a.asyncRegistry.Register("sa-1", "do something", "")
	a.asyncRegistry.Complete("sa-1", &builtin_tools.RunResult{
		Success: true,
		Result:  "sub agent done",
	})

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	beforePlan := a.state.Snapshot().Plan
	a.drainAsyncAgentNotifications(context.Background())

	// 1. stepHistory 应被注入 1 条
	if len(a.stepHistory) != 1 {
		t.Fatalf("expected 1 stepHistory message, got %d", len(a.stepHistory))
	}

	// 2. plan 不变（sub_agent 不通过 UpdateRemotePlanItem 路径）
	afterPlan := a.state.Snapshot().Plan
	for i, it := range afterPlan {
		if it.Status != beforePlan[i].Status {
			t.Fatalf("sub_agent drain unexpectedly changed plan item %s status", it.ID)
		}
	}
}

func TestDrain_RemoteStepNilStateNoOp(t *testing.T) {
	// handleRemoteStepNotification 防御：a.state == nil 时不 panic，仅 MarkDelivered。
	r := NewAsyncAgentRegistry()
	a := &Agent{asyncRegistry: r, state: nil}

	a.asyncRegistry.RegisterInlineStep("c", "")
	a.asyncRegistry.Complete("c", &builtin_tools.RunResult{Success: true, Result: "x"})

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// 不应 panic
	a.drainAsyncAgentNotifications(context.Background())

	// MarkDelivered 应被调用；drain 末尾的 PurgeDelivered 会清掉该条目。
	if entry := r.Get("c"); entry != nil {
		t.Fatalf("entry should be purged after delivery (nil state branch still marks), got %+v", entry)
	}
}

func TestDrain_RemoteStepNilResultMapsToFailed(t *testing.T) {
	// 极端兜底：spawnRemoteStep goroutine panic 后 defer Complete(nil)，
	// drain 应安全把 PlanItem 标 Failed，不 panic 不 lockup。
	a := drainAgent(t)

	a.asyncRegistry.RegisterInlineStep("c", "")
	a.asyncRegistry.Complete("c", nil) // Result==nil

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	a.drainAsyncAgentNotifications(context.Background())

	snap := a.state.Snapshot()
	var c *builtin_tools.PlanItem
	for _, it := range snap.Plan {
		if it != nil && it.ID == "c" {
			c = it
		}
	}
	if c == nil || c.Status != builtin_tools.PlanStepFailed {
		t.Fatalf("expected c Failed (nil-result fallback), got status=%q", c.Status)
	}
}

func TestDrain_RemoteStepStaleNotificationSkipped(t *testing.T) {
	// drain 看到 stale notification（agentID 在 registry 中已不存在）时早退，
	// 不调 UpdateRemotePlanItem 回写。直接推一条孤立 notification 复现。
	a := drainAgent(t)

	beforeC := planItemByIDInSnap(a.state.Snapshot(), "c")

	// 孤立 notification：agentID 在 plan 里存在但 registry 里没有对应 entry。
	// drain 看到 Get() == nil 应立即 continue，state 不被回写。
	a.asyncRegistry.notifications <- &AsyncAgentNotification{
		AgentID: "c",
		Kind:    AsyncAgentKindInlineStep,
		Status:  "completed",
		Result:  &builtin_tools.RunResult{Success: true, Result: "should be ignored"},
	}

	a.drainAsyncAgentNotifications(context.Background())

	afterC := planItemByIDInSnap(a.state.Snapshot(), "c")
	if afterC.Status != beforeC.Status {
		t.Fatalf("c status should be unchanged for stale notification, got %q -> %q",
			beforeC.Status, afterC.Status)
	}
}

// TestDrain_RemoteStepCompletionUnlocksDependents：完成 b 后 d 应被解锁进入 ready。
// drain 内部紧随 UpdateRemotePlanItem 调 fanOutReadyPeers——本测试通过 state 推进证明
// 解锁链路正确（实际 spawn 需 agentFactory，由 selectFanOutPeers 单测覆盖）。
func TestDrain_RemoteStepCompletionUnlocksDependents(t *testing.T) {
	// plan: a Completed → b/c Pending → d depends b
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "a", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Step: "b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "c", Step: "c", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "d", Step: "d 依赖 b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"b"}},
	}, "init", true)
	tracker.EnsureCurrentStep()
	a := &Agent{asyncRegistry: NewAsyncAgentRegistry(), state: tracker}

	// drain 前：d 不在 ready 里（依赖 b 未完成）
	beforeReady := builtin_tools.ReadyRunnablePlanStepIDs(a.state.Snapshot().Plan)
	for _, id := range beforeReady {
		if id == "d" {
			t.Fatal("setup violation: d should not be ready before b completes")
		}
	}

	// b 注册为 remote_step + 完成
	a.asyncRegistry.RegisterInlineStep("b", "")
	a.asyncRegistry.Complete("b", &builtin_tools.RunResult{Success: true, Result: "b done"})

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	a.drainAsyncAgentNotifications(context.Background())

	// drain 后：b=Completed + d 进入 ready（fanOutReadyPeers 链路看到的状态）
	afterReady := builtin_tools.ReadyRunnablePlanStepIDs(a.state.Snapshot().Plan)
	dReady := false
	for _, id := range afterReady {
		if id == "d" {
			dReady = true
		}
	}
	if !dReady {
		t.Fatalf("expected d to be ready after b completes, ready=%v", afterReady)
	}
}

// TestDrain_RemoteStepCompletionNoUnlockWhenLeaf：完成的 step 没有 dependents 时，
// ready 列表不该有新增。fanOutReadyPeers 看到空 peer 集合应直接 no-op。
func TestDrain_RemoteStepCompletionNoUnlockWhenLeaf(t *testing.T) {
	// plan: a Completed → b Pending → c Pending depends b（c 是 leaf）
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "a", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Step: "b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "c", Step: "c (leaf)", Status: builtin_tools.PlanStepPending, DependsOn: []string{"b"}},
	}, "init", true)
	tracker.EnsureCurrentStep()
	a := &Agent{asyncRegistry: NewAsyncAgentRegistry(), state: tracker}

	// c 注册并完成
	a.asyncRegistry.RegisterInlineStep("c", "")
	a.asyncRegistry.Complete("c", &builtin_tools.RunResult{Success: true})

	// 由于 c 依赖未满足（b 还 pending），UpdateRemotePlanItem 仍会回写 c=Completed，
	// 但 c 完成不解锁任何新 step（c 没 dependents）。

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	beforeReady := builtin_tools.ReadyRunnablePlanStepIDs(a.state.Snapshot().Plan)
	a.drainAsyncAgentNotifications(context.Background())
	afterReady := builtin_tools.ReadyRunnablePlanStepIDs(a.state.Snapshot().Plan)

	// ready 集合不应有新增（c 完成不解锁任何东西）
	if len(afterReady) != len(beforeReady) {
		t.Fatalf("expected ready unchanged for leaf completion: before=%v after=%v",
			beforeReady, afterReady)
	}
}

// TestDrain_SubAgentCompletionDoesNotTouchPlanState：sub_agent 完成走原路径，
// 不调 UpdateRemotePlanItem，不触发 fanOutReadyPeers，plan 状态不变。
func TestDrain_SubAgentCompletionDoesNotTouchPlanState(t *testing.T) {
	a := drainAgent(t)
	beforeReady := builtin_tools.ReadyRunnablePlanStepIDs(a.state.Snapshot().Plan)

	a.asyncRegistry.Register("sa-1", "do something", "")
	a.asyncRegistry.Complete("sa-1", &builtin_tools.RunResult{Success: true})

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	a.drainAsyncAgentNotifications(context.Background())

	afterReady := builtin_tools.ReadyRunnablePlanStepIDs(a.state.Snapshot().Plan)
	if len(afterReady) != len(beforeReady) {
		t.Fatalf("sub_agent drain should not change plan ready set: before=%v after=%v",
			beforeReady, afterReady)
	}
}

// TestDrain_RemoteStepFanOutEarlyExitsOnCanceledCtx：ctx 取消后 drain 仍处理通知，
// 但 fanOutReadyPeers 内的 spawnRemoteStep 起手 ctx.Err() 早退，不起空 goroutine。
//
// 间接验证：drain 后 PlanItem 被正确回写（UpdateRemotePlanItem 不读 ctx），
// 但被解锁的 step 不应被注册（spawn 早退）。
func TestDrain_RemoteStepFanOutEarlyExitsOnCanceledCtx(t *testing.T) {
	// plan: a Completed → b/c Pending → d depends b
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "a", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Step: "b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "c", Step: "c", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "d", Step: "d 依赖 b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"b"}},
	}, "init", true)
	tracker.EnsureCurrentStep()

	// 注意：本测试构造一个会触发 fanOutReadyPeers 进入 spawn 路径的 agent
	// （cfg.MaxParallelSteps>=2 + 非 nil agentFactory）。spawnRemoteStep 起手
	// ctx.Err() 检查会早退，d 不应被 RegisterInlineStep。
	a := &Agent{
		asyncRegistry: NewAsyncAgentRegistry(),
		state:         tracker,
		cfg:           &AgentConfig{MaxParallelSteps: 3},
		agentFactory:  &AgentFactory{},
	}

	// 注册 b 完成 + 准备取消的 ctx
	a.asyncRegistry.RegisterInlineStep("b", "")
	a.asyncRegistry.Complete("b", &builtin_tools.RunResult{Success: true})

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	a.drainAsyncAgentNotifications(ctx)

	// 1. b 应被回写（UpdateRemotePlanItem 不依赖 ctx）
	snap := a.state.Snapshot()
	var b *builtin_tools.PlanItem
	for _, it := range snap.Plan {
		if it != nil && it.ID == "b" {
			b = it
		}
	}
	if b == nil || b.Status != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected b Completed (state path is ctx-independent), got %q", b.Status)
	}

	// 2. d 不应被 RegisterInlineStep（spawn 起手 ctx.Err() 早退）
	if a.asyncRegistry.Get("d") != nil {
		t.Fatal("d should not be registered under canceled ctx (spawn early-exit)")
	}
}

// TestDrain_RemoteStepTranscriptBlobRefPropagated：notif.Result.TranscriptBlobRef
// 应该通过 drain 路径写到 StepOutcome.TranscriptBlobRef。
func TestDrain_RemoteStepTranscriptBlobRefPropagated(t *testing.T) {
	a := drainAgent(t)
	a.asyncRegistry.RegisterInlineStep("c", "")
	a.asyncRegistry.Complete("c", &builtin_tools.RunResult{
		Success:           true,
		Result:            "done",
		TranscriptBlobRef: "sha256:transcript-ref-xyz",
	})

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	a.drainAsyncAgentNotifications(context.Background())

	snap := a.state.Snapshot()
	for _, oc := range snap.StepOutcomes {
		if oc != nil && oc.StepID == "c" {
			if oc.TranscriptBlobRef != "sha256:transcript-ref-xyz" {
				t.Fatalf("expected ref propagated to outcome, got %q", oc.TranscriptBlobRef)
			}
			return
		}
	}
	t.Fatal("expected StepOutcome for c with TranscriptBlobRef")
}

func TestDrain_MixedKindsDispatchedCorrectly(t *testing.T) {
	// 同时混入 sub_agent 通知和 remote_step 通知，分别走对应路径。
	a := drainAgent(t)

	a.asyncRegistry.Register("sa-1", "do something", "")
	a.asyncRegistry.RegisterInlineStep("c", "")

	a.asyncRegistry.Complete("sa-1", &builtin_tools.RunResult{Success: true, Result: "sub done"})
	a.asyncRegistry.Complete("c", &builtin_tools.RunResult{Success: true, Result: "remote done"})

	// 等到两个 notification 都进 channel
	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) < 2 {
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for 2 notifications, got %d", len(a.asyncRegistry.notifications))
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	a.drainAsyncAgentNotifications(context.Background())

	// sub_agent 路径：stepHistory 应被注入 1 条
	if len(a.stepHistory) != 1 {
		t.Fatalf("expected 1 stepHistory message from sub_agent, got %d", len(a.stepHistory))
	}

	// remote_step 路径：c 应该 Completed
	snap := a.state.Snapshot()
	var c *builtin_tools.PlanItem
	for _, it := range snap.Plan {
		if it != nil && it.ID == "c" {
			c = it
		}
	}
	if c.Status != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected remote_step c Completed, got %q", c.Status)
	}
}
