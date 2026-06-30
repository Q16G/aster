package react

import (
	"testing"
)

// TestAgentFactory_BuildEnsuresAsyncRegistryForRootAgent：回归 bug 修复。
// 根 Agent 必须在 Build 时就装好 asyncRegistry，否则 fanOutReadyPeers
// 第一道闸门 if a.asyncRegistry == nil { return } 静默早退，X2 完全失效。
// 现状 ensureAsyncRegistry 是懒创建，只有 sub_agent_tool 调用时才触发；
// 任务里 LLM 不用 sub_agent 就 nil 到天荒地老。
func TestAgentFactory_BuildEnsuresAsyncRegistryForRootAgent(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryMaxParallelSteps(3),
	)
	agent, err := factory.Build(AgentDefinition{Name: "test", Instruction: "x"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if agent.asyncRegistry == nil {
		t.Fatal("expected asyncRegistry initialized for root agent (X2 fan-out depends on it)")
	}
}

// TestAgentFactory_BuildDoesNotEnsureAsyncRegistryForSubAgent：sub_agent 不应被注入 registry
// （sub_agent 自身不能 spawn 远程 step，避免嵌套并发）。
func TestAgentFactory_BuildDoesNotEnsureAsyncRegistryForSubAgent(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryMaxParallelSteps(3),
	)
	agent, err := factory.Build(AgentDefinition{Name: "sub-test", Instruction: "x", IsSubAgent: true})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if agent.asyncRegistry != nil {
		t.Fatal("sub_agent should not have asyncRegistry pre-injected")
	}
}

func TestAgentFactory_WithMaxParallelStepsBuildsAgent(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryMaxParallelSteps(5),
	)
	agent, err := factory.Build(AgentDefinition{Name: "test", Instruction: "x"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := agent.maxParallelSteps(); got != 5 {
		t.Fatalf("expected maxParallelSteps=5, got %d", got)
	}
}

func TestAgentFactory_WithoutMaxParallelStepsDefaultsTo1(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	agent, err := factory.Build(AgentDefinition{Name: "test", Instruction: "x"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// 不设 Option → AgentConfig.MaxParallelSteps=0 → getter 兜底返回 1
	if got := agent.maxParallelSteps(); got != 1 {
		t.Fatalf("expected default maxParallelSteps=1, got %d", got)
	}
}

func TestAgentFactory_MaxParallelStepsBelowTwoDoesNotInjectOption(t *testing.T) {
	// factory.maxParallelSteps=1 时 Build 不附加 WithMaxParallelSteps Option，
	// AgentConfig 字段保持零值，getter 兜底返回 1（行为等同未设 Option）。
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryMaxParallelSteps(1),
	)
	agent, err := factory.Build(AgentDefinition{Name: "test", Instruction: "x"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := agent.maxParallelSteps(); got != 1 {
		t.Fatalf("expected maxParallelSteps=1, got %d", got)
	}
	if got := agent.cfg.MaxParallelSteps; got != 0 {
		// 未传 Option 时 cfg 字段保持 0（getter 兜底）
		t.Fatalf("expected cfg.MaxParallelSteps=0 (no Option injected), got %d", got)
	}
}

// TestAgentFactory_BuildPropagatesRequestPool 锁定 AI 请求 pool 全程贯通：
// factory 持有同一把 pool；root 与 sub-agent 都拿到同一引用；cap 与 maxParallelSteps 对齐。
func TestAgentFactory_BuildPropagatesRequestPool(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryMaxParallelSteps(7),
	)
	if factory.requestPool == nil {
		t.Fatal("factory.requestPool must be initialized by NewAgentFactory")
	}
	if got := factory.requestPool.Capacity(); got != 7 {
		t.Fatalf("expected pool cap=7, got %d", got)
	}

	root, err := factory.Build(AgentDefinition{Name: "root", Instruction: "x"})
	if err != nil {
		t.Fatalf("Build root: %v", err)
	}
	if root.requestPool != factory.requestPool {
		t.Fatal("root.requestPool must be the same reference as factory.requestPool")
	}

	sub, err := factory.Build(AgentDefinition{Name: "sub", Instruction: "x", IsSubAgent: true})
	if err != nil {
		t.Fatalf("Build sub: %v", err)
	}
	if sub.requestPool != factory.requestPool {
		t.Fatal("sub-agent.requestPool must share the same factory pool")
	}
}

// TestAgentFactory_RequestPoolFloorsAtOne 锁定 N=0 / N=1 / 未设场景 pool cap=1。
func TestAgentFactory_RequestPoolFloorsAtOne(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	if got := factory.requestPool.Capacity(); got != 1 {
		t.Fatalf("unset MaxParallelSteps must floor pool cap to 1, got %d", got)
	}
}
