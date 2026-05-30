package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"aster/internal/builtin_tools"
	"aster/internal/react"
	tuicontext "aster/internal/tui/context"
)

// markAgentStreamed records that the given agent produced streaming output during
// the current run. It tracks both a per-agent flag (used by the EventTypeResult
// fallback to avoid cross-agent interference) and the run-level flag (used by the
// AgentDoneMsg fallback, which is intentionally run-scoped).
func (m *Model) markAgentStreamed(agentName string) {
	if m.hadStreamByAgent == nil {
		m.hadStreamByAgent = map[string]bool{}
	}
	m.hadStreamByAgent[agentName] = true
	m.hadStreamDuringRun = true
}

func (m *Model) flushStreamAndPersist(agentName string) bool {
	content := m.chat.StreamContent(agentName)
	flushed := m.chat.FlushStream(agentName)
	if flushed && content != "" {
		m.markAgentStreamed(agentName)
		m.persistPartWithAgent("stream", "", agentName, content)
	}
	return flushed
}

// flushAllStreamsAndPersist flushes every agent's pending stream buffer. Used at
// run completion, where no further per-agent boundary events will arrive.
func (m *Model) flushAllStreamsAndPersist() bool {
	flushed := false
	for _, agentName := range m.chat.StreamingAgents() {
		if m.flushStreamAndPersist(agentName) {
			flushed = true
		}
	}
	return flushed
}

// childAgentCallToken extracts the truncated call_id token embedded in a child
// agent name. Both spawning schemes append a truncation of the spawning tool's
// call_id: sub_agent -> "sub-<callID[:8]>", skill fork -> "skill-<name>-<callID[:6]>".
// (A skill name may itself contain '-', so the token is the segment after the
// LAST '-'.) Returns "" for names that match neither scheme (e.g. the root agent).
// isTerminalSubAgentStatus reports whether a sub-agent panel card has already
// reached a final state, so a later duplicate end event (child defer vs.
// cancel-path fallback) does not overwrite the first settled status.
func isTerminalSubAgentStatus(status string) bool {
	switch status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func childAgentCallToken(agentName string) string {
	switch {
	case strings.HasPrefix(agentName, "sub-"):
		return agentName[len("sub-"):]
	case strings.HasPrefix(agentName, "skill-"):
		if i := strings.LastIndex(agentName, "-"); i >= len("skill-") {
			return agentName[i+1:]
		}
	}
	return ""
}

// subAgentDescription pulls a short human description out of a sub_agent tool's
// arguments for the collapsed card. It checks the common field names produced by
// the sub_agent / skill-fork tools and falls back to "" when none are present.
// raw is the original arguments value (may be a map); jsonStr is its marshaled
// form (used when raw was already a JSON string).
func subAgentDescription(raw any, jsonStr string) string {
	pick := func(mp map[string]any) string {
		for _, k := range []string{"description", "task", "prompt", "goal", "instruction", "instructions"} {
			if v, ok := mp[k].(string); ok {
				if s := strings.TrimSpace(v); s != "" {
					return s
				}
			}
		}
		return ""
	}
	switch v := raw.(type) {
	case map[string]any:
		if s := pick(v); s != "" {
			return s
		}
	case string:
		jsonStr = v
	}
	if jsonStr != "" {
		var mp map[string]any
		if err := json.Unmarshal([]byte(jsonStr), &mp); err == nil {
			if s := pick(mp); s != "" {
				return s
			}
		}
	}
	return ""
}

// argsRunInBackground reports whether a sub_agent tool-call argument JSON sets
// run_in_background:true.
func argsRunInBackground(jsonStr string) bool {
	if jsonStr == "" {
		return false
	}
	var mp map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &mp); err != nil {
		return false
	}
	b, _ := mp["run_in_background"].(bool)
	return b
}

