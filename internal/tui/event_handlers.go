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

func (m *Model) flushStreamAndPersist() bool {
	content := m.chat.StreamContent()
	flushed := m.chat.FlushStream()
	if flushed && content != "" {
		m.hadStreamDuringRun = true
		m.persistPart("stream", "", content)
	}
	return flushed
}

func (m *Model) handleAgentEvent(event *react.AgentOutputEvent) {
	if event == nil {
		return
	}
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
		react.EventTypeStepSummaryResult,
		react.EventTypeFinalAnswerResult:
		m.clearRetryState()
	}

	switch event.Type {
	case react.EventTypeStream:
		m.chat.FlushThinking()
		m.chat.AppendStream(event.Content)
		m.hadStreamDuringRun = true

	case react.EventTypeResult:
		m.chat.FlushThinking()
		hadStream := m.flushStreamAndPersist()
		if !hadStream && !m.hadStreamDuringRun {
			if result, ok := event.Payload["result"]; ok {
				resultStr := fmt.Sprintf("%v", result)
				if resultStr != "" && resultStr != "<nil>" {
					m.chat.AddPart(DisplayPart{
						Type: PartTypeText,
						Time: time.Now(),
						Text: &TextPart{Content: resultStr},
					})
					m.persistPart("result", "", resultStr)
				}
			}
		}

	case react.EventTypeToolStart:
		m.chat.FlushThinking()
		m.flushStreamAndPersist()
		toolName, _ := event.Payload["tool_name"].(string)
		callID, _ := event.Payload["call_id"].(string)
		isAgent, _ := event.Payload["is_agent"].(bool)
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
				Name:      toolName,
				CallID:    callID,
				Arguments: args,
				State:     "running",
				IsAgent:   isAgent,
			},
		})
		m.statusText = fmt.Sprintf("calling %s...", toolName)
		m.persistPartWithCallID("tool_start", toolName, callID, args)

	case react.EventTypeToolEnd:
		toolName, _ := event.Payload["tool_name"].(string)
		callID, _ := event.Payload["call_id"].(string)
		result, _ := event.Payload["result"].(string)
		errStr, _ := event.Payload["error"].(string)
		m.chat.UpdateToolByCallID(callID, func(t *ToolPart) {
			if callID == "" && t.Name != toolName {
				return
			}
			t.Result = result
			t.Error = errStr
			if errStr != "" {
				t.State = "error"
			} else {
				t.State = "completed"
			}
			t.Duration = time.Since(m.chat.partTimeByCallID(callID, toolName))
		})
		display := result
		if errStr != "" {
			display = "error: " + errStr
		}
		m.persistPartWithCallID("tool_end", toolName, callID, display)

	case react.EventTypeThink:
		m.flushStreamAndPersist()
		if thinkDelta, _ := event.Payload["think_content"].(string); thinkDelta != "" {
			m.chat.AppendThinking(thinkDelta)
		}
		m.statusText = "thinking..."

	case react.EventTypeIteration:
		m.flushStreamAndPersist()
		current := payloadInt(event.Payload, "current")
		max := payloadInt(event.Payload, "max")
		m.statusText = fmt.Sprintf("iteration %d/%d", current, max)

	case react.EventTypeStateChange:
		m.externalInterrupt = payloadExternalInterrupt(event.Payload)
		if summary := strings.TrimSpace(payloadString(event.Payload, "status_summary")); summary != "" {
			m.statusText = summary
		}

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
		m.flushStreamAndPersist()
		m.statusText = fmt.Sprintf("agent: %s", event.AgentName)

	case react.EventTypeAgentExit:
		m.chat.FlushThinking()
		m.flushStreamAndPersist()
		m.statusText = "ready"

	case react.EventTypeTaskPlan:
		m.chat.FlushThinking()
		m.flushStreamAndPersist()
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
			m.chat.AddPart(DisplayPart{
				Type: PartTypePlan,
				Time: time.Now(),
				Plan: &PlanPart{
					Explanation: explanation,
					Items:       items,
				},
			})
			planJSON, _ := json.Marshal(PlanPart{Explanation: explanation, Items: items})
			m.persistPart("task_plan", "", string(planJSON))
		}

	case react.EventTypeTaskItem:
		itemID := payloadString(event.Payload, "id")
		step, _ := event.Payload["step"].(string)
		status := payloadString(event.Payload, "status")
		if step != "" || itemID != "" {
			updated := false
			m.chat.UpdateLastPlan(func(p *PlanPart) {
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
				m.chat.AddPart(DisplayPart{
					Type: PartTypePlan,
					Time: time.Now(),
					Plan: &PlanPart{
						Items: []PlanItemView{{ID: itemID, Step: step, Status: status}},
					},
				})
			}
			persistName := itemID
			if persistName == "" {
				persistName = step
			}
			m.persistPart("task_item", persistName, status)
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
		m.chat.FlushThinking()
		m.flushStreamAndPersist()
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

	case react.EventTypeFinalAnswerResult:
		m.chat.FlushThinking()
		m.flushStreamAndPersist()
		m.hadFinalAnswerDuringRun = true
		content := payloadString(event.Payload, "content")
		source := payloadString(event.Payload, "source")
		references := payloadStringSlice(event.Payload, "references")
		if content != "" {
			m.chat.AddPart(DisplayPart{
				Type: PartTypeFinalAnswer,
				Time: time.Now(),
				FinalAnswer: &FinalAnswerPart{
					Content:    content,
					Source:     source,
					References: references,
				},
			})
			m.persistPart("final_answer", source, content)
		}

	case react.EventTypeStepFinish, react.EventTypeHistoryCompacted:
		// no-op
	}
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
