package react_test

import (
	"aster/internal/react"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func canonicalPathForTest(t *testing.T, path string) string {
	t.Helper()
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("Abs(%q) failed: %v", path, err)
	}
	return abs
}

func TestWorkspaceRootPath_CanonicalizesAsExpected(t *testing.T) {
	baseDir := t.TempDir()
	sessionID := "11111111-1111-1111-1111-111111111111"
	workspaceRoot := filepath.Join(baseDir, sessionID)
	if canonicalPathForTest(t, workspaceRoot) != canonicalPathForTest(t, filepath.Join(baseDir, sessionID)) {
		t.Fatalf("expected canonical workspace root under base dir")
	}
}

func TestTruncateToolOutput_UsesWorkspaceRootDir(t *testing.T) {
	baseDir := t.TempDir()
	sessionID := "22222222-2222-2222-2222-222222222222"
	workspaceRoot := filepath.Join(baseDir, sessionID)

	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	var builder strings.Builder
	for i := 0; i < 2200; i++ {
		builder.WriteString("line " + strings.Repeat("x", 40) + "\n")
	}
	out, _ := react.TruncateToolOutput("rg", builder.String(), workspaceRoot)

	wantDir := filepath.Join(workspaceRoot, "tool-output")
	if !strings.Contains(out, wantDir) {
		t.Fatalf("expected session tool-output path %q in output, got: %s", wantDir, out)
	}
	entries, err := os.ReadDir(wantDir)
	if err != nil {
		t.Fatalf("read session tool-output dir failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected persisted output file in session tool-output dir")
	}
}