func (m *Model) handleAgentEvent(event *react.AgentOutputEvent) {
	if event == nil {
		return
	}
	// Only the root agent drives global UI state (thinking panel, runtime phase,
	// status line). Sub-agent phase activity (think/replan/summary/final_answer)
	// stays attributed to its own card and must not leak into the main area.
	isRoot := m.chat.isRootAgent(event.AgentName)
	switch event.Type {
	case react.EventTypeStream,
		react.EventTypeResult,
		react.EventTypeThink,
		react.EventTypeToolStart,
		react.EventTypeToolEnd,
		react.EventTypeToolUpdate,
		react.EventTypeTaskPlan,
		react.EventTypeTaskItem,
		react.EventTypeAgentEnter,
		react.EventTypeAgentExit,
		react.EventTypeStepReplanResult,
		react.EventTypeStepSummaryResult,
		react.EventTypeFinalAnswerResult:
		m.clearRetryState()
	}

	switch event.Type {
	case react.EventTypeStream:
		m.chat.FlushThinking()
		m.chat.AppendStream(event.AgentName, event.Content)
		m.markAgentStreamed(event.AgentName)

	case react.EventTypeResult:
		m.chat.FlushThinking()
		hadStream := m.flushStreamAndPersist(event.AgentName)
		if !hadStream && !m.hadStreamByAgent[event.AgentName] {
			if result, ok := event.Payload["result"]; ok {
				resultStr := fmt.Sprintf("%v", result)
				if resultStr != "" && resultStr != "<nil>" {
					m.chat.AddPart(DisplayPart{
						Type: PartTypeText,
						Time: time.Now(),
						Text: &TextPart{Content: resultStr, AgentName: event.AgentName},
					})
					m.persistPart("result", "", resultStr)
				}
			}
		}

	case react.EventTypeToolStart:
		m.chat.FlushThinking()
		m.flushStreamAndPersist(event.AgentName)
		toolName, _ := event.Payload["tool_name"].(string)
		callID, _ := event.Payload["call_id"].(string)
		isAgent, _ := event.Payload["is_agent"].(bool)
		stackDepth := payloadInt(event.Payload, "stack_depth")
		var args string
		switch v := event.Payload["arguments"].(type) {
		case string:
			args = v
		case map[string]any:
			if b, err := json.Marshal(v); err == nil {
				args = string(b)
			}
		}
		m.chat.AddPart(DisplayPart{
			Type: PartTypeTool,
			Time: time.Now(),
			Tool: &ToolPart{
				Name:       toolName,
				CallID:     callID,
				Arguments:  args,
				State:      "running",
				IsAgent:    isAgent,
				StackDepth: stackDepth,
				AgentName:  event.AgentName,
			},
		})
		if isAgent {
			// Key spawn context by the spawning tool's call_id, so the child's
			// later (concurrent) plan events resolve their parent deterministically
			// instead of via a LIFO stack top. call_id works for both sub_agent
			// ("sub-<callID[:8]>") and skill fork ("skill-<name>-<callID[:6]>")
			// children, whose names both embed a truncation of it.
			if callID != "" {
				m.chat.agentSpawnByCallID[callID] = agentSpawnInfo{
					ParentAgent:  event.AgentName,
					ParentStepID: m.chat.activeStepByAgent[event.AgentName],
					CallID:       callID,
					SubScheme:    toolName == builtin_tools.SubAgentToolName,
				}
			}
			// A run_in_background sub_agent returns from its tool call immediately,
			// so its tool_start/tool_end collapse to ~0 and a card created here
			// would vanish at once. Its durable panel card is instead driven by the
			// EventTypeSubAgentBgStart/End bridge (keyed by agent_id), so skip
			// creating one here to avoid a duplicate flicker.
			if !argsRunInBackground(args) {
				m.chat.AddPart(DisplayPart{
					Type: PartTypeSubAgent,
					Time: time.Now(),
					SubAgent: &SubAgentPart{
						AgentName:   toolName,
						CallID:      callID,
						Status:      "running",
						Description: subAgentDescription(event.Payload["arguments"], args),
						StartedAt:   time.Now(),
					},
				})
				// The first sub-agent card makes the right-side panel appear, which
				// reflows the chat width — recompute the layout now.
				m.updateLayout()
			}
			m.statusText = fmt.Sprintf("agent: %s", toolName)
		} else {
			m.statusText = fmt.Sprintf("calling %s...", toolName)
		}
		m.persistPartWithCallIDAndAgent("tool_start", toolName, callID, event.AgentName, args)

	case react.EventTypeToolEnd:
		toolName, _ := event.Payload["tool_name"].(string)
		callID, _ := event.Payload["call_id"].(string)
		result, _ := event.Payload["result"].(string)
		errStr, _ := event.Payload["error"].(string)
		isAgent, _ := event.Payload["is_agent"].(bool)
		stackDepth := payloadInt(event.Payload, "stack_depth")
		m.chat.UpdateToolByCallID(callID, func(t *ToolPart) {
			if callID == "" && t.Name != toolName {
				return
			}
			t.Result = result
			t.Error = errStr
			t.StackDepth = stackDepth
			if errStr != "" {
				t.State = "error"
			} else {
				t.State = "completed"
			}
			t.Duration = time.Since(m.chat.partTimeByCallID(callID, toolName))
			if isAgent {
				m.parseSubAgentResult(t, result)
			}
		})
		if isAgent {
			m.updateSubAgentByCallID(callID, result, errStr)
			// A finished sub-agent drops out of the right-side panel; once the
			// last one ends the panel hides, so reflow the chat width back.
			m.updateLayout()
		}
		display := result
		if errStr != "" {
			display = "error: " + errStr
		}
		m.persistPartWithCallID("tool_end", toolName, callID, display)

	case react.EventTypeSubAgentBgStart:
		agentID, _ := event.Payload["agent_id"].(string)
		if agentID == "" {
			return
		}
		toolName, _ := event.Payload["tool_name"].(string)
		if toolName == "" {
			toolName = builtin_tools.SubAgentToolName
		}
		// Key the card by the parent launcher's call_id (same as sync sub-agents)
		// so EnterChild / partsForChild / spawn attribution all resolve. The
		// agent_id (childName) only embeds a truncation of that call_id.
		cardCallID := agentID
		if info, ok := m.chat.lookupSpawnByChild(agentID); ok {
			cardCallID = info.CallID
		}
		m.chat.AddPart(DisplayPart{
			Type: PartTypeSubAgent,
			Time: time.Now(),
			SubAgent: &SubAgentPart{
				AgentName:   toolName,
				CallID:      cardCallID,
				Status:      "running",
				Description: subAgentDescription(event.Payload["instruction"], ""),
				StartedAt:   time.Now(),
			},
		})
		// First background card makes the right-side panel appear; reflow width.
		m.updateLayout()
		m.statusText = fmt.Sprintf("agent: %s", agentID)

	case react.EventTypeSubAgentBgEnd:
		agentID, _ := event.Payload["agent_id"].(string)
		if agentID == "" {
			return
		}
		status, _ := event.Payload["status"].(string)
		summary, _ := event.Payload["summary"].(string)
		cardCallID := agentID
		if info, ok := m.chat.lookupSpawnByChild(agentID); ok {
			cardCallID = info.CallID
		}
		m.chat.UpdateSubAgentByCallID(cardCallID, func(sa *SubAgentPart) {
			// Idempotent: a card may receive both the child's own terminal event
			// (emitted from its goroutine defer) and the cancel-path fallback.
			// Only the first running->terminal flip wins; ignore later updates.
			if isTerminalSubAgentStatus(sa.Status) {
				return
			}
			if status != "" {
				sa.Status = status
			} else {
				sa.Status = "completed"
			}
			if summary != "" {
				sa.Summary = summary
			}
		})
		// A finished background sub-agent drops out of the panel; reflow width.
		m.updateLayout()

	case react.EventTypeThink:
		m.flushStreamAndPersist(event.AgentName)
		if thinkDelta, _ := event.Payload["think_content"].(string); thinkDelta != "" {
			if isRoot && (m.runtimePhase == "step_replan" || m.runtimePhase == "step_outcomes_reducer") {
				m.replanThinkBuf.WriteString(thinkDelta)
				m.thinkingPanel.UpdateLastEntry(m.runtimePhase, formatStepReplanPanelText(m.replanThinkBuf.String()))
			} else if isRoot && m.isStructuredOutputPhase() {
				m.thinkingPanel.UpdateLastEntry("thinking", "thinking...")
			} else {
				groupID := strings.TrimSpace(event.GroupID)
				// Backward compatibility: if producer doesn't set group_id, fall back to event_id.
				if groupID == "" {
					groupID = strings.TrimSpace(event.EventID)
				}
				m.chat.AppendThinkingForAgent(event.AgentName, thinkDelta, groupID)
			}
		}
		if isRoot {
			m.statusText = "thinking..."
		}

	case react.EventTypeIteration:
		m.flushStreamAndPersist(event.AgentName)
		if isRoot {
			current := payloadInt(event.Payload, "current")
			max := payloadInt(event.Payload, "max")
			iterText := fmt.Sprintf("iteration %d/%d", current, max)
			m.statusText = iterText
			m.thinkingPanel.PushEntry("iteration", iterText)
		}

	case react.EventTypeStateChange:
		if !isRoot {
			// Sub-agent phase/progress changes must not overwrite the main
			// agent's runtime panel, status line, or sidebar.
			break
		}
		m.externalInterrupt = payloadExternalInterrupt(event.Payload)
		statusSummary := strings.TrimSpace(payloadString(event.Payload, "status_summary"))
		if statusSummary != "" {
			m.statusText = statusSummary
		}
		if phase := payloadString(event.Payload, "phase"); phase != "" {
			m.runtimePhase = phase
			m.replanThinkBuf.Reset()
			phaseStatus := ""
			switch phase {
			case "step_replan":
				phaseStatus = "evaluating plan..."
			case "step_summary":
				phaseStatus = "summarizing step..."
			case "final_answer":
				phaseStatus = "composing answer..."
			case "step_outcomes_reducer":
				phaseStatus = "compressing history..."
			case "plan":
				phaseStatus = "planning..."
			case "step":
				phaseStatus = "executing step..."
			}
			switch phase {
			case "step_replan", "step_summary", "final_answer", "step_outcomes_reducer":
				m.thinkingPanel.Show(phase)
				if statusSummary != "" {
					m.thinkingPanel.PushEntry(phase, statusSummary)
				} else if phaseStatus != "" {
					m.thinkingPanel.PushEntry(phase, phaseStatus)
				}
				m.updateLayout()
			default:
				if phaseStatus != "" {
					m.thinkingPanel.PushEntry(phase, phaseStatus)
				}
				if m.thinkingPanel.visible {
					m.thinkingPanel.Hide()
					m.updateLayout()
				}
			}
			if statusSummary == "" {
				m.statusText = phaseStatus
			}
		}
		m.runtimeProgress = payloadInt(event.Payload, "progress")
		m.runtimeGoal = payloadString(event.Payload, "current_goal")
		m.runtimeWarnings = payloadStringSlice(event.Payload, "warnings")
		m.refreshSidebarData()

	case react.EventTypeRetry:
		delayMS := payloadInt64(event.Payload, "delay_ms")
		nextUnixMS := payloadInt64(event.Payload, "next_unix_ms")
		m.retryState = &retryState{
			Message:     payloadString(event.Payload, "message"),
			Attempt:     payloadInt(event.Payload, "attempt"),
			MaxAttempts: payloadInt(event.Payload, "max_attempts"),
			Next:        time.UnixMilli(nextUnixMS),
		}
		if delayMS > 0 && m.retryState.Next.IsZero() {
			m.retryState.Next = time.Now().Add(time.Duration(delayMS) * time.Millisecond)
		}

	case react.EventTypeAgentEnter:
		m.chat.FlushThinking()
		m.flushStreamAndPersist(event.AgentName)
		m.statusText = fmt.Sprintf("agent: %s", event.AgentName)

	case react.EventTypeAgentExit:
		m.chat.FlushThinking()
		m.flushStreamAndPersist(event.AgentName)
		m.statusText = "ready"

	case react.EventTypeTaskPlan:
		m.chat.FlushThinking()
		m.flushStreamAndPersist(event.AgentName)
		var items []PlanItemView
		explanation, _ := event.Payload["explanation"].(string)
		rawPlan := event.Payload["plan"]
		switch v := rawPlan.(type) {
		case []*builtin_tools.PlanItem:
			for _, item := range v {
				if item == nil {
					continue
				}
				status := string(item.Status)
				if status == "" {
					status = "pending"
				}
				items = append(items, PlanItemView{ID: item.ID, Step: item.Step, Status: status})
			}
		case []any:
			for _, item := range v {
				if itemMap, ok := item.(map[string]any); ok {
					id, _ := itemMap["id"].(string)
					step, _ := itemMap["step"].(string)
					status, _ := itemMap["status"].(string)
					if status == "" {
						status = "pending"
					}
					items = append(items, PlanItemView{ID: id, Step: step, Status: status})
				}
			}
		}
		if len(items) > 0 || explanation != "" {
			planPart := &PlanPart{
				AgentName:   event.AgentName,
				Explanation: explanation,
				Items:       items,
			}
			if !m.chat.isRootAgentPlan(planPart) {
				if _, known := m.chat.agentParent[event.AgentName]; !known {
					if info, ok := m.chat.lookupSpawnByChild(event.AgentName); ok {
						m.chat.agentParent[event.AgentName] = info
					}
				}
				if info, ok := m.chat.agentParent[event.AgentName]; ok {
					planPart.ParentStepID = info.ParentStepID
				}
			}
			m.chat.AddPart(DisplayPart{
				Type: PartTypePlan,
				Time: time.Now(),
				Plan: planPart,
			})
			planJSON, _ := json.Marshal(planPart)
			m.persistPartWithAgent("task_plan", "", event.AgentName, string(planJSON))
			m.refreshSidebarData()
		}

	case react.EventTypeTaskItem:
		itemID := payloadString(event.Payload, "id")
		step, _ := event.Payload["step"].(string)
		status := payloadString(event.Payload, "status")
		if status == "in_progress" && itemID != "" {
			m.chat.activeStepByAgent[event.AgentName] = itemID
		}
		if step != "" || itemID != "" {
			updated := false
			m.chat.UpdateLastPlanForAgent(event.AgentName, func(p *PlanPart) {
				for i := range p.Items {
					if itemID != "" && p.Items[i].ID == itemID {
						p.Items[i].Status = status
						if step != "" {
							p.Items[i].Step = step
						}
						updated = true
						return
					}
				}
				if itemID == "" {
					for i := range p.Items {
						if p.Items[i].Step == step {
							p.Items[i].Status = status
							updated = true
							return
						}
					}
				}
				p.Items = append(p.Items, PlanItemView{ID: itemID, Step: step, Status: status})
				updated = true
			})
			if !updated {
				fallbackPlan := &PlanPart{
					AgentName: event.AgentName,
					Items:     []PlanItemView{{ID: itemID, Step: step, Status: status}},
				}
				if info, ok := m.chat.agentParent[event.AgentName]; ok {
					fallbackPlan.ParentStepID = info.ParentStepID
				}
				m.chat.AddPart(DisplayPart{
					Type: PartTypePlan,
					Time: time.Now(),
					Plan: fallbackPlan,
				})
			}
			persistName := itemID
			if persistName == "" {
				persistName = step
			}
			m.persistPartWithAgent("task_item", persistName, event.AgentName, status)
			m.refreshSidebarData()
		}

	case react.EventTypeLog:
		// Runtime log events are internal diagnostics; do not surface them in
		// the user-facing footer status line.

	case react.EventTypeToolUpdate:
		updateCallID, _ := event.Payload["call_id"].(string)
		toolName, _ := event.Payload["tool_name"].(string)
		presentation := payloadString(event.Payload, "presentation")
		if presentation == "step_result" {
			part := StepResultPart{
				StepID:        payloadString(event.Payload, "step_id"),
				StepName:      payloadString(event.Payload, "step_name"),
				Status:        payloadString(event.Payload, "step_status"),
				DisplayResult: payloadString(event.Payload, "display_result"),
				Summary:       payloadString(event.Payload, "summary"),
				Error:         payloadString(event.Payload, "error"),
			}
			if part.DisplayResult != "" || part.Summary != "" || part.Error != "" {
				m.chat.AddPart(DisplayPart{
					Type:       PartTypeStepResult,
					Time:       time.Now(),
					StepResult: &part,
				})
				partJSON, _ := json.Marshal(part)
				persistName := part.StepID
				if persistName == "" {
					persistName = toolName
				}
				m.persistPart("step_result", persistName, string(partJSON))
			}
			return
		}
		msg := payloadString(event.Payload, "message")
		if msg == "" {
			msg = payloadString(event.Payload, "progress")
		}
		if msg != "" {
			m.chat.UpdateToolByCallID(updateCallID, func(t *ToolPart) {
				if t.Result == "" {
					t.Result = msg
				} else {
					t.Result += " " + msg
				}
			})
			m.persistPartWithCallID("tool_update", toolName, updateCallID, msg)
		}

	case react.EventTypeHumanRequest:
		// handled by HumanInputBridge

	case react.EventTypeStepSummaryResult:
		if isRoot {
			m.thinkingPanel.PushEntry("step_summary", "step summary completed")
			m.thinkingPanel.Hide()
			m.updateLayout()
		}
		m.chat.FlushThinking()
		m.flushStreamAndPersist(event.AgentName)
		stepID := payloadString(event.Payload, "step_id")
		stepName := payloadString(event.Payload, "step_name")
		shortSummary := payloadString(event.Payload, "short_summary")
		longSummary := payloadString(event.Payload, "long_summary")
		toolCallsDigest := payloadString(event.Payload, "tool_calls_digest")
		keyFacts := payloadStringSlice(event.Payload, "key_facts")
		openQuestions := payloadStringSlice(event.Payload, "open_questions")
		references := payloadStringSlice(event.Payload, "references")
		if shortSummary != "" || longSummary != "" {
			m.chat.AddPart(DisplayPart{
				Type: PartTypeStepSummary,
				Time: time.Now(),
				StepSummary: &StepSummaryPart{
					AgentName:       event.AgentName,
					StepID:          stepID,
					StepName:        stepName,
					ShortSummary:    shortSummary,
					LongSummary:     longSummary,
					KeyFacts:        keyFacts,
					OpenQuestions:   openQuestions,
					ToolCallsDigest: toolCallsDigest,
					References:      references,
				},
			})
			partJSON, _ := json.Marshal(StepSummaryPart{
				AgentName:       event.AgentName,
				StepID:          stepID,
				StepName:        stepName,
				ShortSummary:    shortSummary,
				LongSummary:     longSummary,
				KeyFacts:        keyFacts,
				OpenQuestions:   openQuestions,
				ToolCallsDigest: toolCallsDigest,
				References:      references,
			})
			m.persistPart("step_summary", stepID, string(partJSON))
		}

	case react.EventTypeStepReplanResult:
		stepID := payloadString(event.Payload, "step_id")
		stepName := payloadString(event.Payload, "step_name")
		shouldReplan := payloadBool(event.Payload, "should_replan")
		replanReason := payloadString(event.Payload, "replan_reason")
		nextGoal := payloadString(event.Payload, "next_goal")
		missingItems := payloadStringSlice(event.Payload, "missing_items")
		warnings := payloadStringSlice(event.Payload, "warnings")
		if isRoot {
			if shouldReplan {
				m.thinkingPanel.PushEntry("step_replan", "replan requested")
			} else {
				m.thinkingPanel.PushEntry("step_replan", "continue current plan")
			}
		}
		part := StepReplanPart{
			AgentName:    event.AgentName,
			StepID:       stepID,
			StepName:     stepName,
			ShouldReplan: shouldReplan,
			ReplanReason: replanReason,
			NextGoal:     nextGoal,
			MissingItems: missingItems,
			Warnings:     warnings,
		}
		m.chat.AddPart(DisplayPart{
			Type:       PartTypeStepReplan,
			Time:       time.Now(),
			StepReplan: &part,
		})
		partJSON, _ := json.Marshal(part)
		m.persistPart("step_replan", stepID, string(partJSON))

	case react.EventTypeFinalAnswerResult:
		if isRoot {
			m.thinkingPanel.PushEntry("final_answer", "answer delivered")
			m.thinkingPanel.Hide()
			m.updateLayout()
			m.hadFinalAnswerDuringRun = true
		}
		m.chat.FlushThinking()
		m.flushStreamAndPersist(event.AgentName)
		content := payloadString(event.Payload, "content")
		source := payloadString(event.Payload, "source")
		references := payloadStringSlice(event.Payload, "references")
		if content != "" {
			m.chat.AddPart(DisplayPart{
				Type: PartTypeFinalAnswer,
				Time: time.Now(),
				FinalAnswer: &FinalAnswerPart{
					AgentName:  event.AgentName,
					Content:    content,
					Source:     source,
					References: references,
				},
			})
			m.persistPartWithAgent("final_answer", source, event.AgentName, content)
		}

	case react.EventTypeStepFinish:
		if raw := event.Payload["usage"]; raw != nil {
			switch usageMap := raw.(type) {
			case map[string]int:
				m.sessionUsage.InputTokens += usageMap["input_tokens"]
				m.sessionUsage.OutputTokens += usageMap["output_tokens"]
				m.sessionUsage.ReasoningTokens += usageMap["reasoning_tokens"]
				m.sessionUsage.CacheReadTokens += usageMap["cache_read_tokens"]
				m.sessionUsage.CacheWriteTokens += usageMap["cache_write_tokens"]
			case map[string]any:
				m.sessionUsage.InputTokens += payloadInt(usageMap, "input_tokens")
				m.sessionUsage.OutputTokens += payloadInt(usageMap, "output_tokens")
				m.sessionUsage.ReasoningTokens += payloadInt(usageMap, "reasoning_tokens")
				m.sessionUsage.CacheReadTokens += payloadInt(usageMap, "cache_read_tokens")
				m.sessionUsage.CacheWriteTokens += payloadInt(usageMap, "cache_write_tokens")
			}
			m.sessionUsage.NormalizeInPlace()
		}
		m.sessionCost += payloadFloat64(event.Payload, "cost_usd")
		m.refreshSidebarData()

	case react.EventTypeHistoryCompacted:
		// no-op
	}
}

