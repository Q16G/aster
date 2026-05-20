package anthropic

import (
	"context"
	"fmt"
	"net"
	"syscall"
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
	for _, code := range []int{400, 401, 403} {
		err := &httpError{StatusCode: code, Body: "error"}
		if isRetryable(err) {
			t.Errorf("expected status %d to be non-retryable", code)
		}
	}
}

func TestIsRetryable_OtherHTTPStatusCodesRetried(t *testing.T) {
	for _, code := range []int{404, 405, 409, 422, 408, 502} {
		err := &httpError{StatusCode: code, Body: "error"}
		if !isRetryable(err) {
			t.Errorf("expected status %d to be retryable (default retry)", code)
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
	if !isRetryable(err) {
		t.Fatal("expected generic error to be retryable (default retry)")
	}
}

func TestIsRetryable_NilError(t *testing.T) {
	if isRetryable(nil) {
		t.Fatal("expected nil error to be non-retryable")
	}
}

func TestIsRetryable_ConnectionReset(t *testing.T) {
	err := &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: fmt.Errorf("read: %w", syscall.ECONNRESET),
	}
	if !isRetryable(err) {
		t.Fatal("expected connection reset to be retryable")
	}
}

func TestIsRetryable_EOF(t *testing.T) {
	err := fmt.Errorf("read body: %w", fmt.Errorf("unexpected EOF"))
	if !isRetryable(err) {
		t.Fatal("expected EOF to be retryable")
	}
}

func TestIsRetryable_WrappedCanceled(t *testing.T) {
	err := fmt.Errorf("operation failed: %w", context.Canceled)
	if isRetryable(err) {
		t.Fatal("expected wrapped context.Canceled to be non-retryable")
	}
}

func TestIsRetryable_BrokenPipe(t *testing.T) {
	err := &net.OpError{
		Op:  "write",
		Net: "tcp",
		Err: fmt.Errorf("write: %w", syscall.EPIPE),
	}
	if !isRetryable(err) {
		t.Fatal("expected broken pipe to be retryable")
	}
}

func TestIsRetryable_HTTP529Overloaded(t *testing.T) {
	err := &httpError{StatusCode: 529, Body: "overloaded"}
	if !isRetryable(err) {
		t.Fatal("expected HTTP 529 (overloaded) to be retryable")
	}
}

func TestIsRetryable_HTTP402(t *testing.T) {
	err := &httpError{StatusCode: 402, Body: "payment required"}
	if !isRetryable(err) {
		t.Fatal("expected HTTP 402 to be retryable (not in blacklist)")
	}
}

func TestNonRetryableStatusCodes_Coverage(t *testing.T) {
	want := map[int]bool{400: true, 401: true, 403: true}
	if len(nonRetryableStatusCodes) != len(want) {
		t.Fatalf("nonRetryableStatusCodes length mismatch: got=%d want=%d", len(nonRetryableStatusCodes), len(want))
	}
	for code, v := range want {
		if nonRetryableStatusCodes[code] != v {
			t.Errorf("nonRetryableStatusCodes[%d] = %v, want %v", code, nonRetryableStatusCodes[code], v)
		}
	}
}
