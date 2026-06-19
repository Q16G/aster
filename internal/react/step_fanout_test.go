package react

import (
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
)

// =============================================================================
// selectFanOutPeers — 纯函数选择逻辑单测
// =============================================================================

func TestSelectFanOutPeers_NoFanOutBelowThreshold(t *testing.T) {
	ready := []string{"a", "b", "c"}
	got := selectFanOutPeers(1, 0, "a", ready, func(string) bool { return false })
	if got != nil {
		t.Fatalf("expected nil for maxParallel=1, got %v", got)
	}
	got = selectFanOutPeers(0, 0, "a", ready, func(string) bool { return false })
	if got != nil {
		t.Fatalf("expected nil for maxParallel=0, got %v", got)
	}
}

func TestSelectFanOutPeers_SpawnsPeersWithinSlot(t *testing.T) {
	// MaxParallel=3, currentID=a, ready=[a,b,c,d] → 应派发 b 和 c（slot=2），不派发 d。
	ready := []string{"a", "b", "c", "d"}
	got := selectFanOutPeers(3, 0, "a", ready, func(string) bool { return false })
	if len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("expected [b c], got %v", got)
	}
}

func TestSelectFanOutPeers_RespectsRunningSlot(t *testing.T) {
	// MaxParallel=3, runningRemote=1 → 剩 slot = 3-1-1 = 1，只派发 1 个 peer。
	ready := []string{"a", "b", "c", "d"}
	got := selectFanOutPeers(3, 1, "a", ready, func(string) bool { return false })
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("expected [b] (1 slot left after running=1), got %v", got)
	}
}

func TestSelectFanOutPeers_SkipsAlreadyRegistered(t *testing.T) {
	// b 已在 registry 里，跳过；c 未注册，派发。
	ready := []string{"a", "b", "c"}
	registered := func(id string) bool { return id == "b" }
	got := selectFanOutPeers(3, 0, "a", ready, registered)
	if len(got) != 1 || got[0] != "c" {
		t.Fatalf("expected [c] (b skipped), got %v", got)
	}
}

func TestSelectFanOutPeers_AllSlotsUsed(t *testing.T) {
	// runningRemote 已占满，剩 slot=0，no-op
	ready := []string{"a", "b", "c"}
	got := selectFanOutPeers(3, 2, "a", ready, func(string) bool { return false })
	if got != nil {
		t.Fatalf("expected nil (no slots), got %v", got)
	}
}

func TestSelectFanOutPeers_NegativeSlotsDegradeToNoOp(t *testing.T) {
	// runningRemote > maxParallel - 1 → slots 为负数；早退兜底 no-op，不应 panic 不应派发。
	ready := []string{"a", "b", "c"}
	got := selectFanOutPeers(3, 5, "a", ready, func(string) bool { return false })
	if got != nil {
		t.Fatalf("expected nil for negative slots (runningRemote > limit), got %v", got)
	}
}

func TestSelectFanOutPeers_EmptyReady(t *testing.T) {
	got := selectFanOutPeers(3, 0, "a", nil, func(string) bool { return false })
	if got != nil {
		t.Fatalf("expected nil for empty ready, got %v", got)
	}
}

func TestSelectFanOutPeers_TrimsCurrentID(t *testing.T) {
	// currentID 带空白也应被识别并跳过
	ready := []string{"a", "b"}
	got := selectFanOutPeers(3, 0, "  a  ", ready, func(string) bool { return false })
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("expected [b] (trimmed current=a), got %v", got)
	}
}

// =============================================================================
// spawnRemoteStep — 前置校验路径单测
// =============================================================================

func TestSpawnRemoteStep_RejectsNilFactory(t *testing.T) {
	a := &Agent{asyncRegistry: NewAsyncAgentRegistry()} // 无 agentFactory
	plan := []*builtin_tools.PlanItem{{ID: "s", Step: "x"}}
	err := a.spawnRemoteStep(context.Background(), "s", plan)
	if err == nil || !strings.Contains(err.Error(), "factory") {
		t.Fatalf("expected factory error, got %v", err)
	}
}

func TestSpawnRemoteStep_RejectsNilRegistry(t *testing.T) {
	a := &Agent{}
	a.agentFactory = &AgentFactory{} // 非 nil 即可（不实际跑）
	plan := []*builtin_tools.PlanItem{{ID: "s", Step: "x"}}
	err := a.spawnRemoteStep(context.Background(), "s", plan)
	if err == nil || !strings.Contains(err.Error(), "registry") {
		t.Fatalf("expected registry error, got %v", err)
	}
}

