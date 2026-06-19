package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// resetSetup 构造一个含所有状态的 plan，方便不同测试用例选定 CurrentStepID 后验证清空/保留行为。
func resetSetup(t *testing.T, currentID string, currentStatus builtin_tools.PlanStepStatus) *StateTracker {
	t.Helper()
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "s-completed", Step: "已完成", Status: builtin_tools.PlanStepCompleted},
		{ID: "s-failed", Step: "已失败", Status: builtin_tools.PlanStepFailed},
		{ID: "s-skipped", Step: "已跳过", Status: builtin_tools.PlanStepSkipped},
		{ID: "s-pending", Step: "待跑", Status: builtin_tools.PlanStepPending},
		{ID: "s-inprogress", Step: "进行中", Status: builtin_tools.PlanStepInProgress},
	}, "init", true)
	// 找到目标 item 覆盖 Status（resetSetup 入参约定）；若 currentID 未指定则不设
	if currentID != "" {
		snap := tracker.Snapshot()
		for _, it := range snap.Plan {
			if it != nil && it.ID == currentID {
				it.Status = currentStatus
				break
			}
		}
		// 直接通过 SetPhase 触发 touch；CurrentStepID 通过下面 hack 设置
		// （没有公开的 SetCurrentStepID，用 EnsureCurrentStep 不一定能选到目标 ID）
		tracker.setCurrentStepIDForTest(currentID)
	}
	return tracker
}

func TestResetCurrentStepIfTerminal_Empty(t *testing.T) {
	// 全 pending plan + 不设 CurrentStepID：避免 currentPlanStep 的 InProgress 兜底，
	// UpdatePlan 走 NextRunnablePlanStepID 选第一个 ready=a；测试 Empty 行为时清空再断言。
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "x", Status: builtin_tools.PlanStepPending},
	}, "init", true)
	tracker.setCurrentStepIDForTest("")
	if tracker.Snapshot().CurrentStepID != "" {
		t.Fatalf("setup precondition: expected empty CurrentStepID")
	}
	snap := tracker.ResetCurrentStepIfTerminal()
	if snap.CurrentStepID != "" {
		t.Fatalf("expected CurrentStepID empty, got %q", snap.CurrentStepID)
	}
}

func TestResetCurrentStepIfTerminal_NotFound(t *testing.T) {
	tracker := NewStateTracker()
	tracker.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "x", Status: builtin_tools.PlanStepPending},
	}, "init", true)
	tracker.setCurrentStepIDForTest("nonexistent")
	snap := tracker.ResetCurrentStepIfTerminal()
	if snap.CurrentStepID != "nonexistent" {
		t.Fatalf("expected CurrentStepID unchanged (defensive no-op), got %q", snap.CurrentStepID)
	}
}

func TestResetCurrentStepIfTerminal_Pending(t *testing.T) {
	tracker := resetSetup(t, "s-pending", builtin_tools.PlanStepPending)
	snap := tracker.ResetCurrentStepIfTerminal()
	if snap.CurrentStepID != "s-pending" {
		t.Fatalf("expected CurrentStepID unchanged for pending, got %q", snap.CurrentStepID)
	}
}

func TestResetCurrentStepIfTerminal_InProgress(t *testing.T) {
	tracker := resetSetup(t, "s-inprogress", builtin_tools.PlanStepInProgress)
	snap := tracker.ResetCurrentStepIfTerminal()
	if snap.CurrentStepID != "s-inprogress" {
		t.Fatalf("expected CurrentStepID unchanged for in_progress, got %q", snap.CurrentStepID)
	}
}

func TestResetCurrentStepIfTerminal_Completed(t *testing.T) {
	tracker := resetSetup(t, "s-completed", builtin_tools.PlanStepCompleted)
	snap := tracker.ResetCurrentStepIfTerminal()
	if snap.CurrentStepID != "" {
		t.Fatalf("expected CurrentStepID cleared for completed, got %q", snap.CurrentStepID)
	}
}

func TestResetCurrentStepIfTerminal_Failed(t *testing.T) {
	tracker := resetSetup(t, "s-failed", builtin_tools.PlanStepFailed)
	snap := tracker.ResetCurrentStepIfTerminal()
	if snap.CurrentStepID != "" {
		t.Fatalf("expected CurrentStepID cleared for failed, got %q", snap.CurrentStepID)
	}
}

func TestResetCurrentStepIfTerminal_Skipped(t *testing.T) {
	tracker := resetSetup(t, "s-skipped", builtin_tools.PlanStepSkipped)
	snap := tracker.ResetCurrentStepIfTerminal()
	if snap.CurrentStepID != "" {
		t.Fatalf("expected CurrentStepID cleared for skipped, got %q", snap.CurrentStepID)
	}
}

// setCurrentStepIDForTest 仅供本 package 内测试使用，绕开公开 API 直接设置 CurrentStepID。
// 生产代码不应该直接操作此字段——通过 EnsureCurrentStep / UpdateCurrentStep / ApplyStepReplan 等路径。
func (t *StateTracker) setCurrentStepIDForTest(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.CurrentStepID = id
	t.touchLocked()
}
