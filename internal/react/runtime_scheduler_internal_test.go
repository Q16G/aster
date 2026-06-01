package react

import (
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type noopChatClientForScheduler struct{}

func (s *noopChatClientForScheduler) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (s *noopChatClientForScheduler) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (s *noopChatClientForScheduler) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestScheduler_FallbackDoesNotSwallowFinalAnswerError(t *testing.T) {
	agent, err := NewReActAgent("test", &noopChatClientForScheduler{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}
	// Intentionally do NOT configure workspaceRuntime: runFinalAnswerPhase should fail
	// while handling a plan-phase error.
	agent.workspaceRuntime = nil

	res, runErr := agent.runSchedulerLoop(context.Background(), nil, "", nil, 1)
	if runErr == nil {
		t.Fatalf("expected error, got result=%#v", res)
	}
	if res != nil {
		t.Fatalf("expected nil result on error, got %#v", res)
	}
	msg := runErr.Error()
	if !strings.Contains(msg, "input timeline is empty") {
		t.Fatalf("expected original phase error to be present, got: %s", msg)
	}
	if !strings.Contains(msg, "final_answer error") {
		t.Fatalf("expected final_answer error context, got: %s", msg)
	}
	if !strings.Contains(msg, "workspace runtime is nil") {
		t.Fatalf("expected final answer root cause, got: %s", msg)
	}
}

func TestMergeReplannedPlan_StepTextDedup(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "step-1", Step: "加载项目结构", Status: builtin_tools.PlanStepCompleted},
		{ID: "step-2", Step: "分析 SQL 注入风险", Status: builtin_tools.PlanStepCompleted},
		{ID: "step-3", Step: "验证数据流路径", Status: builtin_tools.PlanStepInProgress},
	}
	next := []*builtin_tools.PlanItem{
		{ID: "step-1", Step: "加载项目结构", Status: builtin_tools.PlanStepCompleted},
		{ID: "step-2", Step: "分析 SQL 注入风险", Status: builtin_tools.PlanStepCompleted},
		{ID: "step-3", Step: "验证数据流路径", Status: builtin_tools.PlanStepInProgress},
		{ID: "step-new-1", Step: "分析  SQL 注入风险", Status: builtin_tools.PlanStepPending},
		{ID: "step-new-2", Step: "输出审计报告", Status: builtin_tools.PlanStepPending},
	}

	merged := mergeReplannedPlan(prev, next)

	var ids []string
	for _, item := range merged {
		ids = append(ids, item.ID)
	}
	expected := []string{"step-1", "step-2", "step-3", "step-new-2"}
	if len(ids) != len(expected) {
		t.Fatalf("expected %d items %v, got %d items %v", len(expected), expected, len(ids), ids)
	}
	for i, id := range expected {
		if ids[i] != id {
			t.Fatalf("item[%d]: expected %q, got %q", i, id, ids[i])
		}
	}
}

// TestMergeReplannedPlan_DanglingDepsCaughtByNormalize verifies that when a
// replan generates new steps whose depends_on references old pending step IDs
// that were discarded by mergeReplannedPlan, NormalizePlanItems rejects the
// plan. This means the "dangling dep" scenario cannot silently produce
// unreachable pending steps — the plan phase would fail with an explicit error.
func TestMergeReplannedPlan_DanglingDepsCaughtByNormalize(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "基础侦察", Status: builtin_tools.PlanStepCompleted},
		{ID: "get-cred", Step: "获取登录凭证", Status: builtin_tools.PlanStepPending},
		{ID: "auth-test", Step: "认证测试", Status: builtin_tools.PlanStepPending, DependsOn: []string{"get-cred"}},
	}
	next := []*builtin_tools.PlanItem{
		{ID: "install-deps", Step: "安装环境依赖", Status: builtin_tools.PlanStepPending, DependsOn: []string{"get-cred"}},
		{ID: "captcha-script", Step: "编写验证码识别脚本", Status: builtin_tools.PlanStepPending, DependsOn: []string{"install-deps"}},
	}

	merged := mergeReplannedPlan(prev, next)

	for _, item := range merged {
		if item.ID == "get-cred" {
			t.Fatalf("old pending step 'get-cred' should have been discarded by merge")
		}
	}

	// NormalizePlanItems MUST reject the plan — "get-cred" was discarded
	// but "install-deps" still references it.
	_, err := builtin_tools.NormalizePlanItems(merged, true)
	if err == nil {
		t.Fatalf("expected NormalizePlanItems to reject plan with dangling dependency, got nil")
	}
	if !strings.Contains(err.Error(), "get-cred") {
		t.Fatalf("expected error to mention dangling dep 'get-cred', got: %s", err)
	}
	t.Logf("confirmed: NormalizePlanItems catches dangling dep after merge — error: %s", err)
}