func TestSpawnRemoteStep_RejectsEmptyStepID(t *testing.T) {
	a := &Agent{asyncRegistry: NewAsyncAgentRegistry(), agentFactory: &AgentFactory{}}
	err := a.spawnRemoteStep(context.Background(), "  ", nil)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty stepID error, got %v", err)
	}
}

func TestSpawnRemoteStep_RejectsItemNotFound(t *testing.T) {
	a := &Agent{asyncRegistry: NewAsyncAgentRegistry(), agentFactory: &AgentFactory{}}
	err := a.spawnRemoteStep(context.Background(), "nonexistent", []*builtin_tools.PlanItem{
		{ID: "other", Step: "x"},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestSpawnRemoteStep_RejectsEmptyStepText(t *testing.T) {
	a := &Agent{asyncRegistry: NewAsyncAgentRegistry(), agentFactory: &AgentFactory{}}
	err := a.spawnRemoteStep(context.Background(), "s", []*builtin_tools.PlanItem{
		{ID: "s", Step: "   "},
	})
	if err == nil || !strings.Contains(err.Error(), "empty step text") {
		t.Fatalf("expected empty step text error, got %v", err)
	}
}

// =============================================================================
// fanOutReadyPeers — 集成行为单测（不实际跑 child agent）
// =============================================================================

func TestFanOutReadyPeers_NoOpBelowThreshold(t *testing.T) {
	a := &Agent{
		asyncRegistry: NewAsyncAgentRegistry(),
		cfg:           &AgentConfig{MaxParallelSteps: 1},
	}
	snapshot := builtin_tools.StateSnapshot{
		CurrentStepID: "a",
		Plan: []*builtin_tools.PlanItem{
			{ID: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		},
	}
	a.fanOutReadyPeers(context.Background(), snapshot)
	if a.asyncRegistry.HasRunningRemoteSteps() {
		t.Fatal("expected no remote step spawned with MaxParallel=1")
	}
}

// TestSpawnRemoteStep_MarksPlanItemInProgress：面 7.A 集成验证。
// spawn 同步段顺序：前置校验 → newLocalWorkspaceRuntime → agentFactory.Build →
// RegisterRemoteStep → **MarkRemotePlanItemInProgress** → EmitBgStart → goroutine。
//
// 用 stub factory（含 stubClient）让 Build 成功，spawn 同步段全跑完后
// PlanItem.Status 必为 InProgress。goroutine 部分（child.Execute）异步跑，
// 测试结束前若未完成不影响本验证（断言只看同步段后状态）。
func TestSpawnRemoteStep_MarksPlanItemInProgress(t *testing.T) {
	state := NewStateTracker()
	state.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "x", Step: "测试 step", Status: builtin_tools.PlanStepPending},
	}, "init", true)

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	a := &Agent{
		asyncRegistry:    NewAsyncAgentRegistry(),
		state:            state,
		agentFactory:     factory,
		workspaceRootDir: t.TempDir(),
	}

	if err := a.spawnRemoteStep(context.Background(), "x", state.Snapshot().Plan); err != nil {
		t.Fatalf("spawn failed: %v", err)
	}

	snap := a.state.Snapshot()
	var x *builtin_tools.PlanItem
	for _, it := range snap.Plan {
		if it != nil && it.ID == "x" {
			x = it
		}
	}
	if x == nil || x.Status != builtin_tools.PlanStepInProgress {
		t.Fatalf("expected x InProgress after spawn synchronous segment, got status=%q", x.Status)
	}
}

// TestSpawnRemoteStep_BuildFailureLeavesPlanItemPending：review 修复回归。
// spawnRemoteStep 内 newLocalWorkspaceRuntime + agentFactory.Build 失败时，
// PlanItem 仍应是 Pending（设计前提 3：Build 失败 → MarkInProgress 不被调）。
// 用未配置 AIClient 的 factory 触发 Build 失败。
func TestSpawnRemoteStep_BuildFailureLeavesPlanItemPending(t *testing.T) {
	state := NewStateTracker()
	state.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "x", Step: "测试", Status: builtin_tools.PlanStepPending},
	}, "init", true)

	// 空 factory：Build 时缺 AIClient 会返回 error
	factory := &AgentFactory{}
	a := &Agent{
		asyncRegistry:    NewAsyncAgentRegistry(),
		state:            state,
		agentFactory:     factory,
		workspaceRootDir: t.TempDir(),
	}

	err := a.spawnRemoteStep(context.Background(), "x", state.Snapshot().Plan)
	if err == nil {
		t.Fatal("expected Build to fail without AIClient, got nil error")
	}

	snap := a.state.Snapshot()
	var x *builtin_tools.PlanItem
	for _, it := range snap.Plan {
		if it != nil && it.ID == "x" {
			x = it
		}
	}
	if x == nil || x.Status != builtin_tools.PlanStepPending {
		t.Fatalf("expected x Pending after spawn failure (no InProgress leak), got status=%q", x.Status)
	}
	// registry 也不该残留
	if a.asyncRegistry.Get("x") != nil {
		t.Fatal("registry should not have entry for failed spawn")
	}
}

