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

// TestCurrentPhase_FrontierReleasesMultiLaneReady：跨 phase lane 的 ready step
// 都进入 frontier——StepReplan 下 X2 guard 因 frontier 非空绕回 Step。
func TestCurrentPhase_FrontierReleasesMultiLaneReady(t *testing.T) {
	phases := []*builtin_tools.PlanPhase{
		{ID: "phase-a", Status: builtin_tools.PlanPhasePending},
		{ID: "phase-b", Status: builtin_tools.PlanPhasePending},
	}
	plan := []*builtin_tools.PlanItem{
		{ID: "a1", Status: builtin_tools.PlanStepCompleted, PhaseID: "phase-a"},
		{ID: "b1", Status: builtin_tools.PlanStepPending, PhaseID: "phase-b"},
	}
	snap := builtin_tools.StateSnapshot{Phase: builtin_tools.AgentPhaseStepReplan, Plan: plan, Phases: phases}
	if got := currentPhase(snap, 3); got != builtin_tools.AgentPhaseStep {
		t.Fatalf("cross-lane frontier ready should route back to Step, got %q", got)
	}
}

// TestCurrentPhase_PhaseLockedNotReleased：被 depends_on 锁住的 phase 下 step 不进 frontier；
// 唯一未完成 lane 被锁 + 所有 step terminal 时不成立，此处验证 barrier 判定用 frontier。
func TestCurrentPhase_PhaseLockedHoldsStepReplan(t *testing.T) {
	phases := []*builtin_tools.PlanPhase{
		{ID: "phase-a", Status: builtin_tools.PlanPhasePending},
		{ID: "phase-b", Status: builtin_tools.PlanPhasePending, DependsOn: []string{"phase-a"}},
	}
	plan := []*builtin_tools.PlanItem{
		{ID: "a1", Status: builtin_tools.PlanStepInProgress, PhaseID: "phase-a"},
		{ID: "b1", Status: builtin_tools.PlanStepPending, PhaseID: "phase-b"},
	}
	// phase-b 被 phase-a 锁住，b1 不进 frontier；a1 in_progress 非 pending。frontier=空。
	snap := builtin_tools.StateSnapshot{Phase: builtin_tools.AgentPhaseStepReplan, Plan: plan, Phases: phases}
	if got := currentPhase(snap, 3); got != builtin_tools.AgentPhaseStepReplan {
		t.Fatalf("locked phase must not release frontier, stay StepReplan, got %q", got)
	}
}

// TestCurrentPhase_AllTerminalPhasesUnsettledRoutesStepReplan：D6 双条件——所有 step
// terminal 但仍有 phase 未 settled 时回 StepReplan（让 phase_assessments 定夺），不直达 final。
func TestCurrentPhase_AllTerminalPhasesUnsettledRoutesStepReplan(t *testing.T) {
	phases := []*builtin_tools.PlanPhase{{ID: "phase-a", Status: builtin_tools.PlanPhasePending}}
	plan := []*builtin_tools.PlanItem{{ID: "a1", Status: builtin_tools.PlanStepCompleted, PhaseID: "phase-a"}}
	snap := builtin_tools.StateSnapshot{Phase: builtin_tools.AgentPhaseStep, Plan: plan, Phases: phases}
	if got := currentPhase(snap, 3); got != builtin_tools.AgentPhaseStepReplan {
		t.Fatalf("all-terminal + unsettled phase should route StepReplan, got %q", got)
	}

	// phase settled → 直达 FinalAnswer
	phases[0].Status = builtin_tools.PlanPhaseCompleted
	snap = builtin_tools.StateSnapshot{Phase: builtin_tools.AgentPhaseStep, Plan: plan, Phases: phases}
	if got := currentPhase(snap, 3); got != builtin_tools.AgentPhaseFinalAnswer {
		t.Fatalf("all-terminal + settled phases should route FinalAnswer, got %q", got)
	}
}

// TestCollectInlineStepIDs_CrossPhaseFanout：不同 phase lane 的 ready step 汇入同一 frontier
// fan-out（受 maxParallel 全局上限约束）。
func TestCollectInlineStepIDs_CrossPhaseFanout(t *testing.T) {
	a := &Agent{cfg: &AgentConfig{MaxParallelSteps: 3}, asyncRegistry: NewAsyncAgentRegistry()}
	phases := []*builtin_tools.PlanPhase{
		{ID: "phase-a", Status: builtin_tools.PlanPhasePending},
		{ID: "phase-b", Status: builtin_tools.PlanPhasePending},
	}
	plan := []*builtin_tools.PlanItem{
		{ID: "a1", Status: builtin_tools.PlanStepPending, PhaseID: "phase-a"},
		{ID: "b1", Status: builtin_tools.PlanStepPending, PhaseID: "phase-b"},
	}
	snap := builtin_tools.StateSnapshot{Plan: plan, Phases: phases, CurrentStepID: "a1"}
	got := a.collectInlineStepIDs(snap)
	if len(got) != 2 || got[0] != "a1" {
		t.Fatalf("expected [a1 b1] cross-phase fanout, got %v", got)
	}
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	if !seen["b1"] {
		t.Fatalf("cross-phase peer b1 must be included, got %v", got)
	}
}

// TestFrontier_MaxParallel1_SerialDegenerate：max_parallel_steps=1 时 collectInlineStepIDs
// 只含主路径（无 peer），退化等价现状串行。
func TestFrontier_MaxParallel1_SerialDegenerate(t *testing.T) {
	a := &Agent{cfg: &AgentConfig{MaxParallelSteps: 1}, asyncRegistry: NewAsyncAgentRegistry()}
	phases := []*builtin_tools.PlanPhase{
		{ID: "phase-a", Status: builtin_tools.PlanPhasePending},
		{ID: "phase-b", Status: builtin_tools.PlanPhasePending},
	}
	plan := []*builtin_tools.PlanItem{
		{ID: "a1", Status: builtin_tools.PlanStepPending, PhaseID: "phase-a"},
		{ID: "b1", Status: builtin_tools.PlanStepPending, PhaseID: "phase-b"},
	}
	snap := builtin_tools.StateSnapshot{Plan: plan, Phases: phases, CurrentStepID: "a1"}
	got := a.collectInlineStepIDs(snap)
	if len(got) != 1 || got[0] != "a1" {
		t.Fatalf("serial (max=1) should only run main path, got %v", got)
	}
}
