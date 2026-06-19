package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/jsonextractor"
)

type intentClassificationModelOutput struct {
	Action string `json:"action"`
	Reason string `json:"reason"`
}

const submitIntentToolName = builtin_tools.SubmitIntentToolName

func (a *Agent) runIntentClassificationPhase(ctx context.Context, iter int, runClient ai.ChatClient) error {
	_ = a.state.SetPhase(builtin_tools.AgentPhaseIntentClassification)
	snapshot := a.state.Snapshot()
	a.emitter.EmitStateChange(snapshot)
	a.emitRuntimeLog("info", "enter intent classification phase", snapshot, map[string]any{
		"event": "phase_enter",
	})

	input := buildIntentClassificationInput(snapshot)
	input.AgentRole = strings.TrimSpace(a.cfg.Role)
	input.AgentBackground = strings.TrimSpace(a.cfg.Background)
	prompt, err := a.promptManager.BuildIntentClassificationPrompt(input)
	if err != nil {
		a.emitRuntimeLog("warn", "build intent classification prompt failed, fallback to carry", snapshot, map[string]any{
			"error": err.Error(),
		})
		return a.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "carry", Reason: "build prompt fallback"})
	}

	prompt.SystemAgent = a.identityEnvBlock()

	fnTools, allowedTools := a.BuildFunctionTools(nil, builtin_tools.AgentPhaseIntentClassification)
	fnTools = append(fnTools, buildSubmitIntentFunctionTool())

	const maxSubmitRetries = 3
	submitRetries := 0

	// 不再以 maxRounds 计数硬上限：让 intent 阶段按需充分判定（参数校验失败仍走 maxSubmitRetries
	// 退到 carry；模型 hang / ctx 取消由 fallback-on-error 分支接管）。
	for round := 0; ; round++ {
		_ = round
		if ctx != nil && ctx.Err() != nil {
			return a.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "carry", Reason: "context canceled fallback"})
		}
		callCtx, callCancel := context.WithCancel(ctx)
		callResult, callErr := a.AICallProxy(callCtx, nil, iter, runClient, prompt, promptFamilyIntentRecognition, fnTools...)
		callCancel()
		if callErr != nil {
			a.emitRuntimeLog("warn", "intent classification AICallProxy failed, fallback to carry", snapshot, map[string]any{
				"error": callErr.Error(),
			})
			return a.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "carry", Reason: "ai call fallback"})
		}

		// 无工具调用：尝试从文本兜底解析，再不行降级 carry。
		if len(callResult.ToolCalls) == 0 {
			result := parseIntentClassificationOutput(strings.TrimSpace(callResult.AssistantText))
			a.emitIntentClassified(snapshot, result)
			return a.applyIntentClassification(snapshot, result)
		}

		anyUsefulTool := false
		for _, tc := range callResult.ToolCalls {
			if ctx != nil && ctx.Err() != nil {
				break
			}
			if tc == nil || tc.Function == nil {
				continue
			}
			if tc.Function.Name == submitIntentToolName {
				parsed, parseErr := parseSubmitIntentArgs(tc.Function.Arguments)
				if parseErr != nil {
					submitRetries++
					if submitRetries > maxSubmitRetries {
						a.emitRuntimeLog("warn", "submit_intent retries exhausted, fallback to carry", snapshot, map[string]any{
							"error": parseErr.Error(),
						})
						return a.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "carry", Reason: "submit retry fallback"})
					}
					a.AICallProxyWriteToolResult(nil, 
						strings.TrimSpace(tc.Id), submitIntentToolName,
						"", nil, "",
						fmt.Sprintf("submit_intent 参数校验失败: %s\n请修正后重新调用 submit_intent。", parseErr.Error()),
						false,
					)
					anyUsefulTool = true
					continue
				}
				a.emitIntentClassified(snapshot, parsed)
				return a.applyIntentClassification(snapshot, parsed)
			}
			if _, ok := allowedTools[strings.TrimSpace(tc.Function.Name)]; ok {
				anyUsefulTool = true
				if execErr := a.executeToolCall(ctx, iter, tc, allowedTools); execErr != nil {
					a.emitRuntimeLog("warn", "intent classification tool exec failed, fallback to carry", snapshot, map[string]any{
						"tool":  strings.TrimSpace(tc.Function.Name),
						"error": execErr.Error(),
					})
					return a.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "carry", Reason: "tool exec fallback"})
				}
			} else {
				a.AICallProxyWriteToolResult(nil, strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name), "", nil, "", "tool not available in current phase", false)
			}
		}

		// 本轮只产出文本/无可用工具：文本兜底解析后收尾。
		if !anyUsefulTool {
			result := parseIntentClassificationOutput(strings.TrimSpace(callResult.AssistantText))
			a.emitIntentClassified(snapshot, result)
			return a.applyIntentClassification(snapshot, result)
		}
	}
}

