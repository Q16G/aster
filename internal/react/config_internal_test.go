package react

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"aster/internal/ai"
)

func TestResolveToolTimeoutMs_Default(t *testing.T) {
	cfg := &AgentConfig{}
	got := cfg.resolveToolTimeoutMs(nil)
	if got != defaultToolTimeoutMs {
		t.Fatalf("expected %d, got %d", defaultToolTimeoutMs, got)
	}
}

func TestResolveToolTimeoutMs_FromArgs(t *testing.T) {
	cfg := &AgentConfig{}
	args := map[string]any{"timeout_ms": float64(60000)}
	got := cfg.resolveToolTimeoutMs(args)
	if got != 60000 {
		t.Fatalf("expected 60000, got %d", got)
	}
}

func TestResolveToolTimeoutMs_FromArgsString(t *testing.T) {
	cfg := &AgentConfig{}
	args := map[string]any{"timeout_ms": "60000"}
	got := cfg.resolveToolTimeoutMs(args)
	if got != 60000 {
		t.Fatalf("expected 60000, got %d", got)
	}
}

func TestResolveToolTimeoutMs_FromArgsJSONNumber(t *testing.T) {
	cfg := &AgentConfig{}
	args := map[string]any{"timeout_ms": json.Number("60000")}
	got := cfg.resolveToolTimeoutMs(args)
	if got != 60000 {
		t.Fatalf("expected 60000, got %d", got)
	}
}

func TestResolveToolTimeoutMs_ArgsCapAtMax(t *testing.T) {
	cfg := &AgentConfig{}
	args := map[string]any{"timeout_ms": float64(9_000_000)}
	got := cfg.resolveToolTimeoutMs(args)
	if got != maxToolTimeoutMs {
		t.Fatalf("expected %d, got %d", maxToolTimeoutMs, got)
	}
}

func TestResolveToolTimeoutMs_ConfigOverride(t *testing.T) {
	cfg := &AgentConfig{DefaultToolTimeoutMs: 120_000}
	got := cfg.resolveToolTimeoutMs(nil)
	if got != 120_000 {
		t.Fatalf("expected 120000, got %d", got)
	}
}

func TestResolveToolTimeoutMs_ArgsOverrideConfig(t *testing.T) {
	cfg := &AgentConfig{DefaultToolTimeoutMs: 120_000}
	args := map[string]any{"timeout_ms": float64(30000)}
	got := cfg.resolveToolTimeoutMs(args)
	if got != 30000 {
		t.Fatalf("expected 30000, got %d", got)
	}
}

func TestResolveToolTimeoutMs_ZeroArgs(t *testing.T) {
	cfg := &AgentConfig{}
	args := map[string]any{"timeout_ms": float64(0)}
	got := cfg.resolveToolTimeoutMs(args)
	if got != defaultToolTimeoutMs {
		t.Fatalf("expected %d, got %d", defaultToolTimeoutMs, got)
	}
}

func TestResolveToolTimeout_ReturnsDuration(t *testing.T) {
	cfg := &AgentConfig{}
	got := cfg.resolveToolTimeout(nil)
	expected := time.Duration(defaultToolTimeoutMs) * time.Millisecond
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

type noopChatClientForCacheTest struct{}

func (c *noopChatClientForCacheTest) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}
func (c *noopChatClientForCacheTest) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}
func (c *noopChatClientForCacheTest) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestBuildPromptRequestOptions_NilConfig_CacheEnabled(t *testing.T) {
	agent, err := NewReActAgent("test", &noopChatClientForCacheTest{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	opts := agent.buildPromptRequestOptions("think_act", "test prompt", true)
	if opts == nil || !opts.PromptCacheEnabled {
		t.Fatal("expected cache enabled when PromptCacheConfig is nil")
	}
}

func TestBuildPromptRequestOptions_ExplicitDisable(t *testing.T) {
	agent, err := NewReActAgent("test", &noopChatClientForCacheTest{},
		WithEmitter(NewDummyEmitter()),
		WithPromptCacheConfig(&ai.PromptCacheConfig{Enabled: false}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	opts := agent.buildPromptRequestOptions("think_act", "test prompt", true)
	if opts != nil && opts.PromptCacheEnabled {
		t.Fatal("expected cache disabled when PromptCacheConfig.Enabled is false")
	}
}

func TestBuildPromptRequestOptions_TTLInjected(t *testing.T) {
	agent, err := NewReActAgent("test", &noopChatClientForCacheTest{},
		WithEmitter(NewDummyEmitter()),
		WithPromptCacheConfig(&ai.PromptCacheConfig{Enabled: true, TTL: "10m"}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	opts := agent.buildPromptRequestOptions("think_act", "test prompt", true)
	if opts == nil {
		t.Fatal("expected non-nil options")
	}
	if !opts.PromptCacheEnabled {
		t.Fatal("expected cache enabled")
	}
	if opts.PromptCacheRetention != "10m" {
		t.Fatalf("expected PromptCacheRetention=10m, got %q", opts.PromptCacheRetention)
	}
}

func TestFactoryPassesPromptCacheConfig(t *testing.T) {
	pcc := &ai.PromptCacheConfig{Enabled: true, TTL: "5m"}
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&noopChatClientForCacheTest{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryPromptCacheConfig(pcc),
	)
	def := AgentDefinition{Name: "test-agent"}
	agent, err := factory.Build(def)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if agent.cfg.PromptCacheConfig == nil {
		t.Fatal("expected PromptCacheConfig to be propagated from factory")
	}
	if agent.cfg.PromptCacheConfig.TTL != "5m" {
		t.Fatalf("expected TTL=5m, got %q", agent.cfg.PromptCacheConfig.TTL)
	}
}
