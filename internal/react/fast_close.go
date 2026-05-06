package react

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

func (a *Agent) canFastCloseStep(snapshot builtin_tools.StateSnapshot) bool {
	if a.currentIntent == nil {
		return false
	}
	if a.currentIntent.Complexity == "complex" || a.currentIntent.Complexity == "unknown" {
		return false
	}
	if a.requiresPublishedOutput() {
		return false
	}
	if len(snapshot.Plan) != 1 || snapshot.Plan[0] == nil {
		return false
	}
	if snapshot.Plan[0].Status != builtin_tools.PlanStepCompleted {
		return false
	}
	if snapshot.ReplanContext != nil {
		return false
	}
	stepID := strings.TrimSpace(snapshot.Plan[0].ID)
	outcome := findStepOutcome(snapshot.StepOutcomes, stepID)
	if outcome == nil {
		return false
	}
	content := strings.TrimSpace(outcome.DisplayResult)
	if content == "" {
		content = strings.TrimSpace(outcome.Summary)
	}
	return content != ""
}

func (a *Agent) fastCloseStepSummary(
	ctx context.Context,
	snapshot builtin_tools.StateSnapshot,
	stepID string,
	rawOutcome *builtin_tools.StepOutcome,
	writer *artifactWriter,
	artifactPlan *stepArtifactPlan,
	window *StepWindow,
	refs []string,
) error {
	content := strings.TrimSpace(rawOutcome.DisplayResult)
	if content == "" {
		content = strings.TrimSpace(rawOutcome.Summary)
	}

	tmp := cloneStepOutcome(rawOutcome)
	tmp.StatusSummary = "single step completed (fast close)"
	tmp.ShortSummary = truncateByRunes(content, 200)
	tmp.LongSummary = content
	tmp.KeyFacts = nil
	tmp.OpenQuestions = nil
	tmp.ToolCallsDigest = nil
	tmp.TimelineDiffSummary = ""
	tmp.References = refs
	tmp.UpdatedAt = time.Now()
	if strings.TrimSpace(tmp.ContextKey) == "" {
		tmp.ContextKey = "ctx-" + uuid.NewString()
	}

	record, err := writer.PersistStepArtifacts(snapshot, a.workspaceSessionID, a.Name(), snapshot.CurrentStep(), &tmp, window, artifactPlan)
	if err != nil {
		return err
	}
	tmp.ArtifactDir = strings.TrimSpace(record.ArtifactDir)
	tmp.SummaryFile = strings.TrimSpace(record.SummaryFile)
	tmp.ResultFile = strings.TrimSpace(record.ResultFile)

	var inheritedKeys []string
	var inheritedRefIDs []string
	if a.frozenLineageByStep != nil {
		if lineage := a.frozenLineageByStep[frozenLineageKey(snapshot.PlanVersion, stepID)]; lineage != nil {
			inheritedKeys = builtin_tools.CloneStringSlice(lineage.InheritedContextKeys)
			inheritedRefIDs = builtin_tools.CloneStringSlice(lineage.InheritedRefIDs)
		}
	}
	ctxRecord := &builtin_tools.StepContextRecord{
		ContextKey:           strings.TrimSpace(tmp.ContextKey),
		Namespace:            strings.TrimSpace(a.workspaceNamespace),
		StepID:               stepID,
		StepKey:              stepID,
		PlanVersion:          snapshot.PlanVersion,
		AgentProfile:         strings.TrimSpace(a.Name()),
		SummaryFile:          strings.TrimSpace(tmp.SummaryFile),
		ResultFile:           strings.TrimSpace(tmp.ResultFile),
		ResultKeys:           extractTopLevelJSONKeys(tmp.Result),
		ShortSummary:         strings.TrimSpace(tmp.ShortSummary),
		KeyFacts:             builtin_tools.CloneStringSlice(tmp.KeyFacts),
		ToolCallsDigest:      builtin_tools.CloneStringSlice(tmp.ToolCallsDigest),
		References:           builtin_tools.CloneStringSlice(record.ReferenceIDs),
		InheritedContextKeys: inheritedKeys,
		InheritedRefIDs:      inheritedRefIDs,
		CreatedAt:            time.Now(),
	}
	if ctxRecord.ShortSummary == "" {
		ctxRecord.ShortSummary = strings.TrimSpace(tmp.Summary)
	}
	if ctxRecord.ShortSummary == "" {
		ctxRecord.ShortSummary = strings.TrimSpace(tmp.DisplayResult)
	}
	if len(ctxRecord.KeyFacts) == 0 {
		ctxRecord.KeyFacts = nil
	}
	if len(ctxRecord.References) == 0 {
		ctxRecord.References = nil
	}
	if a.workspaceRuntime != nil {
		if err := a.workspaceRuntime.AppendStepContextRecords([]*builtin_tools.StepContextRecord{ctxRecord}); err != nil {
			return err
		}
	} else if err := builtin_tools.AppendWorkspaceStepContextRecords(a.workspaceRootDir, []*builtin_tools.StepContextRecord{ctxRecord}); err != nil {
		return err
	}
	_ = a.consumeFrozenStepLineage(snapshot.PlanVersion, stepID)

	snapshot = a.state.ApplyStepSummary(stepID, stepSummaryUpdate{
		StatusSummary: tmp.StatusSummary,
		ShortSummary:  tmp.ShortSummary,
		LongSummary:   tmp.LongSummary,
		ArtifactDir:   tmp.ArtifactDir,
		SummaryFile:   tmp.SummaryFile,
		ResultFile:    tmp.ResultFile,
		ContextKey:    tmp.ContextKey,
		References:    tmp.References,
		NextPhase:     builtin_tools.AgentPhaseFinalAnswer,
	})
	if err := writer.PersistRuntimeCheckpoint(snapshot, a.workspaceSessionID, "step_summary_fast_close"); err != nil {
		return err
	}
	a.emitter.EmitStateChange(snapshot)
	a.emitter.EmitStepSummaryResult(stepID, "", &tmp)

	a.stepBaselineAt = time.Time{}
	a.stepBaselineStepID = ""

	a.emitRuntimeLog("info", "step summary fast closed", snapshot, map[string]any{
		"event":   "step_summary_fast_closed",
		"step_id": stepID,
	})
	return nil
}

