package openai_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"aster/internal/ai"
	. "aster/internal/ai/openai"
)

func TestStreamOptions_IncludeUsage_SentInRequest(t *testing.T) {
	transport := &reasoningRoundTripper{
		body: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n",
	}

	client := NewClient(
		WithURL("http://fake"),
		WithAPIKey("fake"),
		WithModel("test-model"),
		WithStream(true),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	_, _ = client.ChatText(t.Context(), "hello")

	var body map[string]any
	if err := json.Unmarshal([]byte(transport.reqBody), &body); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}

	raw, ok := body["stream_options"]
	if !ok {
		t.Fatal("stream_options missing from request body")
	}

	opts, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("stream_options is not an object: %T", raw)
	}

	includeUsage, ok := opts["include_usage"]
	if !ok {
		t.Fatal("include_usage missing from stream_options")
	}
	if includeUsage != true {
		t.Fatalf("include_usage = %v, want true", includeUsage)
	}
}

func TestStreamOptions_NotSent_WhenNotStreaming(t *testing.T) {
	transport := &reasoningRoundTripper{
		body: `{"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
	}

	client := NewClient(
		WithURL("http://fake"),
		WithAPIKey("fake"),
		WithModel("test-model"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	_, _ = client.ChatText(t.Context(), "hello")

	var body map[string]any
	if err := json.Unmarshal([]byte(transport.reqBody), &body); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}

	if _, ok := body["stream_options"]; ok {
		t.Fatal("stream_options should not be present when stream=false")
	}
}

func TestStreamUsage_ParsedFromFinalChunk(t *testing.T) {
	sseBody := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"Hello"}}]}`,
		``,
		`data: {"choices":[{"index":0,"delta":{"content":" world"},"finish_reason":"stop"}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"completion_tokens_details":{"reasoning_tokens":2},"prompt_tokens_details":{"cached_tokens":3}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	transport := &reasoningRoundTripper{body: sseBody}

	var chunks []string
	client := NewClient(
		WithURL("http://fake"),
		WithAPIKey("fake"),
		WithModel("test-model"),
		WithStream(true),
		WithHTTPClient(&http.Client{Transport: transport}),
		WithStreamFunc(func(event *StreamEvent) {
			if event.Content != "" {
				chunks = append(chunks, event.Content)
			}
		}),
	)

	resp, err := client.ChatText(t.Context(), "hello")
	if err != nil {
		t.Fatalf("ChatText failed: %v", err)
	}

	if resp != "Hello world" {
		t.Fatalf("response = %q, want %q", resp, "Hello world")
	}

	usage := client.LastTokenUsage()
	if usage == nil {
		t.Fatal("LastTokenUsage() returned nil — usage was not parsed from SSE stream")
	}

	// OpenAI format: prompt_tokens includes cached, so normalizer subtracts: 10-3=7
	assertTokenField(t, "InputTokens", usage.InputTokens, 7)
	assertTokenField(t, "OutputTokens", usage.OutputTokens, 5)
	assertTokenField(t, "ReasoningTokens", usage.ReasoningTokens, 2)
	assertTokenField(t, "CacheReadTokens", usage.CacheReadTokens, 3)
}

func TestStreamUsage_DeepSeekFormat(t *testing.T) {
	sseBody := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_cache_hit_tokens":50,"prompt_cache_miss_tokens":50}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	transport := &reasoningRoundTripper{body: sseBody}
	client := NewClient(
		WithURL("http://fake"),
		WithAPIKey("fake"),
		WithModel("deepseek-v4-flash"),
		WithStream(true),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	_, err := client.ChatText(t.Context(), "hello")
	if err != nil {
		t.Fatalf("ChatText failed: %v", err)
	}

	usage := client.LastTokenUsage()
	if usage == nil {
		t.Fatal("LastTokenUsage() returned nil")
	}

	// DeepSeek: prompt_cache_miss_tokens is NOT cache_write, it's just non-cached input.
	// normalizer: input = 100 - cache_hit(50) = 50
	assertTokenField(t, "InputTokens", usage.InputTokens, 50)
	assertTokenField(t, "OutputTokens", usage.OutputTokens, 20)
	assertTokenField(t, "CacheReadTokens", usage.CacheReadTokens, 50)
	assertTokenField(t, "CacheWriteTokens", usage.CacheWriteTokens, 0)
}

func TestStreamUsage_NoUsageChunk_ReturnsNil(t *testing.T) {
	sseBody := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	transport := &reasoningRoundTripper{body: sseBody}
	client := NewClient(
		WithURL("http://fake"),
		WithAPIKey("fake"),
		WithModel("test-model"),
		WithStream(true),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	_, err := client.ChatText(t.Context(), "hello")
	if err != nil {
		t.Fatalf("ChatText failed: %v", err)
	}

	usage := client.LastTokenUsage()
	if usage != nil {
		t.Fatalf("expected nil usage when no usage chunk, got %+v", usage)
	}
}

func TestStreamUsage_Choices_CarryUsage(t *testing.T) {
	sseBody := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":42,"completion_tokens":7,"total_tokens":49}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	transport := &reasoningRoundTripper{body: sseBody}
	client := NewClient(
		WithURL("http://fake"),
		WithAPIKey("fake"),
		WithModel("test-model"),
		WithStream(true),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	resp, err := client.ChatText(t.Context(), "hello")
	if err != nil {
		t.Fatalf("ChatText failed: %v", err)
	}
	if resp != "hi" {
		t.Fatalf("response = %q, want %q", resp, "hi")
	}

	usage := client.LastTokenUsage()
	if usage == nil {
		t.Fatal("LastTokenUsage() returned nil")
	}
	assertTokenField(t, "InputTokens", usage.InputTokens, 42)
	assertTokenField(t, "OutputTokens", usage.OutputTokens, 7)
}

func assertTokenField(t *testing.T, name string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", name, got, want)
	}
}

// Verify that ChatCompletions also exposes usage on the choices.
func TestStreamUsage_ChatCompletions_ReturnsUsageOnChoices(t *testing.T) {
	sseBody := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":8,"completion_tokens":1,"total_tokens":9}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	transport := &reasoningRoundTripper{body: sseBody}
	client := NewClient(
		WithURL("http://fake"),
		WithAPIKey("fake"),
		WithModel("test-model"),
		WithStream(true),
		WithHTTPClient(&http.Client{Transport: transport}),
		WithStreamFunc(func(event *StreamEvent) {}),
	)

	choices, err := client.ChatEx(t.Context(), []*ai.MsgInfo{
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatalf("ChatCompletions failed: %v", err)
	}

	if len(choices) == 0 {
		t.Fatal("no choices returned")
	}

	usage := choices[0].Usage
	if usage == nil {
		t.Fatal("choices[0].Usage is nil — usage from final SSE chunk not propagated to choices")
	}
	assertTokenField(t, "InputTokens", usage.InputTokens, 8)
	assertTokenField(t, "OutputTokens", usage.OutputTokens, 1)
}

// Real API response from opencode.ai/deepseek-v4-flash with stream_options.
func TestStreamUsage_RealDeepSeekV4Response(t *testing.T) {
	sseBody := strings.Join([]string{
		`data: {"id":"abc","object":"chat.completion.chunk","created":1779866539,"model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"role":"assistant","content":null,"reasoning_content":""},"logprobs":null,"finish_reason":null}],"usage":null}`,
		``,
		`data: {"id":"abc","object":"chat.completion.chunk","created":1779866539,"model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"content":null,"reasoning_content":"thinking"},"logprobs":null,"finish_reason":null}],"usage":null}`,
		``,
		`data: {"id":"abc","object":"chat.completion.chunk","created":1779866539,"model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"content":"","reasoning_content":null},"logprobs":null,"finish_reason":"length"}],"usage":{"prompt_tokens":85,"completion_tokens":10,"total_tokens":95,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":10},"prompt_cache_hit_tokens":0,"prompt_cache_miss_tokens":85}}`,
		``,
		`data: [DONE]`,
		``,
		`data: {"choices":[],"cost":"0"}`,
		``,
	}, "\n")

	transport := &reasoningRoundTripper{body: sseBody}
	client := NewClient(
		WithURL("http://fake"),
		WithAPIKey("fake"),
		WithModel("deepseek-v4-flash"),
		WithStream(true),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	_, err := client.ChatText(t.Context(), "say hi")
	if err != nil {
		t.Fatalf("ChatText failed: %v", err)
	}

	usage := client.LastTokenUsage()
	if usage == nil {
		t.Fatal("LastTokenUsage() returned nil — usage was NOT parsed from real deepseek SSE response")
	}

	t.Logf("Parsed usage: Input=%d Output=%d Reasoning=%d CacheRead=%d CacheWrite=%d Total=%d",
		usage.InputTokens, usage.OutputTokens, usage.ReasoningTokens,
		usage.CacheReadTokens, usage.CacheWriteTokens, usage.TotalTokens)

	// prompt_tokens=85, prompt_cache_miss_tokens=85 (not cache_write), prompt_cache_hit_tokens=0
	// No cache tokens → no normalization adjustment → InputTokens stays 85
	assertTokenField(t, "InputTokens", usage.InputTokens, 85)
	assertTokenField(t, "OutputTokens", usage.OutputTokens, 10)
	assertTokenField(t, "ReasoningTokens", usage.ReasoningTokens, 10)
	assertTokenField(t, "CacheReadTokens", usage.CacheReadTokens, 0)
	assertTokenField(t, "CacheWriteTokens", usage.CacheWriteTokens, 0)

	ctx := usage.ContextCountTokens()
	if ctx == 0 {
		t.Fatal("ContextCountTokens() = 0, sidebar would show '--'")
	}
	assertTokenField(t, "ContextCountTokens", ctx, 95)
	t.Logf("ContextCountTokens() = %d (this is what sidebar displays)", ctx)
}

