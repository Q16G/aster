package react

// localWorkspaceRuntime 收口 Layout+Store 后的契约测试（M5b，R01-R07）：
// 固化 runtime 对外行为语义——scaffold 幂等、state 原子写 roundtrip、
// references / step_contexts 追加语义、planner journal snapshot 重写、
// Layout/Store 与 identity 方法一致性、per-key 锁的并发读写完整性。

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aster/internal/builtin_tools"
)

func newContractRuntime(t *testing.T) (WorkspaceRuntime, string) {
	t.Helper()
	root := t.TempDir()
	rt, err := newLocalWorkspaceRuntime("contract-sess", root, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	return rt, rt.RootDir()
}

// R01：EnsureSharedScaffold 幂等——已存在的 task_context.md 内容绝不被二次 scaffold 覆盖。
func TestContract_EnsureSharedScaffoldIdempotent(t *testing.T) {
	rt, root := newContractRuntime(t)
	seeder := rt.(interface{ EnsureSharedScaffold() error })
	if err := seeder.EnsureSharedScaffold(); err != nil {
		t.Fatalf("first scaffold: %v", err)
	}

	custom := "# 贯穿全程关键事实\n\n## 输入事实\n- 自定义事实 A\n\n## 执行中补充\n- 自定义补充 B\n"
	if err := rt.WriteFileRel("shared/task_context.md", []byte(custom)); err != nil {
		t.Fatalf("write custom task_context.md: %v", err)
	}

	if err := seeder.EnsureSharedScaffold(); err != nil {
		t.Fatalf("second scaffold: %v", err)
	}
	got, err := rt.ReadFileRel("shared/task_context.md")
	if err != nil {
		t.Fatalf("read task_context.md: %v", err)
	}
	if string(got) != custom {
		t.Fatalf("task_context.md 被二次 scaffold 覆盖:\nwant:\n%s\ngot:\n%s", custom, got)
	}
	// open_items.md 骨架仍在。
	if _, err := os.Stat(filepath.Join(root, "shared", "open_items.md")); err != nil {
		t.Fatalf("open_items.md 缺失: %v", err)
	}
}

// R02：state.json Save→Load 字段等价；原子写不留 .tmp 残留。
func TestContract_WorkspaceStateRoundTripAtomic(t *testing.T) {
	rt, root := newContractRuntime(t)
	want := &builtin_tools.WorkspaceState{
		SessionID:          "contract-sess",
		Status:             builtin_tools.TaskStatusRunning,
		CurrentPlanVersion: 3,
		CurrentStepKey:     "p1-s2#1",
		LatestFinalSeq:     2,
		LatestStepOutcomes: map[string]*builtin_tools.WorkspaceStepOutcomePointer{
			"p1-s1": {PlanVersion: 3, StepKey: "p1-s1#1"},
		},
	}
	if err := rt.SaveWorkspaceState(want); err != nil {
		t.Fatalf("SaveWorkspaceState: %v", err)
	}
	got, err := rt.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("LoadWorkspaceState: %v", err)
	}
	if got.SessionID != want.SessionID || got.Status != want.Status ||
		got.CurrentPlanVersion != want.CurrentPlanVersion ||
		got.CurrentStepKey != want.CurrentStepKey || got.LatestFinalSeq != want.LatestFinalSeq {
		t.Fatalf("state roundtrip 字段漂移:\nwant %+v\ngot  %+v", want, got)
	}
	if got.LatestStepOutcomes["p1-s1"] == nil || got.LatestStepOutcomes["p1-s1"].StepKey != "p1-s1#1" {
		t.Fatalf("LatestStepOutcomes 漂移: %+v", got.LatestStepOutcomes)
	}

	entries, err := os.ReadDir(filepath.Join(root, "workspace"))
	if err != nil {
		t.Fatalf("read workspace dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Fatalf("原子写残留 temp 文件: %s", e.Name())
		}
	}
}

