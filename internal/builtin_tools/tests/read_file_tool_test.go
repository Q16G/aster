package builtin_tools_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "aster/internal/builtin_tools"
)

func TestReadFileTool_TextPagination(t *testing.T) {
	repo := t.TempDir()
	filePath := filepath.Join(repo, "x.txt")
	mustWriteFile(t, filePath, "line1\nline2\nline3\nline4\nline5\n")

	tool := NewReadFileTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":   filePath,
		"offset": 1,
		"limit":  2,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		OK           bool   `json:"ok"`
		Kind         string `json:"kind"`
		Content      string `json:"content"`
		Offset       int64  `json:"offset"`
		Limit        int64  `json:"limit"`
		ReturnedRows int64  `json:"returned_lines"`
		TotalLines   int64  `json:"total_lines"`
		HasMore      bool   `json:"has_more"`
		NextOffset   int64  `json:"next_offset"`
		Message      string `json:"message"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK || resp.Kind != "text" {
		t.Fatalf("unexpected resp: %s", out)
	}
	if resp.Content != "line2\nline3\n" {
		t.Fatalf("content = %q", resp.Content)
	}
	if resp.Offset != 1 || resp.Limit != 2 {
		t.Fatalf("unexpected page bounds: %s", out)
	}
	if resp.ReturnedRows != 2 || resp.TotalLines != 5 {
		t.Fatalf("unexpected row metadata: %s", out)
	}
	if !resp.HasMore || resp.NextOffset != 3 {
		t.Fatalf("expected next page metadata, got: %s", out)
	}
	if !strings.Contains(resp.Message, "offset=3") {
		t.Fatalf("expected paging hint, got %q", resp.Message)
	}
}

func TestReadFileTool_TextPaginationAtEOF(t *testing.T) {
	repo := t.TempDir()
	filePath := filepath.Join(repo, "x.txt")
	mustWriteFile(t, filePath, "line1\nline2\nline3\n")

	tool := NewReadFileTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":   filePath,
		"offset": 2,
		"limit":  5,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		OK           bool   `json:"ok"`
		Content      string `json:"content"`
		ReturnedRows int64  `json:"returned_lines"`
		TotalLines   int64  `json:"total_lines"`
		HasMore      bool   `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.ok=false: %s", out)
	}
	if resp.Content != "line3\n" {
		t.Fatalf("content = %q", resp.Content)
	}
	if resp.ReturnedRows != 1 || resp.TotalLines != 3 {
		t.Fatalf("unexpected pagination metadata: %s", out)
	}
	if resp.HasMore {
		t.Fatalf("did not expect more pages: %s", out)
	}
}

func TestReadFileTool_DirectoryPagination(t *testing.T) {
	repo := t.TempDir()
	mustWriteFile(t, filepath.Join(repo, "a.txt"), "a\n")
	mustWriteFile(t, filepath.Join(repo, "b.txt"), "b\n")
	mustWriteFile(t, filepath.Join(repo, "c.txt"), "c\n")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tool := NewReadFileTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path":   repo,
		"offset": 1,
		"limit":  2,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		OK              bool        `json:"ok"`
		Kind            string      `json:"kind"`
		ReturnedEntries int         `json:"returned_entries"`
		TotalEntries    int         `json:"total_entries"`
		HasMore         bool        `json:"has_more"`
		NextOffset      int         `json:"next_offset"`
		Entries         []FileEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK || resp.Kind != "directory" {
		t.Fatalf("unexpected resp: %s", out)
	}
	if resp.ReturnedEntries != 2 || resp.TotalEntries != 4 {
		t.Fatalf("unexpected directory pagination metadata: %s", out)
	}
	if !resp.HasMore || resp.NextOffset != 3 {
		t.Fatalf("expected directory next page metadata: %s", out)
	}
	if len(resp.Entries) != 2 || resp.Entries[0].Name != "b.txt" || resp.Entries[1].Name != "c.txt" {
		t.Fatalf("unexpected directory entries: %+v", resp.Entries)
	}
}

func TestReadFileTool_ImageMetadata(t *testing.T) {
	repo := t.TempDir()
	filePath := filepath.Join(repo, "pixel.png")
	mustWriteBytes(t, filePath, decodeBase64(t, "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO6qvVsAAAAASUVORK5CYII="))

	tool := NewReadFileTool()
	out, err := tool.Execute(context.Background(), map[string]any{
		"path": filePath,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var resp struct {
		OK               bool   `json:"ok"`
		Kind             string `json:"kind"`
		MIMEType         string `json:"mime_type"`
		SizeBytes        int64  `json:"size_bytes"`
		Width            int    `json:"width"`
		Height           int    `json:"height"`
		ContextAvailable bool   `json:"context_available"`
		Message          string `json:"message"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.OK || resp.Kind != "image" {
		t.Fatalf("unexpected image payload: %s", out)
	}
	if resp.MIMEType != "image/png" {
		t.Fatalf("mime_type = %q", resp.MIMEType)
	}
	if resp.SizeBytes <= 0 || resp.Width != 1 || resp.Height != 1 {
		t.Fatalf("unexpected image metadata: %s", out)
	}
	if !resp.ContextAvailable {
		t.Fatalf("expected context_available=true: %s", out)
	}
	if !strings.Contains(resp.Message, "视觉上下文") {
		t.Fatalf("expected visual-context hint, got %q", resp.Message)
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

func TestReadFileTool_RejectsBinaryFile(t *testing.T) {
	repo := t.TempDir()
	filePath := filepath.Join(repo, "data.bin")
	mustWriteBytes(t, filePath, []byte{0x00, 0x01, 0x02, 0x03})

	tool := NewReadFileTool()
	_, err := tool.Execute(context.Background(), map[string]any{
		"path": filePath,
	})
	if err == nil {
		t.Fatal("expected binary file error")
	}
	if !strings.Contains(err.Error(), "binary file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func decodeBase64(t *testing.T, raw string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	return data
}

func mustWriteBytes(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write bytes: %v", err)
	}
}
