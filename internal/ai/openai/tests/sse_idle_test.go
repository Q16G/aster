package openai_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aster/internal/ai/openai"
)

func TestSSEIdleBlocking_ContextCancelInterruptsScanner(t *testing.T) {
	serverReady := make(chan struct{})
	serverHanging := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send 3 normal SSE chunks
		for i := 0; i < 3; i++ {
			chunk := fmt.Sprintf(`data: {"choices":[{"index":0,"delta":{"content":"chunk%d "}}]}`, i)
			fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}

		close(serverReady)

		// Now hang — don't send [DONE], don't close connection
		<-serverHanging
	}))
	defer server.Close()
	defer close(serverHanging)

	client := openai.NewClient(
		openai.WithURL(server.URL),
		openai.WithAPIKey("test-key"),
		openai.WithModel("test-model"),
		openai.WithStream(true),
		openai.WithTimeout(3*time.Second),
		openai.WithMaxRetries(0),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := client.ChatText(ctx, "hello")
	elapsed := time.Since(start)

	t.Logf("=== SSE Idle Blocking Test ===")
	t.Logf("Elapsed:  %v", elapsed)
	t.Logf("Error:    %v", err)

	if err == nil {
		t.Fatal("expected error from hanging SSE stream, but got nil")
	}

	// Check if it returned within a reasonable time (timeout should fire at ~3s)
	if elapsed > 10*time.Second {
		t.Fatalf("SSE read blocked for %v — context cancel did NOT interrupt scanner.Scan(). "+
			"This confirms the root cause: ParseStreamResponse blocks indefinitely on hanging SSE streams", elapsed)
	}

	errStr := strings.ToLower(err.Error())
	isContextErr := strings.Contains(errStr, "context") ||
		strings.Contains(errStr, "deadline") ||
		strings.Contains(errStr, "canceled") ||
		strings.Contains(errStr, "cancelled")

	if isContextErr {
		t.Logf("RESULT: Context cancellation DOES interrupt SSE reading (elapsed=%v). "+
			"The per-attempt timeout mechanism works correctly.", elapsed)
	} else {
		t.Logf("RESULT: SSE read was interrupted but not by context cancel (err=%v). "+
			"Investigate further.", err)
	}
}

func TestSSEIdleBlocking_MeasureRetryEscalation(t *testing.T) {
	hangDurations := make([]time.Duration, 0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		flusher.Flush()

		// Hang forever (until client disconnects)
		<-r.Context().Done()
	}))
	defer server.Close()

	// Use a short base timeout to avoid long test runs
	baseTimeout := 2 * time.Second

	client := openai.NewClient(
		openai.WithURL(server.URL),
		openai.WithAPIKey("test-key"),
		openai.WithModel("test-model"),
		openai.WithStream(true),
		openai.WithTimeout(baseTimeout),
		openai.WithMaxRetries(3),
	)

	start := time.Now()
	_, err := client.ChatText(context.Background(), "hello")
	totalElapsed := time.Since(start)

	t.Logf("=== Retry Escalation Measurement ===")
	t.Logf("Base timeout:    %v", baseTimeout)
	t.Logf("Total elapsed:   %v", totalElapsed)
	t.Logf("Error:           %v", err)
	_ = hangDurations

	// With base=2s, max retries=3:
	// attempt 0: timeout=2s
	// attempt 1: timeout=4s, backoff=2s
	// attempt 2: timeout=8s, backoff=4s  (but cap=8s)
	// attempt 3: timeout=8s, backoff=8s  (but backoff cap=30s)
	// Total timeouts: 2+4+8+8 = 22s, plus backoffs: 2+4+8 = 14s → ~36s total
	// With base=2s, cap=max(2*4,180s)=180s, so actual cap=180s
	// attempt 0: 2s, attempt 1: 4s, attempt 2: 8s, attempt 3: 16s
	// Actual: depends on AttemptTimeoutForAttempt logic

	for attempt := 0; attempt <= 3; attempt++ {
		timeout := openai.AttemptTimeoutForAttempt(baseTimeout, attempt)
		t.Logf("  attempt %d: timeout=%v", attempt, timeout)
	}

	// With real production config (base=300s):
	t.Logf("")
	t.Logf("=== Production Timeout Escalation (base=300s) ===")
	prodBase := 300 * time.Second
	totalProd := time.Duration(0)
	for attempt := 0; attempt <= 3; attempt++ {
		timeout := openai.AttemptTimeoutForAttempt(prodBase, attempt)
		totalProd += timeout
		t.Logf("  attempt %d: timeout=%v (cumulative=%v)", attempt, timeout, totalProd)
	}
	t.Logf("  Total silent wait (timeouts only): %v = %.0f minutes", totalProd, totalProd.Minutes())

	if totalProd > 50*time.Minute {
		t.Logf("  WARNING: Total retry timeout exceeds 50 minutes. " +
			"This explains the 2h+ hang when combined with backoff delays and multiple retry cycles.")
	}
}