// TestMergeReplannedPlan_NoDepsStepsAlwaysRunnable verifies that pending
// steps without depends_on are always picked up by NextRunnablePlanStepID.
// If the pentest session ended with such steps still pending, the cause is
// NOT the scheduler — it must be the StepReplan/FinalAnswer model decision.
func TestMergeReplannedPlan_NoDepsStepsAlwaysRunnable(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "基础侦察", Status: builtin_tools.PlanStepCompleted},
		{ID: "vuln-test", Step: "漏洞测试", Status: builtin_tools.PlanStepCompleted},
		{ID: "summary", Step: "汇总所有发现", Status: builtin_tools.PlanStepCompleted},
		// Credential steps with NO depends_on (common when model doesn't emit deps)
		{ID: "install-deps", Step: "安装环境依赖", Status: builtin_tools.PlanStepPending},
		{ID: "captcha-script", Step: "编写验证码识别脚本", Status: builtin_tools.PlanStepPending},
		{ID: "try-passwords", Step: "尝试弱密码登录", Status: builtin_tools.PlanStepPending},
	}
	normalized, err := builtin_tools.NormalizePlanItems(plan, true)
	if err != nil {
		t.Fatalf("NormalizePlanItems failed: %v", err)
	}

	nextID := builtin_tools.NextRunnablePlanStepID(normalized)
	if nextID != "install-deps" {
		t.Fatalf("expected NextRunnablePlanStepID='install-deps' (no deps → always runnable), got %q", nextID)
	}
	t.Logf("confirmed: pending steps without depends_on are always runnable — scheduler would NOT skip them")
}

func TestMergeReplannedPlan_ValidDependencyChain(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "基础侦察", Status: builtin_tools.PlanStepCompleted},
		{ID: "old-pending", Step: "旧的 pending 步骤", Status: builtin_tools.PlanStepPending},
	}
	// New steps depend on completed "recon" — dependencies will survive merge.
	next := []*builtin_tools.PlanItem{
		{ID: "install-deps", Step: "安装环境依赖", Status: builtin_tools.PlanStepPending, DependsOn: []string{"recon"}},
		{ID: "captcha-script", Step: "编写验证码识别脚本", Status: builtin_tools.PlanStepPending, DependsOn: []string{"install-deps"}},
	}

	merged := mergeReplannedPlan(prev, next)
	normalized, err := builtin_tools.NormalizePlanItems(merged, true)
	if err != nil {
		t.Fatalf("NormalizePlanItems failed: %v", err)
	}

	nextID := builtin_tools.NextRunnablePlanStepID(normalized)
	if nextID != "install-deps" {
		t.Fatalf("expected NextRunnablePlanStepID='install-deps', got %q", nextID)
	}
}

func TestMergeReplannedPlan_SelfContainedNewChain(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "基础侦察", Status: builtin_tools.PlanStepCompleted},
		{ID: "old-pending", Step: "旧的 pending 步骤", Status: builtin_tools.PlanStepPending},
	}
	// New steps form a self-contained chain — first step has no dependencies.
	next := []*builtin_tools.PlanItem{
		{ID: "install-deps", Step: "安装环境依赖", Status: builtin_tools.PlanStepPending},
		{ID: "captcha-script", Step: "编写验证码识别脚本", Status: builtin_tools.PlanStepPending, DependsOn: []string{"install-deps"}},
		{ID: "try-passwords", Step: "尝试弱密码登录", Status: builtin_tools.PlanStepPending, DependsOn: []string{"captcha-script"}},
	}

	merged := mergeReplannedPlan(prev, next)
	normalized, err := builtin_tools.NormalizePlanItems(merged, true)
	if err != nil {
		t.Fatalf("NormalizePlanItems failed: %v", err)
	}

	nextID := builtin_tools.NextRunnablePlanStepID(normalized)
	if nextID != "install-deps" {
		t.Fatalf("expected NextRunnablePlanStepID='install-deps', got %q", nextID)
	}
}

