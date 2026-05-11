package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/jsonextractor"
	"aster/internal/structuredoutput"
)

type stepReplanModelOutput struct {
	ShouldReplan bool     `json:"should_replan"`
	ReplanReason string   `json:"replan_reason"`
	NextGoal     string   `json:"next_goal"`
	MissingItems []string `json:"missing_items"`
	Warnings     []string `json:"warnings"`
}

func (a *Agent) runStepReplanPhase(ctx context.Context, iter int, runClient ai.ChatClient) error {
	_ = a.state.SetPhase(builtin_tools.AgentPhaseStepReplan)
	snapshot := a.state.Snapshot()
	a.emitter.EmitStateChange(snapshot)
	a.emitRuntimeLog("info", "enter step replan phase", snapshot, map[string]any{
		"event": "phase_enter",
	})

	current := snapshot.CurrentStep()
	if current == nil || strings.TrimSpace(current.ID) == "" {
		return fmt.Errorf("step_replan phase missing current step")
	}
	stepID := strings.TrimSpace(current.ID)

	rawOutcome := findOutcome(snapshot.StepOutcomes, stepID)
	if rawOutcome == nil {
		return fmt.Errorf("step_replan phase missing step outcome step_id=%s", stepID)
	}
	artifactDir := ""

	// Fast path: step completed + no open questions → skip LLM
	if rawOutcome.Status == builtin_tools.StepOutcomeCompleted && len(rawOutcome.OpenQuestions) == 0 {
		return a.applyReplanResult(stepID, nil, nil, snapshot, artifactDir)
	}

	// LLM replan decision
	outcomeJSON, _ := json.Marshal(rawOutcome)
	prompt, err := a.BuildStepReplanPrompt(map[string]any{
		"current_goal":  snapshot.CurrentGoal,
		"current_step":  current,
		"step_outcome":  string(outcomeJSON),
		"task_plan":     snapshot.Plan,
		"step_outcomes": snapshot.StepOutcomes,
		"warnings":      snapshot.Warnings,
		"unresolved":    snapshot.Unresolved,
	})
	if err != nil {
		return fmt.Errorf("build step replan prompt failed: %w", err)
	}

	result, err := runStructuredOutputWithRetry(a, ctx, snapshot, runClient, "step_replan", prompt, func(raw string) (stepReplanModelOutput, error) {
		return parseStepReplanOutput(raw)
	})
	if err != nil {
		return fmt.Errorf("step replan LLM call failed: %w", err)
	}

	modelOut := result.Value

	var replanContext *builtin_tools.ReplanContext
	if modelOut.ShouldReplan {
		nextGoal := strings.TrimSpace(modelOut.NextGoal)
		if nextGoal == "" {
			nextGoal = strings.TrimSpace(snapshot.CurrentGoal)
		}
		replanReason := strings.TrimSpace(modelOut.ReplanReason)
		replanContext = &builtin_tools.ReplanContext{
			SourceStepID:   stepID,
			Reason:         replanReason,
			NextGoal:       nextGoal,
			MissingItems:   normalizeStringSlice(modelOut.MissingItems),
			Warnings:       normalizeStringSlice(modelOut.Warnings),
			ReplacePending: true,
		}
	}

	return a.applyReplanResult(stepID, &modelOut, replanContext, snapshot, artifactDir)
}

func (a *Agent) applyReplanResult(stepID string, modelOut *stepReplanModelOutput, replanContext *builtin_tools.ReplanContext, snapshot builtin_tools.StateSnapshot, artifactDir string) error {
	current := snapshot.CurrentStep()

	nextPhase := builtin_tools.AgentPhaseFinalAnswer
	nextRunnableStepID := ""
	if replanContext != nil {
		nextPhase = builtin_tools.AgentPhasePlan
	} else if candidate := strings.TrimSpace(builtin_tools.NextRunnablePlanStepID(snapshot.Plan)); candidate != "" {
		nextRunnableStepID = candidate
		nextPhase = builtin_tools.AgentPhaseStep
	}

	summaryGoal := ""
	if replanContext != nil {
		summaryGoal = strings.TrimSpace(replanContext.NextGoal)
	}

	var replanWarnings, replanMissingItems []string
	if replanContext != nil {
		replanWarnings = replanContext.Warnings
		replanMissingItems = replanContext.MissingItems
	}

	rawOutcome := findOutcome(snapshot.StepOutcomes, stepID)

	snapshot = a.state.ApplyStepReplan(stepID, stepReplanUpdate{
		ArtifactDir:   artifactDir,
		CurrentGoal:   summaryGoal,
		Warnings:      replanWarnings,
		Unresolved:    replanMissingItems,
		ReplanContext: replanContext,
		NextPhase:     nextPhase,
	})
	a.emitter.EmitStateChange(snapshot)

	if rawOutcome != nil {
		a.emitter.EmitStepSummaryResult(stepID, strings.TrimSpace(current.Step), rawOutcome)
	}
	if modelOut != nil {
		a.emitter.EmitStepReplanResult(stepID, strings.TrimSpace(current.Step), modelOut)
	}

	a.emitRuntimeLog("info", "step replan completed", snapshot, map[string]any{
		"event":         "step_replan_completed",
		"step_id":       stepID,
		"next_phase":    nextPhase,
		"next_step_id":  nextRunnableStepID,
		"should_replan": replanContext != nil,
		"artifact_dir":  artifactDir,
	})
	return nil
}

func findOutcome(outcomes []*builtin_tools.StepOutcome, stepID string) *builtin_tools.StepOutcome {
	for _, o := range outcomes {
		if o != nil && strings.TrimSpace(o.StepID) == stepID {
			return o
		}
	}
	return nil
}

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseStepReplanOutput(raw string) (stepReplanModelOutput, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return stepReplanModelOutput{}, fmt.Errorf("empty replan output")
	}
	results, _ := jsonextractor.ExtractJSONWithRaw(raw)
	if len(results) > 0 {
		raw = results[0]
	}
	var out stepReplanModelOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return stepReplanModelOutput{}, fmt.Errorf("parse replan output: %w", err)
	}
	return out, nil
}

var _ structuredoutput.ParseFunc[stepReplanModelOutput] = parseStepReplanOutput
