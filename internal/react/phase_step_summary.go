package react

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/jsonextractor"
	"aster/internal/runtimelog"
	"aster/internal/structuredoutput"
)

type stepSummaryModelOutput struct {
	StatusSummary    string   `json:"status_summary"`
	StepShortSummary string   `json:"step_short_summary"`
	StepLongSummary  string   `json:"step_long_summary"`
	KeyFacts         []string `json:"key_facts"`
	OpenQuestions    []string `json:"open_questions"`
	ShouldReplan     bool     `json:"should_replan"`
	ReplanReason     string   `json:"replan_reason"`
	NextGoal         string   `json:"next_goal"`
	MissingItems     []string `json:"missing_items"`
	Warnings         []string `json:"warnings"`
	ToolCallsDigest  []string `json:"tool_calls_digest"`
}

func (a *Agent) runStepSummaryPhase(ctx context.Context, iter int, runClient ai.ChatClient) error {
	_ = a.state.SetPhase(builtin_tools.AgentPhaseStepSummary)
	snapshot := a.state.Snapshot()
	a.emitter.EmitStateChange(snapshot)
	a.emitRuntimeLog("info", "enter step summary phase", snapshot, map[string]any{
		"event": "phase_enter",
	})

	current := snapshot.CurrentStep()
	if current == nil || strings.TrimSpace(current.ID) == "" {
		return fmt.Errorf("step_summary phase missing current step")
	}
	stepID := strings.TrimSpace(current.ID)

	rawOutcome := findStepOutcome(snapshot.StepOutcomes, stepID)
	if rawOutcome == nil {
		return fmt.Errorf("step_summary phase missing raw step outcome step_id=%s", stepID)
	}

	writer, err := newArtifactWriter(a.workspaceRuntime)
	if err != nil {
		return err
	}

	artifactPlan, err := writer.PlanStepArtifactRel(snapshot.PlanVersion, stepID, stepID)
	if err != nil {
		return err
	}
	artifactDir := strings.TrimSpace(artifactPlan.ArtifactDir)
	summaryFile := strings.TrimSpace(artifactPlan.SummaryFile)
	resultFile := strings.TrimSpace(artifactPlan.ResultFile)

	// 1) raw timeline diff：由 TimelineMemoryDiffer 生成（可能是 patch 风格文本）
	rawTimelineDiff := ""
	if a.stepDiffer != nil && strings.TrimSpace(a.stepBaselineStepID) == stepID && !a.stepBaselineAt.IsZero() {
		if diff, diffErr := a.stepDiffer.Diff(); diffErr == nil {
			rawTimelineDiff = strings.TrimSpace(diff)
		} else {
			a.emitRuntimeLog("warning", "step timeline diff failed", snapshot, map[string]any{
				"event":   "step_diff_failed",
				"step_id": stepID,
				"error":   diffErr.Error(),
			})
		}
	}

	refs := BuildStepReferences(rawOutcome.References, artifactDir, summaryFile, resultFile)
	// 2) diffSummaryHint：runtime 启发式摘要（用于 reducer / step_summary 消费）
	diffSummaryHint := a.buildTimelineDiffForStep(rawOutcome, refs, artifactDir, summaryFile, resultFile)
	stepBudget := ComputeStepWindowTokenBudget(resolveContextBudget(runClient))
	rawTimelineTokens := estimateStringTokens(rawTimelineDiff)

	window := &StepWindow{
		StepID:          stepID,
		RawApproxTokens: rawTimelineTokens,
		BudgetTokens:    stepBudget,
		ExcerptTokens:   stepBudget,
		RawTimelineDiff: func() string {
			raw := strings.TrimSpace(rawTimelineDiff)
			if raw == "" {
				return ""
			}
			if rawTimelineTokens <= stepBudget {
				return raw
			}
			return truncateStringByApproxTokens(raw, stepBudget)
		}(),
	}
	a.emitRuntimeLog("info", "step window computed", snapshot, map[string]any{
		"event":             "step_window_computed",
		"step_id":           stepID,
		"raw_approx_tokens": rawTimelineTokens,
		"budget_tokens":     stepBudget,
		"reducer_triggered": rawTimelineTokens > stepBudget,
	})

	if rawTimelineTokens > stepBudget {
		a.emitRuntimeLog("info", "step reducer started", snapshot, map[string]any{
			"event":             "step_reducer_started",
			"step_id":           stepID,
			"raw_approx_tokens": rawTimelineTokens,
			"budget_tokens":     stepBudget,
		})
		reducerPayload := map[string]any{
			"step_id":           stepID,
			"input_timeline":    snapshot.InputTimeline,
			"current_step":      current,
			"raw_timeline_diff": rawTimelineDiff,
			"diff_summary_hint": diffSummaryHint,
			"references":        refs,
			"artifacts": map[string]any{
				"artifact_id":  strings.TrimSpace(artifactPlan.ArtifactID),
				"artifact_dir": artifactDir,
				"summary_file": summaryFile,
				"result_file":  resultFile,
			},
		}
		if reducerPrompt, promptErr := a.BuildReducerPrompt(reducerPayload); promptErr == nil {
			if retryResult, reduceErr := runStructuredOutputWithRetry(a, ctx, snapshot, runClient, "step_reducer", reducerPrompt, parseStepReducerOutput); reduceErr == nil {
				out := retryResult.Value
				window.Reduced = true
				window.Reducer = &out
				a.emitRuntimeLog("info", "step reducer completed", snapshot, map[string]any{
					"event":                  "step_reducer_completed",
					"step_id":                stepID,
					"new_facts_count":        len(out.NewFacts),
					"references_count":       len(out.References),
					"artifact_changes_count": len(out.ArtifactChanges),
					"open_questions_count":   len(out.OpenQuestions),
				})
			} else {
				a.emitRuntimeLog("warning", "step reducer failed", snapshot, map[string]any{
					"event":   "step_reducer_failed",
					"step_id": stepID,
					"error":   strings.TrimSpace(reduceErr.Error()),
				})
			}
		} else {
			a.emitRuntimeLog("warning", "build reducer prompt failed", snapshot, map[string]any{
				"event":   "step_reducer_prompt_failed",
				"step_id": stepID,
				"error":   promptErr.Error(),
			})
		}
	}

	reducerTriggered := rawTimelineTokens > stepBudget
	a.emitRuntimeLog("info", "reducer done", snapshot, map[string]any{
		"event":     "reducer_done",
		"step_id":   stepID,
		"triggered": reducerTriggered,
		"reduced":   window.Reduced,
		"ok":        !reducerTriggered || window.Reducer != nil,
	})

	resultFileAbs := filepath.Join(a.workspaceRootDir, filepath.FromSlash(resultFile))
	a.emitRuntimeLog("info", "step window ready", snapshot, map[string]any{
		"event":           "step_window_ready",
		"step_id":         stepID,
		"step_window":     window,
		"artifact_dir":    artifactDir,
		"summary_file":    summaryFile,
		"result_file":     resultFile,
		"result_file_abs": resultFileAbs,
	})

	windowSummary := strings.TrimSpace(diffSummaryHint)
	if window.Reducer != nil && strings.TrimSpace(window.Reducer.WindowSummary) != "" {
		windowSummary = strings.TrimSpace(window.Reducer.WindowSummary)
	}

	payload := map[string]any{
		"input_timeline": snapshot.InputTimeline,
		"current_goal":   snapshot.CurrentGoal,
		"current_step":   current,
		"task_plan":      snapshot.Plan,
		"raw_outcome": map[string]any{
			"step_id":        strings.TrimSpace(rawOutcome.StepID),
			"status":         strings.TrimSpace(string(rawOutcome.Status)),
			"summary":        strings.TrimSpace(rawOutcome.Summary),
			"display_result": strings.TrimSpace(rawOutcome.DisplayResult),
			"result":         strings.TrimSpace(rawOutcome.Result),
			"error":          strings.TrimSpace(rawOutcome.Error),
		},
		"step_window":   window,
		"timeline_diff": windowSummary,
		"references":    refs,
		"warnings":      snapshot.Warnings,
		"unresolved":    snapshot.Unresolved,
		"artifacts": map[string]any{
			"artifact_id":  strings.TrimSpace(artifactPlan.ArtifactID),
			"artifact_dir": artifactDir,
			"summary_file": summaryFile,
			"result_file":  resultFile,
		},
	}

	prompt, err := a.BuildStepSummaryPrompt(payload)
	if err != nil {
		return err
	}

	retryResult, err := runStructuredOutputWithRetry(a, ctx, snapshot, runClient, "step_summary", prompt, parseStepSummaryOutput)
	if err != nil {
		rawResponse := strings.TrimSpace(structuredoutput.LastResponse(err))
		runtimelog.LogJSON("error", map[string]any{
			"event":               "step_summary_model_raw_response",
			"phase":               "step_summary",
			"mode":                "parse_failed",
			"step_id":             stepID,
			"error":               strings.TrimSpace(err.Error()),
			"raw_response":        rawResponse,
			"raw_response_length": len(rawResponse),
		})
		a.emitRuntimeLog("error", "step summary model json parse failed", snapshot, map[string]any{
			"event":   "step_summary_model_fallback_error",
			"step_id": stepID,
			"error":   strings.TrimSpace(err.Error()),
		})
		return fmt.Errorf("step_summary structured output retry exhausted: %w", err)
	}
	runtimelog.LogJSON("info", map[string]any{
		"event":               "step_summary_model_raw_response",
		"phase":               "step_summary",
		"mode":                "success",
		"step_id":             stepID,
		"raw_response":        strings.TrimSpace(retryResult.RawResponse),
		"raw_response_length": len(strings.TrimSpace(retryResult.RawResponse)),
	})
	modelOut := retryResult.Value

	tmp := cloneStepOutcome(rawOutcome)
	tmp.StatusSummary = strings.TrimSpace(modelOut.StatusSummary)
	tmp.ShortSummary = strings.TrimSpace(modelOut.StepShortSummary)
	tmp.LongSummary = strings.TrimSpace(modelOut.StepLongSummary)
	tmp.KeyFacts = normalizeStringSlice(modelOut.KeyFacts)
	tmp.OpenQuestions = normalizeStringSlice(modelOut.OpenQuestions)
	tmp.ToolCallsDigest = normalizeStringSlice(modelOut.ToolCallsDigest)
	tmp.TimelineDiffSummary = strings.TrimSpace(windowSummary)
	tmp.References = refs
	tmp.UpdatedAt = time.Now()
	if strings.TrimSpace(tmp.ContextKey) == "" {
		tmp.ContextKey = "ctx-" + uuid.NewString()
	}

	record, err := writer.PersistStepArtifacts(snapshot, a.workspaceSessionID, a.Name(), current, &tmp, window, artifactPlan)
	if err != nil {
		return err
	}
	tmp.ArtifactDir = strings.TrimSpace(record.ArtifactDir)
	tmp.SummaryFile = strings.TrimSpace(record.SummaryFile)
	tmp.ResultFile = strings.TrimSpace(record.ResultFile)
	a.emitRuntimeLog("info", "step artifacts written", snapshot, map[string]any{
		"event":        "step_artifacts_written",
		"step_id":      stepID,
		"artifact_dir": tmp.ArtifactDir,
		"summary_file": tmp.SummaryFile,
		"result_file":  tmp.ResultFile,
	})

	// Append execution lineage record to workspace/step_contexts.jsonl.
	var inheritedKeys []string
	var inheritedRefIDs []string
	if a != nil && a.frozenLineageByStep != nil {
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

	// step_summary 完成后先做本地调度判断：
	// - 需要重规划：直接进入 plan phase
	// - 仍有可执行 step：直接回到 step phase
	// - 无可执行 step：进入 final_answer phase 做任务级收尾/判断
	replanWarnings := normalizeStringSlice(modelOut.Warnings)
	replanMissingItems := normalizeStringSlice(modelOut.MissingItems)
	replanReason := strings.TrimSpace(modelOut.ReplanReason)
	nextGoal := strings.TrimSpace(modelOut.NextGoal)
	if modelOut.ShouldReplan && nextGoal == "" {
		nextGoal = strings.TrimSpace(snapshot.CurrentGoal)
	}
	if modelOut.ShouldReplan && replanReason == "" {
		replanReason = firstNonEmpty(modelOut.StatusSummary, modelOut.StepShortSummary, modelOut.StepLongSummary)
	}
	var replanContext *builtin_tools.ReplanContext
	if modelOut.ShouldReplan {
		replanContext = &builtin_tools.ReplanContext{
			SourceStepID:   stepID,
			Reason:         replanReason,
			NextGoal:       nextGoal,
			MissingItems:   replanMissingItems,
			Warnings:       replanWarnings,
			ReplacePending: true,
		}
	}

	nextPhase := builtin_tools.AgentPhaseFinalAnswer
	nextRunnableStepID := ""
	if replanContext != nil {
		nextPhase = builtin_tools.AgentPhasePlan
	} else if candidate := strings.TrimSpace(builtin_tools.NextRunnablePlanStepID(snapshot.Plan)); candidate != "" {
		nextRunnableStepID = candidate
		nextPhase = builtin_tools.AgentPhaseStep
	}

	snapshot = a.state.ApplyStepSummary(stepID, stepSummaryUpdate{
		StatusSummary:       tmp.StatusSummary,
		ShortSummary:        tmp.ShortSummary,
		LongSummary:         tmp.LongSummary,
		KeyFacts:            tmp.KeyFacts,
		OpenQuestions:       tmp.OpenQuestions,
		ToolCallsDigest:     tmp.ToolCallsDigest,
		TimelineDiffSummary: tmp.TimelineDiffSummary,
		ArtifactDir:         tmp.ArtifactDir,
		SummaryFile:         tmp.SummaryFile,
		ResultFile:          tmp.ResultFile,
		ContextKey:          tmp.ContextKey,
		References:          tmp.References,
		CurrentGoal:         nextGoal,
		Warnings:            replanWarnings,
		Unresolved:          replanMissingItems,
		ReplanContext:       replanContext,
		NextPhase:           nextPhase,
	})
	if err := writer.PersistRuntimeCheckpoint(snapshot, a.workspaceSessionID, "step_summary"); err != nil {
		return err
	}
	a.emitter.EmitStateChange(snapshot)

	// step baseline 清理，下一步重新记录
	a.stepBaselineAt = time.Time{}
	a.stepBaselineStepID = ""

	a.emitRuntimeLog("info", "step summary completed", snapshot, map[string]any{
		"event":         "step_summary_completed",
		"step_id":       stepID,
		"next_phase":    nextPhase,
		"next_step_id":  nextRunnableStepID,
		"should_replan": replanContext != nil,
		"artifact_dir":  artifactDir,
	})
	return nil
}

func parseStepReducerOutput(raw string) (StepWindowReducerOutput, error) {
	var zero StepWindowReducerOutput
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return zero, structuredoutput.MissingJSONObjectError("step reducer output is empty")
	}
	objects := jsonextractor.ExtractObjectsOnly(raw)
	if len(objects) == 0 {
		return zero, structuredoutput.MissingJSONObjectError("step reducer output missing json object")
	}
	var out StepWindowReducerOutput
	if err := json.Unmarshal([]byte(objects[0]), &out); err != nil {
		return zero, structuredoutput.UnmarshalFailedError(err)
	}
	out.StatusSummary = strings.TrimSpace(out.StatusSummary)
	out.WindowSummary = strings.TrimSpace(out.WindowSummary)
	out.NewFacts = normalizeStringSlice(out.NewFacts)
	out.ImportantChanges = normalizeStringSlice(out.ImportantChanges)
	out.References = normalizeStringSlice(out.References)
	out.ArtifactChanges = normalizeStringSlice(out.ArtifactChanges)
	out.OpenQuestions = normalizeStringSlice(out.OpenQuestions)
	out.NoiseRemoved = normalizeStringSlice(out.NoiseRemoved)
	return out, nil
}

