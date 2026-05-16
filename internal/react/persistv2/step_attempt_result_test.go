package persistv2

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestStepAttemptResultPath_Direct(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root, "test-session")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := store.StepAttemptResultPath("step-1", "call_abc123")
	if err != nil {
		t.Fatalf("StepAttemptResultPath: %v", err)
	}
	if !strings.HasSuffix(p, filepath.Join("step-1", "attempts", "call_abc123", "result.json")) {
		t.Fatalf("unexpected path: %s", p)
	}
}

func TestStepAttemptResultPath_RejectsEmpty(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root, "test-session")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := store.StepAttemptResultPath("step-1", ""); err == nil {
		t.Fatal("expected error for empty attemptID")
	}
	if _, err := store.StepAttemptResultPath("", "call_abc"); err == nil {
		t.Fatal("expected error for empty stepID")
	}
}
