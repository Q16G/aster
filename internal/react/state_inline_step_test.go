package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// setupFanOutPlan 构造 fan-out 测试 plan：
//
//	a (Completed) → b, c (Pending, depends a) → d (Pending, depends b+c)
//
// EnsureCurrentStep 选首个 ready (b) 作为 CurrentStepID，方便测试中以 c 模拟远程 step。
func setupFanOutPlan(t *testing.T) *StateTracker {
	t.Helper()
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "前置", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Step: "并发 b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "c", Step: "并发 c", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "d", Step: "汇总", Status: builtin_tools.PlanStepPending, DependsOn: []string{"b", "c"}},
	}, "init", true)
	tracker.EnsureCurrentStep() // 选首个 ready pending → b 作为主路径 current
	return tracker
}

func planItemByIDInSnap(snap builtin_tools.StateSnapshot, id string) *builtin_tools.PlanItem {
	for _, it := range snap.Plan {
		if it != nil && it.ID == id {
			return it
		}
	}
	return nil
}

func TestUpdateInlineStep_EmptyStepID(t *testing.T) {
	tracker := setupFanOutPlan(t)
	tracker.SetPhase(builtin_tools.AgentPhaseStep)
	before := tracker.Snapshot()
	snap := tracker.UpdateInlineStep("", builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepCompleted,
	})
	if snap.Phase != before.Phase {
		t.Fatalf("phase changed for empty stepID: %q -> %q", before.Phase, snap.Phase)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if planItemByIDInSnap(snap, id).Status != planItemByIDInSnap(before, id).Status {
			t.Fatalf("step %s status changed unexpectedly", id)
		}
	}
}

func TestUpdateInlineStep_StepNotFound(t *testing.T) {
	tracker := setupFanOutPlan(t)
	tracker.SetPhase(builtin_tools.AgentPhaseStep)
	before := tracker.Snapshot()
	snap := tracker.UpdateInlineStep("nonexistent", builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepCompleted,
	})
	if snap.Phase != before.Phase {
		t.Fatalf("phase changed for unknown stepID")
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if planItemByIDInSnap(snap, id).Status != planItemByIDInSnap(before, id).Status {
			t.Fatalf("step %s status changed unexpectedly", id)
		}
	}
}

func TestUpdateInlineStep_CompletesItem(t *testing.T) {
	tracker := setupFanOutPlan(t)
	snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status:        builtin_tools.PlanStepCompleted,
		StatusSummary: "ok",
		ShortSummary:  "远程 c 完成",
		KeyFacts:      []string{"fact1"},
		Summary:       "summary text",
	})
	c := planItemByIDInSnap(snap, "c")
	if c.Status != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected c Completed, got %q", c.Status)
	}
	var found bool
	for _, oc := range snap.StepOutcomes {
		if oc != nil && oc.StepID == "c" {
			found = true
			if oc.Status != builtin_tools.StepOutcomeCompleted {
				t.Fatalf("outcome status = %q, want completed", oc.Status)
			}
			if oc.ShortSummary != "远程 c 完成" {
				t.Fatalf("short_summary not upserted, got %q", oc.ShortSummary)
			}
			break
		}
	}
	if !found {
		t.Fatalf("expected StepOutcome upserted for c")
	}
	// Progress: a + c = 2/4 = 50
	if snap.Progress != 50 {
		t.Fatalf("expected Progress=50, got %d", snap.Progress)
	}
}

func TestUpdateInlineStep_DoesNotTouchPhase(t *testing.T) {
	tracker := setupFanOutPlan(t)
	tracker.SetPhase(builtin_tools.AgentPhaseStep)
	snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepCompleted,
	})
	if snap.Phase != builtin_tools.AgentPhaseStep {
		t.Fatalf("expected Phase unchanged (Step), got %q", snap.Phase)
	}
}

func TestUpdateInlineStep_DoesNotTouchCurrentStepID(t *testing.T) {
	tracker := setupFanOutPlan(t)
	before := tracker.Snapshot()
	if before.CurrentStepID != "b" {
		t.Fatalf("setup precondition: expected CurrentStepID=b, got %q", before.CurrentStepID)
	}
	snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepCompleted,
	})
	if snap.CurrentStepID != "b" {
		t.Fatalf("expected CurrentStepID unchanged (b), got %q", snap.CurrentStepID)
	}
}

