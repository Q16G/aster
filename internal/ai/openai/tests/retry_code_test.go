package openai_test

import (
	. "aster/internal/ai/openai"
	"context"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"
)

func TestDefaultConfigRetryCodes(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.RetryCodes) == 0 {
		t.Fatalf("expected default retry codes")
	}

	want := []int{429, 500, 502, 503, 504}
	if len(cfg.RetryCodes) != len(want) {
		t.Fatalf("unexpected default retry codes length: got=%d want=%d", len(cfg.RetryCodes), len(want))
	}
	for i, code := range want {
		if cfg.RetryCodes[i] != code {
			t.Fatalf("unexpected default retry code at %d: got=%d want=%d", i, cfg.RetryCodes[i], code)
		}
	}
}

func TestWithRetryCode(t *testing.T) {
	cfg := DefaultConfig()
	WithRetryCode(429, 503, 503, 700, 99)(cfg)

	want := []int{429, 503}
	if len(cfg.RetryCodes) != len(want) {
		t.Fatalf("unexpected retry codes length: got=%d want=%d", len(cfg.RetryCodes), len(want))
	}
	for i, code := range want {
		if cfg.RetryCodes[i] != code {
			t.Fatalf("unexpected retry code at %d: got=%d want=%d", i, cfg.RetryCodes[i], code)
		}
	}
}

func TestIsRetryableError_DefaultRetryUnlessCanceled(t *testing.T) {
	if IsRetryableError(nil, nil) {
		t.Fatalf("expected nil error to be non-retryable")
	}

	if IsRetryableError(&HTTPError{StatusCode: 418, Body: "I'm a teapot"}, []int{429, 500}) {
		t.Fatalf("expected http 418 to be non-retryable when not configured in retry codes")
	}

	if !IsRetryableError(&HTTPError{StatusCode: 500, Body: "server error"}, []int{429, 500}) {
		t.Fatalf("expected http 500 to be retryable")
	}

	if !IsRetryableError(context.DeadlineExceeded, nil) {
		t.Fatalf("expected context deadline exceeded to be retryable")
	}

	if IsRetryableError(context.Canceled, nil) {
		t.Fatalf("expected context canceled to be non-retryable")
	}
}

func TestNormalizeRetryCodesFallback(t *testing.T) {
	codes := NormalizeRetryCodes([]int{700, 0})
	want := []int{429, 500, 502, 503, 504}
	if len(codes) != len(want) {
		t.Fatalf("unexpected fallback retry codes length: got=%d want=%d", len(codes), len(want))
	}
	for i, code := range want {
		if codes[i] != code {
			t.Fatalf("unexpected fallback retry code at %d: got=%d want=%d", i, codes[i], code)
		}
	}
}

