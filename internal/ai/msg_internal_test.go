package ai

import "testing"

func TestNormalizeMsgInfoInPlace_RestoresChatContexts(t *testing.T) {
	msg := &MsgInfo{
		Role: "tool",
		Content: []any{
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAA"}},
		},
	}

	NormalizeMsgInfoInPlace(msg)

	contexts, ok := msg.Content.([]*ChatContext)
	if !ok {
		t.Fatalf("expected []*ChatContext, got %T", msg.Content)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(contexts))
	}
	if contexts[0] == nil || contexts[0].Type != "text" || contexts[0].Text != "hello" {
		t.Fatalf("unexpected text context: %#v", contexts[0])
	}
	if contexts[1] == nil || contexts[1].Type != "image_url" {
		t.Fatalf("unexpected image context: %#v", contexts[1])
	}
}
