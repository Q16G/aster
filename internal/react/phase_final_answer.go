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
	IsComplete   bool   `json:"is_complete"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
	ShouldReplan bool   `json:"should_replan"`
	NextGoal     string `json:"next_goal"`
	// IncompleteItems 轴①存在性/完成度：当前诉求范围内、根本没做的项。
	IncompleteItems []string `json:"incomplete_items"`
	// DepthGaps 轴②深度/质量：跨 step 来看做了但不扎实的项（shallow_only 未深度确认、分析链条断裂、悬而未决、低价值项占位、抽样冒充全量）。
	DepthGaps []string `json:"depth_gaps"`
	// NewSurfaces 轴③泛化：对照整体诉求全集、尚未被任何已完成工作覆盖的面（聚焦约束下方向外的新面填此字段但不单独驱动 replan）。
	NewSurfaces []string `json:"new_surfaces"`
	Warnings    []string `json:"warnings"`
	UserMessage string   `json:"user_message"`
	References  []string `json:"references"`
}

func (a *Agent) runFinalAnswerPhase(ctx context.Context, iter int, runClient ai.ChatClient) (builtin_tools.StateSnapshot, error) {
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
	workspaceSharedDir := ""
	if a.workspaceRuntime != nil {
		sharedDir := a.workspaceRuntime.SharedDir()
		workspaceSharedDir = strings.TrimSpace(sharedDir)
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
		"goal_understanding": snapshot.GoalUnderstanding,
		"needs_planning":     snapshot.NeedsPlanning,
		// plan/plan_version/step_outcomes 保留供 assessed_state 持久化（resume 回退源）；
		// prompt 注入改走 plan_items 卡片 + 账本全文（CARRIED_* 已由账本吸收）。
		"plan":                 snapshot.Plan,
		"plan_version":         snapshot.PlanVersion,
		"step_outcomes":        stepOutcomeViews,
		"plan_items":           ProjectPlanItemCards(snapshot.Plan, a.workspaceRootDir),
		"open_items_ledger":    readSharedFileForPrompt(workspaceSharedDir, openItemsFileName),
		"external_interrupt":   externalInterrupt,
		"replan_context":       snapshot.ReplanContext,
		"active_skill_names":   snapshot.ActiveSkillNames,
		"warnings":             snapshot.Warnings,
		"workspace_shared_dir": workspaceSharedDir,
	}

	var modelOut FinalAnswerModelOutput
	rawResponse := ""
	if externalInterrupt != nil {
		a.emitRuntimeLog("warning", "final answer model bypassed due to external interrupt", snapshot, map[string]any{
			"event":                  "final_answer_model_bypassed",
			"reason_code":            strings.TrimSpace(externalInterrupt.ReasonCode),
			"retryable":              externalInterrupt.Retryable,
			"warnings_count":         len(snapshot.Warnings),
			"incomplete_items_count": len(carriedAxisItems(snapshot.UnresolvedAxes, axisIncomplete)),
			"depth_gaps_count":       len(carriedAxisItems(snapshot.UnresolvedAxes, axisDepth)),
			"new_surfaces_count":     len(carriedAxisItems(snapshot.UnresolvedAxes, axisNewSurfaces)),
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
				Warnings:     nil,
				UserMessage:  firstNonEmpty(strings.TrimSpace(errText), "任务已取消。"),
			}
		} else {
			a.emitRuntimeLog("info", "final answer model started", snapshot, map[string]any{
				"event":                  "final_answer_model_started",
				"plan_version":           snapshot.PlanVersion,
				"step_outcomes_count":    len(snapshot.StepOutcomes),
				"warnings_count":         len(snapshot.Warnings),
				"incomplete_items_count": len(carriedAxisItems(snapshot.UnresolvedAxes, axisIncomplete)),
				"depth_gaps_count":       len(carriedAxisItems(snapshot.UnresolvedAxes, axisDepth)),
				"new_surfaces_count":     len(carriedAxisItems(snapshot.UnresolvedAxes, axisNewSurfaces)),
			})
			runtimelog.LogJSON("info", map[string]any{
				"event":              "final_answer_model_request",
				"phase":              "final_answer",
				"raw_request_length": len(prompt),
			})

			fnTools, allowedTools := a.BuildFunctionTools(builtin_tools.AgentPhaseFinalAnswer)
			fnTools = append(fnTools, buildSubmitFinalAnswerFunctionTool())

			const maxSubmitRetries = 3
			submitRetries := 0
			gotModelOut := false
			fallbackMode := ""

			for round := 0; ; round++ {
				if ctx != nil && ctx.Err() != nil {
					return snapshot, ctx.Err()
				}
				faCtx, faCancel := context.WithCancel(ctx)
				callResult, callErr := a.AICallProxy(faCtx, iter, runClient, prompt, "", fnTools...)
				faCancel()
				if callErr != nil {
					return snapshot, fmt.Errorf("final_answer AICallProxy failed: %w", callErr)
				}

				// 硬上限：降级提示后仍不提交，按纯文本兜底收尾，避免判定节点失控空转。
				if round >= judgmentExplorationBudget+judgmentGraceRounds {
					a.emitRuntimeLog("warn", "final answer exploration budget exhausted", snapshot, map[string]any{
						"event":  "final_answer_exploration_budget_exhausted",
						"rounds": round,
					})
					modelOut = finalAnswerPlaintextFallback(callResult.AssistantText)
					rawResponse = strings.TrimSpace(callResult.AssistantText)
					fallbackMode = "exploration_budget_exhausted"
					gotModelOut = true
					break
				}

				// 空响应：plaintext 兜底（不 return，必须落到 L189 后处理产出可交付终报）。
				if len(callResult.ToolCalls) == 0 {
					modelOut = finalAnswerPlaintextFallback(callResult.AssistantText)
					rawResponse = strings.TrimSpace(callResult.AssistantText)
					fallbackMode = "fallback_text"
					gotModelOut = true
					break
				}

				anyUsefulTool := false
				for _, tc := range callResult.ToolCalls {
					if ctx != nil && ctx.Err() != nil {
						break
					}
					if tc == nil || tc.Function == nil {
						continue
					}
					if tc.Function.Name == submitFinalAnswerToolName {
						parsed, parseErr := parseSubmitFinalAnswerArgs(tc.Function.Arguments)
						if parseErr != nil {
							submitRetries++
							if submitRetries > maxSubmitRetries {
								return snapshot, fmt.Errorf("submit_final_answer failed after %d retries: %w", maxSubmitRetries, parseErr)
							}
							a.AICallProxyWriteToolResult(
								strings.TrimSpace(tc.Id), submitFinalAnswerToolName,
								"", nil, "",
								fmt.Sprintf("submit_final_answer 参数校验失败: %s\n请修正后重新调用 submit_final_answer。", parseErr.Error()),
								false,
							)
							anyUsefulTool = true
							continue
						}
						modelOut = parsed
						gotModelOut = true
						break
					}
					if _, ok := allowedTools[strings.TrimSpace(tc.Function.Name)]; ok {
						anyUsefulTool = true
						// 探索预算（设计 6.3）：超出后拒绝继续取证，要求基于已有信息降级裁决。
						if round >= judgmentExplorationBudget {
							a.AICallProxyWriteToolResult(
								strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name),
								"", nil, "", finalAnswerBudgetNotice, false,
							)
							continue
						}
						if err := a.executeToolCall(ctx, iter, tc, allowedTools); err != nil {
							return snapshot, err
						}
					} else {
						a.AICallProxyWriteToolResult(strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name), "", nil, "", "tool not available in current phase", false)
					}
				}
				if gotModelOut {
					break
				}
				// 只产出文本/无可用工具：plaintext 兜底。
				if !anyUsefulTool {
					modelOut = finalAnswerPlaintextFallback(callResult.AssistantText)
					rawResponse = strings.TrimSpace(callResult.AssistantText)
					fallbackMode = "fallback_text"
					gotModelOut = true
					break
				}
			}

			logMode := "submit"
			if fallbackMode != "" {
				logMode = fallbackMode
				a.emitRuntimeLog("warning", "final answer fell back to plain text", snapshot, map[string]any{
					"event":           "final_answer_model_fallback_text",
					"mode":            fallbackMode,
					"response_length": len(rawResponse),
				})
			}
			runtimelog.LogJSON("info", map[string]any{
				"event":               "final_answer_model_raw_response",
				"phase":               "final_answer",
				"mode":                logMode,
				"raw_response_length": len(rawResponse),
			})
		}
	}

	decision := normalizeFinalAnswerDecision(modelOut)
	runtimeForcedFail := snapshot.Status == builtin_tools.TaskStatusFailed && strings.TrimSpace(errText) != ""
	if runtimeForcedFail && externalInterrupt == nil && decision.status == builtin_tools.TaskStatusCompleted {
		decision.status = builtin_tools.TaskStatusFailed
		decision.model.Status = string(builtin_tools.TaskStatusFailed)
		decision.model.Reason = firstNonEmpty(strings.TrimSpace(errText), decision.model.Reason)
		if strings.TrimSpace(decision.model.UserMessage) == "" || strings.TrimSpace(decision.model.UserMessage) == "任务已完成。" {
			decision.model.UserMessage = firstNonEmpty(strings.TrimSpace(errText), decision.model.UserMessage)
		}
	}
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
		incompleteItems := builtin_tools.NewAxisItems(normalizeStringSlice(decision.model.IncompleteItems))
		depthGaps := builtin_tools.NewAxisItems(normalizeStringSlice(decision.model.DepthGaps))
		newSurfaces := builtin_tools.NewAxisItems(normalizeStringSlice(decision.model.NewSurfaces))
		snapshot = a.state.ApplyFinalAnswerPhaseUpdate(finalAnswerPhaseUpdate{
			NextPhase:     builtin_tools.AgentPhasePlan,
			Status:        builtin_tools.TaskStatusRunning,
			StatusSummary: firstNonEmpty(strings.TrimSpace(decision.model.Reason), "任务未完成，回流 plan 继续规划。"),
			NextGoal:      nextGoal,
			Warnings:      decision.model.Warnings,
			UnresolvedAxes: &builtin_tools.ReplanAxes{
				IncompleteItems: incompleteItems,
				DepthGaps:       depthGaps,
				NewSurfaces:     newSurfaces,
			},
			ReplanContext: &builtin_tools.ReplanContext{
				Reason:          strings.TrimSpace(decision.model.Reason),
				NextGoal:        nextGoal,
				IncompleteItems: incompleteItems,
				DepthGaps:       depthGaps,
				NewSurfaces:     newSurfaces,
				Warnings:        builtin_tools.CloneStringSlice(decision.model.Warnings),
				ReplacePending:  true,
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
			"event":                  "final_assessment_replan",
			"next_goal":              nextGoal,
			"incomplete_items_count": len(incompleteItems),
			"depth_gaps_count":       len(depthGaps),
			"new_surfaces_count":     len(newSurfaces),
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
		UnresolvedAxes:        &builtin_tools.ReplanAxes{},
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

const submitFinalAnswerToolName = builtin_tools.SubmitFinalAnswerToolName

const finalAnswerBudgetNotice = "取证轮次已达上限：不要再调用只读工具。" +
	"立即基于已有信息调用 submit_final_answer；无法核验的诉求项归置为「仅初步」并写明未核验原因。"

// finalAnswerPlaintextFallback 在模型未通过 submit_final_answer 提交结构化决策时，
// 把其纯文本输出兜底为一个可交付的 completed 终报，保留旧路径「fallback-to-plaintext」语义。
func finalAnswerPlaintextFallback(text string) FinalAnswerModelOutput {
	msg := strings.TrimSpace(text)
	if msg == "" {
		msg = "任务已完成。"
	}
	return FinalAnswerModelOutput{
		IsComplete:   true,
		Status:       string(builtin_tools.TaskStatusCompleted),
		Reason:       "模型未通过 submit_final_answer 提交结构化决策，已回退为直接交付文本。",
		ShouldReplan: false,
		NextGoal:     "",
		UserMessage:  msg,
		References:   []string{},
	}
}

// buildSubmitFinalAnswerFunctionTool 从 builtin_tools.SubmitFinalAnswerTool 取契约
// （名称 / 功能描述 / JSON-SCHEMA 参数），构造供模型调用的 function tool。
func buildSubmitFinalAnswerFunctionTool() *ai.FunctionTool {
	tool := builtin_tools.NewSubmitFinalAnswerTool()
	return &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		},
	}
}

func parseSubmitFinalAnswerArgs(args any) (FinalAnswerModelOutput, error) {
	var raw string
	switch v := args.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return FinalAnswerModelOutput{}, fmt.Errorf("submit_final_answer: marshal args failed: %w", err)
		}
		raw = string(data)
	}
	return parseFinalAnswerOutput(raw)
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
		"incomplete_items",
		"depth_gaps",
		"new_surfaces",
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
	modelOut.IncompleteItems = normalizeReferences(modelOut.IncompleteItems)
	modelOut.DepthGaps = normalizeReferences(modelOut.DepthGaps)
	modelOut.NewSurfaces = normalizeReferences(modelOut.NewSurfaces)
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
