package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/ai/anthropic"
)

func successResponse() map[string]any {
	return map[string]any{
		"id":          "msg_ok",
		"type":        "message",
		"role":        "assistant",
		"stop_reason": "end_turn",
		"content":     []map[string]any{{"type": "text", "text": "ok"}},
		"usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
	}
}

type callCounter struct {
	mu    sync.Mutex
	count int
}

func (c *callCounter) Inc() {
	c.mu.Lock()
	c.count++
	c.mu.Unlock()
}

func (c *callCounter) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

func newTestClient(url string, opts ...anthropic.Option) *anthropic.Client {
	base := []anthropic.Option{
		anthropic.WithURL(url),
		anthropic.WithModel("claude-test"),
		anthropic.WithTimeout(5 * time.Second),
	}
	base = append(base, opts...)
	return anthropic.NewClient(base...)
}

func TestChatEx_RetriesHTTP500UntilSuccess(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		if counter.Count() == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"type":"server_error","message":"internal error"}}`))
			return
		}
		json.NewEncoder(w).Encode(successResponse())
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(1))
	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if counter.Count() != 2 {
		t.Fatalf("expected 2 attempts, got %d", counter.Count())
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil || choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected choices: %#v", choices)
	}
}

func TestChatEx_RetriesHTTP429UntilSuccess(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		if counter.Count() == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"rate limited"}}`))
			return
		}
		json.NewEncoder(w).Encode(successResponse())
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(1))
	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if counter.Count() != 2 {
		t.Fatalf("expected 2 attempts, got %d", counter.Count())
	}
	if choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected content: %q", choices[0].Message.Content)
	}
}

func TestChatEx_RetriesHTTP502UntilSuccess(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		if counter.Count() == 1 {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("bad gateway"))
			return
		}
		json.NewEncoder(w).Encode(successResponse())
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(1))
	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if counter.Count() != 2 {
		t.Fatalf("expected 2 attempts, got %d", counter.Count())
	}
}

func TestChatEx_DoesNotRetryHTTP400(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad request"}}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(2))
	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if counter.Count() != 1 {
		t.Fatalf("expected 1 attempt (no retry for 400), got %d", counter.Count())
	}
	if strings.Contains(err.Error(), "max retries exceeded") {
		t.Fatalf("non-retryable 400 should not be wrapped with 'max retries exceeded', got %v", err)
	}
}

func TestChatEx_DoesNotRetryHTTP401(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(2))
	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if counter.Count() != 1 {
		t.Fatalf("expected 1 attempt (no retry for 401), got %d", counter.Count())
	}
	if strings.Contains(err.Error(), "max retries exceeded") {
		t.Fatalf("non-retryable 401 should not be wrapped with 'max retries exceeded', got %v", err)
	}
}

func TestChatEx_DoesNotRetryHTTP403(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"type":"permission_error","message":"forbidden"}}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(2))
	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if counter.Count() != 1 {
		t.Fatalf("expected 1 attempt (no retry for 403), got %d", counter.Count())
	}
	if strings.Contains(err.Error(), "max retries exceeded") {
		t.Fatalf("non-retryable 403 should not be wrapped with 'max retries exceeded', got %v", err)
	}
}

func TestChatEx_ReturnsMaxRetriesExceeded(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(1))
	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if !strings.Contains(err.Error(), "max retries exceeded") {
		t.Fatalf("expected 'max retries exceeded' error, got %v", err)
	}
	if counter.Count() != 2 {
		t.Fatalf("expected 2 attempts (1 initial + 1 retry), got %d", counter.Count())
	}
}

func TestChatEx_RetriesContextDeadlineExceeded(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		if counter.Count() == 1 {
			// Simulate a slow response that triggers per-attempt timeout
			time.Sleep(3 * time.Second)
			return
		}
		json.NewEncoder(w).Encode(successResponse())
	}))
	defer server.Close()

	// Use very short per-attempt timeout so first attempt times out
	client := newTestClient(server.URL, anthropic.WithTimeout(1*time.Second), anthropic.WithMaxRetries(1))
	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if counter.Count() != 2 {
		t.Fatalf("expected 2 attempts, got %d", counter.Count())
	}
	if choices[0].Message.Content != "ok" {
		t.Fatalf("unexpected content: %q", choices[0].Message.Content)
	}
}

func TestChatEx_StopsOnParentContextCancel(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		// Slow response so the cancel fires during the request or backoff
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(10))
	_, err := client.ChatEx(ctx, []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if counter.Count() >= 10 {
		t.Fatalf("expected cancel to stop retries early, but all %d attempts ran", counter.Count())
	}
}

func TestChatEx_BackoffDelayIncreasesPerAttempt(t *testing.T) {
	counter := &callCounter{}
	var timestamps []time.Time
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		counter.Inc()
		if counter.Count() <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
			return
		}
		json.NewEncoder(w).Encode(successResponse())
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(2))
	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if counter.Count() != 3 {
		t.Fatalf("expected 3 attempts, got %d", counter.Count())
	}

	mu.Lock()
	ts := append([]time.Time(nil), timestamps...)
	mu.Unlock()
	if len(ts) < 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(ts))
	}
	// Backoff: attempt 0 fail → wait 2^0=1s → attempt 1 fail → wait 2^1=2s → attempt 2 success
	gap1 := ts[1].Sub(ts[0])
	gap2 := ts[2].Sub(ts[1])
	if gap1 < 800*time.Millisecond {
		t.Fatalf("first backoff too short: %v (expected ~1s)", gap1)
	}
	if gap2 < 1800*time.Millisecond {
		t.Fatalf("second backoff too short: %v (expected ~2s)", gap2)
	}
	if gap2 <= gap1 {
		t.Fatalf("expected second backoff (%v) > first backoff (%v)", gap2, gap1)
	}
}

func TestChatEx_NoRetryWhenMaxRetriesZero(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(0))
	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err == nil {
		t.Fatal("expected error")
	}
	if counter.Count() != 1 {
		t.Fatalf("expected 1 attempt with MaxRetries=0, got %d", counter.Count())
	}
}

func TestChatStream_RetriesServerError(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		if counter.Count() == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("service unavailable"))
			return
		}
		json.NewEncoder(w).Encode(successResponse())
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(1))
	var content string
	err := client.ChatStream(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")}, func(delta *ai.StreamDelta, done bool) error {
		if delta != nil {
			content += delta.Content
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	if counter.Count() != 2 {
		t.Fatalf("expected 2 attempts, got %d", counter.Count())
	}
	if content != "ok" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestChatText_DoesNotRetryHTTP400(t *testing.T) {
	counter := &callCounter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Inc()
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad"}}`))
	}))
	defer server.Close()

	client := newTestClient(server.URL, anthropic.WithMaxRetries(2))
	_, err := client.ChatText(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error")
	}
	if counter.Count() != 1 {
		t.Fatalf("expected 1 attempt, got %d", counter.Count())
	}
}

func TestDefaultConfig_TimeoutIs300s(t *testing.T) {
	cfg := anthropic.DefaultConfig()
	if cfg.Timeout != 300*time.Second {
		t.Fatalf("expected default timeout 300s, got %v", cfg.Timeout)
	}
}

func TestDefaultConfig_MaxRetries(t *testing.T) {
	cfg := anthropic.DefaultConfig()
	if cfg.MaxRetries != 3 {
		t.Fatalf("expected default MaxRetries=3, got %d", cfg.MaxRetries)
	}
}
