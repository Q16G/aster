package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// buildFinalizeTestAgent 构造一个带真实 workspaceRuntime 的 Agent + 三步 plan：
// s1（主路径 current，已完成）、s2（peer，已完成）、s3（pending）。
// 用于验证 X2 滚动收尾扫描的固化 / 跳过 / 幂等行为。
func buildFinalizeTestAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}

	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "s1", Step: "main", Status: builtin_tools.PlanStepPending},
		{ID: "s2", Step: "peer", Status: builtin_tools.PlanStepPending},
		{ID: "s3", Step: "later", Status: builtin_tools.PlanStepPending},
	}, "init", true)
	tracker.EnsureCurrentStep() // current = s1

	// s1：主路径完成（current 保留为 s1，Phase 翻 StepReplan）。
	tracker.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
		Status:       builtin_tools.PlanStepCompleted,
		Summary:      "main done",
		ShortSummary: "main short",
	})
	// s2：peer 完成（不动 current / Phase）。
	tracker.UpdateInlineStep("s2", builtin_tools.CurrentStepUpdate{
		Status:       builtin_tools.PlanStepCompleted,
		Summary:      "peer done",
		ShortSummary: "peer short",
	})

	a := &Agent{
		state:            tracker,
		asyncRegistry:    NewAsyncAgentRegistry(),
		workspaceRuntime: runtime,
		workspaceRootDir: rootDir,
	}
	return a, rootDir
}

func journalIDs(t *testing.T, rootDir string) map[string]*builtin_tools.PlanItem {
	t.Helper()
	items, _, err := builtin_tools.LoadPlannerJournal(rootDir)
	if err != nil {
		t.Fatalf("load planner journal: %v", err)
	}
	out := make(map[string]*builtin_tools.PlanItem, len(items))
	for _, it := range items {
		if it != nil {
			out[it.ID] = it
		}
	}
	return out
}

// TestFinalizeUnjournaledTerminalSteps_JournalsPeerSkipsCurrent 验证缺陷一修复：
// X2 滚动收尾扫描把已终态的 peer（s2）烘焙并写入 planner.jsonl，
// 同时跳过 current（s1）与未终态（s3）。
func TestFinalizeUnjournaledTerminalSteps_JournalsPeerSkipsCurrent(t *testing.T) {
	a, rootDir := buildFinalizeTestAgent(t)

	a.finalizeUnjournaledTerminalSteps(a.state.Snapshot())

	journaled := journalIDs(t, rootDir)
	if _, ok := journaled["s2"]; !ok {
		t.Fatalf("expected peer s2 journaled, journal=%v", keysOf(journaled))
	}
	if _, ok := journaled["s1"]; ok {
		t.Fatalf("current step s1 should be skipped by sweep, journal=%v", keysOf(journaled))
	}
	if _, ok := journaled["s3"]; ok {
		t.Fatalf("pending step s3 should not be journaled, journal=%v", keysOf(journaled))
	}

	// 烘焙字段落入 planner.jsonl（peer 的 short_summary 写回 plan_item）。
	if s2 := journaled["s2"]; s2 == nil || s2.ShortSummary != "peer short" {
		t.Fatalf("expected s2 baked ShortSummary=%q, got %+v", "peer short", s2)
	}

	if !a.stepAlreadyJournaled("s2") {
		t.Fatalf("expected s2 marked journaled")
	}
	if a.stepAlreadyJournaled("s1") {
		t.Fatalf("current s1 must not be marked journaled by sweep")
	}
}

// TestFinalizeTerminalStep_Idempotent 验证幂等：重复固化同一 step 第二次直接 no-op，
// 不重复落盘（planner.jsonl 内该 ID 仍为单条合并记录）。
func TestFinalizeTerminalStep_Idempotent(t *testing.T) {
	a, rootDir := buildFinalizeTestAgent(t)
	snap := a.state.Snapshot()

	if did := a.finalizeTerminalStep("s2", snap); !did {
		t.Fatalf("first finalize should journal s2")
	}
	if did := a.finalizeTerminalStep("s2", snap); did {
		t.Fatalf("second finalize of s2 should be no-op (already journaled)")
	}

	journaled := journalIDs(t, rootDir)
	if _, ok := journaled["s2"]; !ok {
		t.Fatalf("expected s2 present once, journal=%v", keysOf(journaled))
	}
}

// TestShouldEscalateStepReplan_InProgressPeerBlocksPlanExhausted 验证缺陷二防御：
// ready 枯竭（plan_exhausted）但仍有 in_progress inline peer 在跑时不升级，
// 等 peer 落定后才允许升级（与无 peer 时的 plan_exhausted 升级对比）。
func TestShouldEscalateStepReplan_InProgressPeerBlocksPlanExhausted(t *testing.T) {
	t.Setenv("STEP_REPLAN_HEARTBEAT_K", "-1") // 禁用心跳，仅测 plan_exhausted 维度
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "a", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "b", Status: builtin_tools.PlanStepInProgress}, // peer 仍在跑，无 ready
		},
	}
	outcome := &builtin_tools.StepOutcome{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted}

	// 有 in_progress peer → 不升级（纵深防御）。
	withPeer := &Agent{asyncRegistry: NewAsyncAgentRegistry()}
	withPeer.asyncRegistry.RegisterInlineStep("s2", "")
	if escalate, reason := withPeer.shouldEscalateStepReplan(snapshot, outcome); escalate {
		t.Fatalf("expected no escalation while inline peer running, got escalate=true reason=%q", reason)
	}

	// 无 in_progress peer（registry 空）→ plan_exhausted 正常升级。
	noPeer := &Agent{asyncRegistry: NewAsyncAgentRegistry()}
	if escalate, reason := noPeer.shouldEscalateStepReplan(snapshot, outcome); !escalate || reason != "plan_exhausted" {
		t.Fatalf("expected plan_exhausted escalation with no peer, got (%v, %q)", escalate, reason)
	}
}

func keysOf(m map[string]*builtin_tools.PlanItem) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
