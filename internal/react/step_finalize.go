package react

import (
	"context"
	"fmt"
	"strings"

	"aster/internal/builtin_tools"
)

// markStepJournaled 登记某 step 已固化落盘。仅在调度 goroutine 调用。
//
// 去重按 step_id 单维（不带 plan_version）：每个 step 的 kind=step 记录只在它完成那一刻
// 落盘一次、归属完成时的 plan_version。重规划（NewPlan）会把已完成 step 原样并入新 plan，
// 其 status 仍是终态；若去重带 plan_version，新 plan_version 下会把这些 carried-over 的
// completed step 重复落盘、与原始记录 plan_version 错配（正是 applyReplanResult 注释告警的
// 场景）。故按 step_id 单维“一次落盘永不重写”，与原 step_replan 每 step 只 journal 一次对齐。
func (a *Agent) markStepJournaled(stepID string) {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return
	}
	if a.journaledStepIDs == nil {
		a.journaledStepIDs = make(map[string]struct{})
	}
	a.journaledStepIDs[stepID] = struct{}{}
}

// stepAlreadyJournaled 判断某 step 是否已固化落盘。
func (a *Agent) stepAlreadyJournaled(stepID string) bool {
	if a == nil || a.journaledStepIDs == nil {
		return false
	}
	_, ok := a.journaledStepIDs[strings.TrimSpace(stepID)]
	return ok
}

// resolveStepFinalizePaths 解析一个终态 step 的产出指针 / 元数据，供收尾扫描烘焙落盘。
// 与 applyReplanResult 内联解析的逻辑同源（timeline / step 过程文件 / coverage 指针 +
// context_key + namespace + plan_version）；TranscriptBlobRef / Inherited* 透传 step 自身
// outcome 已写入的值（ForceMeta=false 下不被覆盖），杜绝清空 peer drain 现场。
func (a *Agent) resolveStepFinalizePaths(stepID string, snapshot builtin_tools.StateSnapshot) stepFinalizePaths {
	rawOutcome := findOutcome(snapshot.StepOutcomes, stepID)

	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}

	var timelineFile string
	if a.workspaceRuntime != nil && stepTimelineExists(a.workspaceRuntime.SharedDir(), stepID) {
		timelineFile = stepTimelineRelPath(stepID)
	}
	var stepFile string
	if a.workspaceRuntime != nil {
		if stepFileExists(a.workspaceRuntime.SharedDir(), stepID) {
			stepFile = stepFileRelPath(stepID)
		} else if legacyStepFileExists(a.workspaceRuntime.SharedDir(), stepID) {
			stepFile = fmt.Sprintf("shared/%s/step.md", stepID)
		}
	}
	coverageFile := a.persistCoverageChecklist(stepID, rawOutcome)

	transcriptRef := ""
	var inheritedKeys, inheritedRefs []string
	if rawOutcome != nil {
		transcriptRef = strings.TrimSpace(rawOutcome.TranscriptBlobRef)
		inheritedKeys = rawOutcome.InheritedContextKeys
		inheritedRefs = rawOutcome.InheritedRefIDs
	}

	return stepFinalizePaths{
		ContextKey:           a.resolveStepContextKey(stepID, rawOutcome, snapshot),
		TimelineFile:         timelineFile,
		StepFile:             stepFile,
		CoverageFile:         coverageFile,
		Namespace:            builtin_tools.NormalizeWorkspaceNamespace(a.workspaceNamespace),
		PlanVersion:          planVersion,
		TranscriptBlobRef:    transcriptRef,
		InheritedContextKeys: inheritedKeys,
		InheritedRefIDs:      inheritedRefs,
		ForceMeta:            false,
	}
}

// finalizeTerminalStep 对单个终态 step 做幂等固化：烘焙 plan_item + 写 step_context +
// 写 planner.jsonl 的 kind=step 记录。已登记（被 applyReplanResult 或上一轮扫描固化）即跳过。
// 仅在调度 goroutine 调用（planner.jsonl 单写者纪律）。返回是否本次实际固化。
func (a *Agent) finalizeTerminalStep(stepID string, snapshot builtin_tools.StateSnapshot) bool {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return false
	}
	if a.stepAlreadyJournaled(stepID) {
		return false
	}
	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}

	paths := a.resolveStepFinalizePaths(stepID, snapshot)
	snapshot = a.state.FinalizeTerminalStep(stepID, paths)

	a.appendStepContextRecord(stepID, snapshot)
	a.appendPlannerJournalStepRecordAt(stepID, snapshot, planVersion)
	a.markStepJournaled(stepID)

	if a.emitter != nil {
		a.emitter.EmitStateChange(snapshot)
	}
	return true
}

// finalizeUnjournaledTerminalSteps 是 X2 滚动收尾扫描：把 plan 上所有「终态且尚未固化」的
// step 逐个固化落盘。跳过当前 current step——它要么由 step_replan / applyReplanResult 固化，
// 要么在 step 阶段被 ResetCurrentStepIfTerminal 让位后于下一轮扫描固化，借此保持 step_replan
// 复核 prompt 行为不变（不提前把 current 自身的 journal 注入其复核窗口）。
//
// 解决缺陷一：peer 与「滚动中已完成的主路径 current」从不经 step_replan，故其烘焙 + journal
// 之前被永久跳过；本扫描在调度 goroutine 上兜住它们，保证每个终态 step 恰好落盘一次。
func (a *Agent) finalizeUnjournaledTerminalSteps(snapshot builtin_tools.StateSnapshot) {
	if a == nil || a.state == nil {
		return
	}
	currentID := strings.TrimSpace(snapshot.CurrentStepID)
	for _, item := range snapshot.Plan {
		if item == nil {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" || id == currentID {
			continue
		}
		switch item.Status {
		case builtin_tools.PlanStepCompleted,
			builtin_tools.PlanStepFailed,
			builtin_tools.PlanStepSkipped:
		default:
			continue
		}
		a.finalizeTerminalStep(id, snapshot)
	}
}

// awaitRunningInlineSteps 阻塞到没有 inline step（X2 peer）仍在运行，期间逐个 drain 完成通知。
// 与 awaitAllBackgroundSubAgents 的区别：本函数只等 inline peer，不等后台 sub_agent
// （sub_agent 有自己的 await / A4 流程）；目的是在进 step_replan 前让所有 in_progress peer 落定，
// 避免 step_replan 基于不完整 plan 做复核 / 整盘替换（NewPlan）并与 peer 完成回写竞态。
// 仅在调度 goroutine 调用。
func (a *Agent) awaitRunningInlineSteps(ctx context.Context) {
	if a == nil || a.asyncRegistry == nil {
		return
	}
	for a.asyncRegistry.HasRunningInlineSteps() {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		a.asyncRegistry.WaitForCompletion(ctx)
		a.drainAsyncAgentNotifications(ctx)
	}
	// Final drain：捡起已落定但尚未处理的完成通知。
	a.drainAsyncAgentNotifications(ctx)
}
