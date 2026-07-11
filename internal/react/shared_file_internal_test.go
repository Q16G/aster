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
			got := readSharedFileOptional(nil, tc.sharedDir, tc.fileName)
			if got != tc.want {
				t.Fatalf("readSharedFileOptional(%q, %q) = %q, want %q", tc.sharedDir, tc.fileName, got, tc.want)
			}
		})
	}
}
