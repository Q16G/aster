package openai_test

import (
	. "aster/internal/ai/openai"
	"context"
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
