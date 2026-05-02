package tui

import (
	"fmt"
	"strings"

	"aster/internal/react"
	tuicontext "aster/internal/tui/context"
)

func (m *Model) handleAgentEvent(event *react.AgentOutputEvent) {
	if event == nil {
		return
	}

	switch event.Type {
	case react.EventTypeStream:
		m.chat.AppendStream(event.Content)

	case react.EventTypeResult:
		hadStream := m.chat.FlushStream()
		if !hadStream {
			if result, ok := event.Payload["result"]; ok {
				resultStr := fmt.Sprintf("%v", result)
				if resultStr != "" && resultStr != "<nil>" {
					m.chat.AddMessage(ChatMessage{Role: "assistant", Content: resultStr})
				}
			}
		}

	case react.EventTypeToolStart:
		toolName, _ := event.Payload["tool_name"].(string)
		args, _ := event.Payload["arguments"].(string)
		m.chat.AddMessage(ChatMessage{
			Role:    "tool",
			Content: toolMessageContent(toolName, args, "running..."),
		})
		m.statusText = fmt.Sprintf("calling %s...", toolName)
		m.persistPart("tool_start", toolName, args)

	case react.EventTypeToolEnd:
		toolName, _ := event.Payload["tool_name"].(string)
		result, _ := event.Payload["result"].(string)
		errStr, _ := event.Payload["error"].(string)
		display := result
		if errStr != "" {
			display = "error: " + errStr
		}
		if len(m.chat.messages) > 0 {
			last := &m.chat.messages[len(m.chat.messages)-1]
			if last.Role == "tool" {
				last.Content = toolMessageContent(toolName, "", display)
				m.chat.refreshContent()
			}
		}
		m.persistPart("tool_end", toolName, display)

	case react.EventTypeThink:
		m.statusText = "thinking..."

	case react.EventTypeIteration:
		current, _ := event.Payload["current"].(float64)
		max, _ := event.Payload["max"].(float64)
		m.statusText = fmt.Sprintf("iteration %d/%d", int(current), int(max))

	case react.EventTypeStateChange:
		if phase, ok := event.Payload["phase"].(string); ok {
			m.statusText = phase
		}

	case react.EventTypeAgentEnter:
		m.statusText = fmt.Sprintf("agent: %s", event.AgentName)

	case react.EventTypeAgentExit:
		m.statusText = "ready"

	case react.EventTypeTaskPlan:
		var planLines []string
		if explanation, ok := event.Payload["explanation"].(string); ok && explanation != "" {
			planLines = append(planLines, explanation)
		}
		if items, ok := event.Payload["items"].([]any); ok {
			for _, item := range items {
				if itemMap, ok := item.(map[string]any); ok {
					step, _ := itemMap["step"].(string)
					status, _ := itemMap["status"].(string)
					if status == "" {
						status = "pending"
					}
					planLines = append(planLines, fmt.Sprintf("[%s] %s", status, step))
				}
			}
		}
		if len(planLines) > 0 {
			m.chat.AddMessage(ChatMessage{Role: "plan", Content: strings.Join(planLines, "\n")})
		} else if explanation, ok := event.Payload["explanation"].(string); ok && explanation != "" {
			m.chat.AddMessage(ChatMessage{Role: "plan", Content: explanation})
		}

	case react.EventTypeTaskItem:
		step, _ := event.Payload["step"].(string)
		status, _ := event.Payload["status"].(string)
		if step != "" {
			if len(m.chat.messages) > 0 {
				last := &m.chat.messages[len(m.chat.messages)-1]
				if last.Role == "plan" {
					last.Content = updatePlanItem(last.Content, step, status)
					m.chat.refreshContent()
					return
				}
			}
			m.chat.AddMessage(ChatMessage{Role: "plan", Content: fmt.Sprintf("[%s] %s", status, step)})
		}

	case react.EventTypeLog:
		if message, ok := event.Payload["message"].(string); ok {
			m.statusText = message
		}

	case react.EventTypeToolUpdate:
		if len(m.chat.messages) > 0 {
			last := &m.chat.messages[len(m.chat.messages)-1]
			if last.Role == "tool" {
				if progress, ok := event.Payload["progress"].(string); ok {
					last.Content += " " + progress
					m.chat.refreshContent()
				}
			}
		}

	case react.EventTypeHumanRequest:
		// handled by HumanInputBridge

	case react.EventTypeStepFinish, react.EventTypeHistoryCompacted:
		// no-op
	}
}

func updatePlanItem(planContent, step, status string) string {
	lines := strings.Split(planContent, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range []string{"[pending] ", "[in_progress] ", "[completed] ", "[failed] "} {
			if strings.HasPrefix(trimmed, prefix) {
				remaining := strings.TrimPrefix(trimmed, prefix)
				if remaining == step {
					lines[i] = fmt.Sprintf("[%s] %s", status, step)
					return strings.Join(lines, "\n")
				}
			}
		}
	}
	return planContent + "\n" + fmt.Sprintf("[%s] %s", status, step)
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
				if phase, ok := event.Raw.Payload["phase"].(string); ok {
					cfg := m.syncStore.GetConfig()
					cfg.Phase = phase
					m.syncStore.SetConfig(cfg)
				}
			}
		}
	}
}
