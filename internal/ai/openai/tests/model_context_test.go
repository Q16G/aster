package openai_test

import (
	. "aster/internal/ai/openai"
	"testing"
)

func TestClientModelContextInfo(t *testing.T) {
	client := NewClient(
		WithModel("gpt-4o-mini"),
		WithContextWindowTokens(128000),
		WithInputTokenLimit(96000),
		WithOutputTokenLimit(32000),
	)
	info := client.ModelContextInfo()
	if info.ModelName != "gpt-4o-mini" {
		t.Fatalf("unexpected model name: %s", info.ModelName)
	}
	if info.ContextWindowTokens != 128000 {
		t.Fatalf("unexpected context window: %d", info.ContextWindowTokens)
	}
	if info.InputTokenLimit != 96000 {
		t.Fatalf("unexpected input limit: %d", info.InputTokenLimit)
	}
	if info.OutputTokenLimit != 32000 {
		t.Fatalf("unexpected output limit: %d", info.OutputTokenLimit)
	}
}

func TestClientModelContextInfo_MultimodalCapabilities(t *testing.T) {
	client := NewClient(
		WithModel("gpt-4o"),
		WithSupportsVision(true),
		WithSupportsAudio(true),
	)
	info := client.ModelContextInfo()
	if !info.GetSupportsVision() {
		t.Fatal("expected SupportsVision to be true")
	}
	if !info.GetSupportsAudio() {
		t.Fatal("expected SupportsAudio to be true")
	}
}

func TestClientModelContextInfo_DefaultCapabilitiesNil(t *testing.T) {
	client := NewClient(WithModel("deepseek-chat"))
	info := client.ModelContextInfo()
	if info.SupportsVision != nil {
		t.Fatal("expected SupportsVision to be nil by default")
	}
	if info.SupportsAudio != nil {
		t.Fatal("expected SupportsAudio to be nil by default")
	}
}