func (m *Model) isStructuredOutputPhase() bool {
	switch m.runtimePhase {
	case "step_replan", "step_summary", "final_answer", "step_outcomes_reducer":
		return true
	}
	return false
}

func formatStepReplanPanelText(raw string) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if text == "" {
		return "thinking..."
	}
	return truncateDisplayWidth(text, 80)
}

func payloadExternalInterrupt(payload map[string]any) *builtin_tools.ExternalInterrupt {
	if payload == nil {
		return nil
	}
	raw, ok := payload["external_interrupt"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case *builtin_tools.ExternalInterrupt:
		return builtin_tools.CloneExternalInterrupt(v)
	case map[string]any:
		info := &builtin_tools.ExternalInterrupt{
			ReasonCode:       payloadString(v, "reason_code"),
			Retryable:        payloadBool(v, "retryable"),
			Error:            payloadString(v, "error"),
			UserMessage:      payloadString(v, "user_message"),
			SuggestedActions: payloadStringSlice(v, "suggested_actions"),
		}
		if strings.TrimSpace(info.ReasonCode) == "" && strings.TrimSpace(info.UserMessage) == "" && strings.TrimSpace(info.Error) == "" {
			return nil
		}
		return info
	default:
		return nil
	}
}

func payloadBool(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	switch v := payload[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	default:
		return false
	}
}

