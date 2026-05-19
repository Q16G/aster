package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
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

	// Scheme A: always run the StepReplan LLM loop.
	//
	// Rationale: the old fast-path skip logic only relied on self-reported signals like
	// open_questions/warnings/unresolved, which can be under-reported and cause replan to
	// "never trigger". We intentionally trade cost for correctness here.

	stepResultPath := a.resolveStepResultPath(stepID, rawOutcome)
	stepContextsPath := a.resolveStepContextsPath()
	stepTranscriptPath := ""
	if ref := strings.TrimSpace(a.lastStepTranscriptBlobRef); ref != "" && a.v2Store != nil {
		stepTranscriptPath = a.v2Store.BlobPath(ref)
	}

	prompt, err := a.BuildStepReplanPrompt(map[string]any{
		"current_goal":         snapshot.CurrentGoal,
		"current_step":         current,
		"step_outcome":         rawOutcome,
		"task_plan":            snapshot.Plan,
		"step_outcomes":        snapshot.StepOutcomes,
		"warnings":             snapshot.Warnings,
		"unresolved":           snapshot.Unresolved,
		"step_result_path":     stepResultPath,
		"step_contexts_path":   stepContextsPath,
		"step_transcript_path": stepTranscriptPath,
	})
	if err != nil {
		return fmt.Errorf("build step replan prompt failed: %w", err)
	}

	fnTools, allowedTools := a.BuildFunctionTools(builtin_tools.AgentPhaseStepReplan)
	fnTools = append(fnTools, buildSubmitReplanFunctionTool())

	const maxSubmitRetries = 3
	submitRetries := 0

	for round := 0; ; round++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if round > 0 {
			a.stepHistory = append(a.stepHistory, ai.NewUserMsgInfo(
				fmt.Sprintf("[Round %d] 你已经进行了 %d 轮工具调查。请评估：当前收集的信息是否足够做出重规划决策？如果足够，请立即调用 submit_plan 提交决策。", round+1, round),
			))
		}

		replanCtx, replanCancel := context.WithCancel(ctx)
		callResult, err := a.AICallProxy(replanCtx, iter, runClient, prompt, fnTools...)
		replanCancel()
		if err != nil {
			return fmt.Errorf("step replan AICallProxy failed: %w", err)
		}

		// Replan 允许空响应：语义为"不需要重规划"，默认继续当前计划。
		if len(callResult.ToolCalls) == 0 {
			return a.applyReplanResult(stepID, nil, nil, snapshot, "")
		}

		anyUsefulTool := false
		for _, tc := range callResult.ToolCalls {
			if ctx.Err() != nil {
				break
			}
			if tc == nil || tc.Function == nil {
				continue
			}
			if tc.Function.Name == submitPlanToolName {
				decision, parseErr := parseSubmitReplanArgs(tc.Function.Arguments)
				if parseErr != nil {
					submitRetries++
					if submitRetries > maxSubmitRetries {
						return fmt.Errorf("submit_plan replan failed after %d retries: %w", maxSubmitRetries, parseErr)
					}
					a.AICallProxyWriteToolResult(
						strings.TrimSpace(tc.Id), submitPlanToolName,
						"", nil, "",
						fmt.Sprintf("submit_plan 参数校验失败: %s\n请修正后重新调用 submit_plan。", parseErr.Error()),
						false,
					)
					anyUsefulTool = true
					continue
				}
				return a.applyReplanDecision(stepID, decision, snapshot)
			}
			if _, ok := allowedTools[strings.TrimSpace(tc.Function.Name)]; ok {
				anyUsefulTool = true
				if err := a.executeToolCall(ctx, iter, tc, allowedTools); err != nil {
					return err
				}
			} else {
				a.AICallProxyWriteToolResult(strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name), "", nil, "", "tool not available in current phase", false)
			}
		}
		if !anyUsefulTool {
			return a.applyReplanResult(stepID, nil, nil, snapshot, "")
		}
	}
}

