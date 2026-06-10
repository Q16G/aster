package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"path/filepath"
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
