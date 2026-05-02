package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileTool_Range(t *testing.T) {
	repo := t.TempDir()
	filePath := filepath.Join(repo, "x.txt")
	mustWriteFile(t, filePath, "line1\nline2\nline3\n")

	tool := NewReadFileTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":       filePath,
		"start_line": 2,
		"end_line":   3,
		"max_bytes":  1000,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		OK        bool   `json:"ok"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.ok=false: %s", out)
	}
	if resp.Truncated {
		t.Fatalf("unexpected truncated: %s", out)
	}
	if resp.Content != "line2\nline3\n" {
		t.Fatalf("content = %q", resp.Content)
	}
}

func TestReadFileTool_MaxBytes(t *testing.T) {
	repo := t.TempDir()
	filePath := filepath.Join(repo, "x.txt")
	mustWriteFile(t, filePath, "line1\nline2\nline3\n")

	tool := NewReadFileTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":       filePath,
		"start_line": 2,
		"end_line":   2,
		"max_bytes":  5,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		OK        bool   `json:"ok"`
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.ok=false: %s", out)
	}
	if !resp.Truncated {
		t.Fatalf("expected truncated: %s", out)
	}
	if resp.Content != "line2\n..." {
		t.Fatalf("content = %q", resp.Content)
	}
	if !strings.Contains(resp.Message, "max_bytes") {
		t.Fatalf("expected truncation message with max_bytes, got %q", resp.Message)
	}
}

func TestReadFileTool_LargeFileMarked(t *testing.T) {
	repo := t.TempDir()
	filePath := filepath.Join(repo, "large.txt")
	content := strings.Repeat("a", 20001)
	mustWriteFile(t, filePath, content)

	tool := NewReadFileTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path": filePath,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		OK             bool   `json:"ok"`
		LargeFile      bool   `json:"large_file"`
		ThresholdBytes int64  `json:"threshold_bytes"`
		SizeBytes      int64  `json:"size_bytes"`
		PreviewBytes   int64  `json:"preview_bytes"`
		OmittedBytes   int64  `json:"omitted_bytes"`
		Truncated      bool   `json:"truncated"`
		Content        string `json:"content"`
		Message        string `json:"message"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.ok=false: %s", out)
	}
	if !resp.LargeFile {
		t.Fatalf("expected large_file=true: %s", out)
	}
	if resp.ThresholdBytes != 20000 {
		t.Fatalf("threshold_bytes = %d", resp.ThresholdBytes)
	}
	if resp.SizeBytes != int64(len(content)) {
		t.Fatalf("size_bytes = %d", resp.SizeBytes)
	}
	if !resp.Truncated {
		t.Fatalf("expected truncated=true: %s", out)
	}
	if resp.Content == "" {
		t.Fatalf("expected non-empty preview content, got empty")
	}
	if !strings.Contains(resp.Content, "...") {
		t.Fatalf("expected ellipsis marker in content: %q", resp.Content)
	}
	if resp.PreviewBytes <= 0 {
		t.Fatalf("expected preview_bytes > 0, got %d", resp.PreviewBytes)
	}
	if resp.OmittedBytes <= 0 {
		t.Fatalf("expected omitted_bytes > 0, got %d", resp.OmittedBytes)
	}
	if !strings.Contains(resp.Message, "rg") {
		t.Fatalf("expected rg guidance in message: %q", resp.Message)
	}
	if !strings.Contains(resp.Message, "已返回前") {
		t.Fatalf("expected preview hint in message: %q", resp.Message)
	}
}

func TestReadFileTool_RejectsRelativePath(t *testing.T) {
	tool := NewReadFileTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": "relative/path.txt",
	})
	if err == nil {
		t.Fatalf("expected error for relative path")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("expected absolute path error, got: %v", err)
	}
}

func TestReadFileTool_RejectsNonExistentFile(t *testing.T) {
	tool := NewReadFileTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": "/tmp/nonexistent-file-abc123.txt",
	})
	if err == nil {
		t.Fatalf("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "open file") {
		t.Fatalf("expected open file error, got: %v", err)
	}
}