func TestUpdateInlineStep_FailedPropagatesSkipped(t *testing.T) {
	tracker := setupFanOutPlan(t)
	snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepFailed,
		Error:  "remote crash",
	})
	c := planItemByIDInSnap(snap, "c")
	if c.Status != builtin_tools.PlanStepFailed {
		t.Fatalf("expected c Failed, got %q", c.Status)
	}
	// d 同时依赖 b 和 c；c 失败导致 d 被标 Skipped
	d := planItemByIDInSnap(snap, "d")
	if d.Status != builtin_tools.PlanStepSkipped {
		t.Fatalf("expected d Skipped (depends on failed c), got %q", d.Status)
	}
	// b 不依赖 c，不受影响（仍 Pending）
	b := planItemByIDInSnap(snap, "b")
	if b.Status != builtin_tools.PlanStepPending {
		t.Fatalf("expected b Pending (not affected), got %q", b.Status)
	}
}

func TestUpdateInlineStep_RejectsNonTerminalStatus(t *testing.T) {
	// Status 守卫：误传 Pending / InProgress 应被 no-op 拒收，PlanItem 和 StepOutcome 都不变。
	for _, badStatus := range []builtin_tools.PlanStepStatus{
		builtin_tools.PlanStepPending,
		builtin_tools.PlanStepInProgress,
		"",                    // 空字符串也是非法
		"unknown_status_xxx",  // 未知枚举
	} {
		t.Run(string(badStatus), func(t *testing.T) {
			tracker := setupFanOutPlan(t)
			beforeC := planItemByIDInSnap(tracker.Snapshot(), "c")
			beforeOutcomes := len(tracker.Snapshot().StepOutcomes)

			snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
				Status:       badStatus,
				ShortSummary: "should not be written",
			})

			afterC := planItemByIDInSnap(snap, "c")
			if afterC.Status != beforeC.Status {
				t.Fatalf("status=%q: PlanItem status changed %q -> %q", badStatus, beforeC.Status, afterC.Status)
			}
			if len(snap.StepOutcomes) != beforeOutcomes {
				t.Fatalf("status=%q: StepOutcomes count changed %d -> %d", badStatus, beforeOutcomes, len(snap.StepOutcomes))
			}
		})
	}
}

// TestUpdateInlineStep_TerminalGuard（fix/08 P1-4 红线）：
// item 已是 Completed 时再调 UpdateInlineStep(Failed) 不应退回 Failed。
func TestUpdateInlineStep_TerminalGuard(t *testing.T) {
	tracker := setupFanOutPlan(t)

	// 先把 c 翻 Completed（peer goroutine auto-complete 路径模拟）
	tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status:        builtin_tools.PlanStepCompleted,
		Summary:       "peer success",
		ShortSummary:  "ok",
	})
	c := planItemByIDInSnap(tracker.Snapshot(), "c")
	if c.Status != builtin_tools.PlanStepCompleted {
		t.Fatalf("setup: expected c Completed, got %q", c.Status)
	}

	// drain 兜底 result.Success=false（ctx 取消窗口）→ UpdateInlineStep(Failed)
	// 不应把 Completed 退回 Failed
	snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepFailed,
		Error:  "ctx cancelled after auto-complete",
	})
	cAfter := planItemByIDInSnap(snap, "c")
	if cAfter.Status != builtin_tools.PlanStepCompleted {
		t.Fatalf("terminal guard 失效：expected c Completed unchanged, got %q", cAfter.Status)
	}

	// d 不应被错误地传播为 Skipped——因为 c 没真翻 Failed
	d := planItemByIDInSnap(snap, "d")
	if d.Status == builtin_tools.PlanStepSkipped {
		t.Fatalf("d 不应被传播 Skipped：c 仍是 Completed 而非 Failed")
	}
}

func TestUpdateInlineStep_FailedPropagatesTransitively(t *testing.T) {
	// 链式失败传播：c failed → d skipped → e skipped。
	// 验证 PropagateSkippedPlanSteps 走完所有传递性依赖，不仅止于一跳。
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "前置", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Step: "b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "c", Step: "c", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "d", Step: "d 依赖 c", Status: builtin_tools.PlanStepPending, DependsOn: []string{"c"}},
		{ID: "e", Step: "e 依赖 d", Status: builtin_tools.PlanStepPending, DependsOn: []string{"d"}},
	}, "init", true)
	tracker.EnsureCurrentStep()

	snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepFailed,
		Error:  "remote crash",
	})

	if planItemByIDInSnap(snap, "c").Status != builtin_tools.PlanStepFailed {
		t.Fatalf("expected c Failed")
	}
	if planItemByIDInSnap(snap, "d").Status != builtin_tools.PlanStepSkipped {
		t.Fatalf("expected d Skipped (one-hop)")
	}
	if planItemByIDInSnap(snap, "e").Status != builtin_tools.PlanStepSkipped {
		t.Fatalf("expected e Skipped (transitive: e→d→c)")
	}
	// b 不在传播链上，仍 Pending
	if planItemByIDInSnap(snap, "b").Status != builtin_tools.PlanStepPending {
		t.Fatalf("expected b Pending (unaffected branch)")
	}
}

