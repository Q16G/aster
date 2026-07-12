package react

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureSharedScaffoldCreatesFiles(t *testing.T) {
	rootDir := t.TempDir()
	rt, err := newLocalWorkspaceRuntime("sess", rootDir, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	seeder, ok := rt.(interface{ EnsureSharedScaffold() error })
	if !ok {
		t.Fatalf("runtime does not implement EnsureSharedScaffold")
	}
	if err := seeder.EnsureSharedScaffold(); err != nil {
		t.Fatalf("EnsureSharedScaffold: %v", err)
	}

	sharedDir := filepath.Join(rootDir, "shared")
	cases := map[string][]string{
		"task_context.md": {"# 贯穿全程关键事实", "## 输入事实", "## 执行中补充", "## 历史演进"},
		"open_items.md":   {"## 未解决", "## 不可解局限", "## 已闭环"},
	}
	for name, wantHeaders := range cases {
		data, err := os.ReadFile(filepath.Join(sharedDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		got := string(data)
		for _, h := range wantHeaders {
			if !strings.Contains(got, h) {
				t.Errorf("%s missing header %q, got:\n%s", name, h, got)
			}
		}
	}
}

func TestEnsureSharedScaffoldDoesNotClobber(t *testing.T) {
	rootDir := t.TempDir()
	rt, err := newLocalWorkspaceRuntime("sess", rootDir, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	seeder := rt.(interface{ EnsureSharedScaffold() error })

	sharedDir := filepath.Join(rootDir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatalf("mkdir shared: %v", err)
	}
	existing := "# 贯穿全程关键事实\n\n## 输入事实\n- target: 1.2.3.4:8080\n\n## 执行中补充\n- creds: admin/secret\n"
	taskCtxPath := filepath.Join(sharedDir, "task_context.md")
	if err := os.WriteFile(taskCtxPath, []byte(existing), 0o644); err != nil {
		t.Fatalf("write existing task_context.md: %v", err)
	}

	if err := seeder.EnsureSharedScaffold(); err != nil {
		t.Fatalf("EnsureSharedScaffold: %v", err)
	}

	data, err := os.ReadFile(taskCtxPath)
	if err != nil {
		t.Fatalf("read task_context.md: %v", err)
	}
	if string(data) != existing {
		t.Errorf("task_context.md was clobbered.\nwant:\n%s\ngot:\n%s", existing, string(data))
	}
	// open_items.md was absent → should now be seeded.
	if _, err := os.Stat(filepath.Join(sharedDir, "open_items.md")); err != nil {
		t.Errorf("open_items.md not seeded: %v", err)
	}
}
