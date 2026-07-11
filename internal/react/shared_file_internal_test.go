package react

import (
	"os"
	"path/filepath"
	"testing"

	"aster/internal/workspacefs"
)

func TestReadSharedFileOptional(t *testing.T) {
	root := t.TempDir()
	layout := workspacefs.New(root, "")
	if err := os.MkdirAll(layout.SharedDir(), 0o755); err != nil {
		t.Fatalf("mkdir shared dir failed: %v", err)
	}
	abs := filepath.Join(layout.SharedDir(), "task_context.md")

	cases := []struct {
		name     string
		setup    func()
		layout   workspacefs.Layout
		fileName string
		want     string
	}{
		{
			name:     "empty layout root → empty",
			layout:   workspacefs.Layout{},
			fileName: "task_context.md",
			want:     "",
		},
		{
			name:     "empty fileName → empty",
			layout:   layout,
			fileName: "",
			want:     "",
		},
		{
			name:     "file does not exist → empty",
			layout:   layout,
			fileName: "task_context.md",
			want:     "",
		},
		{
			name: "file empty → empty",
			setup: func() {
				if err := os.WriteFile(abs, []byte("\n  \t\n"), 0o644); err != nil {
					t.Fatalf("write empty file failed: %v", err)
				}
			},
			layout:   layout,
			fileName: "task_context.md",
			want:     "",
		},
		{
			name: "file has content → trimmed content",
			setup: func() {
				if err := os.WriteFile(abs, []byte("\n# 贯穿全程关键事实\n\n## 输入事实\n- 目标: x\n\n"), 0o644); err != nil {
					t.Fatalf("write content file failed: %v", err)
				}
			},
			layout:   layout,
			fileName: "task_context.md",
			want:     "# 贯穿全程关键事实\n\n## 输入事实\n- 目标: x",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup()
			} else {
				_ = os.Remove(abs)
			}
			got := readSharedFileOptional(nil, tc.layout, tc.fileName)
			if got != tc.want {
				t.Fatalf("readSharedFileOptional(%q, %q) = %q, want %q", tc.layout.SharedDir(), tc.fileName, got, tc.want)
			}
		})
	}
}