// =====================================================================
// MarkInlineStepInProgress — 面 7.A 新方法测试
// =====================================================================

func TestMarkInlineStepInProgress_PendingToInProgress(t *testing.T) {
	tracker := setupFanOutPlan(t)
	snap := tracker.MarkInlineStepInProgress("c")
	c := planItemByIDInSnap(snap, "c")
	if c.Status != builtin_tools.PlanStepInProgress {
		t.Fatalf("expected c InProgress, got %q", c.Status)
	}
}

func TestMarkInlineStepInProgress_NoOpOnTerminal(t *testing.T) {
	for _, terminalStatus := range []builtin_tools.PlanStepStatus{
		builtin_tools.PlanStepCompleted,
		builtin_tools.PlanStepFailed,
		builtin_tools.PlanStepSkipped,
	} {
		t.Run(string(terminalStatus), func(t *testing.T) {
			tracker := NewStateTracker()
			tracker.UpdatePlan([]*builtin_tools.PlanItem{
				{ID: "x", Step: "x", Status: terminalStatus},
			}, "init", true)
			snap := tracker.MarkInlineStepInProgress("x")
			x := planItemByIDInSnap(snap, "x")
			if x.Status != terminalStatus {
				t.Fatalf("expected status unchanged (%q), got %q", terminalStatus, x.Status)
			}
		})
	}
}

func TestMarkInlineStepInProgress_NoOpOnInProgress(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "x", Step: "x", Status: builtin_tools.PlanStepInProgress},
	}, "init", true)
	// 重复调用应是 no-op，不应错误地翻回 Pending 或其他
	snap := tracker.MarkInlineStepInProgress("x")
	x := planItemByIDInSnap(snap, "x")
	if x.Status != builtin_tools.PlanStepInProgress {
		t.Fatalf("expected InProgress unchanged, got %q", x.Status)
	}
}

func TestMarkInlineStepInProgress_EmptyOrUnknownID(t *testing.T) {
	tracker := setupFanOutPlan(t)
	before := tracker.Snapshot()

	// 空 ID
	tracker.MarkInlineStepInProgress("")
	tracker.MarkInlineStepInProgress("   ")
	// 找不到 ID
	tracker.MarkInlineStepInProgress("nonexistent")

	after := tracker.Snapshot()
	for _, id := range []string{"a", "b", "c", "d"} {
		if planItemByIDInSnap(before, id).Status != planItemByIDInSnap(after, id).Status {
			t.Fatalf("step %s status unexpectedly changed", id)
		}
	}
}

func TestMarkInlineStepInProgress_DoesNotTouchPhase(t *testing.T) {
	tracker := setupFanOutPlan(t)
	tracker.SetPhase(builtin_tools.AgentPhaseStep)
	snap := tracker.MarkInlineStepInProgress("c")
	if snap.Phase != builtin_tools.AgentPhaseStep {
		t.Fatalf("expected Phase unchanged (Step), got %q", snap.Phase)
	}
}

func TestMarkInlineStepInProgress_DoesNotTouchCurrentStepID(t *testing.T) {
	tracker := setupFanOutPlan(t)
	// setupFanOutPlan 之后 CurrentStepID 应为 b（首个 ready pending）
	before := tracker.Snapshot()
	if before.CurrentStepID != "b" {
		t.Fatalf("precondition: expected CurrentStepID=b, got %q", before.CurrentStepID)
	}
	snap := tracker.MarkInlineStepInProgress("c")
	if snap.CurrentStepID != "b" {
		t.Fatalf("expected CurrentStepID unchanged (b), got %q", snap.CurrentStepID)
	}
}

// =====================================================================

func TestUpdateInlineStep_TranscriptBlobRefWritten(t *testing.T) {
	// 远程 update 带 TranscriptBlobRef → outcome 应该获得该 ref。
	tracker := setupFanOutPlan(t)
	snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status:            builtin_tools.PlanStepCompleted,
		Summary:           "ok",
		TranscriptBlobRef: "sha256:abc123",
	})
	for _, oc := range snap.StepOutcomes {
		if oc != nil && oc.StepID == "c" {
			if oc.TranscriptBlobRef != "sha256:abc123" {
				t.Fatalf("expected TranscriptBlobRef propagated, got %q", oc.TranscriptBlobRef)
			}
			return
		}
	}
	t.Fatal("expected StepOutcome for c")
}

