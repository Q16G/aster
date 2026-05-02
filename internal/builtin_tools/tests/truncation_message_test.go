package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"strings"
	"testing"
)

func TestReadFileLargeTruncationMessage(t *testing.T) {
	msg := ReadFileLargeTruncationMessage(20000, 1024, 4096)
	if !strings.Contains(msg, "已返回前 1024 字节预览") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if !strings.Contains(msg, "省略 4096 字节") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if !strings.Contains(msg, "...") {
		t.Fatalf("expected ellipsis marker hint: %q", msg)
	}
}

func TestReadFileTruncationMessage(t *testing.T) {
	msg := ReadFileTruncationMessage(2048)
	if !strings.Contains(msg, "max_bytes=2048") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if !strings.Contains(msg, "...") {
		t.Fatalf("expected ellipsis hint: %q", msg)
	}
}

func TestRgTruncationMessage(t *testing.T) {
	msg := RgTruncationMessage(20000)
	if !strings.Contains(msg, "capture_limit_bytes=20000") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if !strings.Contains(msg, "...") {
		t.Fatalf("expected ellipsis hint: %q", msg)
	}
}

func TestListFilesTruncationMessage(t *testing.T) {
	msg := ListFilesTruncationMessage(4096)
	if !strings.Contains(msg, "max_output_bytes=4096") {
		t.Fatalf("unexpected message: %q", msg)
	}
	if !strings.Contains(msg, "...") {
		t.Fatalf("expected ellipsis hint: %q", msg)
	}
}

func TestAppendTruncationMarker(t *testing.T) {
	if got := AppendTruncationMarker(""); got != "..." {
		t.Fatalf("unexpected empty marker result: %q", got)
	}
	if got := AppendTruncationMarker("line1\nline2"); got != "line1\nline2\n..." {
		t.Fatalf("unexpected marker result: %q", got)
	}
	if got := AppendTruncationMarker("line1\n..."); got != "line1\n..." {
		t.Fatalf("unexpected marker idempotency: %q", got)
	}
}
