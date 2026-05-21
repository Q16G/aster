package react

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/jsonextractor"
	"aster/internal/runtimelog"
	"aster/internal/structuredoutput"
)

type FinalAnswerModelOutput struct {
	IsComplete   bool     `json:"is_complete"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason"`
	ShouldReplan bool     `json:"should_replan"`
	NextGoal     string   `json:"next_goal"`
	MissingItems []string `json:"missing_items"`
	Warnings     []string `json:"warnings"`
	UserMessage  string   `json:"user_message"`
	References   []string `json:"references"`
}

func (a *Agent) runFinalAnswerPhase(ctx context.Context, iter int, runClient ai.ChatClient) (builtin_tools.StateSnapshot, error) {
	_ = iter
	_ = a.state.SetPhase(builtin_tools.AgentPhaseFinalAnswer)
	snapshot := a.state.Snapshot()
	a.emitter.EmitStateChange(snapshot)
	a.emitRuntimeLog("info", "enter final answer phase", snapshot, map[string]any{
		"event": "phase_enter",
	})

	writer, err := newArtifactWriter(a.workspaceRuntime)
	if err != nil {
		return snapshot, err
	}

	stateStatus := snapshot.Status
	errText := strings.TrimSpace(snapshot.Error)
	externalInterrupt := builtin_tools.CloneExternalInterrupt(snapshot.ExternalInterrupt)

	stepOutcomeViews := collectAllStepContextViews(snapshot.Plan, snapshot.StepOutcomes)
	if a.workspaceRuntime != nil {
		sharedDir := a.workspaceRuntime.SharedDir()
		for i := range stepOutcomeViews {
			stepID := stepOutcomeViews[i].StepID
			if stepID != "" && stepTimelineExists(sharedDir, stepID) {
				stepOutcomeViews[i].TimelineFile = filepath.Join(sharedDir, stepID, "timeline.jsonl")
			}
		}
	}

	payload := map[string]any{
		"status":             stateStatus,
		"state_error":        strings.TrimSpace(snapshot.Error),
		"input_timeline":     snapshot.InputTimeline,
		"needs_planning":     snapshot.NeedsPlanning,
		"show_plan":          snapshot.NeedsPlanning,
		"plan":               snapshot.Plan,
		"plan_version":       snapshot.PlanVersion,
		"step_outcomes":      stepOutcomeViews,
		"external_interrupt": externalInterrupt,
		"replan_context":     snapshot.ReplanContext,
		"active_skill_names": snapshot.ActiveSkillNames,
		"warnings":           snapshot.Warnings,
		"unresolved":         snapshot.Unresolved,
	}

	var modelOut FinalAnswerModelOutput
	rawResponse := ""
	if externalInterrupt != nil {
		a.emitRuntimeLog("warning", "final answer model bypassed due to external interrupt", snapshot, map[string]any{
			"event":            "final_answer_model_bypassed",
			"reason_code":      strings.TrimSpace(externalInterrupt.ReasonCode),
			"retryable":        externalInterrupt.Retryable,
			"warnings_count":   len(snapshot.Warnings),
			"unresolved_count": len(snapshot.Unresolved),
		})
		modelOut = buildExternalInterruptModelOutput(snapshot, externalInterrupt)
	} else {
		prompt, err := a.BuildFinalAnswerPrompt(payload)
		if err != nil {
			return snapshot, err
		}

		if a.canFastCloseFinalAnswer(snapshot, ctx) {
			return a.fastCloseFinalAnswer(snapshot, writer, payload)
		}

		if ctx != nil && ctx.Err() != nil {
			// ctx 已取消时不再调用模型；仍然给出可交付的 final answer。
			modelOut = FinalAnswerModelOutput{
				IsComplete:   true,
				Status:       string(builtin_tools.TaskStatusCanceled),
				Reason:       strings.TrimSpace(ctx.Err().Error()),
				ShouldReplan: false,
				NextGoal:     "",
				MissingItems: nil,
				Warnings:     nil,
				UserMessage:  firstNonEmpty(strings.TrimSpace(errText), "任务已取消。"),
			}
		} else {
			a.emitRuntimeLog("info", "final answer model started", snapshot, map[string]any{
				"event":               "final_answer_model_started",
				"plan_version":        snapshot.PlanVersion,
				"step_outcomes_count": len(snapshot.StepOutcomes),
				"warnings_count":      len(snapshot.Warnings),
				"unresolved_count":    len(snapshot.Unresolved),
			})
			runtimelog.LogJSON("info", map[string]any{
				"event":              "final_answer_model_request",
				"phase":              "final_answer",
				"raw_request_length": len(prompt),
			})
			var retryResult structuredoutput.Result[FinalAnswerModelOutput]
			retryResult, runErr := runStructuredOutputWithRetry(a, ctx, snapshot, runClient, "final_answer", prompt, func(raw string) (FinalAnswerModelOutput, error) {
				return parseFinalAnswerOutput(raw)
			})
			if runErr == nil {
				rawResponse = strings.TrimSpace(retryResult.RawResponse)
				runtimelog.LogJSON("info", map[string]any{
					"event":               "final_answer_model_raw_response",
					"phase":               "final_answer",
					"mode":                "success",
					"raw_response_length": len(rawResponse),
				})
				modelOut = retryResult.Value
			} else {
				rawResponse = strings.TrimSpace(structuredoutput.LastResponse(runErr))
				if rawResponse != "" {
					runtimelog.LogJSON("warning", map[string]any{
						"event":               "final_answer_model_raw_response",
						"phase":               "final_answer",
						"mode":                "fallback_text",
						"error":               strings.TrimSpace(runErr.Error()),
						"raw_response_length": len(rawResponse),
					})
					a.emitRuntimeLog("warning", "final answer model fell back to plain text", snapshot, map[string]any{
						"event":           "final_answer_model_fallback_text",
						"response_length": len(rawResponse),
						"error":           strings.TrimSpace(runErr.Error()),
					})
					modelOut = FinalAnswerModelOutput{
						IsComplete:   true,
						Status:       string(builtin_tools.TaskStatusCompleted),
						Reason:       "模型输出未能满足 assessment JSON schema，重试已耗尽，已回退为直接交付文本。",
						ShouldReplan: false,
						NextGoal:     "",
						UserMessage:  rawResponse,
						References:   []string{},
					}
				} else {
					runtimelog.LogJSON("error", map[string]any{
						"event":               "final_answer_model_raw_response",
						"phase":               "final_answer",
						"mode":                "parse_failed",
						"error":               strings.TrimSpace(runErr.Error()),
						"raw_response_length": len(rawResponse),
					})
					a.emitRuntimeLog("error", "final answer model json parse failed", snapshot, map[string]any{
						"event": "final_answer_model_parse_failed",
						"error": strings.TrimSpace(runErr.Error()),
					})
					return snapshot, fmt.Errorf("final_answer structured output retry exhausted: %w", runErr)
				}
			}
		}
	}

	decision := normalizeFinalAnswerDecision(modelOut)
	if externalInterrupt != nil {
		decision = applyExternalInterruptDecision(snapshot, decision, externalInterrupt)
		if warning := externalInterruptWarning(externalInterrupt); warning != "" {
			decision.model.Warnings = normalizeReferences(append(decision.model.Warnings, warning))
		}
	}
	assessmentPayload := map[string]any{
		"session_id":     strings.TrimSpace(a.workspaceSessionID),
		"plan_version":   snapshot.PlanVersion,
		"assessed_state": payload,
		"assessment":     decision.model,
	}

	if !decision.isTerminal {
		nextGoal := strings.TrimSpace(decision.model.NextGoal)
		if nextGoal == "" {
			nextGoal = strings.TrimSpace(snapshot.CurrentGoal)
		}
		snapshot = a.state.ApplyFinalAnswerPhaseUpdate(finalAnswerPhaseUpdate{
			NextPhase:     builtin_tools.AgentPhasePlan,
			Status:        builtin_tools.TaskStatusRunning,
			StatusSummary: firstNonEmpty(strings.TrimSpace(decision.model.Reason), "任务未完成，回流 plan 继续规划。"),
			NextGoal:      nextGoal,
			Warnings:      decision.model.Warnings,
			Unresolved:    decision.model.MissingItems,
			ReplanContext: &builtin_tools.ReplanContext{
				Reason:         strings.TrimSpace(decision.model.Reason),
				NextGoal:       nextGoal,
				MissingItems:   builtin_tools.CloneStringSlice(decision.model.MissingItems),
				Warnings:       builtin_tools.CloneStringSlice(decision.model.Warnings),
				ReplacePending: true,
			},
		})
		a.emitter.EmitStateChange(snapshot)
		record, err := writer.PersistFinalArtifacts(snapshot, a.workspaceSessionID, assessmentPayload, "")
		if err != nil {
			return snapshot, err
		}
		a.emitRuntimeLog("info", "final assessment written", snapshot, map[string]any{
			"event":                 "final_assessment_written",
			"final_assessment_file": record.FinalAssessmentFile,
			"plan_version":          snapshot.PlanVersion,
		})

		a.emitRuntimeLog("info", "final assessment decided to replan", snapshot, map[string]any{
			"event":       "final_assessment_replan",
			"next_goal":   nextGoal,
			"missing_len": len(decision.model.MissingItems),
		})
		return snapshot, nil
	}

	finalText := strings.TrimSpace(decision.model.UserMessage)
	if finalText == "" {
		finalText = firstNonEmpty(strings.TrimSpace(decision.model.Reason), "任务已完成。")
	}
	if externalInterrupt != nil {
		if interruptText := buildExternalInterruptFinalAnswer(snapshot, externalInterrupt); interruptText != "" {
			finalText = interruptText
		}
	}
	finalAnswerSource := "final_assessment"

	snapshot = a.state.ApplyFinalAnswerPhaseUpdate(finalAnswerPhaseUpdate{
		NextPhase:             builtin_tools.AgentPhaseFinalAnswer,
		Status:                decision.status,
		Error:                 errText,
		FinalAnswerContent:    finalText,
		FinalAnswerSource:     finalAnswerSource,
		FinalAnswerReferences: decision.model.References,
		Warnings:              decision.model.Warnings,
		Unresolved:            []string{},
		ExternalInterrupt:     externalInterrupt,
	})
	a.emitter.EmitStateChange(snapshot)
	if snapshot.FinalAnswer != nil {
		a.emitter.EmitFinalAnswerResult(snapshot.FinalAnswer)
	}
	record, err := writer.PersistFinalArtifacts(snapshot, a.workspaceSessionID, assessmentPayload, finalText)
	if err != nil {
		return snapshot, err
	}
	a.emitRuntimeLog("info", "final assessment written", snapshot, map[string]any{
		"event":                 "final_assessment_written",
		"final_assessment_file": record.FinalAssessmentFile,
		"plan_version":          snapshot.PlanVersion,
	})
	a.emitRuntimeLog("info", "final answer artifact written", snapshot, map[string]any{
		"event":             "final_answer_written",
		"final_answer_file": record.FinalAnswerFile,
		"content_length":    len(strings.TrimSpace(finalText)),
	})

	if strings.TrimSpace(finalText) != "" {
		historyText := truncateForHistory(finalText, finalAnswerSource)
		msg := ai.NewAIMsgInfo(historyText)
		a.history = append(a.history, msg)
		// 用 replace 快照落盘，避免最终答案落到 delta（便于恢复与审计一致性）。
		a.notifyHistoryReplace()
		a.emitRuntimeLog("info", "final answer history persisted", snapshot, map[string]any{
			"event":          "final_answer_history_persisted",
			"history_length": len(a.history),
			"content_length": len(strings.TrimSpace(historyText)),
		})
	}

	a.emitRuntimeLog("info", "final answer completed", snapshot, map[string]any{
		"event":          "final_answer_completed",
		"content_length": len(strings.TrimSpace(finalText)),
		"status":         decision.status,
	})
	return snapshot, nil
}

