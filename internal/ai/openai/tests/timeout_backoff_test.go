package openai_test

import (
	. "aster/internal/ai/openai"
	"testing"
	"time"
)

func TestAttemptTimeoutForAttempt_CapsAsExpected(t *testing.T) {
	// base=30s => cap=max(180s, 120s)=180s
	if got := AttemptTimeoutForAttempt(30*time.Second, 0); got != 30*time.Second {
		t.Fatalf("attempt0 timeout mismatch: got=%s want=%s", got, 30*time.Second)
	}
	if got := AttemptTimeoutForAttempt(30*time.Second, 1); got != 60*time.Second {
		t.Fatalf("attempt1 timeout mismatch: got=%s want=%s", got, 60*time.Second)
	}
	if got := AttemptTimeoutForAttempt(30*time.Second, 2); got != 120*time.Second {
		t.Fatalf("attempt2 timeout mismatch: got=%s want=%s", got, 120*time.Second)
	}
	if got := AttemptTimeoutForAttempt(30*time.Second, 3); got != 180*time.Second {
		t.Fatalf("attempt3 timeout mismatch: got=%s want=%s", got, 180*time.Second)
	}
	if got := AttemptTimeoutForAttempt(30*time.Second, 4); got != 180*time.Second {
		t.Fatalf("attempt4 timeout mismatch: got=%s want=%s", got, 180*time.Second)
	}

	// base=120s => cap=max(180s, 480s)=480s
	if got := AttemptTimeoutForAttempt(120*time.Second, 0); got != 120*time.Second {
		t.Fatalf("base=120 attempt0 mismatch: got=%s want=%s", got, 120*time.Second)
	}
	if got := AttemptTimeoutForAttempt(120*time.Second, 1); got != 240*time.Second {
		t.Fatalf("base=120 attempt1 mismatch: got=%s want=%s", got, 240*time.Second)
	}
	if got := AttemptTimeoutForAttempt(120*time.Second, 2); got != 480*time.Second {
		t.Fatalf("base=120 attempt2 mismatch: got=%s want=%s", got, 480*time.Second)
	}
	if got := AttemptTimeoutForAttempt(120*time.Second, 3); got != 480*time.Second {
		t.Fatalf("base=120 attempt3 mismatch: got=%s want=%s", got, 480*time.Second)
	}
}

func TestAttemptTimeoutForAttempt_ZeroBase(t *testing.T) {
	if got := AttemptTimeoutForAttempt(0, 0); got != 0 {
		t.Fatalf("expected 0 for base=0, got %s", got)
	}
	if got := AttemptTimeoutForAttempt(-1*time.Second, 2); got != 0 {
		t.Fatalf("expected 0 for base<0, got %s", got)
	}
}