// TestNextRunnableReturnsEmptyWhenAllCompleted verifies that
// NextRunnablePlanStepID returns "" when all steps are completed —
// this is the direct trigger for the scheduler to enter FinalAnswer.
// If a sub-agent has pending steps in its own plan, the root scheduler
// does NOT see them (separate plan objects).
func TestNextRunnableReturnsEmptyWhenAllCompleted(t *testing.T) {
	rootPlan := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "全局侦察", Status: builtin_tools.PlanStepCompleted},
		{ID: "vuln-scan", Step: "漏洞扫描", Status: builtin_tools.PlanStepCompleted},
		{ID: "auth-review", Step: "认证授权审计", Status: builtin_tools.PlanStepCompleted},
		{ID: "biz-logic", Step: "业务逻辑审计", Status: builtin_tools.PlanStepCompleted},
		{ID: "dep-audit", Step: "依赖安全审计", Status: builtin_tools.PlanStepCompleted},
		{ID: "dataflow", Step: "数据流分析", Status: builtin_tools.PlanStepCompleted},
		{ID: "summary", Step: "汇总所有发现", Status: builtin_tools.PlanStepCompleted},
	}

	// Root scheduler only sees rootPlan — sub-agent plan is separate
	nextID := builtin_tools.NextRunnablePlanStepID(rootPlan)
	if nextID != "" {
		t.Fatalf("expected empty NextRunnablePlanStepID for all-completed plan, got %q", nextID)
	}

	if !builtin_tools.AllPlanStepsTerminal(rootPlan) {
		t.Fatal("all-completed plan should be terminal")
	}

	// Meanwhile, sub-agent plan (separate object) still has pending steps
	subPlan := []*builtin_tools.PlanItem{
		{ID: "install-deps", Step: "安装环境依赖", Status: builtin_tools.PlanStepPending},
		{ID: "captcha-script", Step: "编写验证码识别脚本", Status: builtin_tools.PlanStepPending},
		{ID: "try-passwords", Step: "尝试弱密码登录", Status: builtin_tools.PlanStepPending},
	}

	subNextID := builtin_tools.NextRunnablePlanStepID(subPlan)
	if subNextID == "" {
		t.Fatal("sub-agent plan should have runnable steps")
	}

	t.Log("CONFIRMED: root scheduler sees empty plan → enters FinalAnswer.")
	t.Log("Sub-agent plan has runnable steps but is invisible to root scheduler.")
	t.Log("This is the mechanism: root terminates while sub-agent pending steps persist in sidebar.")
}

// TestPendingStepBlockedByInProgressDep verifies that a pending step
// whose dependency is still in_progress (not completed) will NOT be
// picked by NextRunnablePlanStepID. Only completed deps count as ready.
func TestPendingStepBlockedByInProgressDep(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "侦察", Status: builtin_tools.PlanStepCompleted},
		{ID: "auth-test", Step: "认证测试", Status: builtin_tools.PlanStepInProgress},
		{ID: "post-auth", Step: "认证后测试", Status: builtin_tools.PlanStepPending, DependsOn: []string{"auth-test"}},
		{ID: "report", Step: "生成报告", Status: builtin_tools.PlanStepPending, DependsOn: []string{"post-auth"}},
	}
	normalized, err := builtin_tools.NormalizePlanItems(plan, true)
	if err != nil {
		t.Fatalf("NormalizePlanItems failed: %v", err)
	}

	nextID := builtin_tools.NextRunnablePlanStepID(normalized)
	// post-auth depends on auth-test which is in_progress (NOT completed)
	// → should NOT be runnable
	if nextID != "" {
		t.Fatalf("expected no runnable step (in_progress dep blocks), got %q", nextID)
	}

	// Verify not terminal — there are still non-terminal steps
	if builtin_tools.AllPlanStepsTerminal(normalized) {
		t.Fatal("plan with in_progress and pending steps should not be terminal")
	}

	t.Log("CONFIRMED: in_progress dep blocks downstream pending steps.")
	t.Log("If a step stays in_progress indefinitely, its dependents are stuck.")
}

// TestApplyReplanResultEntersFinalAnswerWhenNoPendingSteps simulates
// the exact scenario from phase_step_replan.go:174-181: when StepReplan
// decides should_replan=false and NextRunnablePlanStepID returns "",
// the scheduler enters FinalAnswer.
func TestApplyReplanResultEntersFinalAnswerWhenNoPendingSteps(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "侦察", Status: builtin_tools.PlanStepCompleted},
		{ID: "vuln-scan", Step: "漏洞扫描", Status: builtin_tools.PlanStepCompleted},
		{ID: "summary", Step: "汇总", Status: builtin_tools.PlanStepCompleted},
	}

	// Simulate the decision logic from applyReplanResult
	var replanContext *struct{ NextGoal string } // nil = should_replan=false
	nextPhase := "final_answer"
	nextRunnableStepID := ""

	if replanContext != nil {
		nextPhase = "plan"
	} else if candidate := builtin_tools.NextRunnablePlanStepID(plan); candidate != "" {
		nextRunnableStepID = candidate
		nextPhase = "step"
	}

	if nextPhase != "final_answer" {
		t.Fatalf("expected final_answer phase, got %q (nextRunnable=%q)", nextPhase, nextRunnableStepID)
	}
	t.Log("CONFIRMED: all-completed plan + should_replan=false → FinalAnswer phase.")
}

