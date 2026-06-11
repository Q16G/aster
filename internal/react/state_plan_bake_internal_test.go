package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// TestApplyStepReplan_BakesOutcomeIntoPlanItem 校验 step 终态后产出与指针字段
// 烘焙进 plan_item（plan 真相源载体），覆盖清单与 coverage_file 互斥。
func TestApplyStepReplan_BakesOutcomeIntoPlanItem(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "侦察", Status: builtin_tools.PlanStepPending},
		{ID: "step-2", Step: "分析", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
	}, "init", true)
	tracker.EnsureCurrentStep()

	tracker.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
		Status:        builtin_tools.PlanStepCompleted,
		StatusSummary: "完成",
		ShortSummary:  "完成侦察，发现入口 A",
		KeyFacts:      []string{"入口 A 位于模块 X"},
		CoverageChecklist: []builtin_tools.CoverageChecklistItem{
			{Item: "模块 X", Status: "verified", Evidence: "scan 输出"},
		},
		OpenItemIDs: []string{"OI-001"},
		References:  []string{"/tmp/report.md"},
	})

	snap := tracker.ApplyStepReplan("step-1", stepReplanUpdate{
		TimelineFile: "shared/step-1/timeline.jsonl",
		ResultFile:   "shared/step_artifacts/step-1.result.json",
		PlanVersion:  1,
		NextPhase:    builtin_tools.AgentPhaseStep,
	})

	var item *builtin_tools.PlanItem
	for _, it := range snap.Plan {
		if it.ID == "step-1" {
			item = it
		}
	}
	if item == nil {
		t.Fatal("step-1 not found in plan")
	}
	if item.ShortSummary != "完成侦察，发现入口 A" {
		t.Fatalf("expected short_summary baked, got %q", item.ShortSummary)
	}
	if len(item.KeyFacts) != 1 {
		t.Fatalf("expected key_facts baked, got %+v", item.KeyFacts)
	}
	if item.TimelineFile != "shared/step-1/timeline.jsonl" {
		t.Fatalf("expected timeline_file baked, got %q", item.TimelineFile)
	}
	if item.ResultFile != "shared/step_artifacts/step-1.result.json" {
		t.Fatalf("expected result_file baked, got %q", item.ResultFile)
	}
	if len(item.OpenItemIDs) != 1 || item.OpenItemIDs[0] != "OI-001" {
		t.Fatalf("expected open_item_ids baked, got %+v", item.OpenItemIDs)
	}
	// 未超阈值：清单内联，coverage_file 为空。
	if item.CoverageFile != "" || len(item.CoverageChecklist) != 1 {
		t.Fatalf("expected inline coverage checklist, got file=%q list=%+v", item.CoverageFile, item.CoverageChecklist)
	}

	// 超阈值场景：coverage_file 写入后内联清单清空。
	tracker2 := NewStateTracker()
	tracker2.UpdatePlan([]*builtin_tools.PlanItem{{ID: "s1", Step: "x", Status: builtin_tools.PlanStepPending}}, "", true)
	tracker2.EnsureCurrentStep()
	tracker2.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepCompleted, StatusSummary: "ok", ShortSummary: "ok",
		CoverageChecklist: []builtin_tools.CoverageChecklistItem{{Item: "a", Status: "verified"}},
	})
	snap2 := tracker2.ApplyStepReplan("s1", stepReplanUpdate{
		CoverageFile: "shared/s1/coverage.json",
		PlanVersion:  1,
		NextPhase:    builtin_tools.AgentPhaseFinalAnswer,
	})
	if got := snap2.Plan[0]; got.CoverageFile != "shared/s1/coverage.json" || got.CoverageChecklist != nil {
		t.Fatalf("expected coverage_file exclusive with inline list, got file=%q list=%+v", got.CoverageFile, got.CoverageChecklist)
	}
}
