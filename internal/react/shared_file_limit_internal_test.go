package react

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSharedFileLimitBytes(t *testing.T) {
	const hard = 20 * 1024
	cases := []struct {
		name    string
		cw      int
		wantMax int
	}{
		{"zero uses default", 0, min(hard, int(float64(defaultContextWindowTokens)*0.40*defaultCharsPerToken))},
		{"huge window → hard cap", 10_000_000, hard},
		{"128k window → 40% = 20480 == hard", 128_000, hard},               // 128000*0.4*4 = 204800 > 20480
		{"small 8k window → dynamic < hard", 8_000, 8_000 * 4 * 40 / 100}, // 12800
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sharedFileLimitBytes(tc.cw)
			if got != tc.wantMax {
				t.Errorf("sharedFileLimitBytes(%d) = %d, want %d", tc.cw, got, tc.wantMax)
			}
		})
	}
}

func TestReadSharedFileForPromptWithLimit_Truncated(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("一行数据\n", 2000) // ~12KB Chinese
	if err := os.WriteFile(filepath.Join(dir, "open_items.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	const limit = 1024
	result := readSharedFileForPromptWithLimit(dir, "open_items.md", limit)

	if !strings.Contains(result, "[截断]") {
		t.Error("truncated result must contain warning marker")
	}
	if !strings.Contains(result, "open_items.md") {
		t.Error("truncated result must contain file path hint")
	}
	// 截断后总长应超出 limit（因为有提示行），但实际内容部分不超过 limit。
	contentPart := result[:strings.Index(result, "[截断]")]
	if len(contentPart) > limit+10 {
		t.Errorf("content part too long: %d > %d", len(contentPart), limit)
	}
	// 内容部分必须是合法 UTF-8（不能在多字节字符中间截断）。
	if !utf8.ValidString(contentPart) {
		t.Error("content part must be valid UTF-8 after truncation")
	}
}

// TestReadSharedFileForPromptWithLimit_UTF8Boundary 专门校验在多字节中文字符边界处截断不产生破损 UTF-8。
func TestReadSharedFileForPromptWithLimit_UTF8Boundary(t *testing.T) {
	dir := t.TempDir()
	// 每个汉字 3 字节，limitBytes=10 会落在汉字中间。
	content := "AB一二三四五六七八九十" // 2 + 10*3 = 32 bytes
	if err := os.WriteFile(filepath.Join(dir, "f.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result := readSharedFileForPromptWithLimit(dir, "f.md", 10)
	if !strings.Contains(result, "[截断]") {
		t.Error("expected truncation marker")
	}
	contentPart := result[:strings.Index(result, "[截断]")]
	contentPart = strings.TrimSpace(contentPart)
	if !utf8.ValidString(contentPart) {
		t.Errorf("content part is invalid UTF-8: %q", contentPart)
	}
}

func TestReadSharedFileForPromptWithLimit_NoBigFile(t *testing.T) {
	dir := t.TempDir()
	small := "## 未解决\n- [OI-001] 测试\n"
	if err := os.WriteFile(filepath.Join(dir, "open_items.md"), []byte(small), 0o644); err != nil {
		t.Fatal(err)
	}

	result := readSharedFileForPromptWithLimit(dir, "open_items.md", 20*1024)
	if strings.Contains(result, "[截断]") {
		t.Error("small file must not trigger truncation warning")
	}
	if result != strings.TrimSpace(small) {
		t.Errorf("small file content mismatch: %q", result)
	}
}

func TestReadSharedFileForPromptWithLimit_ZeroLimit(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("x", 30*1024)
	if err := os.WriteFile(filepath.Join(dir, "big.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// limit=0 表示不截断
	result := readSharedFileForPromptWithLimit(dir, "big.md", 0)
	if strings.Contains(result, "[截断]") {
		t.Error("limit=0 must not truncate")
	}
	if len(result) != len(content) {
		t.Errorf("expected full content (%d), got %d", len(content), len(result))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
