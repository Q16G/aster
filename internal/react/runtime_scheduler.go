package react

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
	"aster/internal/runtimelog"
	"aster/internal/structuredoutput"

	"github.com/google/uuid"
)

func (a *Agent) runSchedulerLoop(ctx context.Context, runClient ai.ChatClient, extraText string, taskContext *TaskContextData, maxIterations int) (*builtin_tools.RunResult, error) {
	for iter := 1; iter <= maxIterations; iter++ {
		if ctx != nil && ctx.Err() != nil {
			snapshot := a.state.Snapshot()
			if a.v2Store != nil && errors.Is(context.Cause(ctx), ErrTurnAbortRequested) {
				if _, err := a.v2Store.AppendEvent(&persistv2.Event{
					Type:    "TURN_ABORT_REQUESTED",
					GroupID: strings.TrimSpace(a.currentGroupID),
					TurnID:  strings.TrimSpace(a.currentTurnID),
				}); err != nil {
					// Best-effort signal: the turn is already being aborted, but persistence failure
					// must still be visible to the user for diagnostics.
					a.emitRuntimeLog("error", "persistence failed: append_event", snapshot, map[string]any{
						"kind":   "persistence",
						"action": "append_event",
						"err":    err.Error(),
					})
				}
			}
			a.emitRuntimeLog("warning", "scheduler context canceled", snapshot, map[string]any{
				"event": "scheduler_context_canceled",
				"error": ctx.Err().Error(),
			})
			// 取消也统一进入 final_answer phase（final_answer 内部会避免再调模型）。
			_ = a.state.EnterFinalAnswer(builtin_tools.TaskStatusCanceled, ctx.Err().Error())
			a.syncStepHistoryLayer(a.state.Snapshot())
			snapshot, _ = a.runFinalAnswerPhase(ctx, iter, runClient)
			a.emitter.EmitIteration(iter, maxIterations, "terminal")
			return a.finalizeResult(snapshot), nil
		}

		_ = a.state.SetIteration(iter)

		snapshot := a.state.Snapshot()
		phase := currentPhase(snapshot)
		if phase != snapshot.Phase {
			_ = a.state.SetPhase(phase)
			snapshot = a.state.Snapshot()
		}
		a.syncStepHistoryLayer(snapshot)
		a.emitRuntimeLog("info", "scheduler iteration started", snapshot, map[string]any{
			"event":                "scheduler_iteration_start",
			"selected_phase":       phase,
			"input_timeline_count": len(snapshot.InputTimeline),
			"terminal":             snapshot.Terminal(),
		})
		a.emitRuntimeLog("info", "scheduler selected phase", snapshot, map[string]any{
			"event":          "phase_selected",
			"selected_phase": phase,
		})
		a.emitter.EmitIteration(iter, maxIterations, "phase:"+string(phase))

		switch phase {
		case builtin_tools.AgentPhasePlan:
			if err := a.runPlanPhase(ctx, iter, runClient, extraText, taskContext); err != nil {
				return a.handlePhaseError(ctx, err, iter, maxIterations, runClient)
			}
		case builtin_tools.AgentPhaseStep:
			if err := a.runStepPhase(ctx, iter, runClient, extraText, taskContext); err != nil {
				return a.handlePhaseError(ctx, err, iter, maxIterations, runClient)
			}
		case builtin_tools.AgentPhaseStepReplan:
			if err := a.runStepReplanPhase(ctx, iter, runClient); err != nil {
				return a.handlePhaseError(ctx, err, iter, maxIterations, runClient)
			}
		case builtin_tools.AgentPhaseFinalAnswer:
			if _, err := a.runFinalAnswerPhase(ctx, iter, runClient); err != nil {
				return nil, err
			}
		}

		snapshot = a.state.Snapshot()
		a.syncStepHistoryLayer(snapshot)
		if snapshot.Phase == builtin_tools.AgentPhaseFinalAnswer && snapshot.Terminal() {
			a.emitRuntimeLog("info", "scheduler iteration ended", snapshot, map[string]any{
				"event":                "scheduler_iteration_end",
				"next_phase":           snapshot.Phase,
				"terminal":             true,
				"will_continue":        false,
				"input_timeline_count": len(snapshot.InputTimeline),
			})
			a.emitter.EmitIteration(iter, maxIterations, "terminal")
			return a.finalizeResult(snapshot), nil
		}

		a.emitRuntimeLog("info", "scheduler iteration ended", snapshot, map[string]any{
			"event":                "scheduler_iteration_end",
			"next_phase":           snapshot.Phase,
			"terminal":             snapshot.Terminal(),
			"will_continue":        true,
			"input_timeline_count": len(snapshot.InputTimeline),
		})
		a.emitter.EmitIteration(iter, maxIterations, "iteration_end")
	}

	snapshot := a.state.Snapshot()
	a.emitRuntimeLog("warning", "scheduler max iterations reached", snapshot, map[string]any{
		"event":          "scheduler_max_iterations_reached",
		"max_iterations": maxIterations,
	})
	_ = a.state.EnterFinalAnswer(builtin_tools.TaskStatusFailed, fmt.Sprintf("reach max iterations: %d", maxIterations))
	a.syncStepHistoryLayer(a.state.Snapshot())
	snapshot, _ = a.runFinalAnswerPhase(ctx, maxIterations, runClient)
	return a.finalizeResult(snapshot), nil
}

func currentPhase(snapshot builtin_tools.StateSnapshot) builtin_tools.AgentPhase {
	switch snapshot.Phase {
	case builtin_tools.AgentPhasePlan, builtin_tools.AgentPhaseStep, builtin_tools.AgentPhaseStepReplan, builtin_tools.AgentPhaseFinalAnswer:
		return snapshot.Phase
	default:
		return builtin_tools.AgentPhasePlan
	}
}