func (a *Agent) emitIntentClassified(snapshot builtin_tools.StateSnapshot, result intentClassificationModelOutput) {
	a.emitRuntimeLog("info", "intent classification result", snapshot, map[string]any{
		"event":  "intent_classified",
		"action": result.Action,
		"reason": result.Reason,
	})
}

func buildIntentClassificationInput(snapshot builtin_tools.StateSnapshot) IntentClassificationPromptInput {
	input := IntentClassificationPromptInput{
		GoalUnderstanding: strings.TrimSpace(snapshot.GoalUnderstanding),
		PreviousGoal:      strings.TrimSpace(snapshot.CurrentGoal),
		Status:            string(snapshot.Status),
		HasFinalAnswer:    snapshot.FinalAnswer != nil,
		Interrupted:       snapshot.ExternalInterrupt != nil,
		LatestInput:       latestInputContent(snapshot),
	}

	for _, item := range snapshot.Plan {
		if item == nil {
			continue
		}
		input.TotalCount++
		if item.Status == builtin_tools.PlanStepCompleted {
			input.CompletedCount++
		} else {
			id := strings.TrimSpace(item.ID)
			step := strings.TrimSpace(item.Step)
			if id != "" && step != "" {
				input.PendingSteps = append(input.PendingSteps, IntentPendingStep{
					ID:   id,
					Step: step,
				})
			}
		}
	}

	for _, o := range snapshot.StepOutcomes {
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
			StepID:        strings.TrimSpace(o.StepID),
			Status:        string(o.Status),
			ShortSummary:  short,
			LongSummary:   strings.TrimSpace(o.LongSummary),
			KeyFacts:      o.KeyFacts,
			OpenQuestions: o.OpenQuestions,
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

// buildSubmitIntentFunctionTool 从 builtin_tools.SubmitIntentTool 取契约（名称 / 描述 /
// JSON-SCHEMA 参数），构造供模型调用的 function tool。
func buildSubmitIntentFunctionTool() *ai.FunctionTool {
	tool := builtin_tools.NewSubmitIntentTool()
	return &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		},
	}
}

func parseSubmitIntentArgs(args any) (intentClassificationModelOutput, error) {
	var raw string
	switch v := args.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return intentClassificationModelOutput{}, fmt.Errorf("submit_intent: marshal args failed: %w", err)
		}
		raw = string(data)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return intentClassificationModelOutput{}, fmt.Errorf("submit_intent: empty arguments")
	}

	var out intentClassificationModelOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil && isValidIntentAction(out.Action) {
		return out, nil
	}
	for _, candidate := range jsonextractor.ExtractObjectsOnly(raw) {
		if err := json.Unmarshal([]byte(candidate), &out); err == nil && isValidIntentAction(out.Action) {
			return out, nil
		}
	}
	return intentClassificationModelOutput{}, fmt.Errorf("submit_intent: invalid or missing action in %q", raw)
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
		latest := latestInputContent(snapshot)
		if reason := strings.TrimSpace(result.Reason); reason != "" {
			a.state.SetReplanContext(&builtin_tools.ReplanContext{
				Reason:         reason,
				NextGoal:       latest,
				ReplacePending: false,
				UserInitiated:  true,
			})
		}
		_ = a.state.SetPhase(builtin_tools.AgentPhasePlan)

	case "replan":
		latest := latestInputContent(snapshot)
		a.state.SetReplanContext(&builtin_tools.ReplanContext{
			Reason:         strings.TrimSpace(result.Reason),
			NextGoal:       latest,
			ReplacePending: true,
			RegenerateGoal: true,
			UserInitiated:  true,
		})
		_ = a.state.SetPhase(builtin_tools.AgentPhasePlan)

	case "cold_start":
		latest := latestInputContent(snapshot)
		a.state.Reset()
		if latest != "" {
			_ = a.state.AppendInputTimeline(latest)
		}
		// agent_execute.go 在 scheduler 前已 append history；cold_start 语义需清空重建，此处覆盖是预期行为
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