// R03：references 多批 append → Load 保序全量。
func TestContract_ReferencesAppendOrdered(t *testing.T) {
	rt, _ := newContractRuntime(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := rt.AppendWorkspaceReferences([]*builtin_tools.WorkspaceReferenceRecord{
		{RefID: "ref-1", Kind: "url", Title: "one", URI: "https://example.com/1", CreatedAt: now},
		{RefID: "ref-2", Kind: "url", Title: "two", URI: "https://example.com/2", CreatedAt: now},
	}); err != nil {
		t.Fatalf("append batch 1: %v", err)
	}
	if err := rt.AppendWorkspaceReferences([]*builtin_tools.WorkspaceReferenceRecord{
		{RefID: "ref-3", Kind: "file", Title: "three", URI: "shared/x.md", CreatedAt: now},
	}); err != nil {
		t.Fatalf("append batch 2: %v", err)
	}
	refs, err := rt.LoadWorkspaceReferences()
	if err != nil {
		t.Fatalf("LoadWorkspaceReferences: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
	for i, wantID := range []string{"ref-1", "ref-2", "ref-3"} {
		if refs[i].RefID != wantID {
			t.Fatalf("refs 保序失败: idx=%d want=%s got=%s", i, wantID, refs[i].RefID)
		}
	}
}

// R04：step_contexts 写 5 条 limit 3 → 尾部 3 条（原顺序）。
func TestContract_StepContextsLimitTail(t *testing.T) {
	rt, _ := newContractRuntime(t)
	now := time.Now()
	records := make([]*builtin_tools.StepContextRecord, 0, 5)
	for i := 1; i <= 5; i++ {
		records = append(records, &builtin_tools.StepContextRecord{
			ContextKey:  fmt.Sprintf("ctx-%d", i),
			StepID:      fmt.Sprintf("step-%d", i),
			PlanVersion: 1,
			CreatedAt:   now,
		})
	}
	if err := rt.AppendStepContextRecords(records); err != nil {
		t.Fatalf("AppendStepContextRecords: %v", err)
	}
	got, err := rt.LoadStepContextRecords(3)
	if err != nil {
		t.Fatalf("LoadStepContextRecords: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 records, got %d", len(got))
	}
	for i, wantKey := range []string{"ctx-3", "ctx-4", "ctx-5"} {
		if got[i].ContextKey != wantKey {
			t.Fatalf("limit 尾部语义失败: idx=%d want=%s got=%s", i, wantKey, got[i].ContextKey)
		}
	}
}

// R05：planner journal 快照重写——同 plan_version 两批 append（不同 item id）后，
// 文件行数 = 合并后条目数（非累加双份），LoadPlannerJournalSnapshot 重放一致。
func TestContract_PlannerJournalSnapshotRewrite(t *testing.T) {
	_, root := newContractRuntime(t)
	if err := AppendPlannerJournalRecords(root, []*builtin_tools.PlannerJournalRecord{
		{Kind: builtin_tools.PlannerJournalKindPlan, PlanVersion: 1, Item: &builtin_tools.PlanItem{ID: "s1", Step: "one"}},
	}); err != nil {
		t.Fatalf("append batch 1: %v", err)
	}
	if err := AppendPlannerJournalRecords(root, []*builtin_tools.PlannerJournalRecord{
		{Kind: builtin_tools.PlannerJournalKindPlan, PlanVersion: 1, Item: &builtin_tools.PlanItem{ID: "s2", Step: "two"}},
	}); err != nil {
		t.Fatalf("append batch 2: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "workspace", "planner.jsonl"))
	if err != nil {
		t.Fatalf("read planner.jsonl: %v", err)
	}
	lines := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("snapshot 重写应为 2 行（合并后条目数），got %d:\n%s", len(lines), raw)
	}

	items, phases, version, err := LoadPlannerJournalSnapshot(root)
	if err != nil {
		t.Fatalf("LoadPlannerJournalSnapshot: %v", err)
	}
	if version != 1 || len(items) != 2 || items[0].ID != "s1" || items[1].ID != "s2" {
		t.Fatalf("snapshot 重放不一致: version=%d items=%+v", version, items)
	}
	if phases != nil {
		t.Fatalf("无 phase 行时 phases 应为 nil, got %+v", phases)
	}
}

// R06：Layout()/Store() 与 identity 方法一致。
func TestContract_LayoutStoreIdentityConsistent(t *testing.T) {
	rt, _ := newContractRuntime(t)
	if got := rt.Layout().Root; got != rt.RootDir() {
		t.Fatalf("layout.Root=%q != RootDir()=%q", got, rt.RootDir())
	}
	if got := rt.Store().Root(); got != rt.RootDir() {
		t.Fatalf("store.Root()=%q != RootDir()=%q", got, rt.RootDir())
	}
	if got := rt.Layout().SharedDir(); got != rt.SharedDir() {
		t.Fatalf("layout.SharedDir()=%q != SharedDir()=%q", got, rt.SharedDir())
	}
	if rt.Namespace() != "root" {
		t.Fatalf("顶层 Namespace() 语义漂移: %q", rt.Namespace())
	}
}

// R07：锁超集（-race 守门）——多 goroutine 并发 WriteFileRel 同一
// shared/prompt_context/x.md + ReadFileRel 同文件，读到的内容恒为某次完整写入。
func TestContract_ConcurrentReadWriteWholeFile(t *testing.T) {
	rt, _ := newContractRuntime(t)
	const rel = "shared/prompt_context/x.md"
	const writers = 8
	const readsPerReader = 50
	const payloadLen = 64 * 1024

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{byte('A' + i)}, payloadLen)
			for j := 0; j < 10; j++ {
				if err := rt.WriteFileRel(rel, payload); err != nil {
					t.Errorf("WriteFileRel: %v", err)
					return
				}
			}
		}(i)
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < readsPerReader; j++ {
				data, err := rt.ReadFileRel(rel)
				if err != nil {
					if os.IsNotExist(err) {
						continue // 首个写者尚未落盘
					}
					t.Errorf("ReadFileRel: %v", err)
					return
				}
				if len(data) != payloadLen {
					t.Errorf("读到半截写入: len=%d want=%d", len(data), payloadLen)
					return
				}
				for _, b := range data {
					if b != data[0] {
						t.Errorf("读到撕裂内容: 首字节 %q 混入 %q", data[0], b)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}
