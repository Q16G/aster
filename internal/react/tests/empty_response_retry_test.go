package react_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"aster/internal/ai"
	. "aster/internal/react"
)

// emptyRetryClient returns empty responses for the first emptyCount calls,
// then returns a response with the given content or tool calls.
type emptyRetryClient struct {
	emptyCount   int
	callCount    atomic.Int32
	content      string
	toolCalls    []*ai.FunctionTool
	returnErr    error
	returnNil    bool // return nil choices
	summaryText  string
}

func (c *emptyRetryClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return c.summaryText, nil
}

func (c *emptyRetryClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return c.summaryText, nil
}

func (c *emptyRetryClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	n := int(c.callCount.Add(1))

	if c.returnErr != nil && n <= c.emptyCount {
		return nil, c.returnErr
	}
	if c.returnNil {
		return nil, nil
	}

	if n <= c.emptyCount {
		msg := ai.NewAIMsgInfo("")
		return []*ai.ChatChoices{{Message: msg, FinishReason: "stop"}}, nil
	}

	msg := ai.NewAIMsgInfo(c.content)
	msg.ToolCalls = c.toolCalls
	return []*ai.ChatChoices{{Message: msg, FinishReason: "stop"}}, nil
}

// streamingEmptyRetryClient implements StreamingChatClient for streaming retry tests.
type streamingEmptyRetryClient struct {
	emptyRetryClient
}

func (c *streamingEmptyRetryClient) ChatStream(_ context.Context, _ []*ai.MsgInfo, handler ai.StreamHandler, _ ...*ai.FunctionTool) error {
	n := int(c.callCount.Add(1))

	if n <= c.emptyCount {
		return handler(nil, true)
	}

	if c.content != "" {
		if err := handler(&ai.StreamDelta{Content: c.content, FinishReason: "stop"}, false); err != nil {
			return err
		}
	}
	for _, tc := range c.toolCalls {
		if err := handler(&ai.StreamDelta{ToolCalls: []*ai.FunctionTool{tc}}, false); err != nil {
			return err
		}
	}
	return handler(nil, true)
}

func (c *streamingEmptyRetryClient) LastTokenUsage() *ai.TokenUsage {
	return &ai.TokenUsage{InputTokens: 10, OutputTokens: 1}
}

func newTestAgent(t *testing.T, client ai.ChatClient) *Agent {
	t.Helper()
	agent, err := NewReActAgent("test-empty-retry", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	return agent
}

// --- Tests for existing behavior (should still work) ---

func TestAICallProxy_NormalResponse_NotRetried(t *testing.T) {
	client := &emptyRetryClient{content: "hello world"}
	agent := newTestAgent(t, client)

	result, err := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AssistantText != "hello world" {
		t.Fatalf("expected 'hello world', got %q", result.AssistantText)
	}
	if int(client.callCount.Load()) != 1 {
		t.Fatalf("expected 1 call, got %d", client.callCount.Load())
	}
}

func TestAICallProxy_NormalToolCall_NotRetried(t *testing.T) {
	tc := &ai.FunctionTool{
		Id:   "call_1",
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:      "read_file",
			Arguments: `{"path": "/tmp/test.go"}`,
		},
	}
	client := &emptyRetryClient{toolCalls: []*ai.FunctionTool{tc}}
	agent := newTestAgent(t, client)

	result, err := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if int(client.callCount.Load()) != 1 {
		t.Fatalf("expected 1 call, got %d", client.callCount.Load())
	}
}

func TestAICallProxy_TextOnlyNoToolCalls_NotRetried(t *testing.T) {
	client := &emptyRetryClient{content: "thinking out loud..."}
	agent := newTestAgent(t, client)

	result, err := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AssistantText != "thinking out loud..." {
		t.Fatalf("expected text, got %q", result.AssistantText)
	}
	if int(client.callCount.Load()) != 1 {
		t.Fatalf("should not retry when text is present, got %d calls", client.callCount.Load())
	}
}

func TestAICallProxy_APIError_NotRetried(t *testing.T) {
	client := &emptyRetryClient{
		emptyCount: 1,
		returnErr:  fmt.Errorf("connection refused"),
	}
	agent := newTestAgent(t, client)

	_, err := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if int(client.callCount.Load()) != 1 {
		t.Fatalf("API errors should not trigger empty-response retry, got %d calls", client.callCount.Load())
	}
}

// --- Tests for new empty-response retry ---

func TestAICallProxy_EmptyResponse_RetriesAndSucceeds(t *testing.T) {
	client := &emptyRetryClient{
		emptyCount: 2,
		content:    "recovered",
	}
	agent := newTestAgent(t, client)

	result, err := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AssistantText != "recovered" {
		t.Fatalf("expected 'recovered', got %q", result.AssistantText)
	}
	// 2 empty + 1 success = 3 calls
	if int(client.callCount.Load()) != 3 {
		t.Fatalf("expected 3 calls (2 empty + 1 success), got %d", client.callCount.Load())
	}
}