func parseStepSummaryOutput(raw string) (stepSummaryModelOutput, error) {
	var zero stepSummaryModelOutput
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return zero, structuredoutput.MissingJSONObjectError("step_summary output is empty")
	}
	objects := jsonextractor.ExtractObjectsOnly(raw)
	if len(objects) == 0 {
		return zero, structuredoutput.MissingJSONObjectError("step_summary output missing json object")
	}
	var out stepSummaryModelOutput
	if err := json.Unmarshal([]byte(objects[0]), &out); err != nil {
		return zero, structuredoutput.UnmarshalFailedError(err)
	}
	out.StatusSummary = strings.TrimSpace(out.StatusSummary)
	out.StepShortSummary = strings.TrimSpace(out.StepShortSummary)
	out.StepLongSummary = strings.TrimSpace(out.StepLongSummary)
	out.KeyFacts = normalizeStringSlice(out.KeyFacts)
	out.OpenQuestions = normalizeStringSlice(out.OpenQuestions)
	out.ReplanReason = strings.TrimSpace(out.ReplanReason)
	out.NextGoal = strings.TrimSpace(out.NextGoal)
	out.MissingItems = normalizeStringSlice(out.MissingItems)
	out.Warnings = normalizeStringSlice(out.Warnings)
	out.ToolCallsDigest = normalizeStringSlice(out.ToolCallsDigest)
	return out, nil
}

