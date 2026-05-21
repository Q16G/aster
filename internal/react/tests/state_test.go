package react_test

import (
	. "aster/internal/react"
	"testing"
	"time"

	"aster/internal/builtin_tools"
)

func TestNewStateTracker_DefaultPhaseIsPlan(t *testing.T) {
	tracker := NewStateTracker()
	if got := tracker.Snapshot().Phase; got != builtin_tools.AgentPhasePlan {
		t.Fatalf("expected default phase %q, got %q", builtin_tools.AgentPhasePlan, got)
	}
}

func TestStateTracker_Reset_DefaultPhaseIsPlan(t *testing.T) {
	tracker := NewStateTracker()
	tracker.SetPhase(builtin_tools.AgentPhaseStep)

	snapshot := tracker.Snapshot()
	if snapshot.Phase != builtin_tools.AgentPhaseStep {
		t.Fatalf("expected mutated phase %q before reset, got %q", builtin_tools.AgentPhaseStep, snapshot.Phase)
	}

	tracker.Reset()
	if got := tracker.Snapshot().Phase; got != builtin_tools.AgentPhasePlan {
		t.Fatalf("expected reset phase %q, got %q", builtin_tools.AgentPhasePlan, got)
	}
}

func TestStateTracker_UpdatePlan_SetsNeedsPlanning(t *testing.T) {
	tracker := NewStateTracker()
	snapshot := tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "第一步", Status: builtin_tools.PlanStepPending},
	}, "need plan", true)

	if !snapshot.NeedsPlanning {
		t.Fatalf("expected needs_planning=true after UpdatePlan")
	}

	snapshot = tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "第一步", Status: builtin_tools.PlanStepPending},
	}, "no plan", false)
	if snapshot.NeedsPlanning {
		t.Fatalf("expected needs_planning=false after UpdatePlan")
	}
}

func TestStateTracker_UpdateCurrentStepFailed_PropagatesSkipped(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "第一步", Status: builtin_tools.PlanStepPending},
		{ID: "step-2", Step: "第二步", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
	}, "", true)

	if got := tracker.Snapshot().CurrentStepID; got != "step-1" {
		t.Fatalf("expected current_step_id=step-1, got %q", got)
	}

	snapshot := tracker.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepFailed,
		Error:  "boom",
	})

	if snapshot.CurrentStepID != "step-1" {
		t.Fatalf("expected current_step_id kept for step_summary, got %q", snapshot.CurrentStepID)
	}
	if len(snapshot.Plan) != 2 {
		t.Fatalf("expected 2 plan items, got %d", len(snapshot.Plan))
	}
	if snapshot.Plan[1].Status != builtin_tools.PlanStepSkipped {
		t.Fatalf("expected step-2 skipped, got %s", snapshot.Plan[1].Status)
	}
}

func TestStateTracker_UpdatePlan_HydratesResolvedDependsOn(t *testing.T) {
	tracker := NewStateTracker()
	snapshot := tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "第一步", Status: builtin_tools.PlanStepPending},
		{ID: "step-2", Step: "第二步", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
	}, "", true)

	if len(snapshot.Plan) != 2 {
		t.Fatalf("expected 2 plan items, got %d", len(snapshot.Plan))
	}
	if len(snapshot.Plan[1].ResolvedDependsOn) != 1 {
		t.Fatalf("expected resolved dependency, got %+v", snapshot.Plan[1].ResolvedDependsOn)
	}
	if snapshot.Plan[1].ResolvedDependsOn[0] != snapshot.Plan[0] {
		t.Fatalf("expected resolved dependency to point first plan item")
	}
}

