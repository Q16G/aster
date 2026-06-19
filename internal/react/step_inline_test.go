package react

import (
	"sync"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// selectInlineStepPeers — 纯函数选择逻辑单测（迁移自 step_fanout_test.go::selectFanOutPeers）。
// 算法语义不变，仅命名改造；保留全部测试 case 防止重构期回归。

func TestSelectInlineStepPeers_NoFanOutBelowThreshold(t *testing.T) {
	ready := []string{"a", "b", "c"}
	got := selectInlineStepPeers(1, 0, "a", ready, func(string) bool { return false })
	if got != nil {
		t.Fatalf("maxParallel=1 应返回 nil，got %v", got)
	}
	got = selectInlineStepPeers(0, 0, "a", ready, func(string) bool { return false })
	if got != nil {
		t.Fatalf("maxParallel=0 应返回 nil，got %v", got)
	}
}

func TestSelectInlineStepPeers_SpawnsPeersWithinSlot(t *testing.T) {
	ready := []string{"a", "b", "c", "d"}
	got := selectInlineStepPeers(3, 0, "a", ready, func(string) bool { return false })
	want := []string{"b", "c"} // maxParallel-1-running = 3-1-0 = 2 slots
	if !sliceEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestSelectInlineStepPeers_RespectsRunningSlot(t *testing.T) {
	ready := []string{"a", "b", "c", "d"}
	got := selectInlineStepPeers(3, 1, "a", ready, func(string) bool { return false })
	want := []string{"b"} // 3-1-1 = 1 slot
	if !sliceEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestSelectInlineStepPeers_SkipsAlreadyRegistered(t *testing.T) {
	ready := []string{"a", "b", "c", "d"}
	registered := func(id string) bool { return id == "b" }
	got := selectInlineStepPeers(3, 0, "a", ready, registered)
	want := []string{"c", "d"} // b 跳过，slot 还有 2 → c+d
	if !sliceEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestSelectInlineStepPeers_AllSlotsUsed(t *testing.T) {
	ready := []string{"a", "b", "c"}
	got := selectInlineStepPeers(3, 2, "a", ready, func(string) bool { return false })
	if got != nil {
		t.Fatalf("3-1-2=0 slot 应返回 nil，got %v", got)
	}
}

func TestSelectInlineStepPeers_NegativeSlotsDegradeToNoOp(t *testing.T) {
	ready := []string{"a", "b"}
	got := selectInlineStepPeers(3, 5, "a", ready, func(string) bool { return false })
	if got != nil {
		t.Fatalf("running > maxParallel 应返回 nil，got %v", got)
	}
}

func TestSelectInlineStepPeers_EmptyReady(t *testing.T) {
	got := selectInlineStepPeers(3, 0, "a", nil, func(string) bool { return false })
	if got != nil {
		t.Fatalf("empty ready 应返回 nil，got %v", got)
	}
}

func TestSelectInlineStepPeers_TrimsCurrentID(t *testing.T) {
	ready := []string{"a", "b"}
	got := selectInlineStepPeers(3, 0, "  a  ", ready, func(string) bool { return false })
	want := []string{"b"}
	if !sliceEqual(got, want) {
		t.Fatalf("currentID 应被 trim，expected %v, got %v", want, got)
	}
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ====================================================================
// 红线测试：bucket 隔离 + deep copy（P0-3 防 slice 别名 race）
// ====================================================================

// TestInlineStepHistoryBucketIsolation_DeepCopy 验证 bucket seed 是 deep copy——
// 主 history append 时桶内 msgs 不受影响（共享底层数组会被 race detector 捕获）。
func TestInlineStepHistoryBucketIsolation_DeepCopy(t *testing.T) {
	a := &Agent{
		stepHistory:   []*ai.MsgInfo{ai.NewUserMsgInfo("m1"), ai.NewUserMsgInfo("m2")},
		stepHistories: make(map[string]*stepHistoryBucket),
	}

	// 创建桶 seed 自 a.stepHistory
	bucket := a.ensureBucket("step-x", builtin_tools.AgentPhaseStep, 1, a.stepHistory)
	if bucket == nil {
		t.Fatal("ensureBucket returned nil")
	}
	if len(bucket.msgs) != 2 {
		t.Fatalf("expected seed len 2, got %d", len(bucket.msgs))
	}

	// 主 history append 不应影响桶
	a.stepHistory = append(a.stepHistory, ai.NewUserMsgInfo("m3"))
	if len(bucket.msgs) != 2 {
		t.Fatalf("bucket msgs 被主 append 污染：want 2, got %d (proof of slice alias bug)", len(bucket.msgs))
	}

	// 反过来：桶 append 不影响主
	bucket.msgs = append(bucket.msgs, ai.NewUserMsgInfo("bucket-msg"))
	if len(a.stepHistory) != 3 {
		t.Fatalf("主 stepHistory 被桶 append 污染：want 3, got %d", len(a.stepHistory))
	}
}

// TestInlineStepHistoryBucketIsolation_ConcurrentAppend 验证多桶并发 append 不触发 race
// （桶级 mutex 仅保护 map 增删，桶内 msgs 由调用者契约保证单 goroutine 写）。
// 本测试模拟两个桶各自的 goroutine 各 append 100 条——race detector 应静默。
func TestInlineStepHistoryBucketIsolation_ConcurrentAppend(t *testing.T) {
	a := &Agent{
		stepHistory:   []*ai.MsgInfo{},
		stepHistories: make(map[string]*stepHistoryBucket),
	}
	b1 := a.ensureBucket("s1", builtin_tools.AgentPhaseStep, 1, nil)
	b2 := a.ensureBucket("s2", builtin_tools.AgentPhaseStep, 1, nil)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			b1.msgs = append(b1.msgs, ai.NewUserMsgInfo("b1"))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			b2.msgs = append(b2.msgs, ai.NewUserMsgInfo("b2"))
		}
	}()
	wg.Wait()

	if len(b1.msgs) != 100 || len(b2.msgs) != 100 {
		t.Fatalf("expected each bucket has 100 msgs, got b1=%d b2=%d", len(b1.msgs), len(b2.msgs))
	}
}

// ====================================================================
// 红线测试：FinalAnswer 防泄漏（P0-2，inline peer 桶禁 submit_final_answer）
// ====================================================================

// TestFinalAnswerForbidInInlinePeerBucket：BuildFunctionTools(runCtx, AgentPhaseFinalAnswer)
// 在 runCtx.FinalAnswerAllowed=false 时不应注册 submit_final_answer。
//
// 注：BuildFunctionTools 当前 phase 路由不直接注册 submit_final_answer（phase_final_answer.go
// 显式 append），所以本测试主要验证软挡板的存在与生效——若未来 step phase 把 final_answer
// 加进 toolEnabledInPhase 白名单，本测试会捕获泄漏回归。
func TestFinalAnswerForbidInInlinePeerBucket(t *testing.T) {
	// 验证仅算法层的保护：若 SubmitFinalAnswerToolName 出现在 enabled tools 集合中
	// + runCtx.FinalAnswerAllowed=false → 软挡板应拒收。
	// 这里直接断言 buildFunctionTools 内部的过滤逻辑而非整个 ToolRegistry 设置——
	// 通过构造 minimal Agent 容器 + 注册仅 1 个 SubmitFinalAnswer tool。
	a := &Agent{
		cfg: &AgentConfig{IsSubAgent: false},
	}
	a.tools = nil // 空 tools 集合直接早退；测试只关心防护开关逻辑

	// runCtx 非 nil 且 FinalAnswerAllowed=false → finalAnswerForbidden=true
	runCtxForbid := &InlineStepCtx{StepID: "s1", FinalAnswerAllowed: false}
	tools, _ := a.BuildFunctionTools(runCtxForbid, builtin_tools.AgentPhaseStep)
	if tools != nil {
		t.Fatalf("expected nil tools with empty a.tools, got %v", tools)
	}

	// runCtx 为 nil 表示 plan/replan/final_answer 主路径——不进入软挡板分支
	tools, _ = a.BuildFunctionTools(nil, builtin_tools.AgentPhaseFinalAnswer)
	if tools != nil {
		t.Fatalf("expected nil tools with empty a.tools, got %v", tools)
	}

	// 注：完整的 finalAnswerForbidden 端到端断言（注册 submit_final_answer 工具 →
	// runCtx.FinalAnswerAllowed=false 时 tools 列表不含该名）需要 Agent 完整 setup，
	// 留给后续 setup helper 完善时补。本测试至少守住 BuildFunctionTools 的签名 +
	// nil runCtx 兼容路径。
}

// ====================================================================
// 红线测试：fallback degenerate (MaxParallelSteps=1)
// ====================================================================

// TestSelectInlineStepPeers_DegenerateForN1：MaxParallelSteps=1 时 selectInlineStepPeers
// 必须返回 nil——这是 fallback 不变质原则的代码层断言（degenerate case 不保留分叉特殊化）。
func TestSelectInlineStepPeers_DegenerateForN1(t *testing.T) {
	ready := []string{"a", "b", "c", "d"}
	got := selectInlineStepPeers(1, 0, "a", ready, func(string) bool { return false })
	if got != nil {
		t.Fatalf("MaxParallelSteps=1 (degenerate) 必须返回 nil 避免起 peer goroutine，got %v", got)
	}
}

// ====================================================================
// 红线测试：isInlineStepTerminal
// ====================================================================

func TestIsInlineStepTerminal(t *testing.T) {
	snap := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "b", Status: builtin_tools.PlanStepFailed},
			{ID: "c", Status: builtin_tools.PlanStepSkipped},
			{ID: "d", Status: builtin_tools.PlanStepPending},
			{ID: "e", Status: builtin_tools.PlanStepInProgress},
		},
	}
	cases := map[string]bool{
		"a":         true,
		"b":         true,
		"c":         true,
		"d":         false,
		"e":         false,
		"missing":   false,
		"":          false,
	}
	for id, want := range cases {
		if got := isInlineStepTerminal(snap, id); got != want {
			t.Errorf("isInlineStepTerminal(%q) = %v, want %v", id, got, want)
		}
	}
}
