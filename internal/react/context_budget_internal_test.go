package react

import (
	"testing"

	"aster/internal/ai"
)

func TestInferKnownModelContext_DeepSeekV4(t *testing.T) {
	cases := []struct {
		name              string
		model             string
		wantContextWindow int
		wantOutputLimit   int
	}{
		{name: "deepseek-v4-pro", model: "deepseek-v4-pro", wantContextWindow: 1000000, wantOutputLimit: 384000},
		{name: "deepseek-v4-flash", model: "deepseek-v4-flash", wantContextWindow: 1000000, wantOutputLimit: 384000},
		// Legacy aliases that DeepSeek routes to V4-Flash.
		{name: "deepseek-chat", model: "deepseek-chat", wantContextWindow: 1000000, wantOutputLimit: 384000},
		{name: "deepseek-reasoner", model: "deepseek-reasoner", wantContextWindow: 1000000, wantOutputLimit: 384000},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			info, ok := inferKnownModelContext(tc.model)
			if !ok {
				t.Fatalf("expected model %q to be inferred", tc.model)
			}
			if info.ContextWindowTokens != tc.wantContextWindow {
				t.Fatalf("unexpected context window: got=%d want=%d", info.ContextWindowTokens, tc.wantContextWindow)
			}
			if info.OutputTokenLimit != tc.wantOutputLimit {
				t.Fatalf("unexpected output limit: got=%d want=%d", info.OutputTokenLimit, tc.wantOutputLimit)
			}
		})
	}
}

func TestEstimateMsgTokens_ImageAware(t *testing.T) {
	textOnly := &ai.MsgInfo{
		Role: "user",
		Content: []*ai.ChatContext{
			{Type: "text", Text: "hello"},
		},
	}
	textTokens := estimateMsgTokens(textOnly)

	withImage := &ai.MsgInfo{
		Role: "user",
		Content: []*ai.ChatContext{
			{Type: "text", Text: "hello"},
			{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,abc"}},
		},
	}
	imageTokens := estimateMsgTokens(withImage)

	diff := imageTokens - textTokens
	if diff < imageTokenEstimate || diff > imageTokenEstimate+5 {
		t.Fatalf("image should add ~%d tokens, got diff=%d (text=%d, image=%d)", imageTokenEstimate, diff, textTokens, imageTokens)
	}
}

func TestEstimateMsgTokens_StringContentUnchanged(t *testing.T) {
	msg := &ai.MsgInfo{
		Role:    "tool",
		Content: "some tool result text",
	}
	tokens := estimateMsgTokens(msg)
	if tokens <= 0 {
		t.Fatalf("expected positive token count for string content, got %d", tokens)
	}
}

func TestEstimateMsgTokens_ImageOnly(t *testing.T) {
	if estimateStringTokens("") != 0 {
		t.Fatalf("precondition: estimateStringTokens(\"\") should be 0")
	}
	msg := &ai.MsgInfo{
		Role: "user",
		Content: []*ai.ChatContext{
			{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAA"}},
			{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,BBB"}},
		},
	}
	tokens := estimateMsgTokens(msg)
	roleTokens := estimateStringTokens("user")
	expected := roleTokens + 2*imageTokenEstimate
	if tokens != expected {
		t.Fatalf("expected %d tokens for 2 images, got %d", expected, tokens)
	}
}

func TestEstimateMsgTokens_NilContent(t *testing.T) {
	msg := &ai.MsgInfo{
		Role:    "user",
		Content: nil,
	}
	tokens := estimateMsgTokens(msg)
	if tokens <= 0 {
		t.Fatalf("expected positive tokens for role alone, got %d", tokens)
	}
}

func TestInferKnownModelContext_Claude(t *testing.T) {
	cases := []struct {
		model         string
		wantVision    bool
		wantCtxWindow int
	}{
		{"claude-opus-4-20250514", true, 200000},
		{"claude-sonnet-4-20250514", true, 200000},
		{"claude-3-5-haiku-20241022", true, 200000},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			info, ok := inferKnownModelContext(tc.model)
			if !ok {
				t.Fatalf("expected model %q to be inferred", tc.model)
			}
			if info.ContextWindowTokens != tc.wantCtxWindow {
				t.Fatalf("context window: got=%d want=%d", info.ContextWindowTokens, tc.wantCtxWindow)
			}
			if info.SupportsVision == nil || *info.SupportsVision != tc.wantVision {
				t.Fatalf("vision support: got=%v want=%v", info.SupportsVision, tc.wantVision)
			}
		})
	}
}