func (a *Agent) runPlanPhase(ctx context.Context, iter int, runClient ai.ChatClient, extraText string, taskContext *TaskContextData) error {
	_ = a.state.SetPhase(builtin_tools.AgentPhasePlan)
	snapshot := a.state.Snapshot()
	a.emitter.EmitStateChange(snapshot)
	a.emitRuntimeLog("info", "enter plan phase", snapshot, map[string]any{
		"event":                "phase_enter",
		"input_timeline_count": len(snapshot.InputTimeline),
	})

	planner := a.GetTaskPlanner()
	if planner == nil {
		return fmt.Errorf("task planner not configured")
	}

	snapshot = a.state.Snapshot()
	input := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{
		UserInstruction:    strings.TrimSpace(a.cfg.Instruction),
		ExtraContext:       strings.TrimSpace(extraText),
		WorkspaceRootDir:   strings.TrimSpace(a.workspaceRootDir),
		WorkspaceNamespace: strings.TrimSpace(a.workspaceNamespace),
	})
	if input == "" {
		a.emitRuntimeLog("error", "plan phase rejected empty input timeline", snapshot, map[string]any{
			"event":                "plan_input_missing",
			"input_timeline_count": len(snapshot.InputTimeline),
		})
		return fmt.Errorf("input timeline is empty")
	}

	var res *builtin_tools.TaskPlannerResult
	if promptBuilder, ok := planner.(PlannerPromptBuilder); ok {
		planRes, err := a.runPlanPhaseWithTools(ctx, iter, runClient, input, promptBuilder)
		if err != nil {
			return err
		}
		res = planRes
	} else {
		plannerCtx := structuredoutput.WithLogger(ctx, a.structuredOutputLogger(snapshot))
		cfg := a.resolveStructuredOutputConfig(nil)
		cfg.StreamHandler = a.buildStructuredOutputStreamHandler()
		plannerCtx = structuredoutput.WithConfig(plannerCtx, cfg)
		planRes, err := planner.Plan(plannerCtx, input)
		if err != nil {
			return err
		}
		res = planRes
	}

	var items []*builtin_tools.PlanItem
	needsPlanning := false
	plannerExplanation := ""
	if res != nil {
		needsPlanning = res.NeedsPlanning
		plannerExplanation = strings.TrimSpace(res.Explanation)
	}
	explanation := plannerExplanation
	if snapshot.ReplanContext != nil {
		needsPlanning = true
		explanation = firstNonEmpty(snapshot.ReplanContext.Reason, plannerExplanation)
	}

	if res != nil && len(res.Plan) > 0 {
		planItems := res.Plan
		if snapshot.ReplanContext != nil && snapshot.ReplanContext.ReplacePending {
			planItems = mergeReplannedPlan(snapshot.Plan, planItems)
		}
		normalized, err := builtin_tools.NormalizePlanItems(planItems, true)
		if err != nil {
			return fmt.Errorf("planner returned invalid plan: %w", err)
		}
		items = normalized
	}

	if len(items) == 0 {
		directResponse := ""
		if res != nil {
			directResponse = strings.TrimSpace(res.DirectResponse)
		}
		if directResponse == "" {
			directResponse = strings.TrimSpace(explanation)
		}
		if directResponse == "" {
			directResponse = "已完成。"
		}

		snapshot = a.state.ApplyFinalAnswerPhaseUpdate(finalAnswerPhaseUpdate{
			NextPhase:          builtin_tools.AgentPhaseFinalAnswer,
			Status:             builtin_tools.TaskStatusCompleted,
			FinalAnswerContent: directResponse,
			FinalAnswerSource:  "planner_direct",
		})
		a.emitter.EmitStateChange(snapshot)
		if snapshot.FinalAnswer != nil {
			a.emitter.EmitFinalAnswerResult(snapshot.FinalAnswer)
		}

		historyText := truncateForHistory(directResponse, "planner_direct")
		a.history = append(a.history, ai.NewAIMsgInfo(historyText))
		a.notifyHistoryReplace()

		a.emitRuntimeLog("info", "planner direct response: no plan items", snapshot, map[string]any{
			"event":          "planner_direct_response",
			"needs_planning": needsPlanning,
			"explanation":    explanation,
			"content_length": len(directResponse),
		})
		return nil
	}

	snapshot = a.ApplyPlanAndEmit(ctx, items, explanation, needsPlanning)
	a.emitRuntimeLog("info", "planner applied plan", snapshot, map[string]any{
		"event":               "plan_applied",
		"plan_count":          len(items),
		"needs_planning":      needsPlanning,
		"explanation":         explanation,
		"planner_explanation": plannerExplanation,
	})
	return nil
}

const submitPlanToolName = "submit_plan"

