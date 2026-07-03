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

func TestPlannerJournal_PhaseRecordsSurviveRewrite(t *testing.T) {
	root := t.TempDir()

	// plan 提交：item 行在前、phase 行在后（版本提升批次契约）
	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPlan, PlanVersion: 1, Item: &PlanItem{ID: "a1", Step: "A1", Status: PlanStepPending, PhaseID: "phase-a"}},
		{Kind: PlannerJournalKindPhase, PlanVersion: 1, Phase: &PlanPhase{ID: "phase-a", Name: "lane A", Status: PlanPhasePending}},
		{Kind: PlannerJournalKindPhase, PlanVersion: 1, Phase: &PlanPhase{ID: "phase-b", Name: "lane B", DependsOn: []string{"phase-a"}, Status: PlanPhasePending}},
	}); err != nil {
		t.Fatalf("append plan+phase records failed: %v", err)
	}

	// step 终态增量落盘触发 snapshot 原子重写——phase 行必须存活
	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindStep, PlanVersion: 1, Item: &PlanItem{ID: "a1", Step: "A1", Status: PlanStepCompleted, PhaseID: "phase-a"}},
	}); err != nil {
		t.Fatalf("append step record failed: %v", err)
	}

	items, phases, version, err := LoadPlannerJournalSnapshot(root)
	if err != nil {
		t.Fatalf("load snapshot failed: %v", err)
	}
	if version != 1 || len(items) != 1 {
		t.Fatalf("unexpected items/version: %d %+v", version, items)
	}
	if len(phases) != 2 || phases[0].ID != "phase-a" || phases[1].ID != "phase-b" {
		t.Fatalf("phase records lost after rewrite: %+v", phases)
	}
	if len(phases[1].DependsOn) != 1 || phases[1].DependsOn[0] != "phase-a" {
		t.Fatalf("phase depends_on lost: %+v", phases[1])
	}

	// phase 状态增量覆盖（step_replan 承接路径）
	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPhase, PlanVersion: 1, Phase: &PlanPhase{ID: "phase-a", Name: "lane A", Status: PlanPhaseCompleted}},
	}); err != nil {
		t.Fatalf("append phase upsert failed: %v", err)
	}
	_, phases, _, err = LoadPlannerJournalSnapshot(root)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	if phases[0].Status != PlanPhaseCompleted {
		t.Fatalf("expected phase-a completed, got %+v", phases[0])
	}

	// 重规划版本提升：plan 行在前 reset 旧 phases，新 phase 行随后落地
	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPlan, PlanVersion: 2, Item: &PlanItem{ID: "c1", Step: "C1", Status: PlanStepPending, PhaseID: "phase-c"}},
		{Kind: PlannerJournalKindPhase, PlanVersion: 2, Phase: &PlanPhase{ID: "phase-c", Status: PlanPhasePending}},
	}); err != nil {
		t.Fatalf("append v2 records failed: %v", err)
	}
	items, phases, version, err = LoadPlannerJournalSnapshot(root)
	if err != nil {
		t.Fatalf("reload v2 failed: %v", err)
	}
	if version != 2 || len(items) != 1 || items[0].ID != "c1" {
		t.Fatalf("unexpected v2 items: %d %+v", version, items)
	}
	if len(phases) != 1 || phases[0].ID != "phase-c" {
		t.Fatalf("old phases must reset on version bump, got %+v", phases)
	}
}

func TestPlannerJournal_LegacyFileNoPhases(t *testing.T) {
	root := t.TempDir()
	// 旧格式：只有 item 行（无 phase 概念、item 无 phase_id）
	if err := AppendPlannerJournalRecords(root, []*PlannerJournalRecord{
		{Kind: PlannerJournalKindPlan, PlanVersion: 1, Item: &PlanItem{ID: "s1", Step: "legacy", Status: PlanStepPending}},
	}); err != nil {
		t.Fatalf("append failed: %v", err)
	}
	items, phases, version, err := LoadPlannerJournalSnapshot(root)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if version != 1 || len(items) != 1 {
		t.Fatalf("unexpected items: %d %+v", version, items)
	}
	if phases != nil {
		t.Fatalf("legacy file must yield nil phases (runtime synthesizes), got %+v", phases)
	}
}
