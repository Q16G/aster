package react

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/runtimelog"
	"aster/internal/structuredoutput"
)

//go:embed prompts/step_outcomes_reducer.prompt
var stepOutcomesReducerPrompt string

const (
	stepOutcomesReducerKeepLast    = 3
	stepOutcomesReducerBudgetRatio = 0.4
)

type reducedStepOutcome struct {
	StepID          string   `json:"step_id"`
	Status          string   `json:"status"`
	Summary         string   `json:"summary"`
	KeyFacts        []string `json:"key_facts,omitempty"`
	LongSummary     string   `json:"long_summary,omitempty"`
	OpenQuestions   []string `json:"open_questions,omitempty"`
	ToolCallsDigest []string `json:"tool_calls_digest,omitempty"`
	References      []string `json:"references,omitempty"`
	StatusSummary   string   `json:"status_summary,omitempty"`
	Error           string   `json:"error,omitempty"`
	SummaryFile     string   `json:"summary_file,omitempty"`
	ResultFile      string   `json:"result_file,omitempty"`
	TimelineFile    string   `json:"timeline_file,omitempty"`
}

func stepOutcomesExceedBudget(client ai.ChatClient, outcomes []*builtin_tools.StepOutcome) (totalTokens int, tokenBudget int, exceeded bool) {
	if len(outcomes) <= stepOutcomesReducerKeepLast {
		return 0, 0, false
	}
	budget := resolveContextBudget(client)
	tokenBudget = int(float64(budget.UsableInputTokens) * stepOutcomesReducerBudgetRatio)
	if tokenBudget <= 0 {
		return 0, 0, false
	}
	outcomesJSON, err := json.Marshal(outcomes)
	if err != nil {
		return 0, tokenBudget, false
	}
	totalTokens = countTokens(string(outcomesJSON))
	return totalTokens, tokenBudget, totalTokens > tokenBudget
}

func (a *Agent) reduceStepOutcomesIfNeeded(ctx context.Context, client ai.ChatClient, outcomes []*builtin_tools.StepOutcome) ([]*builtin_tools.StepOutcome, error) {
	totalTokens, tokenBudget, exceeded := stepOutcomesExceedBudget(client, outcomes)
	if !exceeded {
		return outcomes, nil
	}

	runtimelog.LogJSON("info", map[string]any{
		"event":         "step_outcomes_reducer_triggered",
		"total_tokens":  totalTokens,
		"token_budget":  tokenBudget,
		"total_steps":   len(outcomes),
		"reduce_steps":  len(outcomes) - stepOutcomesReducerKeepLast,
		"protect_steps": stepOutcomesReducerKeepLast,
	})

	splitAt := len(outcomes) - stepOutcomesReducerKeepLast
	toReduce := outcomes[:splitAt]
	toKeep := outcomes[splitAt:]

	reduced, err := a.runStepOutcomesReducer(ctx, client, toReduce)
	if err != nil {
		runtimelog.LogJSON("warning", map[string]any{
			"event": "step_outcomes_reducer_failed",
			"error": strings.TrimSpace(err.Error()),
		})
		return outcomes, nil
	}

	result := make([]*builtin_tools.StepOutcome, 0, len(reduced)+len(toKeep))
	result = append(result, reduced...)
	result = append(result, toKeep...)

	reducedJSON, _ := json.Marshal(result)
	reducedTokens := countTokens(string(reducedJSON))
	runtimelog.LogJSON("info", map[string]any{
		"event":          "step_outcomes_reducer_completed",
		"before_tokens":  totalTokens,
		"after_tokens":   reducedTokens,
		"before_steps":   len(outcomes),
		"after_steps":    len(result),
		"reduced_steps":  len(reduced),
		"protected_steps": len(toKeep),
	})

	return result, nil
}

func (a *Agent) reduceStepOutcomesInState(ctx context.Context, client ai.ChatClient) {
	snapshot := a.state.Snapshot()
	outcomes := snapshot.StepOutcomes

	if _, _, exceeded := stepOutcomesExceedBudget(client, outcomes); !exceeded {
		return
	}

	origPhase := snapshot.Phase
	_ = a.state.SetPhase(builtin_tools.AgentPhaseStepOutcomesReducer)
	a.emitter.EmitStateChange(a.state.Snapshot())
	defer func() {
		_ = a.state.SetPhase(origPhase)
		a.emitter.EmitStateChange(a.state.Snapshot())
	}()

	a.emitter.EmitLogPayload(map[string]any{
		"event": "step_outcomes_reducing",
		"total": len(outcomes),
	})

	reduced, err := a.reduceStepOutcomesIfNeeded(ctx, client, outcomes)
	if err != nil {
		return
	}
	a.state.ReplaceStepOutcomes(reduced)
}

func (a *Agent) runStepOutcomesReducer(ctx context.Context, client ai.ChatClient, outcomes []*builtin_tools.StepOutcome) ([]*builtin_tools.StepOutcome, error) {
	if a == nil || a.promptManager == nil {
		return nil, fmt.Errorf("prompt manager is nil")
	}

	outcomesJSON, err := json.Marshal(outcomes)
	if err != nil {
		return nil, fmt.Errorf("marshal step outcomes failed: %w", err)
	}

	prompt, err := a.promptManager.BuildStepOutcomesReducerPrompt(StepOutcomesReducerPromptInput{
		StepOutcomes: string(outcomesJSON),
	})
	if err != nil {
		return nil, fmt.Errorf("build reducer prompt failed: %w", err)
	}

	snapshot := a.state.Snapshot()
	result, err := runStructuredOutputWithRetry(a, ctx, snapshot, client, "step_outcomes_reducer", prompt, parseReducedOutcomes)
	if err != nil {
		return nil, fmt.Errorf("reducer LLM call failed: %w", err)
	}

	return result.Value, nil
}

func parseReducedOutcomes(raw string) ([]*builtin_tools.StepOutcome, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, structuredoutput.MissingJSONObjectError("reducer output is empty")
	}

	// Extract JSON array
	start := strings.Index(raw, "[")
	end := strings.LastIndex(raw, "]")
	if start < 0 || end < 0 || end <= start {
		return nil, structuredoutput.MissingJSONObjectError("reducer output missing json array")
	}
	arrayJSON := raw[start : end+1]

	var reduced []reducedStepOutcome
	if err := json.Unmarshal([]byte(arrayJSON), &reduced); err != nil {
		return nil, structuredoutput.UnmarshalFailedError(err)
	}

	out := make([]*builtin_tools.StepOutcome, 0, len(reduced))
	for _, r := range reduced {
		out = append(out, &builtin_tools.StepOutcome{
			StepID:          strings.TrimSpace(r.StepID),
			Status:          builtin_tools.StepOutcomeStatus(strings.TrimSpace(r.Status)),
			Summary:         strings.TrimSpace(r.Summary),
			KeyFacts:        r.KeyFacts,
			LongSummary:     strings.TrimSpace(r.LongSummary),
			OpenQuestions:   r.OpenQuestions,
			ToolCallsDigest: r.ToolCallsDigest,
			References:      r.References,
			StatusSummary:   strings.TrimSpace(r.StatusSummary),
			Error:           strings.TrimSpace(r.Error),
			SummaryFile:     strings.TrimSpace(r.SummaryFile),
			ResultFile:      strings.TrimSpace(r.ResultFile),
			TimelineFile:    strings.TrimSpace(r.TimelineFile),
		})
	}
	return out, nil
}
