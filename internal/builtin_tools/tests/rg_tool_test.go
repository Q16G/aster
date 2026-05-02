package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRgTool_FilesWithMatches_DefaultMode(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "src", "app.go"), "func main() { println(\"hello\") }\n")
	mustWriteFile(t, filepath.Join(repo, "src", "skip.txt"), "hello\n")

	tool := NewRgTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    filepath.Join(repo, "src"),
		"pattern": "hello",
		"type":    "go",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		Mode      string   `json:"mode"`
		NumFiles  int      `json:"num_files"`
		Filenames []string `json:"filenames"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Mode != "files_with_matches" {
		t.Fatalf("unexpected mode: %+v", resp)
	}
	if resp.NumFiles != 1 || len(resp.Filenames) != 1 || resp.Filenames[0] != "app.go" {
		t.Fatalf("unexpected file matches: %+v", resp)
	}
}

func TestRgTool_ContentMode_WithContext(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "x.txt"), "line1\nneedle\nline3\n")

	tool := NewRgTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":              repo,
		"pattern":           "needle",
		"output_mode":       "content",
		"context":           1,
		"show_line_numbers": true,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		Mode     string `json:"mode"`
		Content  string `json:"content"`
		NumLines int    `json:"num_lines"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Mode != "content" {
		t.Fatalf("unexpected mode: %+v", resp)
	}
	if !strings.Contains(resp.Content, "1-line1") && !strings.Contains(resp.Content, "1:line1") {
		t.Fatalf("expected line1 with line numbers, got %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "needle") || !strings.Contains(resp.Content, "line3") {
		t.Fatalf("expected matched content with context, got %q", resp.Content)
	}
	if resp.NumLines <= 0 {
		t.Fatalf("expected num_lines > 0, got %+v", resp)
	}
}

func TestRgTool_CountMode(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "a.txt"), "needle\nneedle\n")
	mustWriteFile(t, filepath.Join(repo, "b.txt"), "needle\n")

	tool := NewRgTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":        repo,
		"pattern":     "needle",
		"output_mode": "count",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		Mode       string `json:"mode"`
		NumFiles   int    `json:"num_files"`
		NumMatches int    `json:"num_matches"`
		Counts     []struct {
			Path  string `json:"path"`
			Count int    `json:"count"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Mode != "count" || resp.NumFiles != 2 || resp.NumMatches != 3 {
		t.Fatalf("unexpected count response: %+v", resp)
	}
}

func TestRgTool_FixedPatternMode(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "pipe.txt"), "alpha\nbeta\na|b\n")

	tool := NewRgTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":         repo,
		"pattern":      "a|b",
		"pattern_mode": "fixed",
		"output_mode":  "content",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp.Content, "a|b") || strings.Contains(resp.Content, "alpha") || strings.Contains(resp.Content, "beta") {
		t.Fatalf("unexpected fixed mode content: %q", resp.Content)
	}
}

func TestRgTool_Multiline(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "m.txt"), "alpha\nbeta\n")

	tool := NewRgTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":        repo,
		"pattern":     "alpha[\\s\\S]*beta",
		"output_mode": "content",
		"multiline":   true,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !strings.Contains(resp.Content, "alpha") || !strings.Contains(resp.Content, "beta") {
		t.Fatalf("expected multiline content, got %q", resp.Content)
	}
}

func TestRgTool_PathResolvesAgainstWorkspaceRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(workspaceRoot, "src", "app.go"), "needle\n")

	ctx := WithToolRuntime(context.Background(), ToolRuntimeInfo{
		Emitter:          noopEmitter{},
		WorkspaceRootDir: workspaceRoot,
	})

	tool := NewRgTool()
	out, err := tool.Execute(ctx, map[string]any{
		"path":    "src",
		"pattern": "needle",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		NumFiles  int      `json:"num_files"`
		Filenames []string `json:"filenames"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.NumFiles != 1 || len(resp.Filenames) != 1 || resp.Filenames[0] != "app.go" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestRgTool_RequiresPath(t *testing.T) {
	tool := NewRgTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"pattern": "needle",
	})
	if err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestRipgrepConfig_DefaultsToEmbeddedWhenEnvUnset(t *testing.T) {
	oldPath := os.Getenv("PATH")
	oldFlag, hadFlag := os.LookupEnv("USE_BUILTIN_RIPGREP")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		if hadFlag {
			_ = os.Setenv("USE_BUILTIN_RIPGREP", oldFlag)
		} else {
			_ = os.Unsetenv("USE_BUILTIN_RIPGREP")
		}
	})

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "rg"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(dir, "rg"), 0o755); err != nil {
		t.Fatalf("chmod fake rg: %v", err)
	}
	if err := os.Setenv("PATH", dir); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	if err := os.Unsetenv("USE_BUILTIN_RIPGREP"); err != nil {
		t.Fatalf("unset USE_BUILTIN_RIPGREP: %v", err)
	}

	cfg, err := RipgrepConfig()
	if err != nil {
		t.Fatalf("resolveRipgrepConfig: %v", err)
	}
	if cfg.Mode != "embedded" {
		t.Fatalf("expected embedded mode, got %+v", cfg)
	}
	if strings.TrimSpace(cfg.Command) == "" || cfg.Command == "rg" {
		t.Fatalf("expected embedded command path, got %+v", cfg)
	}
}

func TestRipgrepConfig_UsesSystemWhenBuiltinDisabled(t *testing.T) {
	oldPath := os.Getenv("PATH")
	oldFlag, hadFlag := os.LookupEnv("USE_BUILTIN_RIPGREP")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		if hadFlag {
			_ = os.Setenv("USE_BUILTIN_RIPGREP", oldFlag)
		} else {
			_ = os.Unsetenv("USE_BUILTIN_RIPGREP")
		}
	})

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "rg"), "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(dir, "rg"), 0o755); err != nil {
		t.Fatalf("chmod fake rg: %v", err)
	}
	if err := os.Setenv("PATH", dir); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	if err := os.Setenv("USE_BUILTIN_RIPGREP", "false"); err != nil {
		t.Fatalf("set USE_BUILTIN_RIPGREP: %v", err)
	}

	cfg, err := RipgrepConfig()
	if err != nil {
		t.Fatalf("resolveRipgrepConfig: %v", err)
	}
	if cfg.Mode != "system" || cfg.Command != "rg" {
		t.Fatalf("expected system rg, got %+v", cfg)
	}
}

func TestRgTool_UsesEmbeddedRipgrepWhenPathMissing(t *testing.T) {
	oldPath := os.Getenv("PATH")
	oldFlag, hadFlag := os.LookupEnv("USE_BUILTIN_RIPGREP")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
		if hadFlag {
			_ = os.Setenv("USE_BUILTIN_RIPGREP", oldFlag)
		} else {
			_ = os.Unsetenv("USE_BUILTIN_RIPGREP")
		}
	})

	if err := os.Setenv("PATH", ""); err != nil {
		t.Fatalf("set PATH: %v", err)
	}
	if err := os.Unsetenv("USE_BUILTIN_RIPGREP"); err != nil {
		t.Fatalf("unset USE_BUILTIN_RIPGREP: %v", err)
	}

	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "src", "app.go"), "needle\n")

	tool := NewRgTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":    repo,
		"pattern": "needle",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var resp struct {
		NumFiles  int      `json:"num_files"`
		Filenames []string `json:"filenames"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.NumFiles != 1 || len(resp.Filenames) != 1 || resp.Filenames[0] != "src/app.go" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
