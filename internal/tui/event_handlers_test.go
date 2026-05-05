package tui

import (
	"strings"
	"testing"
	"time"

	"aster/internal/react"
)

func TestHandleAgentEventLogDoesNotOverwriteStatus(t *testing.T) {
	m := NewModel(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	m.statusText = "thinking..."

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeLog,
		Payload: map[string]any{
			"message": "simple reply path started",
		},
	})

	if m.statusText != "thinking..." {
		t.Fatalf("expected statusText to remain unchanged, got %q", m.statusText)
	}
}

func TestHandleAgentEventStateChangePrefersStatusSummary(t *testing.T) {
	m := NewModel(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	m.statusText = "thinking..."

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeStateChange,
		Payload: map[string]any{
			"phase":          "step_summary",
			"status_summary": "正在整理结果",
		},
	})

	if m.statusText != "正在整理结果" {
		t.Fatalf("expected status summary, got %q", m.statusText)
	}
}

func TestHandleAgentEventRetryUpdatesRetryState(t *testing.T) {
	m := NewModel(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	next := time.Now().Add(2 * time.Second)

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeRetry,
		Payload: map[string]any{
			"message":      "Too Many Requests",
			"attempt":      1,
			"max_attempts": 4,
			"next_unix_ms": next.UnixMilli(),
		},
	})

	if m.retryState == nil {
		t.Fatalf("expected retry state to be populated")
	}
	if m.retryState.Message != "Too Many Requests" || m.retryState.Attempt != 1 || m.retryState.MaxAttempts != 4 {
		t.Fatalf("unexpected retry state: %#v", m.retryState)
	}
}

func TestHandleAgentEventStateChangeDoesNotOverrideRetryLabel(t *testing.T) {
	m := NewModel(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeRetry,
		Payload: map[string]any{
			"message":      "Too Many Requests",
			"attempt":      1,
			"max_attempts": 4,
			"next_unix_ms": time.Now().Add(2 * time.Second).UnixMilli(),
		},
	})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeStateChange,
		Payload: map[string]any{
			"phase":          "plan",
			"status_summary": "正在规划",
		},
	})

	if m.statusText != "正在规划" {
		t.Fatalf("expected latest status summary to be preserved, got %q", m.statusText)
	}
	label := m.loadingLabel(80)
	if label == "" || label == "正在规划" {
		t.Fatalf("expected retry label to stay visible, got %q", label)
	}
	if want := "Too Many Requests"; !strings.HasPrefix(label, want) {
		t.Fatalf("expected retry label to start with %q, got %q", want, label)
	}
}
