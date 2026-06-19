package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// TestCurrentPhase_X2RoutesBackToStep_WhenReady：X2 滚动 guard 核心。
// Phase=StepReplan + MaxParallel>=2 + 仍有 ready → 绕回 Step 让主路径滚动。
func TestCurrentPhase_X2RoutesBackToStep_WhenReady(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "a", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
	}
	snap := builtin_tools.StateSnapshot{
		Phase: builtin_tools.AgentPhaseStepReplan,
		Plan:  plan,
	}
	got := currentPhase(snap, 3)
	if got != builtin_tools.AgentPhaseStep {
		t.Fatalf("X2 guard should route StepReplan -> Step when ready, got %q", got)
	}
}

// TestCurrentPhase_X2DoesNotRouteWhenSerial：MaxParallel=1（串行）时 guard 不生效，保持原行为。
func TestCurrentPhase_X2DoesNotRouteWhenSerial(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "a", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
	}
	snap := builtin_tools.StateSnapshot{
		Phase: builtin_tools.AgentPhaseStepReplan,
		Plan:  plan,
	}
	got := currentPhase(snap, 1)
	if got != builtin_tools.AgentPhaseStepReplan {
		t.Fatalf("serial path should keep StepReplan, got %q", got)
	}
}

// TestCurrentPhase_X2DoesNotRouteWhenNoReady：MaxParallel>=2 但 ready=空时不绕回。
// 此时 plan 上没有可派发的 step，应真进 step_replan 进行复核。
func TestCurrentPhase_X2DoesNotRouteWhenNoReady(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "a", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Status: builtin_tools.PlanStepInProgress, DependsOn: []string{"a"}}, // 仍在跑，非 pending
	}
	snap := builtin_tools.StateSnapshot{
		Phase: builtin_tools.AgentPhaseStepReplan,
		Plan:  plan,
	}
	got := currentPhase(snap, 3)
	if got != builtin_tools.AgentPhaseStepReplan {
		t.Fatalf("no ready (only in_progress) should keep StepReplan, got %q", got)
	}
}

// TestCurrentPhase_X2DoesNotRouteWhenPhaseStep：guard 只作用于 StepReplan，
// 不影响其他 phase（Step 本身、Plan、FinalAnswer 等）的路由。
func TestCurrentPhase_X2DoesNotRouteWhenPhaseStep(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "a", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
	}
	snap := builtin_tools.StateSnapshot{
		Phase: builtin_tools.AgentPhaseStep,
		Plan:  plan,
	}
	got := currentPhase(snap, 3)
	if got != builtin_tools.AgentPhaseStep {
		t.Fatalf("Step phase should stay Step, got %q", got)
	}
}

// TestCurrentPhase_TerminalDefenseStillFiresUnderX2：step-terminal-defense
// 优先级高于 X2 guard——所有 step 都 terminal 时应进 FinalAnswer 而非 X2 路由。
func TestCurrentPhase_TerminalDefenseStillFiresUnderX2(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "a", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Status: builtin_tools.PlanStepCompleted, DependsOn: []string{"a"}},
	}
	snap := builtin_tools.StateSnapshot{
		Phase: builtin_tools.AgentPhaseStep,
		Plan:  plan,
	}
	got := currentPhase(snap, 3)
	if got != builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("terminal defense should fire even under X2, got %q", got)
	}
}