func parseFinalAnswerOutput(raw string) (FinalAnswerModelOutput, error) {
	var zero FinalAnswerModelOutput
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return zero, structuredoutput.MissingJSONObjectError("final_answer output is empty")
	}
	objects := jsonextractor.ExtractObjectsOnly(raw)
	if len(objects) == 0 {
		return zero, structuredoutput.MissingJSONObjectError("final_answer output missing json object")
	}

	objText := strings.TrimSpace(objects[0])
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(objText), &rawMap); err != nil {
		return zero, structuredoutput.UnmarshalFailedError(err)
	}
	requiredKeys := []string{
		"is_complete",
		"status",
		"reason",
		"should_replan",
		"next_goal",
		"missing_items",
		"warnings",
		"user_message",
		"references",
	}
	for _, key := range requiredKeys {
		if _, ok := rawMap[key]; ok {
			continue
		}
		return zero, structuredoutput.UnmarshalFailedError(fmt.Errorf("final_answer missing required field %q", key))
	}

	var out FinalAnswerModelOutput
	if err := json.Unmarshal([]byte(objText), &out); err != nil {
		return zero, structuredoutput.UnmarshalFailedError(err)
	}
	return out, nil
}

type finalAnswerDecision struct {
	model      FinalAnswerModelOutput
	status     builtin_tools.TaskStatus
	isTerminal bool
}

