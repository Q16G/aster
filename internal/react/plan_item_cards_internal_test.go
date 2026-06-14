package react

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

// TestResolvePlannerJournalPointer 锁定 helper 的四个分支：空 rootDir / 不存在 / 0 字节 / 有内容。
func TestResolvePlannerJournalPointer(t *testing.T) {
	if got := resolvePlannerJournalPointer(""); got != "" {
		t.Fatalf("empty rootDir should return empty, got %q", got)
	}

	root := t.TempDir()
	// 文件不存在
	if got := resolvePlannerJournalPointer(root); got != "" {
		t.Fatalf("missing journal should return empty, got %q", got)
	}

	journalPath := builtin_tools.WorkspacePlannerJournalFileAbs(root)
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// 0 字节
	if err := os.WriteFile(journalPath, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty journal failed: %v", err)
	}
	if got := resolvePlannerJournalPointer(root); got != "" {
		t.Fatalf("zero-byte journal should return empty, got %q", got)
	}

	// 有内容
	if err := os.WriteFile(journalPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write content journal failed: %v", err)
	}
	if got := resolvePlannerJournalPointer(root); got != journalPath {
		t.Fatalf("non-empty journal should return path, got %q want %q", got, journalPath)
	}

	// rootDir 头尾空白被 trim
	if got := resolvePlannerJournalPointer("  " + root + "  "); got != journalPath {
		t.Fatalf("trim rootDir should still resolve, got %q want %q", got, journalPath)
	}
}

func TestProjectPlanItemCardsSlim_DropsDigestKeepsPointers(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{
			ID:              "s1",
			Step:            "扫描",
			Status:          builtin_tools.PlanStepCompleted,
			ShortSummary:    "完成扫描",
			KeyFacts:        []string{"入口 A"},
			ToolCallsDigest: []string{"rg -> 定位入口"},
			StepFile:        "shared/step_s1.md",
			TimelineFile:    "shared/s1/timeline.jsonl",
		},
		{ID: "s2", Step: "分析", Status: builtin_tools.PlanStepPending, DependsOn: []string{"s1"}},
	}

	slim := ProjectPlanItemCardsSlim(plan, "/ws")
	if len(slim) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(slim))
	}
	if slim[0].ToolCallsDigest != nil {
		t.Fatalf("slim card must drop tool_calls_digest, got %+v", slim[0].ToolCallsDigest)
	}
	if slim[0].ShortSummary != "完成扫描" || len(slim[0].KeyFacts) != 1 {
		t.Fatalf("slim card must keep outcome small fields, got %+v", slim[0])
	}
	if !strings.HasPrefix(slim[0].StepFile, "/ws/") || !strings.HasPrefix(slim[0].TimelineFile, "/ws/") {
		t.Fatalf("slim card pointers must be absolute, got step_file=%q timeline_file=%q", slim[0].StepFile, slim[0].TimelineFile)
	}

	// 全量投影不受影响：digest 保留。
	full := ProjectPlanItemCards(plan, "/ws")
	if len(full[0].ToolCallsDigest) != 1 {
		t.Fatalf("full card must keep tool_calls_digest, got %+v", full[0].ToolCallsDigest)
	}
}

func TestBuildReplanStepCard_CoveragePointerTruncatesInline(t *testing.T) {
	items := make([]builtin_tools.CoverageChecklistItem, coverageChecklistInlineMaxItems+10)
	for i := range items {
		items[i] = builtin_tools.CoverageChecklistItem{
			Item:   fmt.Sprintf("item-%d", i),
			Status: "verified",
		}
	}
	current := &builtin_tools.PlanItem{ID: "s1", Step: "覆盖测试"}
	outcome := &builtin_tools.StepOutcome{
		Status:            builtin_tools.StepOutcomeCompleted,
		CoverageChecklist: items,
	}

	// 有指针：内联截留前 N 条。
	card := buildReplanStepCard(current, outcome, "", "", "/ws/shared/s1/coverage.json")
	if card.CoverageFile != "/ws/shared/s1/coverage.json" {
		t.Fatalf("unexpected coverage file: %q", card.CoverageFile)
	}
	if len(card.CoverageChecklist) != coverageChecklistInlineMaxItems {
		t.Fatalf("expected inline truncated to %d, got %d", coverageChecklistInlineMaxItems, len(card.CoverageChecklist))
	}

	// 无指针：原样内联，不截断。
	card = buildReplanStepCard(current, outcome, "", "", "")
	if card.CoverageFile != "" {
		t.Fatalf("expected empty coverage file, got %q", card.CoverageFile)
	}
	if len(card.CoverageChecklist) != coverageChecklistInlineMaxItems+10 {
		t.Fatalf("expected full inline checklist, got %d", len(card.CoverageChecklist))
	}
}
