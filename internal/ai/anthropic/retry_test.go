package anthropic

import (
	"context"
	"fmt"
	"testing"
)

func TestHttpError_ErrorFormat(t *testing.T) {
	e := &httpError{StatusCode: 429, Body: "rate limited"}
	got := e.Error()
	want := "anthropic api error: status=429 body=rate limited"
	if got != want {
		t.Fatalf("httpError.Error() = %q, want %q", got, want)
	}
}

func TestHttpError_EmptyBody(t *testing.T) {
	e := &httpError{StatusCode: 500, Body: ""}
	got := e.Error()
	want := "anthropic api error: status=500 body="
	if got != want {
		t.Fatalf("httpError.Error() = %q, want %q", got, want)
	}
}

func TestIsRetryable_RetryableStatusCodes(t *testing.T) {
	for _, code := range []int{429, 500, 502, 503, 504} {
		err := &httpError{StatusCode: code, Body: "error"}
		if !isRetryable(err) {
			t.Errorf("expected status %d to be retryable", code)
		}
	}
}

func TestIsRetryable_NonRetryableStatusCodes(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 405, 409, 422} {
		err := &httpError{StatusCode: code, Body: "error"}
		if isRetryable(err) {
			t.Errorf("expected status %d to be non-retryable", code)
		}
	}
}

func TestIsRetryable_ContextDeadlineExceeded(t *testing.T) {
	if !isRetryable(context.DeadlineExceeded) {
		t.Fatal("expected context.DeadlineExceeded to be retryable")
	}
}

func TestIsRetryable_ContextCanceled(t *testing.T) {
	if isRetryable(context.Canceled) {
		t.Fatal("expected context.Canceled to be non-retryable")
	}
}

func TestIsRetryable_WrappedDeadlineExceeded(t *testing.T) {
	wrapped := fmt.Errorf("timeout: %w", context.DeadlineExceeded)
	if !isRetryable(wrapped) {
		t.Fatal("expected wrapped DeadlineExceeded to be retryable")
	}
}

func TestIsRetryable_HttpRequestError(t *testing.T) {
	err := fmt.Errorf("http request: connection refused")
	if !isRetryable(err) {
		t.Fatal("expected 'http request:' error to be retryable")
	}
}

func TestIsRetryable_GenericError(t *testing.T) {
	err := fmt.Errorf("some unknown error")
	if isRetryable(err) {
		t.Fatal("expected generic error to be non-retryable")
	}
}

func TestIsRetryable_NilErrorPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil error, but no panic occurred")
		}
	}()
	_ = isRetryable(nil)
}

func TestRetryableStatusCodes_Coverage(t *testing.T) {
	want := map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}
	if len(retryableStatusCodes) != len(want) {
		t.Fatalf("retryableStatusCodes length mismatch: got=%d want=%d", len(retryableStatusCodes), len(want))
	}
	for code, v := range want {
		if retryableStatusCodes[code] != v {
			t.Errorf("retryableStatusCodes[%d] = %v, want %v", code, retryableStatusCodes[code], v)
		}
	}
}
