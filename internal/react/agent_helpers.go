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
	prev := a.state.Snapshot()
	snapshot := a.state.UpdatePlan(plan, explanation, needsPlanning)
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
		emitTaskItemDiffs(a.emitter, prev.Plan, snapshot.Plan, snapshot.CurrentStepID, explanation)
	}
	return snapshot
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
