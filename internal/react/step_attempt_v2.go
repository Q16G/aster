package react

import (
	"encoding/json"
	"strings"
	"time"

	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
)

func (a *Agent) writeV2StepAttemptResult(stepID, stepName, attemptID string, status builtin_tools.PlanStepStatus, outcome *builtin_tools.StepOutcome) {
	if a == nil || a.v2Store == nil {
		return
	}
	stepID = strings.TrimSpace(stepID)
	attemptID = strings.TrimSpace(attemptID)
	if stepID == "" || attemptID == "" || outcome == nil {
		return
	}

	turnID := strings.TrimSpace(a.currentTurnID)
	sessionID := strings.TrimSpace(a.workspaceSessionID)

	attemptStatus := "succeeded"
	if status == builtin_tools.PlanStepFailed {
		attemptStatus = "failed"
	}

	var structured any
	raw := strings.TrimSpace(outcome.Result)
	if raw != "" {
		var parsed any
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			structured = parsed
		} else {
			structured = raw
		}
	}

	artifacts := make([]persistv2.StepAttemptArtifact, 0)
	for _, ref := range outcome.References {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		artifacts = append(artifacts, persistv2.StepAttemptArtifact{
			Kind:  "reference",
			Ref:   ref,
			Title: ref,
		})
	}
	if len(artifacts) == 0 {
		artifacts = nil
	}

	now := time.Now().UnixMilli()
	title := strings.TrimSpace(stepName)
	if title == "" {
		title = stepID
	}
	res := &persistv2.StepAttemptResult{
		SessionID:     sessionID,
		TurnID:        turnID,
		StepID:        stepID,
		AttemptID:     attemptID,
		Status:        attemptStatus,
		ShortSummary:  strings.TrimSpace(outcome.ShortSummary),
		LongSummary:   strings.TrimSpace(outcome.LongSummary),
		OpenQuestions: builtin_tools.CloneStringSlice(outcome.OpenQuestions),
		Warnings:        nil,
		ToolCallsDigest: builtin_tools.CloneStringSlice(outcome.ToolCallsDigest),
		Display: &persistv2.StepAttemptDisplay{
			Title:   title,
			Summary: firstNonEmpty(strings.TrimSpace(outcome.StatusSummary), strings.TrimSpace(outcome.ShortSummary)),
		},
		Result: &persistv2.StepAttemptPayload{
			Structured: structured,
			Artifacts:  artifacts,
		},
		Timing: &persistv2.StepAttemptTiming{
			StartedAt:  now,
			FinishedAt: now,
		},
	}
	if _, err := a.v2Store.WriteStepAttemptResult(stepID, attemptID, res); err != nil {
		a.emitPersistenceError("write_step_attempt_result", err)
	}
}

