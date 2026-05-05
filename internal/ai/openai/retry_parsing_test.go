package openai

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfterHeaderMilliseconds(t *testing.T) {
	got := parseRetryAfterHeader(http.Header{
		"Retry-After-Ms": []string{"1250"},
	})
	if got != 1250*time.Millisecond {
		t.Fatalf("unexpected retry-after-ms: got=%s want=%s", got, 1250*time.Millisecond)
	}
}

func TestParseRetryAfterHeaderSeconds(t *testing.T) {
	got := parseRetryAfterHeader(http.Header{
		"Retry-After": []string{"2"},
	})
	if got != 2*time.Second {
		t.Fatalf("unexpected retry-after seconds: got=%s want=%s", got, 2*time.Second)
	}
}

func TestParseRetryAfterHeaderHTTPDate(t *testing.T) {
	retryAt := time.Now().Add(1500 * time.Millisecond).UTC()
	got := parseRetryAfterHeader(http.Header{
		"Retry-After": []string{retryAt.Format(http.TimeFormat)},
	})
	if got < 500*time.Millisecond || got > 2500*time.Millisecond {
		t.Fatalf("unexpected retry-after date duration: got=%s", got)
	}
}
