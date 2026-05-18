package react_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "aster/internal/react"
)

func TestTruncateToolOutput_NoTruncate(t *testing.T) {
	got, truncated := TruncateToolOutput("demo", "ok\n", "")
	if got != "ok\n" {
		t.Fatalf("unexpected content: %q", got)
	}
	if truncated {
		t.Fatal("expected no truncation")
	}
}

func TestTruncateToolOutput_WritesToWorkspaceToolOutputDir(t *testing.T) {
	workspaceRoot := t.TempDir()
	lines := make([]string, 0, 2105)
	for idx := 0; idx < 2105; idx++ {
		lines = append(lines, "line")
	}

	got, truncated := TruncateToolOutput("demo", strings.Join(lines, "\n"), workspaceRoot)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if !strings.Contains(got, "The tool call succeeded but the output was truncated.") {
		t.Fatalf("expected truncation hint, got %q", got)
	}

	wantDir, err := filepath.Abs(filepath.Join(workspaceRoot, "tool-output"))
	if err != nil {
		t.Fatalf("Abs failed: %v", err)
	}
	entries, err := os.ReadDir(wantDir)
	if err != nil {
		t.Fatalf("expected tool-output dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one saved file, got %d", len(entries))
	}
	if !strings.Contains(got, filepath.Join(wantDir, entries[0].Name())) {
		t.Fatalf("expected output path in content, got %q", got)
	}
}

func TestTruncateToolOutput_UsesTempFallbackDir(t *testing.T) {
	lines := make([]string, 0, 2105)
	for idx := 0; idx < 2105; idx++ {
		lines = append(lines, "line")
	}

	got, _ := TruncateToolOutput("demo", strings.Join(lines, "\n"), "")
	if !strings.Contains(got, filepath.Join(os.TempDir(), "sastpro-tool-output")) {
		t.Fatalf("expected temp fallback dir in content, got %q", got)
	}
}
