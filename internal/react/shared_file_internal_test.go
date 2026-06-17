package react

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSharedFileOptional(t *testing.T) {
	sharedDir := t.TempDir()
	abs := filepath.Join(sharedDir, "task_context.md")

	cases := []struct {
		name      string
		setup     func()
		sharedDir string
		fileName  string
		want      string
	}{
		{
			name:      "empty sharedDir → empty",
			sharedDir: "",
			fileName:  "task_context.md",
			want:      "",
		},
		{
			name:      "empty fileName → empty",
			sharedDir: sharedDir,
			fileName:  "",
			want:      "",
		},
		{
			name:      "file does not exist → empty",
			sharedDir: sharedDir,
			fileName:  "task_context.md",
			want:      "",
		},
		{
			name: "file empty → empty",
			setup: func() {
				if err := os.WriteFile(abs, []byte("\n  \t\n"), 0o644); err != nil {
					t.Fatalf("write empty file failed: %v", err)
				}
			},
			sharedDir: sharedDir,
			fileName:  "task_context.md",
			want:      "",
		},
		{
			name: "file has content → trimmed content",
			setup: func() {
				if err := os.WriteFile(abs, []byte("\n# 贯穿全程关键事实\n\n## 输入事实\n- 目标: x\n\n"), 0o644); err != nil {
					t.Fatalf("write content file failed: %v", err)
				}
			},
			sharedDir: sharedDir,
			fileName:  "task_context.md",
			want:      "# 贯穿全程关键事实\n\n## 输入事实\n- 目标: x",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup()
			} else {
				_ = os.Remove(abs)
			}
			got := readSharedFileOptional(tc.sharedDir, tc.fileName)
			if got != tc.want {
				t.Fatalf("readSharedFileOptional(%q, %q) = %q, want %q", tc.sharedDir, tc.fileName, got, tc.want)
			}
		})
	}
}

// TestReadSharedFileForPrompt_PreservesPlaceholders 锁定 readSharedFileForPrompt 仍返回中文占位
// 字符串——step_replan 阶段的 OPEN_ITEMS_LEDGER / TASK_CONTEXT_BOARD 注入依赖这些占位告诉模型文件状态。
func TestReadSharedFileForPrompt_PreservesPlaceholders(t *testing.T) {
	if got := readSharedFileForPrompt("", "x.md"); got != "(共享区不可用)" {
		t.Fatalf("empty sharedDir placeholder = %q", got)
	}
	dir := t.TempDir()
	if got := readSharedFileForPrompt(dir, "missing.md"); got != "(文件尚不存在)" {
		t.Fatalf("missing file placeholder = %q", got)
	}
	abs := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(abs, []byte("  \n"), 0o644); err != nil {
		t.Fatalf("write empty file failed: %v", err)
	}
	if got := readSharedFileForPrompt(dir, "empty.md"); got != "(文件为空)" {
		t.Fatalf("empty file placeholder = %q", got)
	}
}
