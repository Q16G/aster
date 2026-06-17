package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlannerJournal_AppendAndLoad_RoundTrip(t *testing.T) {
	root := t.TempDir()

	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPlan, PlanVersion: 1, Item: &PlanItem{ID: "step-1", Step: "侦察", Status: PlanStepPending}},
		{Kind: PlannerJournalKindPlan, PlanVersion: 1, Item: &PlanItem{ID: "step-2", Step: "分析", Status: PlanStepPending, DependsOn: []string{"step-1"}}},
	}); err != nil {
		t.Fatalf("append plan records failed: %v", err)
	}

	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindStep, PlanVersion: 1, Item: &PlanItem{
			ID:              "step-1",
			Step:            "侦察",
			Status:          PlanStepCompleted,
			ShortSummary:    "完成侦察",
			KeyFacts:        []string{"发现入口 A"},
			ToolCallsDigest: []string{"bash: scan target"},
			StepFile:        "shared/step-1/step_侦察.md",
			TimelineFile:    "shared/step-1/timeline.jsonl",
		}},
	}); err != nil {
		t.Fatalf("append step record failed: %v", err)
	}

	items, version, err := LoadPlannerJournal(root)
	if err != nil {
		t.Fatalf("load planner journal failed: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected plan version 1, got %d", version)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != "step-1" || items[1].ID != "step-2" {
		t.Fatalf("unexpected item order: %+v", items)
	}
	if items[0].Status != PlanStepCompleted {
		t.Fatalf("expected step-1 latest status completed, got %q", items[0].Status)
	}
	if items[0].ShortSummary != "完成侦察" || len(items[0].KeyFacts) != 1 {
		t.Fatalf("expected产出字段随增量覆盖, got %+v", items[0])
	}
	if want := filepath.ToSlash(filepath.Join(root, "shared", "step-1", "timeline.jsonl")); items[0].TimelineFile != want {
		t.Fatalf("expected timeline_file absolute, want=%q got=%q", want, items[0].TimelineFile)
	}
	if items[1].Status != PlanStepPending {
		t.Fatalf("expected step-2 keep pending, got %q", items[1].Status)
	}
}

func TestPlannerJournal_ReplanSupersedesByVersion(t *testing.T) {
	root := t.TempDir()

	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPlan, PlanVersion: 1, Item: &PlanItem{ID: "step-1", Step: "A", Status: PlanStepPending}},
		{Kind: PlannerJournalKindStep, PlanVersion: 1, Item: &PlanItem{ID: "step-1", Step: "A", Status: PlanStepCompleted}},
	}); err != nil {
		t.Fatalf("append v1 records failed: %v", err)
	}

	// 重规划：v2 全量集合含保留的 completed 项与新增项；v1 中已被剔除的条目不应再出现。
	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPlan, PlanVersion: 2, Item: &PlanItem{ID: "step-1", Step: "A", Status: PlanStepCompleted}},
		{Kind: PlannerJournalKindPlan, PlanVersion: 2, Item: &PlanItem{ID: "step-3", Step: "C", Status: PlanStepPending}},
	}); err != nil {
		t.Fatalf("append v2 records failed: %v", err)
	}

	items, version, err := LoadPlannerJournal(root)
	if err != nil {
		t.Fatalf("load planner journal failed: %v", err)
	}
	if version != 2 {
		t.Fatalf("expected plan version 2, got %d", version)
	}
	if len(items) != 2 {
		t.Fatalf("expected v2 full set of 2 items, got %d: %+v", len(items), items)
	}
	if items[0].ID != "step-1" || items[1].ID != "step-3" {
		t.Fatalf("unexpected v2 items: %+v", items)
	}
}

func TestPlannerJournal_LoadMissingFileReturnsEmpty(t *testing.T) {
	items, version, err := LoadPlannerJournal(t.TempDir())
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if items != nil || version != 0 {
		t.Fatalf("expected empty result, got items=%v version=%d", items, version)
	}
}

// TestPlannerJournal_SnapshotRewriteDropsOldVersionLines 断言 snapshot 语义：
// 新 plan_version 写入后磁盘文件不应再包含旧 plan_version 的行；行数 = 最新
// plan 全量 items 数量，每行 kind=plan。
func TestPlannerJournal_SnapshotRewriteDropsOldVersionLines(t *testing.T) {
	root := t.TempDir()

	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPlan, PlanVersion: 1, Item: &PlanItem{ID: "step-1", Step: "A", Status: PlanStepPending}},
		{Kind: PlannerJournalKindPlan, PlanVersion: 1, Item: &PlanItem{ID: "step-old", Step: "X", Status: PlanStepPending}},
	}); err != nil {
		t.Fatalf("append v1 failed: %v", err)
	}

	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPlan, PlanVersion: 2, Item: &PlanItem{ID: "step-1", Step: "A", Status: PlanStepCompleted}},
		{Kind: PlannerJournalKindPlan, PlanVersion: 2, Item: &PlanItem{ID: "step-3", Step: "C", Status: PlanStepPending}},
	}); err != nil {
		t.Fatalf("append v2 failed: %v", err)
	}

	raw, err := os.ReadFile(WorkspacePlannerJournalFileAbs(root))
	if err != nil {
		t.Fatalf("read planner.jsonl failed: %v", err)
	}
	lines := bytes.Split(bytes.TrimRight(raw, "\n"), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("expected exactly 2 lines after snapshot rewrite, got %d:\n%s", len(lines), raw)
	}
	if strings.Contains(string(raw), `"step-old"`) {
		t.Fatalf("snapshot must not retain v1-only item step-old:\n%s", raw)
	}
	if strings.Contains(string(raw), `"plan_version":1`) {
		t.Fatalf("snapshot must not retain plan_version=1 lines:\n%s", raw)
	}

	for i, line := range lines {
		var rec PlannerJournalRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("line %d not valid json: %v\n%s", i, err, line)
		}
		if rec.Kind != PlannerJournalKindPlan {
			t.Fatalf("line %d kind=%q, want kind=plan", i, rec.Kind)
		}
		if rec.PlanVersion != 2 {
			t.Fatalf("line %d plan_version=%d, want 2", i, rec.PlanVersion)
		}
	}

	// 重放仍得到正确的最新状态。
	items, version, err := LoadPlannerJournal(root)
	if err != nil {
		t.Fatalf("reload after snapshot failed: %v", err)
	}
	if version != 2 || len(items) != 2 {
		t.Fatalf("reload mismatch: version=%d items=%d", version, len(items))
	}
}

func TestPlannerJournal_RejectsInvalidRecords(t *testing.T) {
	root := t.TempDir()
	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: "bogus", PlanVersion: 1, Item: &PlanItem{ID: "step-1", Step: "A"}},
	}); err == nil {
		t.Fatal("expected error for unknown kind")
	}
	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPlan, PlanVersion: 0, Item: &PlanItem{ID: "step-1", Step: "A"}},
	}); err == nil {
		t.Fatal("expected error for missing plan_version")
	}
}
