package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// TestShouldEscalateStepReplan_StepError 验证 step 失败时强制升级。
func TestShouldEscalateStepReplan_StepError(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "5")
	a := &Agent{consecutiveStepsSinceReplan: 0}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "b", Status: builtin_tools.PlanStepPending},
		},
	}
	outcome := &builtin_tools.StepOutcome{StepID: "s1", Status: builtin_tools.StepOutcomeFailed}

	escalate, reason := a.shouldEscalateStepReplan(snapshot, outcome)
	if !escalate {
		t.Fatalf("expected escalate=true for failed step, got false")
	}
	if reason != "step_error" {
		t.Fatalf("expected reason=step_error, got %q", reason)
	}
}

// TestShouldEscalateStepReplan_PlanExhausted 验证 plan 中无下一可跑 step 时强制升级。
func TestShouldEscalateStepReplan_PlanExhausted(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "5")
	a := &Agent{consecutiveStepsSinceReplan: 0}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "b", Status: builtin_tools.PlanStepCompleted},
		},
	}
	outcome := &builtin_tools.StepOutcome{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted}

	escalate, reason := a.shouldEscalateStepReplan(snapshot, outcome)
	if !escalate {
		t.Fatalf("expected escalate=true for plan exhausted, got false")
	}
	if reason != "plan_exhausted" {
		t.Fatalf("expected reason=plan_exhausted, got %q", reason)
	}
}

// TestShouldEscalateStepReplan_Heartbeat 验证心跳阈值命中时强制升级。
func TestShouldEscalateStepReplan_Heartbeat(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "3")
	a := &Agent{consecutiveStepsSinceReplan: 3}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "b", Status: builtin_tools.PlanStepPending},
		},
	}
	outcome := &builtin_tools.StepOutcome{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted}

	escalate, reason := a.shouldEscalateStepReplan(snapshot, outcome)
	if !escalate {
		t.Fatalf("expected escalate=true at heartbeat threshold, got false")
	}
	if reason != "heartbeat" {
		t.Fatalf("expected reason=heartbeat, got %q", reason)
	}
}

// TestShouldEscalateStepReplan_Bypass 验证三条触发条件都不命中时允许跳过。
func TestShouldEscalateStepReplan_Bypass(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "5")
	a := &Agent{consecutiveStepsSinceReplan: 1}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "b", Status: builtin_tools.PlanStepPending},
			{ID: "s3", Step: "c", Status: builtin_tools.PlanStepPending},
		},
	}
	outcome := &builtin_tools.StepOutcome{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted}

	escalate, reason := a.shouldEscalateStepReplan(snapshot, outcome)
	if escalate {
		t.Fatalf("expected escalate=false (bypass), got true with reason=%q", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty reason on bypass, got %q", reason)
	}
}

// TestShouldEscalateStepReplan_FailureOverridesAll 验证 step_error 触发条件优先级最高，
// 即使 plan 仍有可跑 step、心跳未到也会立即升级。
func TestShouldEscalateStepReplan_FailureOverridesAll(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "10")
	a := &Agent{consecutiveStepsSinceReplan: 0}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "b", Status: builtin_tools.PlanStepPending},
		},
	}
	outcome := &builtin_tools.StepOutcome{StepID: "s1", Status: builtin_tools.StepOutcomeFailed}

	escalate, reason := a.shouldEscalateStepReplan(snapshot, outcome)
	if !escalate || reason != "step_error" {
		t.Fatalf("expected step_error to win, got (%v, %q)", escalate, reason)
	}
}

// TestShouldEscalateStepReplan_NilOutcomeNoCrash 验证 outcome=nil 时函数不 panic 且不触发 step_error。
func TestShouldEscalateStepReplan_NilOutcomeNoCrash(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "5")
	a := &Agent{consecutiveStepsSinceReplan: 0}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "b", Status: builtin_tools.PlanStepPending},
		},
	}

	escalate, reason := a.shouldEscalateStepReplan(snapshot, nil)
	if escalate {
		t.Fatalf("expected escalate=false with nil outcome, got true reason=%q", reason)
	}
}

// TestStepReplanHeartbeatK_DefaultAndOverride 验证心跳阈值的默认值与环境变量覆盖。
func TestStepReplanHeartbeatK_DefaultAndOverride(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "")
	if got := stepReplanHeartbeatK(); got != 2 {
		t.Fatalf("default heartbeat K expected 2, got %d", got)
	}

	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "3")
	if got := stepReplanHeartbeatK(); got != 3 {
		t.Fatalf("override heartbeat K expected 3, got %d", got)
	}

	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "not-a-number")
	if got := stepReplanHeartbeatK(); got != 2 {
		t.Fatalf("invalid value falls back to default 2, got %d", got)
	}
}

// TestStepReplanBypassDisabled_EnvValues 验证回滚开关解析对常见 truthy 值都成立。
func TestStepReplanBypassDisabled_EnvValues(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"false": false,
		"0":     false,
		"true":  true,
		"1":     true,
		"yes":   true,
		"on":    true,
		"TRUE":  true,
	}
	for v, expect := range cases {
		t.Setenv("STEP_REPLAN_BYPASS_DISABLED", v)
		if got := stepReplanBypassDisabled(); got != expect {
			t.Fatalf("env=%q expected disabled=%v, got %v", v, expect, got)
		}
	}
}

// TestShouldEscalateStepReplan_HeartbeatEveryStep 验证 K=0 时每步必触发心跳（零跳过容忍）。
func TestShouldEscalateStepReplan_HeartbeatEveryStep(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "0")
	a := &Agent{consecutiveStepsSinceReplan: 0}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "b", Status: builtin_tools.PlanStepPending},
		},
	}
	outcome := &builtin_tools.StepOutcome{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted}

	escalate, reason := a.shouldEscalateStepReplan(snapshot, outcome)
	if !escalate {
		t.Fatalf("expected escalate=true at K=0 (every-step heartbeat), got false")
	}
	if reason != "heartbeat" {
		t.Fatalf("expected reason=heartbeat, got %q", reason)
	}
}

// TestShouldEscalateStepReplan_HeartbeatDisabled 验证 K<0 时心跳禁用（K=0 不再禁用，每步触发）。
func TestShouldEscalateStepReplan_HeartbeatDisabled(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "-1")
	a := &Agent{consecutiveStepsSinceReplan: 99}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "b", Status: builtin_tools.PlanStepPending},
		},
	}
	outcome := &builtin_tools.StepOutcome{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted}

	escalate, reason := a.shouldEscalateStepReplan(snapshot, outcome)
	if escalate {
		t.Fatalf("expected escalate=false when heartbeat disabled (K<0), got true reason=%q", reason)
	}
}