func (a *Agent) runPlanPhaseWithTools(ctx context.Context, iter int, runClient ai.ChatClient, input string, promptBuilder PlannerPromptBuilder) (*builtin_tools.TaskPlannerResult, error) {
	prompt, err := promptBuilder.BuildPrompt(input)
	if err != nil {
		return nil, fmt.Errorf("build task planner prompt failed: %w", err)
	}

	fnTools, allowedTools := a.BuildFunctionTools(builtin_tools.AgentPhasePlan)
	fnTools = append(fnTools, buildSubmitPlanFunctionTool())

	const maxSubmitRetries = 3
	submitRetries := 0

	for round := 0; ; round++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if round > 0 {
			a.stepHistory = append(a.stepHistory, ai.NewUserMsgInfo(
				fmt.Sprintf("[Round %d] 你已经进行了 %d 轮工具调查。请评估：当前收集的信息是否足够输出一个高效的执行计划？如果足够，请立即调用 submit_plan 提交计划。", round+1, round),
			))
		}

		planCtx, planCancel := context.WithCancel(ctx)
		callResult, callErr := a.AICallProxy(planCtx, iter, runClient, prompt, fnTools...)
		planCancel()
		if callErr != nil {
			return nil, fmt.Errorf("plan phase AICallProxy failed: %w", callErr)
		}

		// Plan 阶段必须产出结果（plan 或 direct_response），空响应是硬错误。
		if len(callResult.ToolCalls) == 0 {
			return nil, fmt.Errorf("planner produced no plan and no tool calls")
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
				parsed, parseErr := parseSubmitPlanArgs(tc.Function.Arguments)
				if parseErr != nil {
					submitRetries++
					if submitRetries > maxSubmitRetries {
						return nil, fmt.Errorf("submit_plan failed after %d retries: %w", maxSubmitRetries, parseErr)
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
				if parsed.NeedsPlanning && len(parsed.Plan) > 0 {
					if _, normErr := builtin_tools.NormalizePlanItems(parsed.Plan, true); normErr != nil {
						submitRetries++
						if submitRetries > maxSubmitRetries {
							return nil, fmt.Errorf("submit_plan plan validation failed after %d retries: %w", maxSubmitRetries, normErr)
						}
						a.AICallProxyWriteToolResult(
							strings.TrimSpace(tc.Id), submitPlanToolName,
							"", nil, "",
							fmt.Sprintf("submit_plan plan 结构校验失败: %s\n请修正 depends_on 引用后重新调用 submit_plan。", normErr.Error()),
							false,
						)
						anyUsefulTool = true
						continue
					}
				}
				return parsed, nil
			}
			if _, ok := allowedTools[strings.TrimSpace(tc.Function.Name)]; ok {
				anyUsefulTool = true
				if err := a.executeToolCall(ctx, iter, tc, allowedTools); err != nil {
					return nil, err
				}
			} else {
				a.AICallProxyWriteToolResult(strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name), "", nil, "", "tool not available in current phase", false)
			}
		}
		if !anyUsefulTool {
			return nil, fmt.Errorf("planner produced no plan and no usable tool calls")
		}
	}
}

func buildSubmitPlanFunctionTool() *ai.FunctionTool {
	return &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        submitPlanToolName,
			Description: "当你完成调查、准备好输出执行计划时，调用此工具提交计划。参数即为计划的结构化内容。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"needs_planning", "plan", "explanation"},
				"properties": map[string]any{
					"needs_planning": map[string]any{"type": "boolean"},
					"plan": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"id", "step", "status", "depends_on"},
							"properties": map[string]any{
								"id":   map[string]any{"type": "string"},
								"step": map[string]any{"type": "string"},
								"status": map[string]any{
									"type": "string",
									"enum": []string{"pending", "in_progress", "completed", "failed"},
								},
								"depends_on": map[string]any{
									"type":  "array",
									"items": map[string]any{"type": "string"},
								},
							},
						},
					},
					"explanation":     map[string]any{"type": "string"},
					"direct_response": map[string]any{"type": "string"},
				},
			},
		},
	}
}

func parseSubmitPlanArgs(args any) (*builtin_tools.TaskPlannerResult, error) {
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
			return nil, fmt.Errorf("submit_plan: marshal args failed: %w", err)
		}
	}
	var result builtin_tools.TaskPlannerResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("submit_plan: parse args failed: %w", err)
	}
	if result.NeedsPlanning && len(result.Plan) == 0 {
		return nil, fmt.Errorf("submit_plan: needs_planning=true but plan is empty")
	}
	if !result.NeedsPlanning && strings.TrimSpace(result.DirectResponse) == "" {
		return nil, fmt.Errorf("submit_plan: needs_planning=false but direct_response is empty")
	}
	return &result, nil
}

type PlannerInputOptions struct {
	UserInstruction    string
	ExtraContext       string
	WorkspaceRootDir   string
	WorkspaceNamespace string
}