func normalizeFinalAnswerDecision(modelOut FinalAnswerModelOutput) finalAnswerDecision {
	modelOut.Status = strings.ToLower(strings.TrimSpace(modelOut.Status))
	modelOut.Reason = strings.TrimSpace(modelOut.Reason)
	modelOut.NextGoal = strings.TrimSpace(modelOut.NextGoal)
	modelOut.UserMessage = strings.TrimSpace(modelOut.UserMessage)
	modelOut.MissingItems = normalizeReferences(modelOut.MissingItems)
	modelOut.Warnings = normalizeReferences(modelOut.Warnings)
	modelOut.References = normalizeReferences(modelOut.References)

	status := builtin_tools.TaskStatusRunning
	switch modelOut.Status {
	case string(builtin_tools.TaskStatusCompleted):
		status = builtin_tools.TaskStatusCompleted
	case string(builtin_tools.TaskStatusFailed):
		status = builtin_tools.TaskStatusFailed
	case string(builtin_tools.TaskStatusCanceled):
		status = builtin_tools.TaskStatusCanceled
	case string(builtin_tools.TaskStatusRunning):
		status = builtin_tools.TaskStatusRunning
	default:
		if modelOut.IsComplete {
			status = builtin_tools.TaskStatusCompleted
		}
	}

	isTerminal := modelOut.IsComplete
	if status == builtin_tools.TaskStatusCompleted || status == builtin_tools.TaskStatusFailed || status == builtin_tools.TaskStatusCanceled {
		isTerminal = true
	}
	if status == builtin_tools.TaskStatusRunning {
		isTerminal = false
	}
	if modelOut.IsComplete && status == builtin_tools.TaskStatusRunning {
		status = builtin_tools.TaskStatusCompleted
		isTerminal = true
	}
	if !modelOut.IsComplete && status != builtin_tools.TaskStatusRunning {
		modelOut.IsComplete = true
		isTerminal = true
	}

	if !modelOut.IsComplete {
		modelOut.ShouldReplan = true
	}
	return finalAnswerDecision{
		model:      modelOut,
		status:     status,
		isTerminal: isTerminal,
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (a *Agent) canFastCloseFinalAnswer(snapshot builtin_tools.StateSnapshot, ctx context.Context) bool {
	if snapshot.NeedsPlanning {
		return false
	}
	if snapshot.ExternalInterrupt != nil {
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
		if outcome := findOutcome(snapshot.StepOutcomes, stepID); outcome != nil {
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
	}

	a.emitRuntimeLog("info", "final answer fast closed", snapshot, map[string]any{
		"event":          "final_answer_fast_closed",
		"content_length": len(finalText),
	})
	return snapshot, nil
}
