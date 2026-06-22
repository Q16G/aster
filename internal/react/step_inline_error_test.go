package react

import (
	"context"
	"fmt"
	"testing"
)

func TestIsFatalStepError(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		fatal bool
	}{
		{"nil", nil, false},
		{"ctx canceled", context.Canceled, true},
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"wrapped ctx canceled", fmt.Errorf("op failed: %w", context.Canceled), true},
		{"context overflow text", fmt.Errorf("This model's maximum context length is 128000 tokens"), true},
		{"http 520 recoverable", fmt.Errorf("HTTP 520: error code: 520"), false},
		{"parse failed recoverable", fmt.Errorf("validation_failed: missing field"), false},
		{"empty output recoverable", fmt.Errorf("step produced no tool calls and empty content"), false},
		{"http 401 auth fatal", fmt.Errorf("HTTP 401: unauthorized"), true},
		{"http 403 forbidden fatal", fmt.Errorf("HTTP 403: forbidden"), true},
		{"http 402 quota fatal", fmt.Errorf("HTTP 402: insufficient_quota"), true},
		{"http 429 ratelimit recoverable", fmt.Errorf("HTTP 429: rate limit reached"), false},
	}
	for _, c := range cases {
		if got := isFatalStepError(c.err); got != c.fatal {
			t.Errorf("%s: isFatalStepError=%v want=%v", c.name, got, c.fatal)
		}
	}
}

func TestIsContextOverflowError(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		overflow bool
	}{
		{"nil", nil, false},
		{"context_length_exceeded", fmt.Errorf("context_length_exceeded"), true},
		{"maximum context length", fmt.Errorf("This model's maximum context length is 200000 tokens"), true},
		{"token limit", fmt.Errorf("token limit reached"), true},
		{"plain 500", fmt.Errorf("HTTP 500: server error"), false},
		{"plain 520", fmt.Errorf("HTTP 520: error code: 520"), false},
	}
	for _, c := range cases {
		if got := isContextOverflowError(c.err); got != c.overflow {
			t.Errorf("%s: isContextOverflowError=%v want=%v", c.name, got, c.overflow)
		}
	}
}