func extractTopLevelJSONKeys(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	objects := jsonextractor.ExtractObjectsOnly(raw)
	if len(objects) > 0 {
		raw = objects[0]
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || len(obj) == 0 {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	return keys
}

func findStepOutcome(outcomes []*builtin_tools.StepOutcome, stepID string) *builtin_tools.StepOutcome {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return nil
	}
	for _, outcome := range outcomes {
		if outcome == nil {
			continue
		}
		if strings.TrimSpace(outcome.StepID) == stepID {
			return outcome
		}
	}
	return nil
}

func cloneStepOutcome(in *builtin_tools.StepOutcome) builtin_tools.StepOutcome {
	if in == nil {
		return builtin_tools.StepOutcome{}
	}
	out := *in
	out.References = append([]string{}, in.References...)
	out.KeyFacts = append([]string{}, in.KeyFacts...)
	out.OpenQuestions = append([]string{}, in.OpenQuestions...)
	out.ToolCallsDigest = append([]string{}, in.ToolCallsDigest...)
	return out
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

func ComputeStepWindowTokenBudget(budget ContextBudget) int {
	base := budget.UsableInputTokens
	if base <= 0 {
		base = budget.ContextWindowTokens
	}
	if base <= 0 {
		base = defaultContextWindowTokens
	}
	limit := int(float64(base) * 0.10)
	if limit <= 0 {
		return 1
	}
	return limit
}

func truncateStringByApproxTokens(text string, maxTokens int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxTokens <= 0 {
		return ""
	}
	if estimateStringTokens(text) <= maxTokens {
		return text
	}
	maxBytes := maxTokens * defaultCharsPerToken
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	var (
		usedBytes int
		runes     []rune
	)
	for _, r := range text {
		runeBytes := len(string(r))
		if usedBytes+runeBytes > maxBytes {
			break
		}
		usedBytes += runeBytes
		runes = append(runes, r)
	}
	if len(runes) == 0 {
		return strings.TrimSpace(text) + "\n...(truncated)\n"
	}
	return strings.TrimSpace(string(runes)) + "\n...(truncated)\n"
}

func BuildStepReferences(explicit []string, artifactDir string, summaryFile string, resultFile string) []string {
	refs := make([]string, 0, len(explicit)+3)
	add := func(items ...string) {
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			refs = append(refs, item)
		}
	}

	add(artifactDir, summaryFile, resultFile)
	add(explicit...)
	return normalizeStringSlice(refs)
}
