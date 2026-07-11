package react

// S06（M4）：workspacefs.Store.AbsPath 与现存 react resolveAbsPath 的判定等价安全网。
// M5 防穿越收口（resolveAbsPath 删除、runtime 改走 Store）后本测试随之删除。

import (
	"testing"

	"aster/internal/workspacefs"
)

func TestStoreAbsPathMatchesLegacyResolve(t *testing.T) {
	rt, err := newLocalWorkspaceRuntime("s06-sess", t.TempDir(), "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	lw, ok := rt.(*localWorkspaceRuntime)
	if !ok {
		t.Fatalf("runtime 不是 localWorkspaceRuntime")
	}
	st, err := workspacefs.NewLocalStore(rt.RootDir())
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}

	cases := []string{
		// 合法
		"shared/task_context.md", "a/b.txt", "workspace/planner.jsonl",
		// 归一后合法
		"./a", "a//b", "a/./b", "a/b/", "a/x/../b",
		// 必须拒绝
		"../x", "a/../../x", "/etc/passwd", ".", "", "   ", "..", "\\evil",
	}
	for _, rel := range cases {
		absOld, errOld := lw.resolveAbsPath(rel)
		absNew, errNew := st.AbsPath(rel)
		if (errOld == nil) != (errNew == nil) {
			t.Errorf("AbsPath(%q) 判定不一致: 旧 err=%v 新 err=%v", rel, errOld, errNew)
			continue
		}
		if errOld == nil && absOld != absNew {
			t.Errorf("AbsPath(%q) 输出不一致: 旧=%q 新=%q", rel, absOld, absNew)
		}
	}
}