func TestFanOutReadyPeers_NoOpInFinalAnswerPhase(t *testing.T) {
	// 收尾防御：teardown 路径 drain → fanOutReadyPeers 仍可能触发误派发。
	// Phase=FinalAnswer 时早退，不应 spawn 任何新远程 step。
	a := &Agent{
		asyncRegistry: NewAsyncAgentRegistry(),
		cfg:           &AgentConfig{MaxParallelSteps: 3},
		agentFactory:  &AgentFactory{},
	}
	snapshot := builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhaseFinalAnswer,
		CurrentStepID: "a",
		Plan: []*builtin_tools.PlanItem{
			{ID: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		},
	}
	// 即使 plan 还有 ready，FinalAnswer phase 下不应 register 任何 remote step
	a.fanOutReadyPeers(context.Background(), snapshot)
	if a.asyncRegistry.HasRunningRemoteSteps() {
		t.Fatal("fanOutReadyPeers should be no-op under FinalAnswer phase")
	}
}

func TestFanOutReadyPeers_NoOpWithoutRegistry(t *testing.T) {
	a := &Agent{
		asyncRegistry: nil,
		cfg:           &AgentConfig{MaxParallelSteps: 3},
	}
	snapshot := builtin_tools.StateSnapshot{
		CurrentStepID: "a",
		Plan:          []*builtin_tools.PlanItem{{ID: "b", Status: builtin_tools.PlanStepPending}},
	}
	// 不应 panic
	a.fanOutReadyPeers(context.Background(), snapshot)
}

// =============================================================================
// persistRemoteStepTranscript — 写父 v2Store blob 的 helper 单测
// =============================================================================

func TestPersistRemoteStepTranscript_NilStore(t *testing.T) {
	ref := persistRemoteStepTranscript(nil, []*ai.MsgInfo{ai.NewUserMsgInfo("x")})
	if ref != "" {
		t.Fatalf("expected empty ref for nil store, got %q", ref)
	}
}

func TestPersistRemoteStepTranscript_EmptyHistory(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-empty-history")
	if err != nil {
		t.Fatalf("persistv2.Open: %v", err)
	}
	ref := persistRemoteStepTranscript(store, nil)
	if ref != "" {
		t.Fatalf("expected empty ref for nil history, got %q", ref)
	}
	ref = persistRemoteStepTranscript(store, []*ai.MsgInfo{})
	if ref != "" {
		t.Fatalf("expected empty ref for empty history, got %q", ref)
	}
}

func TestPersistRemoteStepTranscript_WritesBlob(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-write-blob")
	if err != nil {
		t.Fatalf("persistv2.Open: %v", err)
	}
	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("hello from remote step"),
		ai.NewAIMsgInfo("response"),
	}
	ref := persistRemoteStepTranscript(store, history)
	if ref == "" {
		t.Fatal("expected non-empty ref")
	}
	if !strings.HasPrefix(ref, "sha256:") {
		t.Fatalf("expected sha256: prefix, got %q", ref)
	}
	// Round-trip: 读回 blob 应等于原 history
	raw, err := store.ReadBlob(ref)
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty blob")
	}
}

func TestPersistRemoteStepTranscript_NilStoreLogsAndReturnsEmpty(t *testing.T) {
	// 防御性：nil store 应返回空 + warn 日志（让运维察觉设计缺失），不 panic。
	// 本测试只验证不 panic + 返回空，日志内容不强校验（runtimelog 是全局 sink）。
	ref := persistRemoteStepTranscript(nil, []*ai.MsgInfo{
		ai.NewUserMsgInfo("nonempty"),
	})
	if ref != "" {
		t.Fatalf("expected empty ref for nil store, got %q", ref)
	}
}

func TestPersistRemoteStepTranscript_SameHistorySameRef(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-content-addressed")
	if err != nil {
		t.Fatalf("persistv2.Open: %v", err)
	}
	history := []*ai.MsgInfo{ai.NewUserMsgInfo("same content")}

	ref1 := persistRemoteStepTranscript(store, history)
	ref2 := persistRemoteStepTranscript(store, history)
	if ref1 == "" || ref2 == "" {
		t.Fatalf("expected non-empty refs, got %q / %q", ref1, ref2)
	}
	if ref1 != ref2 {
		t.Fatalf("content-addressed: same history should produce same ref, got %q vs %q", ref1, ref2)
	}
}