func TestBuildRetryDecision_ConnectionReset(t *testing.T) {
	err := &net.OpError{
		Op:  "read",
		Net: "tcp",
		Err: fmt.Errorf("read: %w", syscall.ECONNRESET),
	}
	d := BuildRetryDecision(err, nil)
	if !d.Retry {
		t.Fatal("expected connection reset to be retryable")
	}
	if d.ReasonCode != "connection_interrupted" {
		t.Fatalf("expected reason code connection_interrupted, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_BrokenPipe(t *testing.T) {
	err := &net.OpError{
		Op:  "write",
		Net: "tcp",
		Err: fmt.Errorf("write: %w", syscall.EPIPE),
	}
	d := BuildRetryDecision(err, nil)
	if !d.Retry {
		t.Fatal("expected broken pipe to be retryable")
	}
}

func TestBuildRetryDecision_GenericErrorRetried(t *testing.T) {
	err := fmt.Errorf("some transient glitch")
	d := BuildRetryDecision(err, nil)
	if !d.Retry {
		t.Fatal("expected unrecognized error to be retryable (default retry)")
	}
	if d.ReasonCode != "provider_transient" {
		t.Fatalf("expected reason code provider_transient, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_ContextCanceledNotRetried(t *testing.T) {
	d := BuildRetryDecision(context.Canceled, nil)
	if d.Retry {
		t.Fatal("expected context.Canceled to not be retried")
	}
}

func TestBuildRetryDecision_AuthNotRetried(t *testing.T) {
	d := BuildRetryDecision(&HTTPError{StatusCode: 401, Body: "unauthorized"}, nil)
	if d.Retry {
		t.Fatal("expected HTTP 401 to not be retried")
	}
}

func TestBuildRetryDecision_QuotaNotRetried(t *testing.T) {
	d := BuildRetryDecision(&HTTPError{StatusCode: 402, Body: `{"error":{"message":"insufficient_quota"}}`}, nil)
	if d.Retry {
		t.Fatal("expected quota error to not be retried")
	}
}

func TestBuildRetryDecision_ContextOverflowNotRetried(t *testing.T) {
	d := BuildRetryDecision(&APIError{Message: "context_length_exceeded: too many tokens"}, nil)
	if d.Retry {
		t.Fatal("expected context overflow to not be retried")
	}
}

func TestBuildRetryDecision_EOF(t *testing.T) {
	d := BuildRetryDecision(io.EOF, nil)
	if !d.Retry {
		t.Fatal("expected io.EOF to be retryable")
	}
	if d.ReasonCode != "connection_interrupted" {
		t.Fatalf("expected reason code connection_interrupted, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_UnexpectedEOF(t *testing.T) {
	d := BuildRetryDecision(io.ErrUnexpectedEOF, nil)
	if !d.Retry {
		t.Fatal("expected io.ErrUnexpectedEOF to be retryable")
	}
	if d.ReasonCode != "connection_interrupted" {
		t.Fatalf("expected reason code connection_interrupted, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_WrappedEOF(t *testing.T) {
	err := fmt.Errorf("read body: %w", io.EOF)
	d := BuildRetryDecision(err, nil)
	if !d.Retry {
		t.Fatal("expected wrapped io.EOF to be retryable")
	}
}

func TestBuildRetryDecision_DeadlineExceeded(t *testing.T) {
	d := BuildRetryDecision(context.DeadlineExceeded, nil)
	if !d.Retry {
		t.Fatal("expected DeadlineExceeded to be retryable")
	}
	if d.ReasonCode != "request_timeout" {
		t.Fatalf("expected reason code request_timeout, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_WrappedCanceledNotRetried(t *testing.T) {
	err := fmt.Errorf("operation failed: %w", context.Canceled)
	d := BuildRetryDecision(err, nil)
	if d.Retry {
		t.Fatal("expected wrapped context.Canceled to not be retried")
	}
}

func TestBuildRetryDecision_NetTimeout(t *testing.T) {
	err := &timeoutError{msg: "i/o timeout"}
	d := BuildRetryDecision(err, nil)
	if !d.Retry {
		t.Fatal("expected net timeout to be retryable")
	}
	if d.ReasonCode != "request_timeout" {
		t.Fatalf("expected reason code request_timeout, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_NonOverflowAPIErrorRetried(t *testing.T) {
	d := BuildRetryDecision(&APIError{Message: "invalid_request_error: unknown parameter"}, nil)
	if !d.Retry {
		t.Fatal("expected non-overflow APIError to be retried (fallback)")
	}
	if d.ReasonCode != "provider_transient" {
		t.Fatalf("expected reason code provider_transient, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_HTTP403NotRetried(t *testing.T) {
	d := BuildRetryDecision(&HTTPError{StatusCode: 403, Body: "forbidden"}, nil)
	if d.Retry {
		t.Fatal("expected HTTP 403 to not be retried")
	}
	if d.ReasonCode != "provider_auth" {
		t.Fatalf("expected reason code provider_auth, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_HTTPBodyContextOverflow(t *testing.T) {
	d := BuildRetryDecision(&HTTPError{
		StatusCode: 400,
		Body:       `{"error":{"message":"This model's maximum context length is 128000 tokens"}}`,
	}, nil)
	if d.Retry {
		t.Fatal("expected HTTP with context overflow body to not be retried")
	}
}

func TestBuildRetryDecision_HTTP429RateLimit(t *testing.T) {
	d := BuildRetryDecision(&HTTPError{
		StatusCode: 429,
		Body:       `{"error":{"message":"Rate limit reached"}}`,
	}, nil)
	if !d.Retry {
		t.Fatal("expected HTTP 429 to be retryable")
	}
	if d.ReasonCode != "rate_limit_transient" {
		t.Fatalf("expected reason code rate_limit_transient, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_HTTP502Retried(t *testing.T) {
	d := BuildRetryDecision(&HTTPError{StatusCode: 502, Body: "Bad Gateway"}, nil)
	if !d.Retry {
		t.Fatal("expected HTTP 502 to be retryable")
	}
	if d.ReasonCode != "provider_transient" {
		t.Fatalf("expected reason code provider_transient, got %q", d.ReasonCode)
	}
}

func TestBuildRetryDecision_NilError(t *testing.T) {
	d := BuildRetryDecision(nil, nil)
	if d.Retry {
		t.Fatal("expected nil error to not be retried")
	}
	if d.ReasonCode != "" {
		t.Fatalf("expected empty reason code for nil, got %q", d.ReasonCode)
	}
}

// timeoutError implements net.Error with Timeout() == true.
type timeoutError struct {
	msg string
}

func (e *timeoutError) Error() string   { return e.msg }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