func TestStateTracker_UpdatePlan_ClearsReplanContext(t *testing.T) {
	tracker := NewStateTracker()
	tracker.Replace(builtin_tools.StateSnapshot{
		Phase:       builtin_tools.AgentPhasePlan,
		Status:      builtin_tools.TaskStatusRunning,
		CurrentGoal: "新目标",
		ReplanContext: &builtin_tools.ReplanContext{
			SourceStepID:   "step-1",
			NextGoal:       "新目标",
			MissingItems:   []string{"missing-1"},
			Warnings:       []string{"warn-1"},
			ReplacePending: true,
		},
	})

	snapshot := tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "第一步", Status: builtin_tools.PlanStepCompleted},
		{ID: "step-2", Step: "第二步", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
	}, "", true)

	if snapshot.ReplanContext != nil {
		t.Fatalf("expected replan context cleared after applying new plan, got %+v", snapshot.ReplanContext)
	}
	if snapshot.CurrentGoal != "第二步" {
		t.Fatalf("expected current goal synced to next runnable step text, got %q", snapshot.CurrentGoal)
	}
	if snapshot.PlanVersion <= 0 {
		t.Fatalf("expected plan version assigned, got %d", snapshot.PlanVersion)
	}
	if snapshot.CurrentStepID != "step-2" {
		t.Fatalf("expected next runnable step selected, got %q", snapshot.CurrentStepID)
	}
}

func TestStateTracker_SoftReset_PreservesOutcomesAndTimeline(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "第一步", Status: builtin_tools.PlanStepCompleted},
	}, "", true)
	tracker.SetPhase(builtin_tools.AgentPhaseStep)

	outcomes := []*builtin_tools.StepOutcome{
		{StepID: "step-1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "done"},
	}
	timeline := []*builtin_tools.TimelineInput{
		{Content: "analyze main.go", CreatedAt: time.Now()},
	}

	tracker.SoftReset(outcomes, timeline)
	snap := tracker.Snapshot()

	if snap.Phase != builtin_tools.AgentPhasePlan {
		t.Errorf("Phase = %q, want plan", snap.Phase)
	}
	if snap.Status != builtin_tools.TaskStatusPreparing {
		t.Errorf("Status = %q, want preparing", snap.Status)
	}
	if len(snap.StepOutcomes) != 1 || snap.StepOutcomes[0].StepID != "step-1" {
		t.Errorf("StepOutcomes not preserved: %+v", snap.StepOutcomes)
	}
	if len(snap.InputTimeline) != 1 || snap.InputTimeline[0].Content != "analyze main.go" {
		t.Errorf("InputTimeline not preserved: %+v", snap.InputTimeline)
	}
}

func TestStateTracker_SoftReset_ClearsExecutionState(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "第一步", Status: builtin_tools.PlanStepPending},
	}, "goal", true)
	tracker.SetPhase(builtin_tools.AgentPhaseStep)

	tracker.SoftReset(nil, nil)
	snap := tracker.Snapshot()

	if len(snap.Plan) != 0 {
		t.Errorf("Plan should be cleared, got %d items", len(snap.Plan))
	}
	if snap.CurrentStepID != "" {
		t.Errorf("CurrentStepID should be empty, got %q", snap.CurrentStepID)
	}
	if snap.CurrentGoal != "" {
		t.Errorf("CurrentGoal should be empty, got %q", snap.CurrentGoal)
	}
	if snap.ReplanContext != nil {
		t.Errorf("ReplanContext should be nil, got %+v", snap.ReplanContext)
	}
}

func TestStateTracker_SetReplanContext(t *testing.T) {
	tracker := NewStateTracker()
	tracker.SetReplanContext(&builtin_tools.ReplanContext{
		Reason:         "user wants different approach",
		NextGoal:       "new goal",
		ReplacePending: true,
	})

	snap := tracker.Snapshot()
	if snap.ReplanContext == nil {
		t.Fatal("ReplanContext should not be nil")
	}
	if snap.ReplanContext.Reason != "user wants different approach" {
		t.Errorf("Reason = %q", snap.ReplanContext.Reason)
	}
	if snap.ReplanContext.NextGoal != "new goal" {
		t.Errorf("NextGoal = %q", snap.ReplanContext.NextGoal)
	}
	if !snap.ReplanContext.ReplacePending {
		t.Error("ReplacePending should be true")
	}
}
