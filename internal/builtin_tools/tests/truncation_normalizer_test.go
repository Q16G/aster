package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeToolStructuredOutput_ReadFile(t *testing.T) {
	in := `{"ok":true,"truncated":true,"content":"line2","max_bytes":5}`
	out := NormalizeToolStructuredOutput(ReadFileToolName, in)

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	content, _ := payload["content"].(string)
	if !strings.HasSuffix(content, "...") {
		t.Fatalf("expected read_file content with ellipsis, got %q", content)
	}
	msg, _ := payload["message"].(string)
	if !strings.Contains(msg, "max_bytes") {
		t.Fatalf("expected read_file message with max_bytes, got %q", msg)
	}
}

func TestNormalizeToolStructuredOutput_ListFiles(t *testing.T) {
	in := `{"ok":true,"truncated":true,"max_output_bytes":1200,"entries":[{"name":"a.txt"}]}`
	out := NormalizeToolStructuredOutput(ListFilesToolName, in)

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	msg, _ := payload["message"].(string)
	if !strings.Contains(msg, "max_output_bytes=1200") {
		t.Fatalf("expected list_files message with max_output_bytes, got %q", msg)
	}
}

func TestNormalizeToolStructuredOutput_Rg(t *testing.T) {
	in := `{"ok":true,"truncated":true,"capture_limit_bytes":256,"content":"line1\nline2"}`
	out := NormalizeToolStructuredOutput(RgToolName, in)

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	content, _ := payload["content"].(string)
	if !strings.HasSuffix(content, "...") {
		t.Fatalf("expected rg content suffix ellipsis, got %q", content)
	}
	msg, _ := payload["message"].(string)
	if !strings.Contains(msg, "capture_limit_bytes=256") {
		t.Fatalf("expected rg message with capture_limit_bytes, got %q", msg)
	}
}
