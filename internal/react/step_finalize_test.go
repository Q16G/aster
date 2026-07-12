package react

import (
	"context"
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
	items, _, err := LoadPlannerJournal(rootDir)
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

// TestFinalizeUnjournaledTerminalSteps_SkipsPeerUntilDrained 验证修复 A(竞态):
// peer auto-complete 翻终态后、其 registry entry 被 drain/purge 之前,sweep 必须跳过它——
// 否则会把缺 TranscriptBlobRef 的半成品永久落盘。entry purge 后才允许固化。
func TestFinalizeUnjournaledTerminalSteps_SkipsPeerUntilDrained(t *testing.T) {
	a, rootDir := buildFinalizeTestAgent(t)
	// 模拟 s2 的 peer goroutine 已翻终态(buildFinalizeTestAgent 里已 UpdateInlineStep completed),
	// 但其 registry entry 仍在(尚未 Complete→drain→purge)。
	a.asyncRegistry.RegisterInlineStep("s2", "")

	a.finalizeUnjournaledTerminalSteps(a.state.Snapshot())
	if _, ok := journalIDs(t, rootDir)["s2"]; ok {
		t.Fatalf("s2 must NOT be journaled while its registry entry is present (not yet drained)")
	}
	if a.stepAlreadyJournaled("s2") {
		t.Fatalf("s2 must not be marked journaled while undrained")
	}

	// 完成 + drain → entry 被 purge、ref 回写到位 → 再 sweep 应固化。
	a.asyncRegistry.Complete("s2", &builtin_tools.RunResult{Success: true, Result: "s2 done"})
	a.drainAsyncAgentNotifications(context.Background())
	if a.asyncRegistry.Get("s2") != nil {
		t.Fatalf("setup: s2 registry entry should be purged after drain")
	}

	a.finalizeUnjournaledTerminalSteps(a.state.Snapshot())
	if _, ok := journalIDs(t, rootDir)["s2"]; !ok {
		t.Fatalf("s2 should be journaled after its peer fully drained")
	}
}

// TestFinalizeUnjournaledTerminalSteps_SkipsSkippedWithoutOutcome 验证修复 B(串行行为不变):
// 被依赖失败传播为 skipped 的 step 无 outcome,sweep 不应为其写 kind=step 记录。
func TestFinalizeUnjournaledTerminalSteps_SkipsSkippedWithoutOutcome(t *testing.T) {
	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "s1", Step: "main", Status: builtin_tools.PlanStepPending},
		{ID: "s4", Step: "dep", Status: builtin_tools.PlanStepPending, DependsOn: []string{"s1"}},
	}, "init", true)
	tracker.EnsureCurrentStep() // current = s1
	// s1 失败 → PropagateSkippedPlanSteps 把依赖 s1 的 s4 标 skipped(s4 无 outcome)。
	tracker.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
		Status:  builtin_tools.PlanStepFailed,
		Summary: "boom",
	})
	a := &Agent{
		state:            tracker,
		asyncRegistry:    NewAsyncAgentRegistry(),
		workspaceRuntime: runtime,
		workspaceRootDir: rootDir,
	}

	a.finalizeUnjournaledTerminalSteps(a.state.Snapshot())

	j := journalIDs(t, rootDir)
	if _, ok := j["s4"]; ok {
		t.Fatalf("skipped step s4 without outcome should not be journaled (serial behavior preserved)")
	}
	if _, ok := j["s1"]; ok {
		t.Fatalf("current step s1 should be skipped by sweep, journal=%v", keysOf(j))
	}
}

func keysOf(m map[string]*builtin_tools.PlanItem) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