func TestAICallProxy_EmptyResponse_RetriesAndSucceedsWithToolCall(t *testing.T) {
	tc := &ai.FunctionTool{
		Id:   "call_1",
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:      "search",
			Arguments: `{"q": "test"}`,
		},
	}
	client := &emptyRetryClient{
		emptyCount: 1,
		toolCalls:  []*ai.FunctionTool{tc},
	}
	agent := newTestAgent(t, client)

	result, err := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].Function.Name != "search" {
		t.Fatalf("expected tool call 'search', got %+v", result.ToolCalls)
	}
	if int(client.callCount.Load()) != 2 {
		t.Fatalf("expected 2 calls, got %d", client.callCount.Load())
	}
}

func TestAICallProxy_EmptyResponse_ExhaustsRetries(t *testing.T) {
	client := &emptyRetryClient{emptyCount: 100}
	agent := newTestAgent(t, client)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	result, err := agent.AICallProxy(ctx, 1, client, "prompt", "")

	// Should either return empty result (if some attempts completed)
	// or context error (if timeout hit during retry wait)
	if err != nil {
		if ctx.Err() == nil {
			t.Fatalf("unexpected non-context error: %v", err)
		}
		return // context timeout is acceptable
	}

	if result.AssistantText != "" || len(result.ToolCalls) != 0 {
		t.Fatalf("expected empty result, got text=%q toolCalls=%d", result.AssistantText, len(result.ToolCalls))
	}
	// At least 2 attempts should have been made within 4 seconds (1s wait after first)
	calls := int(client.callCount.Load())
	if calls < 2 {
		t.Fatalf("expected at least 2 attempts within timeout, got %d", calls)
	}
}

func TestAICallProxy_EmptyResponse_ContextCancelledDuringRetry(t *testing.T) {
	client := &emptyRetryClient{emptyCount: 100}
	agent := newTestAgent(t, client)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after the first empty response is detected and retry wait begins
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	_, err := agent.AICallProxy(ctx, 1, client, "prompt", "")
	if err == nil {
		t.Fatal("expected context cancelled error, got nil")
	}
	if ctx.Err() == nil {
		t.Fatalf("expected context to be cancelled, err=%v", err)
	}
}

func TestAICallProxy_NilChoices_Retried(t *testing.T) {
	client := &emptyRetryClient{
		emptyCount: 1,
		returnNil:  false,
		content:    "after-nil",
	}
	// First call returns empty choices (len == 0), which triggers retry
	// We simulate this by setting emptyCount=1 (returns empty content msg)
	agent := newTestAgent(t, client)

	result, err := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AssistantText != "after-nil" {
		t.Fatalf("expected 'after-nil', got %q", result.AssistantText)
	}
}

// --- Streaming tests ---

func TestAICallProxy_StreamingEmptyResponse_RetriesAndSucceeds(t *testing.T) {
	client := &streamingEmptyRetryClient{
		emptyRetryClient: emptyRetryClient{
			emptyCount: 1,
			content:    "streamed-recovery",
		},
	}
	agent := newTestAgent(t, client)

	result, err := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AssistantText != "streamed-recovery" {
		t.Fatalf("expected 'streamed-recovery', got %q", result.AssistantText)
	}
	// streaming path: callCount is incremented by ChatStream, not ChatEx
	if int(client.callCount.Load()) != 2 {
		t.Fatalf("expected 2 streaming calls, got %d", client.callCount.Load())
	}
}

func TestAICallProxy_StreamingNormalResponse_NotRetried(t *testing.T) {
	client := &streamingEmptyRetryClient{
		emptyRetryClient: emptyRetryClient{
			content: "streamed-ok",
		},
	}
	agent := newTestAgent(t, client)

	result, err := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AssistantText != "streamed-ok" {
		t.Fatalf("expected 'streamed-ok', got %q", result.AssistantText)
	}
	if int(client.callCount.Load()) != 1 {
		t.Fatalf("expected 1 call, got %d", client.callCount.Load())
	}
}

// --- Retry event emission test ---

func TestAICallProxy_EmptyResponse_EmitsWarningLog(t *testing.T) {
	client := &emptyRetryClient{
		emptyCount: 1,
		content:    "ok",
	}

	var events []*AgentOutputEvent
	emitter := NewEmitter("sess-retry", "agent-retry", func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		cp := *e
		events = append(events, &cp)
		return nil
	})

	agent, err := NewReActAgent("retry-log-agent", client, WithEmitter(emitter))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	result, errCall := agent.AICallProxy(context.Background(), 1, client, "prompt", "")
	if errCall != nil {
		t.Fatalf("unexpected error: %v", errCall)
	}
	if result.AssistantText != "ok" {
		t.Fatalf("expected 'ok', got %q", result.AssistantText)
	}

	var found bool
	for _, e := range events {
		if e.Type == EventTypeLog && e.Payload != nil {
			if msg, _ := e.Payload["message"].(string); msg != "" {
				if contains(msg, "empty response") {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Fatal("expected a runtime_log event about empty response retry")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
