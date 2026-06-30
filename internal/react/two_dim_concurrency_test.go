package react

import (
	"strings"
	"testing"
)

// TestEffectiveWaveWidth 锁定有效波宽 E = max(1,N_step) × max(1,N_chain)，
// 各维度 0/负 兜底 ≥1；N_chain=1 时退化为 maxParallelSteps()。
func TestEffectiveWaveWidth(t *testing.T) {
	cases := []struct {
		nStep, nChain, wantE, wantStep, wantChain int
	}{
		{0, 0, 1, 1, 1},   // 全默认 → 串行
		{2, 0, 2, 2, 1},   // 仅链内
		{1, 3, 3, 1, 3},   // 仅链间（N_step=1 时 E 由 N_chain 放大）
		{2, 3, 6, 2, 3},   // 两维相乘
		{-5, -2, 1, 1, 1}, // 负数兜底
		{5, 1, 5, 5, 1},   // N_chain=1 退化等于 N_step
	}
	for _, c := range cases {
		a := &Agent{cfg: &AgentConfig{MaxParallelSteps: c.nStep, MaxParallelChains: c.nChain}}
		if got := a.maxParallelSteps(); got != c.wantStep {
			t.Fatalf("maxParallelSteps(nStep=%d)=%d, want %d", c.nStep, got, c.wantStep)
		}
		if got := a.maxParallelChains(); got != c.wantChain {
			t.Fatalf("maxParallelChains(nChain=%d)=%d, want %d", c.nChain, got, c.wantChain)
		}
		if got := a.effectiveWaveWidth(); got != c.wantE {
			t.Fatalf("effectiveWaveWidth(nStep=%d,nChain=%d)=%d, want %d", c.nStep, c.nChain, got, c.wantE)
		}
	}
}

// TestAIRequestPool_CapEqualsProduct 锁定全局 AI 请求池容量 = N_step × N_chain。
func TestAIRequestPool_CapEqualsProduct(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryMaxParallelSteps(2),
		WithFactoryMaxParallelChains(3),
	)
	if got := factory.requestPool.Capacity(); got != 6 {
		t.Fatalf("expected pool cap = N_step×N_chain = 6, got %d", got)
	}
}

// TestBackwardCompat_NChainOne 锁定零回归：N_chain=0/1 时池容量与有效波宽都等于
// 仅 N_step 的现状（字节级行为不变）。
func TestBackwardCompat_NChainOne(t *testing.T) {
	for _, nChain := range []int{0, 1} {
		factory := NewAgentFactory(
			WithFactoryDefaultAIClient(&stubClient{}),
			WithFactoryEmitter(NewDummyEmitter()),
			WithFactoryMaxParallelSteps(5),
			WithFactoryMaxParallelChains(nChain),
		)
		if got := factory.requestPool.Capacity(); got != 5 {
			t.Fatalf("N_chain=%d must keep pool cap = N_step = 5, got %d", nChain, got)
		}
	}
	a := &Agent{cfg: &AgentConfig{MaxParallelSteps: 5, MaxParallelChains: 1}}
	if a.effectiveWaveWidth() != a.maxParallelSteps() {
		t.Fatalf("N_chain=1 must degenerate effectiveWaveWidth to maxParallelSteps")
	}
}

// TestSelectInlineStepPeers_HonorsEffectiveWidth 锁定 selectInlineStepPeers 用有效
// 波宽 E 做派发上限：E=4、0 running、5 个 ready → 派发 E-1=3 个 peer。
func TestSelectInlineStepPeers_HonorsEffectiveWidth(t *testing.T) {
	ready := []string{"current", "s2", "s3", "s4", "s5"}
	const e = 4 // 模拟 N_step=2 × N_chain=2
	peers := selectInlineStepPeers(e, 0, "current", ready, nil)
	if len(peers) != e-1 {
		t.Fatalf("E=%d with 0 running must dispatch %d peers, got %d (%v)", e, e-1, len(peers), peers)
	}
	// 已有 runningInline 占额后剩余收缩。
	peers2 := selectInlineStepPeers(e, 2, "current", ready, nil)
	if len(peers2) != e-1-2 {
		t.Fatalf("E=%d with 2 running must dispatch %d peers, got %d", e, e-1-2, len(peers2))
	}
}

// TestPlanningSystemPromptRendersChainBudgetWhenN2 锁定 MaxParallelChains≥2 时渲染
// 链间对象并行预算段。
func TestPlanningSystemPromptRendersChainBudgetWhenN2(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:             "test",
		MaxParallelChains: 4,
	})
	if err != nil {
		t.Fatalf("BuildTaskPlannerPrompt failed: %v", err)
	}
	rendered := parts.SystemRules
	for _, needle := range []string{"并发预算", "链间对象并行", "MAX_PARALLEL_CHAINS = 4"} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("chain budget section missing %q, got:\n%s", needle, rendered)
		}
	}
	// 仅链间维度（N_step 未设）时不应渲染链内段。
	if strings.Contains(rendered, "MAX_PARALLEL_STEPS") {
		t.Fatalf("MAX_PARALLEL_STEPS must not render when MaxParallelSteps unset")
	}
}

// TestPlanningSystemPromptHidesChainBudgetWhenN1 锁定 N_chain<2 时链间段不渲染，
// 且与链内段独立门控（N_step=4 时父段与链内段仍在）。
func TestPlanningSystemPromptHidesChainBudgetWhenN1(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	for _, nChain := range []int{0, 1} {
		parts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
			Input:             "test",
			MaxParallelSteps:  4,
			MaxParallelChains: nChain,
		})
		if err != nil {
			t.Fatalf("BuildTaskPlannerPrompt failed (N_chain=%d): %v", nChain, err)
		}
		rendered := parts.SystemRules
		if !strings.Contains(rendered, "链内深度并行") {
			t.Fatalf("链内段应渲染（N_step=4），N_chain=%d", nChain)
		}
		if strings.Contains(rendered, "MAX_PARALLEL_CHAINS") {
			t.Fatalf("MAX_PARALLEL_CHAINS must not render when MaxParallelChains=%d", nChain)
		}
		if strings.Contains(rendered, "链间对象并行") {
			t.Fatalf("链间段不应渲染 when MaxParallelChains=%d", nChain)
		}
	}
}
