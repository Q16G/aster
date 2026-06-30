package react

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"aster/internal/builtin_tools"
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
		"task_context.md": {"# 贯穿全程关键事实", "## 输入事实", "## 执行中补充"},
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

// TestMutateChildAgent_AtomicNoLostUpdate 锁定 A.3：并发 MutateChildAgent
// 必须串行化 load→改→save，N 路并发派发 N 条 ChildAgents 时不能丢更新。
func TestMutateChildAgent_AtomicNoLostUpdate(t *testing.T) {
	rootDir := t.TempDir()
	rt, err := newLocalWorkspaceRuntime("sess-mut", rootDir, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}

	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := "sub-" + string(rune('a'+idx%26)) + string(rune('0'+idx/26))
			_ = rt.MutateChildAgent(name, func(_ *builtin_tools.WorkspaceChildAgentPointer) *builtin_tools.WorkspaceChildAgentPointer {
				return &builtin_tools.WorkspaceChildAgentPointer{
					Status:          "running",
					ParentStepKey:   "step-x",
					ArtifactRootDir: "/tmp/" + name,
				}
			})
			_ = rt.MutateChildAgent(name, func(prev *builtin_tools.WorkspaceChildAgentPointer) *builtin_tools.WorkspaceChildAgentPointer {
				if prev == nil {
					t.Errorf("finalize observed nil prev for %s — register lost", name)
					return nil
				}
				prev.Status = "completed"
				return prev
			})
		}(i)
	}
	wg.Wait()

	state, err := rt.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("LoadWorkspaceState: %v", err)
	}
	if got := len(state.ChildAgents); got != n {
		t.Fatalf("expected %d ChildAgents after concurrent mutate, got %d", n, got)
	}
	for name, ptr := range state.ChildAgents {
		if ptr == nil {
			t.Fatalf("nil pointer for %s", name)
		}
		if ptr.Status != "completed" {
			t.Fatalf("expected status=completed for %s, got %q", name, ptr.Status)
		}
	}
}

// TestMutateChildAgent_DeleteOnNilReturn 锁定 mutate 返回 nil 时删除该条。
func TestMutateChildAgent_DeleteOnNilReturn(t *testing.T) {
	rootDir := t.TempDir()
	rt, err := newLocalWorkspaceRuntime("sess-del", rootDir, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}

	_ = rt.MutateChildAgent("sub-x", func(_ *builtin_tools.WorkspaceChildAgentPointer) *builtin_tools.WorkspaceChildAgentPointer {
		return &builtin_tools.WorkspaceChildAgentPointer{Status: "running"}
	})
	_ = rt.MutateChildAgent("sub-x", func(_ *builtin_tools.WorkspaceChildAgentPointer) *builtin_tools.WorkspaceChildAgentPointer {
		return nil
	})

	state, err := rt.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("LoadWorkspaceState: %v", err)
	}
	if _, ok := state.ChildAgents["sub-x"]; ok {
		t.Fatal("sub-x should have been deleted when mutate returned nil")
	}
}