type plannerStepOutcomeView struct {
	StepID        string   `json:"step_id,omitempty"`
	Status        string   `json:"status,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	ShortSummary  string   `json:"short_summary,omitempty"`
	KeyFacts      []string `json:"key_facts,omitempty"`
	OpenQuestions []string `json:"open_questions,omitempty"`
	References    []string `json:"references,omitempty"`
	SummaryFile   string   `json:"summary_file,omitempty"`
	ResultFile    string   `json:"result_file,omitempty"`
	ContextKey    string   `json:"context_key,omitempty"`
}

type plannerStepContextView struct {
	ContextKey           string   `json:"context_key,omitempty"`
	Namespace            string   `json:"namespace,omitempty"`
	StepID               string   `json:"step_id,omitempty"`
	PlanVersion          int      `json:"plan_version,omitempty"`
	AgentProfile         string   `json:"agent_profile,omitempty"`
	ShortSummary         string   `json:"short_summary,omitempty"`
	KeyFacts             []string `json:"key_facts,omitempty"`
	ResultKeys           []string `json:"result_keys,omitempty"`
	SummaryFile          string   `json:"summary_file,omitempty"`
	ResultFile           string   `json:"result_file,omitempty"`
	References           []string `json:"references,omitempty"`
	InheritedContextKeys []string `json:"inherited_context_keys,omitempty"`
}

func PlannerInputFromSnapshot(snapshot builtin_tools.StateSnapshot, opts PlannerInputOptions) string {
	if len(snapshot.InputTimeline) == 0 {
		return ""
	}

	opts.UserInstruction = strings.TrimSpace(opts.UserInstruction)
	opts.ExtraContext = strings.TrimSpace(opts.ExtraContext)
	opts.WorkspaceRootDir = strings.TrimSpace(opts.WorkspaceRootDir)
	opts.WorkspaceNamespace = strings.TrimSpace(opts.WorkspaceNamespace)

	var builder strings.Builder

	if opts.UserInstruction != "" {
		builder.WriteString("<USER_INSTRUCTION>\n")
		builder.WriteString(opts.UserInstruction)
		builder.WriteString("\n</USER_INSTRUCTION>\n\n")
	}
	if opts.ExtraContext != "" {
		builder.WriteString("<HANDOFF_CONTEXT>\n")
		builder.WriteString(opts.ExtraContext)
		builder.WriteString("\n</HANDOFF_CONTEXT>\n\n")
	}

	lines := make([]string, 0, len(snapshot.InputTimeline))
	for _, item := range snapshot.InputTimeline {
		if item == nil {
			continue
		}
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		if item.CreatedAt.IsZero() {
			lines = append(lines, "- "+content)
			continue
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", item.CreatedAt.Format(time.RFC3339), content))
	}
	if len(lines) == 0 {
		return ""
	}
	builder.WriteString("<INPUT_TIMELINE>\n")
	builder.WriteString("用户输入时间线：\n")
	builder.WriteString(strings.Join(lines, "\n"))
	builder.WriteString("\n</INPUT_TIMELINE>\n")

	if len(snapshot.Plan) > 0 {
		builder.WriteString("\n\n<TASK_ITEMS>\n")
		builder.WriteString(prettyJSON(snapshot.Plan))
		builder.WriteString("\n</TASK_ITEMS>\n")
	}
	if snapshot.ReplanContext != nil {
		builder.WriteString("\n\n<REPLAN_CONTEXT>\n")
		builder.WriteString(prettyJSON(snapshot.ReplanContext))
		builder.WriteString("\n</REPLAN_CONTEXT>\n")
	}

	// Execution line: summarize the latest known outcomes without dumping raw results.
	if len(snapshot.StepOutcomes) > 0 || len(snapshot.Plan) > 0 || strings.TrimSpace(snapshot.CurrentStepID) != "" || strings.TrimSpace(snapshot.CurrentGoal) != "" {
		outcomesByID := make(map[string]*builtin_tools.StepOutcome, len(snapshot.StepOutcomes))
		for _, outcome := range snapshot.StepOutcomes {
			if outcome == nil {
				continue
			}
			stepID := strings.TrimSpace(outcome.StepID)
			if stepID == "" {
				continue
			}
			prev := outcomesByID[stepID]
			if prev == nil || outcome.UpdatedAt.After(prev.UpdatedAt) {
				outcomesByID[stepID] = outcome
			}
		}
		views := make([]plannerStepOutcomeView, 0, len(outcomesByID))
		addView := func(stepID string, outcome *builtin_tools.StepOutcome) {
			if outcome == nil {
				return
			}
			short := strings.TrimSpace(outcome.ShortSummary)
			if short == "" {
				short = strings.TrimSpace(outcome.StatusSummary)
			}
			if short == "" {
				short = strings.TrimSpace(outcome.Summary)
			}
			if short == "" {
				short = strings.TrimSpace(outcome.DisplayResult)
			}
			view := plannerStepOutcomeView{
				StepID:        strings.TrimSpace(outcome.StepID),
				Status:        strings.TrimSpace(string(outcome.Status)),
				UpdatedAt:     outcome.UpdatedAt.Format(time.RFC3339),
				ShortSummary:  short,
				KeyFacts:      cloneAndTruncateStrings(outcome.KeyFacts, 20, 240),
				OpenQuestions: cloneAndTruncateStrings(outcome.OpenQuestions, 12, 240),
				References:    builtin_tools.CloneStringSlice(outcome.References),
				SummaryFile:   strings.TrimSpace(outcome.SummaryFile),
				ResultFile:    strings.TrimSpace(outcome.ResultFile),
				ContextKey:    strings.TrimSpace(outcome.ContextKey),
			}
			if len(view.KeyFacts) == 0 {
				view.KeyFacts = nil
			}
			if len(view.OpenQuestions) == 0 {
				view.OpenQuestions = nil
			}
			if len(view.References) == 0 {
				view.References = nil
			}
			views = append(views, view)
		}

		// Stable ordering: follow current plan order first, then append remaining outcomes.
		seen := make(map[string]struct{}, len(outcomesByID))
		for _, it := range snapshot.Plan {
			if it == nil {
				continue
			}
			stepID := strings.TrimSpace(it.ID)
			if stepID == "" {
				continue
			}
			outcome := outcomesByID[stepID]
			if outcome == nil {
				continue
			}
			addView(stepID, outcome)
			seen[stepID] = struct{}{}
		}
		for stepID, outcome := range outcomesByID {
			if outcome == nil {
				continue
			}
			if _, ok := seen[stepID]; ok {
				continue
			}
			addView(stepID, outcome)
		}

		executionLine := map[string]any{
			"phase":           strings.TrimSpace(string(snapshot.Phase)),
			"status":          strings.TrimSpace(string(snapshot.Status)),
			"plan_version":    snapshot.PlanVersion,
			"progress":        snapshot.Progress,
			"needs_planning":  snapshot.NeedsPlanning,
			"current_goal":    strings.TrimSpace(snapshot.CurrentGoal),
			"current_step_id": strings.TrimSpace(snapshot.CurrentStepID),
			"warnings":        snapshot.Warnings,
			"unresolved":      snapshot.Unresolved,
			"step_outcomes":   views,
		}
		builder.WriteString("\n\n<EXECUTION_LINE>\n")
		builder.WriteString(prettyJSON(executionLine))
		builder.WriteString("\n</EXECUTION_LINE>\n")
	}

	// Include a compact execution lineage index so the planner can see child-agent/local contexts.
	if opts.WorkspaceRootDir != "" && (len(snapshot.StepOutcomes) > 0 || len(snapshot.Plan) > 0) {
		if records, err := builtin_tools.LoadWorkspaceStepContextRecords(opts.WorkspaceRootDir, 60); err == nil && len(records) > 0 {
			views := make([]plannerStepContextView, 0, len(records))
			for _, rec := range records {
				if rec == nil {
					continue
				}
				view := plannerStepContextView{
					ContextKey:           strings.TrimSpace(rec.ContextKey),
					Namespace:            strings.TrimSpace(rec.Namespace),
					StepID:               strings.TrimSpace(rec.StepID),
					PlanVersion:          rec.PlanVersion,
					AgentProfile:         strings.TrimSpace(rec.AgentProfile),
					ShortSummary:         truncateByRunes(strings.TrimSpace(rec.ShortSummary), 300),
					KeyFacts:             cloneAndTruncateStrings(rec.KeyFacts, 16, 240),
					ResultKeys:           builtin_tools.CloneStringSlice(rec.ResultKeys),
					SummaryFile:          strings.TrimSpace(rec.SummaryFile),
					ResultFile:           strings.TrimSpace(rec.ResultFile),
					References:           builtin_tools.CloneStringSlice(rec.References),
					InheritedContextKeys: builtin_tools.CloneStringSlice(rec.InheritedContextKeys),
				}
				if len(view.KeyFacts) == 0 {
					view.KeyFacts = nil
				}
				if len(view.ResultKeys) == 0 {
					view.ResultKeys = nil
				}
				if len(view.References) == 0 {
					view.References = nil
				}
				if len(view.InheritedContextKeys) == 0 {
					view.InheritedContextKeys = nil
				}
				views = append(views, view)
			}
			builder.WriteString("\n\n<WORKSPACE_STEP_CONTEXTS>\n")
			builder.WriteString(prettyJSON(map[string]any{
				"workspace_namespace": builtin_tools.NormalizeWorkspaceNamespace(opts.WorkspaceNamespace),
				"records":             views,
			}))
			builder.WriteString("\n</WORKSPACE_STEP_CONTEXTS>\n")
		}
	}

	return builder.String()
}

func mergeReplannedPlan(prev []*builtin_tools.PlanItem, next []*builtin_tools.PlanItem) []*builtin_tools.PlanItem {
	if len(prev) == 0 || len(next) == 0 {
		return next
	}
	merged := make([]*builtin_tools.PlanItem, 0, len(prev)+len(next))
	preserved := make(map[string]struct{}, len(prev))
	for _, item := range prev {
		if item == nil || item.Status != builtin_tools.PlanStepCompleted {
			continue
		}
		clone := &builtin_tools.PlanItem{
			ID:        strings.TrimSpace(item.ID),
			Step:      strings.TrimSpace(item.Step),
			Status:    item.Status,
			DependsOn: builtin_tools.CloneStringSlice(item.DependsOn),
		}
		merged = append(merged, clone)
		if clone.ID != "" {
			preserved[clone.ID] = struct{}{}
		}
	}
	for _, item := range next {
		if item == nil {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id != "" {
			if _, exists := preserved[id]; exists {
				continue
			}
		}
		merged = append(merged, item)
	}
	if len(merged) == 0 {
		return next
	}
	return merged
}

func truncateByRunes(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func cloneAndTruncateStrings(items []string, maxItems int, maxRunesPerItem int) []string {
	if len(items) == 0 || maxItems == 0 {
		return nil
	}
	if maxItems < 0 {
		maxItems = len(items)
	}
	out := make([]string, 0, min(len(items), maxItems))
	for _, it := range items {
		if len(out) >= maxItems {
			break
		}
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		out = append(out, truncateByRunes(it, maxRunesPerItem))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func normalizeRuntimeErrorText(err error) string {
	if err == nil {
		return ""
	}
	var exhausted *structuredoutput.ExhaustedError
	if errors.As(err, &exhausted) && exhausted != nil {
		if last := exhausted.LastAttempt(); last != nil && last.ErrorType == structuredoutput.ErrorTypeModelCallFailed && strings.TrimSpace(last.Error) != "" {
			return strings.TrimSpace(last.Error)
		}
	}
	return strings.TrimSpace(err.Error())
}

func (a *Agent) handlePhaseError(
	ctx context.Context,
	err error,
	iter, maxIterations int,
	runClient ai.ChatClient,
) (*builtin_tools.RunResult, error) {
	if tri, ok := isTurnInterruptRaised(err); ok {
		return &builtin_tools.RunResult{
			Success:          false,
			TurnID:           strings.TrimSpace(a.currentTurnID),
			TurnStatus:       string(persistv2.TurnStatusInterrupted),
			PendingInterrupt: tri.Pending(),
		}, nil
	}
	a.prepareTerminalInterrupt(err)
	_ = a.state.EnterFinalAnswer(builtin_tools.TaskStatusFailed, normalizeRuntimeErrorText(err))
	a.syncStepHistoryLayer(a.state.Snapshot())
	snapshot, faErr := a.runFinalAnswerPhase(ctx, iter, runClient)
	if faErr != nil {
		a.emitRuntimeLog("error", "final answer phase failed during error handling", snapshot, map[string]any{
			"event":          "final_answer_phase_error_in_fallback",
			"original_error": err.Error(),
			"final_error":    faErr.Error(),
		})
		a.emitter.EmitIteration(iter, maxIterations, "terminal")
		return nil, fmt.Errorf("phase error: %v; final_answer error: %w", err, faErr)
	}
	a.emitter.EmitIteration(iter, maxIterations, "terminal")
	return a.finalizeResult(snapshot), nil
}

func (a *Agent) prepareTerminalInterrupt(err error) {
	if a == nil || a.state == nil {
		return
	}
	if info := classifyExternalInterrupt(err); info != nil {
		_ = a.state.SetExternalInterrupt(info)
	}
}

func (a *Agent) runStepPhase(ctx context.Context, iter int, runClient ai.ChatClient, extraText string, taskContext *TaskContextData) error {
	_ = a.state.SetPhase(builtin_tools.AgentPhaseStep)
	_ = a.state.EnsureCurrentStep()
	prevSnapshot := a.state.Snapshot()
	prevPlan := builtin_tools.ClonePlanItems(prevSnapshot.Plan)
	snapshot := a.state.MarkCurrentStepInProgress()
	emitTaskItemDiffs(a.emitter, prevPlan, snapshot.Plan, snapshot.CurrentStepID, "")
	// Ensure the in-step transcript layer is bound to current_step_id before calling the model.
	// Otherwise the first tool transcript may be written while step id is empty, and then
	// cleared by the next sync transition.
	a.syncStepHistoryLayer(snapshot)

	currentStep := snapshot.CurrentStep()
	a.emitRuntimeLog("info", "enter step phase", snapshot, map[string]any{
		"event":        "phase_enter",
		"current_step": currentStep,
	})

	// Freeze execution lineage at step start (before prompt building).
	if _, err := a.ensureFrozenStepLineage(snapshot); err != nil {
		return err
	}
	prompt := a.BuildThinkActPrompt(ctx, extraText, taskContext)
	fnTools, allowedTools := a.BuildFunctionTools(builtin_tools.AgentPhaseStep)

	thinkCtx, thinkCancel := context.WithCancel(ctx)
	defer thinkCancel()
	callResult, err := a.AICallProxy(thinkCtx, iter, runClient, prompt, fnTools...)
	if err != nil {
		return err
	}

	snapshot = a.state.Snapshot()
	if shouldStopAfterCompaction(callResult.Compaction, snapshot) {
		reason := ""
		if callResult.Compaction != nil {
			reason = callResult.Compaction.TerminalReason
		}
		return &CompactionTerminatedError{
			Reason:  reason,
			Message: buildHistoryCompactionStopMessage(callResult.Compaction),
		}
	}

	// step phase 必须推进当前 step：如果模型未调用任何工具但输出了正文，
	// runtime 将其视为该 step 的最小可交付事实，自动提交 step 终态，避免空转到迭代上限。
	if callResult != nil && len(callResult.ToolCalls) == 0 {
		assistantText := strings.TrimSpace(callResult.AssistantText)
		if assistantText == "" {
			a.emitRuntimeLog("error", "step phase produced empty output", snapshot, map[string]any{
				"event":   "step_phase_empty_output_error",
				"step_id": stepIDOf(currentStep),
			})
			return fmt.Errorf("step produced no tool calls and empty content")
		}
		snapshot = a.state.Snapshot()
		current := snapshot.CurrentStep()
		if current == nil || strings.TrimSpace(current.ID) == "" {
			return fmt.Errorf("step missing current plan item")
		}
		snapshot = a.state.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
			Status:        builtin_tools.PlanStepCompleted,
			Summary:       assistantText,
			DisplayResult: assistantText,
		})
		a.emitter.EmitStateChange(snapshot)
		a.emitRuntimeLog("warning", "auto completed step from assistant content", snapshot, map[string]any{
			"event":        "auto_step_complete",
			"step_id":      strings.TrimSpace(current.ID),
			"content_size": len(assistantText),
		})
		return nil
	}

	executedToolCalls := 0
	for _, tc := range callResult.ToolCalls {
		if ctx.Err() != nil {
			break
		}
		if tc == nil || tc.Function == nil {
			continue
		}
		if err := a.executeToolCall(ctx, iter, tc, allowedTools); err != nil {
			return err
		}
		executedToolCalls++
		if currentSnapshot := a.state.Snapshot(); currentSnapshot.Terminal() {
			a.emitRuntimeLog("info", "step phase executed tool calls", currentSnapshot, map[string]any{
				"event":                "step_phase_tool_calls_executed",
				"tool_calls_requested": len(callResult.ToolCalls),
				"tool_calls_executed":  executedToolCalls,
				"will_continue":        false,
			})
			return nil
		}
	}

	snapshot = a.state.Snapshot()
	a.emitRuntimeLog("info", "step phase executed tool calls", snapshot, map[string]any{
		"event":                "step_phase_tool_calls_executed",
		"tool_calls_requested": len(callResult.ToolCalls),
		"tool_calls_executed":  executedToolCalls,
		"will_continue":        !snapshot.Terminal(),
	})
	return nil
}

func (a *Agent) executeToolCall(ctx context.Context, iter int, tc *ai.FunctionTool, allowedTools map[string]struct{}) error {
	callID := strings.TrimSpace(tc.Id)
	toolName := strings.TrimSpace(tc.Function.Name)
	if toolName == "" {
		return nil
	}
	prevSnapshot := a.state.Snapshot()
	prevPlan := builtin_tools.ClonePlanItems(prevSnapshot.Plan)
	if len(allowedTools) > 0 {
		if _, ok := allowedTools[toolName]; !ok {
			a.AICallProxyWriteToolResult(callID, toolName, "", map[string]any{}, "", "tool not available in current phase", false)
			return nil
		}
	}

	argsMap, argErr := ParseToolArguments(tc.Function.Arguments)
	if argsMap == nil {
		argsMap = map[string]any{}
	}
	if argErr != nil {
		rawArgs := ""
		if s, ok := tc.Function.Arguments.(string); ok {
			if len(s) > 500 {
				rawArgs = s[:500] + "..."
			} else {
				rawArgs = s
			}
		}
		errMsg := fmt.Sprintf("tool args parse failed: %v\n\nThe arguments JSON you provided is malformed. Raw arguments (truncated):\n%s\n\nPlease retry the tool call with valid JSON arguments.", argErr, rawArgs)
		a.AICallProxyWriteToolResult(callID, toolName, "", argsMap, "", errMsg, false)
		return nil
	}

	tool, exists := a.GetTool(toolName)
	if !exists || tool == nil {
		a.AICallProxyWriteToolResult(callID, toolName, "", argsMap, "", "tool not found", false)
		return nil
	}

	isAgent := IsAgentTool(tool)
	stackDepth := 0
	if parentToolRuntime, ok := builtin_tools.GetToolRuntime(ctx); ok {
		stackDepth = parentToolRuntime.StackDepth + 1
	}
	a.emitter.EmitToolStart(iter, builtin_tools.ToolCall{
		ID:         callID,
		Name:       toolName,
		IsAgent:    isAgent,
		StackDepth: stackDepth,
		Arguments:  builtin_tools.CloneAnyMap(argsMap),
	})

	// Durable human-in-the-loop: raise a persisted interrupt and end the current turn.
	// This must be crash-safe and resume-safe; we snapshot the runtime state + step transcript
	// before unwinding the scheduler.
	//
	// Important: We intentionally do NOT generate a tool-result message here. The outstanding
	// tool_call_id is completed only when the user resolves the interrupt.
	if toolName == builtin_tools.HumanConfirmToolName {
		question, inputType, options, ctxMap := parseHumanConfirmArgs(argsMap)
		interruptID := "interrupt-" + uuid.NewString()

		// Persistence barrier (P0 hard requirement):
		// - runtime_state + step_history MUST be durably written (as blobs)
		// - INTERRUPT_RAISED MUST be appended with blob refs in payload
		// - snapshot must be updated successfully
		// Otherwise we must NOT enter WAITING_FOR_HUMAN, because resume would be unreliable.
		if a.v2Store == nil {
			a.emitRuntimeLog("error", "persistence store missing for human_confirm", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "human_confirm_raise",
			})
			return fmt.Errorf("persistence store is not available for human_confirm")
		}

		rawState, err := json.Marshal(a.state.Snapshot())
		if err != nil || len(rawState) == 0 {
			errText := "empty runtime_state"
			if err != nil {
				errText = err.Error()
			}
			a.emitRuntimeLog("error", "marshal runtime_state failed", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "write_blob",
				"err":    errText,
			})
			return fmt.Errorf("marshal runtime_state failed: %w", err)
		}
		runtimeRef, err := a.v2Store.WriteBlob(rawState)
		if err != nil || strings.TrimSpace(runtimeRef) == "" {
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			a.emitRuntimeLog("error", "persistence failed: write_blob(runtime_state)", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "write_blob",
				"err":    errText,
			})
			return fmt.Errorf("write runtime_state blob failed: %w", err)
		}

		rawHistory, err := json.Marshal(ai.NormalizeMsgInfoSlice(a.stepHistory))
		if err != nil || len(rawHistory) == 0 {
			errText := "empty step_history"
			if err != nil {
				errText = err.Error()
			}
			a.emitRuntimeLog("error", "marshal step_history failed", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "write_blob",
				"err":    errText,
			})
			return fmt.Errorf("marshal step_history failed: %w", err)
		}
		historyRef, err := a.v2Store.WriteBlob(rawHistory)
		if err != nil || strings.TrimSpace(historyRef) == "" {
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			a.emitRuntimeLog("error", "persistence failed: write_blob(step_history)", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "write_blob",
				"err":    errText,
			})
			return fmt.Errorf("write step_history blob failed: %w", err)
		}

		var convHistoryRef string
		if len(a.history) > 0 {
			rawConvHistory, cerr := json.Marshal(ai.NormalizeMsgInfoSlice(a.history))
			if cerr != nil || len(rawConvHistory) == 0 {
				errText := "empty conversation_history"
				if cerr != nil {
					errText = cerr.Error()
				}
				a.emitRuntimeLog("error", "marshal conversation_history failed", prevSnapshot, map[string]any{
					"kind":   "persistence",
					"action": "write_blob",
					"err":    errText,
				})
				return fmt.Errorf("marshal conversation_history failed: %w", cerr)
			}
			convHistoryRef, err = a.v2Store.WriteBlob(rawConvHistory)
			if err != nil || strings.TrimSpace(convHistoryRef) == "" {
				errText := ""
				if err != nil {
					errText = err.Error()
				}
				a.emitRuntimeLog("error", "persistence failed: write_blob(conversation_history)", prevSnapshot, map[string]any{
					"kind":   "persistence",
					"action": "write_blob",
					"err":    errText,
				})
				return fmt.Errorf("write conversation_history blob failed: %w", err)
			}
		}

		payload := map[string]any{
			"question":               question,
			"input_type":             inputType,
			"options":                options,
			"context":                ctxMap,
			"tool_call_id":           callID,
			"runtime_state_blob_ref": strings.TrimSpace(runtimeRef),
			"step_history_blob_ref":  strings.TrimSpace(historyRef),
		}
		if convHistoryRef != "" {
			payload["conversation_history_blob_ref"] = strings.TrimSpace(convHistoryRef)
		}

		ev, err := a.v2Store.AppendEvent(&persistv2.Event{
			Type:        "INTERRUPT_RAISED",
			GroupID:     strings.TrimSpace(a.currentGroupID),
			TurnID:      strings.TrimSpace(a.currentTurnID),
			InterruptID: interruptID,
			Payload:     payload,
		})
		if err != nil {
			a.emitRuntimeLog("error", "persistence failed: append_event(INTERRUPT_RAISED)", prevSnapshot, map[string]any{
				"kind":         "persistence",
				"action":       "append_event",
				"event_type":   "INTERRUPT_RAISED",
				"interrupt_id": interruptID,
				"err":          err.Error(),
			})
			return fmt.Errorf("append INTERRUPT_RAISED event failed: %w", err)
		}

		snap, lerr := a.v2Store.LoadSnapshot()
		if lerr != nil {
			a.emitRuntimeLog("error", "persistence failed: load_snapshot after interrupt raised", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "load_snapshot",
				"err":    lerr.Error(),
			})
			return fmt.Errorf("load snapshot after interrupt raised failed: %w", lerr)
		}
		if snap == nil {
			return fmt.Errorf("snapshot is nil after interrupt raised")
		}
		if rerr := persistv2.ReduceSnapshot(snap, ev); rerr != nil {
			return fmt.Errorf("reduce snapshot failed: %w", rerr)
		}
		// Redundant after reducer fix — kept as defensive double-write.
		snap.RuntimeStateBlobRef = strings.TrimSpace(runtimeRef)
		snap.StepHistoryBlobRef = strings.TrimSpace(historyRef)
		if convHistoryRef != "" {
			snap.ConversationHistoryBlobRef = strings.TrimSpace(convHistoryRef)
		}
		if serr := a.v2Store.SaveSnapshotAtomic(snap); serr != nil {
			a.emitRuntimeLog("error", "persistence failed: save_snapshot after interrupt raised", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "save_snapshot",
				"err":    serr.Error(),
			})
			return fmt.Errorf("save snapshot after interrupt raised failed: %w", serr)
		}

		waitSnap := a.state.UpdateTaskStatus(builtin_tools.TaskStatusUpdate{
			Task:     "等待人工确认",
			Status:   builtin_tools.TaskStatusWaiting,
			Message:  firstNonEmpty(question, "等待人工输入"),
			Progress: -1,
		})
		a.emitter.EmitStateChange(waitSnap)

		// Mark the tool call as "waiting" in UI so it does not stay spinning forever.
		a.emitter.EmitToolEnd(iter, builtin_tools.ToolResult{
			ID:         callID,
			Name:       toolName,
			IsAgent:    isAgent,
			StackDepth: stackDepth,
			Result:     "WAITING_FOR_HUMAN",
			Error:      "",
		})

		return &turnInterruptRaised{
			pending: &builtin_tools.PendingInterrupt{
				InterruptID: interruptID,
				Question:    question,
				InputType:   inputType,
				Options:     options,
				Context:     ctxMap,
			},
			toolCall: tc,
		}
	}

	callCtx := ctx
	if isAgent {
		callCtx = WithNextAgentCallInfo(ctx, strings.TrimSpace(a.cfg.AgentID), strings.TrimSpace(a.agentName))
		a.InjectAgentToolExtra(callCtx, toolName, argsMap)
	}
	sharedDir := ""
	if a.workspaceRuntime != nil {
		sharedDir = a.workspaceRuntime.SharedDir()
	}
	callCtx = builtin_tools.WithToolRuntime(callCtx, builtin_tools.ToolRuntimeInfo{
		Emitter:            a.emitter,
		RunID:              strings.TrimSpace(a.currentRunID),
		CallID:             callID,
		ToolName:           toolName,
		Iteration:          iter,
		IsAgent:            isAgent,
		StackDepth:         stackDepth,
		WorkspaceSessionID: strings.TrimSpace(a.workspaceSessionID),
		WorkspaceRootDir:   strings.TrimSpace(a.workspaceRootDir),
		WorkspaceNamespace: strings.TrimSpace(a.workspaceNamespace),
		WorkspaceSharedDir: sharedDir,
		CurrentStepID:      strings.TrimSpace(prevSnapshot.CurrentStepID),
	})

	toolTimeout := a.cfg.resolveToolTimeout(argsMap)
	execCtx, cancelTimeout := context.WithTimeout(callCtx, toolTimeout)
	defer cancelTimeout()

	out, err := tool.Execute(execCtx, argsMap)
	if err != nil && execCtx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("tool %q timed out after %s: %w", toolName, toolTimeout, err)
	}
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	out = TruncateToolOutput(toolName, out, a.workspaceRootDir)
	errText = TruncateToolOutput(toolName+"-error", errText, a.workspaceRootDir)

	// 一些前端仅展示 result 字段而忽略 error 字段；为了避免"失败但无输出"，把错误信息也放进展示结果里。
	displayOut := out
	if strings.TrimSpace(displayOut) == "" && strings.TrimSpace(errText) != "" {
		displayOut = fmt.Sprintf("Error: %s", errText)
	}
	a.handleSkillToolStateSync(toolName, argsMap, out, errText)
	a.AICallProxyWriteToolResult(callID, toolName, tool.Description(), argsMap, displayOut, errText, isAgent)

	a.emitter.EmitToolEnd(iter, builtin_tools.ToolResult{
		ID:         callID,
		Name:       toolName,
		IsAgent:    isAgent,
		StackDepth: stackDepth,
		Result:     displayOut,
		Error:      errText,
	})

	if toolName == builtin_tools.UpdateCurrentStepToolName {
		nextSnapshot := a.state.Snapshot()
		explanation := builtin_tools.ToolRuntimeValue(argsMap["summary"])
		if explanation == "" {
			explanation = builtin_tools.ToolRuntimeValue(argsMap["display_result"])
		}
		emitTaskItemDiffs(a.emitter, prevPlan, nextSnapshot.Plan, nextSnapshot.CurrentStepID, explanation)

		stepID := strings.TrimSpace(prevSnapshot.CurrentStepID)
		stepName := ""
		if current := prevSnapshot.CurrentStep(); current != nil {
			if stepID == "" {
				stepID = strings.TrimSpace(current.ID)
			}
			stepName = strings.TrimSpace(current.Step)
		}
		outcome := findOutcome(nextSnapshot.StepOutcomes, stepID)
		status := builtin_tools.PlanStepStatus(builtin_tools.ToolRuntimeValue(argsMap["status"]))
		a.writeV2StepAttemptResult(stepID, stepName, callID, status, outcome)
		a.state.SetStepOutcomeAttemptID(stepID, callID)
	}
	return nil
}

func (a *Agent) emitRuntimeLog(level string, message string, snapshot builtin_tools.StateSnapshot, extra map[string]any) {
	payload := builtin_tools.CloneAnyMap(extra)
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["level"] = strings.TrimSpace(level)
	payload["message"] = strings.TrimSpace(message)
	payload["phase"] = snapshot.Phase
	payload["status"] = snapshot.Status
	payload["iteration"] = snapshot.Iteration
	payload["progress"] = snapshot.Progress
	payload["current_step_id"] = strings.TrimSpace(snapshot.CurrentStepID)
	if currentStep := snapshot.CurrentStep(); currentStep != nil {
		payload["current_step"] = currentStep
	}
	if latestInput := snapshot.LatestInput(); latestInput != nil {
		payload["latest_input"] = latestInput
	}
	if strings.TrimSpace(snapshot.StatusSummary) != "" {
		payload["status_summary"] = strings.TrimSpace(snapshot.StatusSummary)
	}
	if strings.TrimSpace(snapshot.Error) != "" {
		payload["state_error"] = strings.TrimSpace(snapshot.Error)
	}
	runtimelog.LogJSON(level, payload)
	if a == nil || a.emitter == nil {
		return
	}
	a.emitter.EmitLogPayload(payload)
}

func stepIDOf(step *builtin_tools.PlanItem) string {
	if step == nil {
		return ""
	}
	return strings.TrimSpace(step.ID)
}

// blockingStepFailure 已由 step_summary -> final_answer 的统一链路覆盖；本次重构不再提前截断。