func (m *Model) handleBatchedEvents(events []TuiEvent) {
	for _, event := range events {
		if event.Raw == nil {
			continue
		}
		switch event.Type {
		case TuiEventAgentStream:
			m.syncStore.AppendMessage(m.currentSessionID, tuicontext.MessageEntry{
				Role:    "assistant_stream",
				Content: event.Raw.Content,
			})
		case TuiEventAgentResult:
			msgs := m.syncStore.GetMessages(m.currentSessionID)
			var consolidated []tuicontext.MessageEntry
			var streamBuf strings.Builder
			for _, msg := range msgs {
				if msg.Role == "assistant_stream" {
					streamBuf.WriteString(msg.Content)
				} else {
					if streamBuf.Len() > 0 {
						consolidated = append(consolidated, tuicontext.MessageEntry{
							Role:    "assistant",
							Content: streamBuf.String(),
							Time:    msg.Time,
						})
						streamBuf.Reset()
					}
					consolidated = append(consolidated, msg)
				}
			}
			if streamBuf.Len() > 0 {
				consolidated = append(consolidated, tuicontext.MessageEntry{
					Role:    "assistant",
					Content: streamBuf.String(),
				})
			}
			m.syncStore.SetMessages(m.currentSessionID, consolidated)
		case TuiEventStateChange:
			if event.Raw.Payload != nil {
				if phase := payloadString(event.Raw.Payload, "phase"); phase != "" {
					cfg := m.syncStore.GetConfig()
					cfg.Phase = phase
					m.syncStore.SetConfig(cfg)
				}
			}
		}
	}
}