func (a *Agent) applyReplanDecision(stepID string, decision stepReplanModelOutput, snapshot builtin_tools.StateSnapshot) error {
	var replanContext *builtin_tools.ReplanContext
	if decision.ShouldReplan {
		nextGoal := strings.TrimSpace(decision.NextGoal)
		if nextGoal == "" {
			nextGoal = strings.TrimSpace(snapshot.CurrentGoal)
		}
		replanContext = &builtin_tools.ReplanContext{
			SourceStepID:   stepID,
			Reason:         strings.TrimSpace(decision.ReplanReason),
			NextGoal:       nextGoal,
			MissingItems:   normalizeStringSlice(decision.MissingItems),
			Warnings:       normalizeStringSlice(decision.Warnings),
			ReplacePending: true,
		}
	}
	return a.applyReplanResult(stepID, &decision, replanContext, snapshot, "")
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

	contextKey := a.resolveStepContextKey(stepID, rawOutcome, snapshot)

	snapshot = a.state.ApplyStepReplan(stepID, stepReplanUpdate{
		ArtifactDir:   artifactDir,
		ContextKey:    contextKey,
		CurrentGoal:   summaryGoal,
		Warnings:      replanWarnings,
		Unresolved:    replanMissingItems,
		ReplanContext: replanContext,
		NextPhase:     nextPhase,
	})

	a.appendStepContextRecord(stepID, snapshot)

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

func (a *Agent) resolveStepContextKey(stepID string, outcome *builtin_tools.StepOutcome, snapshot builtin_tools.StateSnapshot) string {
	if outcome != nil {
		if ck := strings.TrimSpace(outcome.ContextKey); ck != "" {
			return ck
		}
	}
	namespace := builtin_tools.NormalizeWorkspaceNamespace(a.workspaceNamespace)
	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}
	return fmt.Sprintf("%s:%d:%s", namespace, planVersion, stepID)
}

func (a *Agent) appendStepContextRecord(stepID string, snapshot builtin_tools.StateSnapshot) {
	if a.workspaceRuntime == nil {
		return
	}
	outcome := findOutcome(snapshot.StepOutcomes, stepID)
	if outcome == nil {
		return
	}
	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}
	record := &builtin_tools.StepContextRecord{
		ContextKey:        strings.TrimSpace(outcome.ContextKey),
		Namespace:         builtin_tools.NormalizeWorkspaceNamespace(a.workspaceNamespace),
		StepID:            stepID,
		PlanVersion:       planVersion,
		ShortSummary:      strings.TrimSpace(outcome.ShortSummary),
		KeyFacts:          builtin_tools.CloneStringSlice(outcome.KeyFacts),
		ToolCallsDigest:   builtin_tools.CloneStringSlice(outcome.ToolCallsDigest),
		References:        builtin_tools.CloneStringSlice(outcome.References),
		SummaryFile:       strings.TrimSpace(outcome.SummaryFile),
		ResultFile:        strings.TrimSpace(outcome.ResultFile),
		TranscriptBlobRef: a.lastStepTranscriptBlobRef,
		CreatedAt:         time.Now(),
	}
	a.lastStepTranscriptBlobRef = ""
	if err := a.workspaceRuntime.AppendStepContextRecords(
		[]*builtin_tools.StepContextRecord{record},
	); err != nil {
		a.emitRuntimeLog("warn", "append step context record failed", snapshot, map[string]any{
			"event":   "step_context_append_failed",
			"step_id": stepID,
			"error":   err.Error(),
		})
	}
}

func (a *Agent) resolveStepResultPath(stepID string, outcome *builtin_tools.StepOutcome) string {
	if a == nil || a.v2Store == nil {
		return ""
	}
	if outcome != nil {
		if aid := strings.TrimSpace(outcome.AttemptID); aid != "" {
			p, err := a.v2Store.StepAttemptResultPath(stepID, aid)
			if err == nil {
				return p
			}
		}
	}
	return ""
}

func (a *Agent) resolveStepContextsPath() string {
	if a == nil {
		return ""
	}
	return builtin_tools.WorkspaceStepContextsFileAbs(a.workspaceRootDir)
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

func buildSubmitReplanFunctionTool() *ai.FunctionTool {
	return &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        submitPlanToolName,
			Description: "当你完成评估、准备好输出重规划决策时，调用此工具提交。参数即为决策的结构化内容。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"should_replan", "replan_reason", "next_goal", "missing_items", "warnings"},
				"properties": map[string]any{
					"should_replan": map[string]any{"type": "boolean"},
					"replan_reason": map[string]any{"type": "string"},
					"next_goal":     map[string]any{"type": "string"},
					"missing_items": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
					"warnings": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

func parseSubmitReplanArgs(args any) (stepReplanModelOutput, error) {
	var data []byte
	switch v := args.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: marshal args failed: %w", err)
		}
	}
	var result stepReplanModelOutput
	if err := json.Unmarshal(data, &result); err != nil {
		return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: parse args failed: %w", err)
	}
	if result.ShouldReplan && strings.TrimSpace(result.NextGoal) == "" {
		return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: should_replan=true but next_goal is empty")
	}
	return result, nil
}
