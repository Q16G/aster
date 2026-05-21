package react

import (
	"context"
	"encoding/json"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/jsonextractor"
)

type intentClassificationModelOutput struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

func (a *Agent) runIntentClassificationPhase(ctx context.Context, iter int, runClient ai.ChatClient) error {
	_ = a.state.SetPhase(builtin_tools.AgentPhaseIntentClassification)
	snapshot := a.state.Snapshot()
	a.emitter.EmitStateChange(snapshot)
	a.emitRuntimeLog("info", "enter intent classification phase", snapshot, map[string]any{
		"event": "phase_enter",
	})

	input := buildIntentClassificationInput(snapshot)
	prompt, err := a.promptManager.BuildIntentClassificationPrompt(input)
	if err != nil {
		a.emitRuntimeLog("warn", "build intent classification prompt failed, fallback to carry", snapshot, map[string]any{
			"error": err.Error(),
		})
		return a.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "carry"})
	}

	callCtx, callCancel := context.WithCancel(ctx)
	callResult, err := a.AICallProxy(callCtx, iter, runClient, prompt)
	callCancel()
	if err != nil {
		a.emitRuntimeLog("warn", "intent classification AICallProxy failed, fallback to carry", snapshot, map[string]any{
			"error": err.Error(),
		})
		return a.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "carry"})
	}

	result := parseIntentClassificationOutput(strings.TrimSpace(callResult.AssistantText))

	a.emitRuntimeLog("info", "intent classification result", snapshot, map[string]any{
		"event":  "intent_classified",
		"action": result.Action,
		"reason": result.Reason,
	})

	return a.applyIntentClassification(snapshot, result)
}

func buildIntentClassificationInput(snapshot builtin_tools.StateSnapshot) IntentClassificationPromptInput {
	input := IntentClassificationPromptInput{
		PreviousGoal: strings.TrimSpace(snapshot.CurrentGoal),
	}

	for _, item := range snapshot.Plan {
		if item == nil {
			continue
		}
		input.TotalCount++
		if item.Status == builtin_tools.PlanStepCompleted {
			input.CompletedCount++
		}
	}

	const maxRecentOutcomes = 3
	outcomes := snapshot.StepOutcomes
	start := 0
	if len(outcomes) > maxRecentOutcomes {
		start = len(outcomes) - maxRecentOutcomes
	}
	for _, o := range outcomes[start:] {
		if o == nil {
			continue
		}
		short := strings.TrimSpace(o.ShortSummary)
		if short == "" {
			short = strings.TrimSpace(o.Summary)
		}
		if short == "" {
			continue
		}
		input.RecentOutcomes = append(input.RecentOutcomes, IntentOutcomeSummary{
			StepID:       strings.TrimSpace(o.StepID),
			ShortSummary: short,
		})
	}

	for _, t := range snapshot.InputTimeline {
		if t == nil {
			continue
		}
		timeStr := ""
		if !t.CreatedAt.IsZero() {
			timeStr = t.CreatedAt.Format("15:04:05")
		}
		input.InputTimeline = append(input.InputTimeline, IntentTimelineEntry{
			Time:    timeStr,
			Content: strings.TrimSpace(t.Content),
		})
	}

	return input
}

func parseIntentClassificationOutput(raw string) intentClassificationModelOutput {
	if raw == "" {
		return intentClassificationModelOutput{Action: "carry", Reason: "empty response fallback"}
	}

	var out intentClassificationModelOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		if isValidIntentAction(out.Action) {
			return out
		}
	}

	for _, candidate := range jsonextractor.ExtractObjectsOnly(raw) {
		if err := json.Unmarshal([]byte(candidate), &out); err == nil {
			if isValidIntentAction(out.Action) {
				return out
			}
		}
	}

	return intentClassificationModelOutput{Action: "carry", Reason: "parse fallback"}
}

func isValidIntentAction(action string) bool {
	switch strings.TrimSpace(strings.ToLower(action)) {
	case "carry", "replan", "cold_start":
		return true
	}
	return false
}

func latestInputContent(snapshot builtin_tools.StateSnapshot) string {
	ti := snapshot.LatestInput()
	if ti == nil {
		return ""
	}
	return strings.TrimSpace(ti.Content)
}

func (a *Agent) applyIntentClassification(snapshot builtin_tools.StateSnapshot, result intentClassificationModelOutput) error {
	action := strings.TrimSpace(strings.ToLower(result.Action))

	switch action {
	case "carry":
		_ = a.state.SetPhase(builtin_tools.AgentPhasePlan)

	case "replan":
		latest := latestInputContent(snapshot)
		a.state.SetReplanContext(&builtin_tools.ReplanContext{
			Reason:         strings.TrimSpace(result.Reason),
			NextGoal:       latest,
			ReplacePending: true,
		})
		_ = a.state.SetPhase(builtin_tools.AgentPhasePlan)

	case "cold_start":
		latest := latestInputContent(snapshot)
		a.state.Reset()
		if latest != "" {
			_ = a.state.AppendInputTimeline(latest)
		}
		a.history = nil
		if latest != "" {
			a.history = append(a.history, ai.NewUserMsgInfo(latest))
			a.notifyHistoryReplace()
		}

	default:
		_ = a.state.SetPhase(builtin_tools.AgentPhasePlan)
	}

	return nil
}