// TestCurrentPhaseGuard exercises the defensive reroute in currentPhase with a
// table of positive cases (must reroute to FinalAnswer) and negative cases
// (must keep the stored phase). The guard only fires for a plan that is parked
// in Step phase, has no runnable step, AND is fully terminal — the exact state
// that caused the observed sub-agent spin.
func TestCurrentPhaseGuard(t *testing.T) {
	completed := builtin_tools.PlanStepCompleted
	failed := builtin_tools.PlanStepFailed
	skipped := builtin_tools.PlanStepSkipped
	pending := builtin_tools.PlanStepPending
	inProgress := builtin_tools.PlanStepInProgress

	step := func(id string, status builtin_tools.PlanStepStatus) *builtin_tools.PlanItem {
		return &builtin_tools.PlanItem{ID: id, Step: id, Status: status}
	}

	cases := []struct {
		name  string
		phase builtin_tools.AgentPhase
		plan  []*builtin_tools.PlanItem
		want  builtin_tools.AgentPhase
	}{
		// ---- positive: guard must reroute to final_answer ----
		{
			name:  "step/all completed",
			phase: builtin_tools.AgentPhaseStep,
			plan:  []*builtin_tools.PlanItem{step("a", completed), step("b", completed)},
			want:  builtin_tools.AgentPhaseFinalAnswer,
		},
		{
			name:  "step/mixed terminal (completed+failed+skipped)",
			phase: builtin_tools.AgentPhaseStep,
			plan:  []*builtin_tools.PlanItem{step("a", completed), step("b", failed), step("c", skipped)},
			want:  builtin_tools.AgentPhaseFinalAnswer,
		},
		{
			name:  "step/single completed",
			phase: builtin_tools.AgentPhaseStep,
			plan:  []*builtin_tools.PlanItem{step("only", completed)},
			want:  builtin_tools.AgentPhaseFinalAnswer,
		},

		// ---- negative: guard must NOT fire ----
		{
			name:  "step/has runnable pending",
			phase: builtin_tools.AgentPhaseStep,
			plan:  []*builtin_tools.PlanItem{step("a", completed), step("b", pending)},
			want:  builtin_tools.AgentPhaseStep,
		},
		{
			// No runnable step (pending blocked by in_progress dep) but NOT all
			// terminal — a step is still running, so we must NOT force-finalize.
			name:  "step/in_progress still running, dependent blocked",
			phase: builtin_tools.AgentPhaseStep,
			plan: []*builtin_tools.PlanItem{
				step("a", completed),
				step("b", inProgress),
				{ID: "c", Step: "c", Status: pending, DependsOn: []string{"b"}},
			},
			want: builtin_tools.AgentPhaseStep,
		},
		{
			name:  "step/empty plan (handled by plan phase)",
			phase: builtin_tools.AgentPhaseStep,
			plan:  nil,
			want:  builtin_tools.AgentPhaseStep,
		},
		{
			name:  "plan/all terminal stays plan",
			phase: builtin_tools.AgentPhasePlan,
			plan:  []*builtin_tools.PlanItem{step("a", completed)},
			want:  builtin_tools.AgentPhasePlan,
		},
		{
			name:  "step_replan/all terminal stays step_replan",
			phase: builtin_tools.AgentPhaseStepReplan,
			plan:  []*builtin_tools.PlanItem{step("a", completed)},
			want:  builtin_tools.AgentPhaseStepReplan,
		},
		{
			name:  "final_answer/all terminal stays final_answer",
			phase: builtin_tools.AgentPhaseFinalAnswer,
			plan:  []*builtin_tools.PlanItem{step("a", completed)},
			want:  builtin_tools.AgentPhaseFinalAnswer,
		},
		{
			name:  "unknown phase defaults to plan",
			phase: builtin_tools.AgentPhase("bogus"),
			plan:  []*builtin_tools.PlanItem{step("a", completed)},
			want:  builtin_tools.AgentPhasePlan,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := builtin_tools.StateSnapshot{Phase: tc.phase, Plan: tc.plan}
			if got := currentPhase(snap); got != tc.want {
				t.Fatalf("currentPhase(phase=%q) = %q, want %q", tc.phase, got, tc.want)
			}
		})
	}
}

func TestNormalizeStepText(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"分析  SQL 注入风险", "分析 sql 注入风险"},
		{"分析 SQL 注入风险", "分析 sql 注入风险"},
		{"加载项目结构（P0）", "加载项目结构(p0)"},
		{"  ", ""},
	}
	for _, tc := range cases {
		got := normalizeStepText(tc.in)
		if got != tc.want {
			t.Errorf("normalizeStepText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
