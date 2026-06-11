package react

import (
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

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