// TestMainPath_UpdateCurrentStep_DoesNotClearTranscriptBlobRef：端到端断言主路径
// UpdateCurrentStep（不填 TranscriptBlobRef）不会清空已被远程路径填入的 ref。
// 模拟 ApplyStepReplan + UpdateInlineStep 等场景中 outcome 已有 ref，再调
// UpdateCurrentStep 验证 ref 仍在。
func TestMainPath_UpdateCurrentStep_DoesNotClearTranscriptBlobRef(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "main path", Status: builtin_tools.PlanStepPending},
	}, "init", true)
	tracker.EnsureCurrentStep()

	// 模拟远程路径或 ApplyStepReplan 已经写入 ref（用 UpdateInlineStep 是最简）。
	// 注意 a 现在是 current step，UpdateInlineStep 不动 CurrentStepID，
	// 但会更新 outcome.TranscriptBlobRef + 把 PlanItem.Status 翻成 Completed。
	tracker.UpdateInlineStep("a", builtin_tools.CurrentStepUpdate{
		Status:            builtin_tools.PlanStepCompleted,
		TranscriptBlobRef: "sha256:from-previous-path",
	})

	// 让 a 回到 in_progress 以模拟主路径再次跑同一 step（异常路径但 state 允许）
	// 然后主路径调 UpdateCurrentStep 不填 TranscriptBlobRef，断言 ref 仍在。
	tracker.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
		Status:        builtin_tools.PlanStepCompleted,
		ShortSummary:  "main path re-completion",
		StatusSummary: "ok",
		// TranscriptBlobRef 留空——主路径默认行为
	})

	snap := tracker.Snapshot()
	for _, oc := range snap.StepOutcomes {
		if oc != nil && oc.StepID == "a" {
			if oc.TranscriptBlobRef != "sha256:from-previous-path" {
				t.Fatalf("main path UpdateCurrentStep cleared existing ref: %q", oc.TranscriptBlobRef)
			}
			if oc.ShortSummary != "main path re-completion" {
				t.Fatalf("other fields should still update, short_summary=%q", oc.ShortSummary)
			}
			return
		}
	}
	t.Fatal("expected StepOutcome for a")
}

func TestUpdateInlineStep_TranscriptBlobRefEmptyNoOverwrite(t *testing.T) {
	// 关键防御：upsertStepOutcomeLocked 收到空 TranscriptBlobRef 时不该覆盖既有值。
	// 保护主路径 UpdateCurrentStep（不填 TranscriptBlobRef）不误清空已有 ref。
	tracker := setupFanOutPlan(t)
	// 先设置一个 ref
	tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status:            builtin_tools.PlanStepCompleted,
		TranscriptBlobRef: "sha256:existing",
	})
	// 再次更新但不带 ref（空字符串）
	snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status:       builtin_tools.PlanStepCompleted,
		ShortSummary: "second update",
		// TranscriptBlobRef 留空
	})
	for _, oc := range snap.StepOutcomes {
		if oc != nil && oc.StepID == "c" {
			if oc.TranscriptBlobRef != "sha256:existing" {
				t.Fatalf("empty TranscriptBlobRef should not overwrite existing, got %q", oc.TranscriptBlobRef)
			}
			if oc.ShortSummary != "second update" {
				t.Fatalf("other fields should still update, short_summary=%q", oc.ShortSummary)
			}
			return
		}
	}
	t.Fatal("expected StepOutcome for c")
}

func TestUpdateInlineStep_UpsertOutcomeIdempotent(t *testing.T) {
	tracker := setupFanOutPlan(t)
	tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status:       builtin_tools.PlanStepCompleted,
		ShortSummary: "first",
	})
	snap := tracker.UpdateInlineStep("c", builtin_tools.CurrentStepUpdate{
		Status:       builtin_tools.PlanStepCompleted,
		ShortSummary: "second",
	})
	count := 0
	var last *builtin_tools.StepOutcome
	for _, oc := range snap.StepOutcomes {
		if oc != nil && oc.StepID == "c" {
			count++
			last = oc
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 outcome for c after 2 calls, got %d", count)
	}
	if last.ShortSummary != "second" {
		t.Fatalf("expected second upsert overwrite, got short_summary=%q", last.ShortSummary)
	}
}
