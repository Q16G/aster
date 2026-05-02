package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestListFilesTool_Basic(t *testing.T) {
	repo := t.TempDir()

	mustWriteFile(t, filepath.Join(repo, "a.txt"), "hello\n")
	mustWriteFile(t, filepath.Join(repo, "sub", "b.go"), "package sub\n")
	mustWriteFile(t, filepath.Join(repo, "node_modules", "ignored.js"), "console.log('x')\n")

	tool := NewListFilesTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":         repo,
		"max_results":  1000,
		"max_depth":    10,
		"include_exts": []any{".txt", ".go", ".js"},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		OK      bool        `json:"ok"`
		Entries []FileEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.ok=false: %s", out)
	}
	names := make([]string, 0, len(resp.Entries))
	for _, entry := range resp.Entries {
		names = append(names, entry.Name)
	}

	if !slices.Contains(names, "a.txt") {
		t.Fatalf("missing a.txt: %#v", names)
	}
	if !slices.Contains(names, filepath.ToSlash(filepath.Join("sub", "b.go"))) {
		t.Fatalf("missing sub/b.go: %#v", names)
	}
	if slices.Contains(names, filepath.ToSlash(filepath.Join("node_modules", "ignored.js"))) {
		t.Fatalf("node_modules should be ignored: %#v", names)
	}
}

func TestListFilesTool_MaxOutputBytes(t *testing.T) {
	repo := t.TempDir()

	for i := 0; i < 120; i++ {
		name := fmt.Sprintf("very_long_file_name_%03d_with_extra_chars_for_payload_size.go", i)
		mustWriteFile(t, filepath.Join(repo, name), "package main\n")
	}

	tool := NewListFilesTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":             repo,
		"max_results":      5000,
		"max_output_bytes": 1200,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if len(out) > 1200 {
		t.Fatalf("output too long: len=%d", len(out))
	}

	var resp struct {
		OK             bool        `json:"ok"`
		Path           string      `json:"path"`
		Entries        []FileEntry `json:"entries"`
		Truncated      bool        `json:"truncated"`
		MaxOutputBytes int64       `json:"max_output_bytes"`
		Message        string      `json:"message"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.ok=false: %s", out)
	}
	if !resp.Truncated {
		t.Fatalf("expected truncated=true: %s", out)
	}
	if filepath.Clean(resp.Path) != filepath.Clean(repo) {
		t.Fatalf("expected response path=%q, got %q", repo, resp.Path)
	}
	if resp.MaxOutputBytes != 1200 {
		t.Fatalf("max_output_bytes = %d", resp.MaxOutputBytes)
	}
	if len(resp.Entries) == 0 {
		t.Fatalf("expected some entries in truncated result: %s", out)
	}
	if !strings.Contains(resp.Message, "max_output_bytes") {
		t.Fatalf("expected unified truncation message with max_output_bytes: %q", resp.Message)
	}
	if !strings.Contains(resp.Message, "...") {
		t.Fatalf("expected ellipsis hint in message: %q", resp.Message)
	}
}

func TestListFilesTool_RejectsRelativePath(t *testing.T) {
	tool := NewListFilesTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": ".",
	})
	if err == nil {
		t.Fatalf("expected relative path to be rejected")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("expected absolute path error, got %v", err)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