func (a *Agent) canFastCloseFinalAnswer(snapshot builtin_tools.StateSnapshot, ctx context.Context) bool {
	if a.currentIntent == nil {
		return false
	}
	if snapshot.ExternalInterrupt != nil {
		return false
	}
	if a.currentIntent.Complexity == "complex" || a.currentIntent.Complexity == "unknown" {
		return false
	}
	if a.requiresPublishedOutput() {
		return false
	}
	if len(snapshot.Plan) != 1 {
		return false
	}
	if snapshot.Plan[0] == nil || snapshot.Plan[0].Status != builtin_tools.PlanStepCompleted {
		return false
	}
	if snapshot.ReplanContext != nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	return true
}

func (a *Agent) fastCloseFinalAnswer(
	snapshot builtin_tools.StateSnapshot,
	writer *artifactWriter,
	assessedPayload map[string]any,
) (builtin_tools.StateSnapshot, error) {
	stepID := ""
	if len(snapshot.Plan) == 1 && snapshot.Plan[0] != nil {
		stepID = strings.TrimSpace(snapshot.Plan[0].ID)
	}
	finalText := ""
	if stepID != "" {
		if outcome := findStepOutcome(snapshot.StepOutcomes, stepID); outcome != nil {
			if c := strings.TrimSpace(outcome.DisplayResult); c != "" {
				finalText = c
			} else if c := strings.TrimSpace(outcome.Summary); c != "" {
				finalText = c
			}
		}
	}
	if finalText == "" {
		finalText = "任务已完成。"
	}

	finalAnswerSource := "fast_close"

	snapshot = a.state.ApplyFinalAnswerPhaseUpdate(finalAnswerPhaseUpdate{
		NextPhase:          builtin_tools.AgentPhaseFinalAnswer,
		Status:             builtin_tools.TaskStatusCompleted,
		FinalAnswerContent: finalText,
		FinalAnswerSource:  finalAnswerSource,
	})
	a.emitter.EmitStateChange(snapshot)
	if snapshot.FinalAnswer != nil {
		a.emitter.EmitFinalAnswerResult(snapshot.FinalAnswer)
	}

	assessmentPayload := map[string]any{
		"session_id":     strings.TrimSpace(a.workspaceSessionID),
		"plan_version":   snapshot.PlanVersion,
		"assessed_state": assessedPayload,
		"assessment": FinalAnswerModelOutput{
			IsComplete:  true,
			Status:      string(builtin_tools.TaskStatusCompleted),
			Reason:      "single step fast close",
			UserMessage: finalText,
		},
	}
	_, err := writer.PersistFinalArtifacts(snapshot, a.workspaceSessionID, assessmentPayload, finalText)
	if err != nil {
		return snapshot, err
	}

	if strings.TrimSpace(finalText) != "" {
		historyText := truncateForHistory(finalText, finalAnswerSource)
		msg := ai.NewAIMsgInfo(historyText)
		a.history = append(a.history, msg)
		a.notifyHistoryReplace()
		a.AddMemoryAssistantOutput(historyText)
	}

	a.emitRuntimeLog("info", "final answer fast closed", snapshot, map[string]any{
		"event":          "final_answer_fast_closed",
		"content_length": len(finalText),
	})
	return snapshot, nil
}
