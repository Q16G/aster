package react

import (
	"aster/internal/builtin_tools"
	"context"
	"strings"
)

func (a *Agent) ApplyPlanAndEmit(ctx context.Context, plan []*builtin_tools.PlanItem, explanation string, needsPlanning bool) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	// UpdatePlan 内部 diff → observer 自动 emit task_item，旧 emitTaskItemDiffs 调用已删。
	snapshot := a.state.UpdatePlan(plan, explanation, needsPlanning)
	a.appendPlannerJournalFullPlan(snapshot)
	if writer, err := newArtifactWriter(a.workspaceRuntime); err == nil {
		if persistErr := writer.PersistPlanArtifacts(snapshot, a.workspaceSessionID, explanation); persistErr != nil {
			a.emitRuntimeLog("warning", "persist plan artifacts failed", snapshot, map[string]any{
				"event":   "plan_artifacts_persist_failed",
				"error":   persistErr.Error(),
				"context": ctx != nil,
			})
		}
	} else {
		a.emitRuntimeLog("warning", "create artifact writer failed", snapshot, map[string]any{
			"event": "plan_artifact_writer_failed",
			"error": err.Error(),
		})
	}
	if a.emitter != nil {
		a.emitter.EmitStateChange(snapshot)
		a.emitter.EmitTaskPlan(snapshot.Plan, explanation)
	}
	return snapshot
}

// appendPlannerJournalFullPlan 在 plan 提交（首次规划 / 重规划）时把全部条目（含 pending）
// 全量 append 到 planner.jsonl，使其成为 plan 真相源；崩溃恢复据此重建。
func (a *Agent) appendPlannerJournalFullPlan(snapshot builtin_tools.StateSnapshot) {
	if a.workspaceRuntime == nil || len(snapshot.Plan) == 0 {
		return
	}
	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}
	records := make([]*builtin_tools.PlannerJournalRecord, 0, len(snapshot.Plan)+len(snapshot.Phases))
	for _, item := range snapshot.Plan {
		if item == nil {
			continue
		}
		records = append(records, &builtin_tools.PlannerJournalRecord{
			Kind:        builtin_tools.PlannerJournalKindPlan,
			PlanVersion: planVersion,
			Item:        item,
		})
	}
	// phase 行必须排在 kind=plan 行之后（版本提升批次契约：plan 行触发全量 reset）。
	for _, phase := range snapshot.Phases {
		if phase == nil {
			continue
		}
		records = append(records, &builtin_tools.PlannerJournalRecord{
			Kind:        builtin_tools.PlannerJournalKindPhase,
			PlanVersion: planVersion,
			Phase:       phase,
		})
	}
	if err := builtin_tools.AppendPlannerJournalRecords(a.workspaceRuntime.RootDir(), records); err != nil {
		a.emitRuntimeLog("warn", "append planner journal failed", snapshot, map[string]any{
			"event": "planner_journal_append_failed",
			"error": err.Error(),
		})
	}
}

// appendPlannerJournalPhaseRecords 把 step_replan 承接后状态变化的 phase 以 kind=phase
// 增量落 planner.jsonl（同 id 覆盖），使 phase 状态与 plan 真相源同源、崩溃恢复可重建。
func (a *Agent) appendPlannerJournalPhaseRecords(phases []*builtin_tools.PlanPhase, planVersion int) {
	if a.workspaceRuntime == nil || len(phases) == 0 {
		return
	}
	if planVersion <= 0 {
		planVersion = 1
	}
	records := make([]*builtin_tools.PlannerJournalRecord, 0, len(phases))
	for _, phase := range phases {
		if phase == nil {
			continue
		}
		records = append(records, &builtin_tools.PlannerJournalRecord{
			Kind:        builtin_tools.PlannerJournalKindPhase,
			PlanVersion: planVersion,
			Phase:       phase,
		})
	}
	if err := builtin_tools.AppendPlannerJournalRecords(a.workspaceRuntime.RootDir(), records); err != nil {
		a.emitRuntimeLog("warn", "append planner journal phase records failed", a.state.Snapshot(), map[string]any{
			"event": "planner_journal_phase_append_failed",
			"error": err.Error(),
		})
	}
}

func emitTaskItemDiffs(emitter *Emitter, prev []*builtin_tools.PlanItem, next []*builtin_tools.PlanItem, currentStepID string, explanation string) {
	if emitter == nil {
		return
	}
	prevStatusByKey := make(map[string]builtin_tools.PlanStepStatus, len(prev))
	currentStepID = strings.TrimSpace(currentStepID)
	for _, it := range prev {
		if it == nil {
			continue
		}
		key := planItemDiffKey(it)
		if key == "" {
			continue
		}
		if _, exists := prevStatusByKey[key]; exists {
			continue
		}
		prevStatusByKey[key] = it.Status
	}

	for index, it := range next {
		if it == nil {
			continue
		}
		key := planItemDiffKey(it)
		if key == "" {
			continue
		}
		prevStatus, existed := prevStatusByKey[key]
		if existed {
			if prevStatus == it.Status {
				continue
			}
			emitter.EmitTaskItem(it, prevStatus, index, explanation)
			continue
		}

		// Avoid emitting N task_item events when a whole plan is created. Only surface
		// the currently selected step as a milestone.
		if it.Status == builtin_tools.PlanStepInProgress || (currentStepID != "" && strings.TrimSpace(it.ID) == currentStepID) {
			emitter.EmitTaskItem(it, builtin_tools.PlanStepStatus(""), index, explanation)
		}
	}
}

func planItemDiffKey(item *builtin_tools.PlanItem) string {
	if item == nil {
		return ""
	}
	if id := strings.TrimSpace(item.ID); id != "" {
		return id
	}
	return strings.TrimSpace(item.Step)
}