func payloadString(p map[string]any, key string) string {
	v := p[key]
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func payloadInt(p map[string]any, key string) int {
	v := p[key]
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

func payloadInt64(p map[string]any, key string) int64 {
	v := p[key]
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return int64(n)
	case int8:
		return int64(n)
	case int16:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case uint:
		return int64(n)
	case uint8:
		return int64(n)
	case uint16:
		return int64(n)
	case uint32:
		return int64(n)
	case uint64:
		return int64(n)
	case float32:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		if parsed, err := n.Int64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func payloadFloat64(p map[string]any, key string) float64 {
	v := p[key]
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		if parsed, err := n.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return parsed
		}
	}
	return 0
}

func payloadStringSlice(p map[string]any, key string) []string {
	v := p[key]
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

func (m *Model) parseSubAgentResult(t *ToolPart, result string) {
	if result == "" {
		return
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return
	}
	if name, ok := parsed["agent_name"].(string); ok {
		t.AgentName = name
	}
	if ns, ok := parsed["workspace_root"].(string); ok {
		t.WorkspaceRoot = ns
	}
	if s, ok := parsed["summary"].(string); ok {
		t.Summary = s
	}
	if s, ok := parsed["status"].(string); ok && t.State == "completed" {
		if s == "failed" {
			t.State = "error"
		}
	}
	if agentName := t.AgentName; agentName != "" {
		m.cancelSubAgentPendingItems(agentName)
	}
}

func (m *Model) cancelSubAgentPendingItems(agentName string) {
	m.chat.UpdateLastPlanForAgent(agentName, func(p *PlanPart) {
		for i := range p.Items {
			switch p.Items[i].Status {
			case "pending", "in_progress":
				p.Items[i].Status = "cancelled"
			}
		}
	})
}

func (m *Model) updateSubAgentByCallID(callID, result, errStr string) {
	m.chat.UpdateSubAgentByCallID(callID, func(sa *SubAgentPart) {
		if errStr != "" {
			sa.Status = "failed"
			sa.Summary = errStr
			return
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(result), &parsed); err != nil {
			sa.Status = "completed"
			return
		}
		if name, ok := parsed["agent_name"].(string); ok {
			sa.AgentName = name
		}
		if ns, ok := parsed["workspace_root"].(string); ok {
			sa.WorkspaceRoot = ns
		}
		if s, ok := parsed["status"].(string); ok {
			sa.Status = s
		}
		if s, ok := parsed["summary"].(string); ok {
			sa.Summary = s
		}
	})
}
