package react

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSharedFileOptional(t *testing.T) {
	rt, err := newLocalWorkspaceRuntime("sess-shared", t.TempDir(), "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	if err := os.MkdirAll(rt.SharedDir(), 0o755); err != nil {
		t.Fatalf("mkdir shared dir failed: %v", err)
	}
	abs := filepath.Join(rt.SharedDir(), "task_context.md")

	cases := []struct {
		name     string
		setup    func()
		rt       WorkspaceRuntime
		fileName string
		want     string
	}{
		{
			name:     "nil runtime → empty",
			rt:       nil,
			fileName: "task_context.md",
			want:     "",
		},
		{
			name:     "empty fileName → empty",
			rt:       rt,
			fileName: "",
			want:     "",
		},
		{
			name:     "file does not exist → empty",
			rt:       rt,
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
			rt:       rt,
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
			rt:       rt,
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
			got := readSharedFileOptional(tc.rt, tc.fileName)
			if got != tc.want {
				t.Fatalf("readSharedFileOptional(%q) = %q, want %q", tc.fileName, got, tc.want)
			}
		})
	}
}
