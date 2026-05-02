package openai_test

import (
	. "aster/internal/ai/openai"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"aster/internal/ai"
)

type retryRoundTripStep struct {
	err        error
	statusCode int
	body       string
	header     http.Header
}

type retryRoundTripper struct {
	mu        sync.Mutex
	callCount int
	steps     []retryRoundTripStep
}

func (rt *retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	stepIndex := rt.callCount
	rt.callCount++

	step := retryRoundTripStep{}
	if stepIndex < len(rt.steps) {
		step = rt.steps[stepIndex]
	} else if len(rt.steps) > 0 {
		step = rt.steps[len(rt.steps)-1]
	}

	if step.err != nil {
		return nil, step.err
	}

	statusCode := step.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}

	header := make(http.Header)
	for key, values := range step.header {
		for _, value := range values {
			header.Add(key, value)
		}
	}

	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(step.body)),
		Header:     header,
	}, nil
}

func (rt *retryRoundTripper) Calls() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.callCount
}

func newRetryTestClient(transport http.RoundTripper, opts ...Option) *Client {
	baseOpts := []Option{
		WithURL("https://example.com/v1/chat/completions"),
		WithURLAutoComplete(false),
		WithModel("gpt-4o-mini"),
		WithHTTPClient(&http.Client{Transport: transport}),
	}
	baseOpts = append(baseOpts, opts...)
	return NewClient(baseOpts...)
}

func TestChatEx_RetriesPlainErrorsUntilSuccess(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{err: io.EOF},
			{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if transport.Calls() != 2 {
		t.Fatalf("expected 2 attempts, got %d", transport.Calls())
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil || choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected choices: %#v", choices)
	}
}

func TestChatEx_RetriesHTTP500UntilSuccess(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{statusCode: http.StatusInternalServerError, body: `server error`},
			{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"recovered"}}]}`},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if transport.Calls() != 2 {
		t.Fatalf("expected 2 attempts, got %d", transport.Calls())
	}
	if got := choices[0].Message.Content; got != "recovered" {
		t.Fatalf("expected recovered content, got %#v", got)
	}
}

func TestEmbedding_RetriesHTTP500UntilSuccess(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{statusCode: http.StatusInternalServerError, body: `server error`},
			{body: `{"data":[{"embedding":[1.5,2.5]}]}`},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	embeddings, err := client.Embedding(context.Background(), []string{"hello"}, "text-embedding-3-small")
	if err != nil {
		t.Fatalf("Embedding failed: %v", err)
	}
	if transport.Calls() != 2 {
		t.Fatalf("expected 2 attempts, got %d", transport.Calls())
	}
	if len(embeddings) != 1 || len(embeddings[0]) != 2 {
		t.Fatalf("unexpected embeddings shape: %#v", embeddings)
	}
	if embeddings[0][0] != 1.5 || embeddings[0][1] != 2.5 {
		t.Fatalf("unexpected embeddings values: %#v", embeddings)
	}
}

func TestChatEx_DoesNotRetryHTTP400(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{statusCode: http.StatusBadRequest, body: `bad request`},
			{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"should-not-happen"}}]}`},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err == nil {
		t.Fatalf("expected error")
	}
	if transport.Calls() != 1 {
		t.Fatalf("expected 1 attempt, got %d", transport.Calls())
	}
}

func TestChatEx_RetriesContextDeadlineExceededUntilSuccess(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{err: context.DeadlineExceeded},
			{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"retry-ok"}}]}`},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if transport.Calls() != 2 {
		t.Fatalf("expected 2 attempts, got %d", transport.Calls())
	}
	if got := choices[0].Message.Content; got != "retry-ok" {
		t.Fatalf("expected retry-ok content, got %#v", got)
	}
}

func TestChatEx_DoesNotRetryContextCanceled(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{err: context.Canceled},
			{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"should-not-happen"}}]}`},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
	if transport.Calls() != 1 {
		t.Fatalf("expected 1 attempt, got %d", transport.Calls())
	}
}

func TestChatEx_ReturnsMaxRetriesExceeded(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{err: io.ErrUnexpectedEOF},
			{err: io.ErrUnexpectedEOF},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "max retries exceeded") {
		t.Fatalf("expected max retries exceeded error, got %v", err)
	}
	if transport.Calls() != 2 {
		t.Fatalf("expected 2 attempts, got %d", transport.Calls())
	}
}

func TestChatText_UsesRetryFlow(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{err: io.EOF},
			{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"chat-text-ok"}}]}`},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	resp, err := client.ChatText(context.Background(), "hello")
	if err != nil {
		t.Fatalf("ChatText failed: %v", err)
	}
	if resp != "chat-text-ok" {
		t.Fatalf("expected chat-text-ok, got %q", resp)
	}
	if transport.Calls() != 2 {
		t.Fatalf("expected 2 attempts, got %d", transport.Calls())
	}
}

func TestChatStream_UsesRetryFlow(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{err: context.DeadlineExceeded},
			{
				body: strings.Join([]string{
					`data: {"choices":[{"index":0,"delta":{"role":"assistant"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
					`data: {"choices":[{"index":0,"delta":{"content":"stream-ok"},"finish_reason":"stop"}]}`,
					`data: [DONE]`,
					"",
				}, "\n"),
				header: http.Header{
					"Content-Type": []string{"text/event-stream; charset=utf-8"},
				},
			},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	var contentBuilder strings.Builder
	err := client.ChatStream(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")}, func(delta *ai.StreamDelta, done bool) error {
		if done || delta == nil {
			return nil
		}
		contentBuilder.WriteString(delta.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	if transport.Calls() != 2 {
		t.Fatalf("expected 2 attempts, got %d", transport.Calls())
	}
	if got := contentBuilder.String(); got != "stream-ok" {
		t.Fatalf("expected stream-ok, got %q", got)
	}
}

func TestChatEx_LogsRetryAttempt(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{err: io.EOF},
			{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	out := captureOpenAILogOutput(t, func() {
		if _, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")}); err != nil {
			t.Fatalf("ChatEx failed: %v", err)
		}
	})

	if !strings.Contains(out, "[openai.retry]") {
		t.Fatalf("expected retry log, got %q", out)
	}
	if !strings.Contains(out, "current_attempt=1") || !strings.Contains(out, "next_attempt=2") {
		t.Fatalf("expected retry attempt metadata, got %q", out)
	}
}

func TestChatEx_DoesNotLogRetryForCanceled(t *testing.T) {
	transport := &retryRoundTripper{
		steps: []retryRoundTripStep{
			{err: context.Canceled},
		},
	}
	client := newRetryTestClient(transport, WithStream(false), WithMaxRetries(1))

	out := captureOpenAILogOutput(t, func() {
		_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got %v", err)
		}
	})

	if strings.Contains(out, "[openai.retry]") {
		t.Fatalf("did not expect retry log for canceled request, got %q", out)
	}
}
