package builtin_tools

import (
	"testing"
)

func TestReadyRunnablePlanStepIDs_NilPlan(t *testing.T) {
	if got := ReadyRunnablePlanStepIDs(nil); got != nil {
		t.Fatalf("expected nil for nil plan, got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_EmptyPlan(t *testing.T) {
	if got := ReadyRunnablePlanStepIDs([]*PlanItem{}); got != nil {
		t.Fatalf("expected nil for empty plan, got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_NoReady(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a", Status: PlanStepPending},
		{ID: "b", Status: PlanStepPending, DependsOn: []string{"a"}},
	}
	// a is pending (not completed), so b's dependency is not satisfied.
	// But a itself has no deps so it's ready.
	got := ReadyRunnablePlanStepIDs(plan)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("expected [a], got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_NoReadyDepBlocked(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a", Status: PlanStepInProgress},
		{ID: "b", Status: PlanStepPending, DependsOn: []string{"a"}},
	}
	if got := ReadyRunnablePlanStepIDs(plan); got != nil {
		t.Fatalf("expected nil (a in_progress, b blocked), got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_SingleReady(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a", Status: PlanStepCompleted},
		{ID: "b", Status: PlanStepPending, DependsOn: []string{"a"}},
	}
	got := ReadyRunnablePlanStepIDs(plan)
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("expected [b], got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_MultipleReady_FanOut(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a", Status: PlanStepCompleted},
		{ID: "b", Status: PlanStepPending, DependsOn: []string{"a"}},
		{ID: "c", Status: PlanStepPending, DependsOn: []string{"a"}},
		{ID: "d", Status: PlanStepPending, DependsOn: []string{"a"}},
	}
	got := ReadyRunnablePlanStepIDs(plan)
	if len(got) != 3 || got[0] != "b" || got[1] != "c" || got[2] != "d" {
		t.Fatalf("expected [b c d], got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_SkipNonPending(t *testing.T) {
	plan := []*PlanItem{
		{ID: "a", Status: PlanStepCompleted},
		{ID: "b", Status: PlanStepCompleted, DependsOn: []string{"a"}},
		{ID: "c", Status: PlanStepInProgress, DependsOn: []string{"a"}},
		{ID: "d", Status: PlanStepFailed, DependsOn: []string{"a"}},
		{ID: "e", Status: PlanStepSkipped, DependsOn: []string{"a"}},
		{ID: "f", Status: PlanStepPending, DependsOn: []string{"a"}},
	}
	got := ReadyRunnablePlanStepIDs(plan)
	if len(got) != 1 || got[0] != "f" {
		t.Fatalf("expected [f] (only pending kept), got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_SkipNilItem(t *testing.T) {
	plan := []*PlanItem{
		nil,
		{ID: "a", Status: PlanStepPending},
		nil,
	}
	got := ReadyRunnablePlanStepIDs(plan)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("expected [a] (nil items ignored), got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_EmptyID(t *testing.T) {
	plan := []*PlanItem{
		{ID: "", Status: PlanStepPending},
		{ID: "   ", Status: PlanStepPending},
		{ID: "a", Status: PlanStepPending},
	}
	got := ReadyRunnablePlanStepIDs(plan)
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("expected [a] (empty/blank IDs skipped), got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_DiamondDependency(t *testing.T) {
	// 菱形依赖：a→b,c; d depends on both b and c。
	// b 完成 c 未完成时，d 不该出现在 ready 列表（只有 c 该出现）。
	plan := []*PlanItem{
		{ID: "a", Status: PlanStepCompleted},
		{ID: "b", Status: PlanStepCompleted, DependsOn: []string{"a"}},
		{ID: "c", Status: PlanStepPending, DependsOn: []string{"a"}},
		{ID: "d", Status: PlanStepPending, DependsOn: []string{"b", "c"}},
	}
	got := ReadyRunnablePlanStepIDs(plan)
	if len(got) != 1 || got[0] != "c" {
		t.Fatalf("expected [c] (d blocked by missing c), got %v", got)
	}
}

func TestReadyRunnablePlanStepIDs_ResolvedDependsOn(t *testing.T) {
	a := &PlanItem{ID: "a", Status: PlanStepCompleted}
	b := &PlanItem{
		ID:                "b",
		Status:            PlanStepPending,
		ResolvedDependsOn: []*PlanItem{a},
	}
	plan := []*PlanItem{a, b}
	got := ReadyRunnablePlanStepIDs(plan)
	if len(got) != 1 || got[0] != "b" {
		t.Fatalf("expected [b] (ResolvedDependsOn path), got %v", got)
	}
}
