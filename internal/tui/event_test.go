package tui

import (
	"aster/internal/react"
	"testing"
)

func TestMapReactEvent_AllTypes(t *testing.T) {
	tests := []struct {
		reactType react.EventType
		expected  TuiEventType
	}{
		{react.EventTypeStream, TuiEventAgentStream},
		{react.EventTypeResult, TuiEventAgentResult},
		{react.EventTypeToolStart, TuiEventToolStart},
		{react.EventTypeToolEnd, TuiEventToolEnd},
		{react.EventTypeToolUpdate, TuiEventToolUpdate},
		{react.EventTypeThink, TuiEventThink},
		{react.EventTypeIteration, TuiEventIteration},
		{react.EventTypeStateChange, TuiEventStateChange},
		{react.EventTypeRetry, TuiEventRetry},
		{react.EventTypeAgentEnter, TuiEventAgentEnter},
		{react.EventTypeAgentExit, TuiEventAgentExit},
		{react.EventTypeTaskPlan, TuiEventTaskPlan},
		{react.EventTypeTaskItem, TuiEventTaskItem},
		{react.EventTypeLog, TuiEventLog},
		{react.EventTypeStepFinish, TuiEventStepFinish},
		{react.EventTypeHistoryCompacted, TuiEventHistoryCompacted},
		{react.EventTypeHumanRequest, TuiEventHumanRequest},
	}

	for _, tt := range tests {
		e := &react.AgentOutputEvent{
			Type:    tt.reactType,
			Payload: map[string]any{"key": "value"},
		}
		result := MapReactEvent(e)
		if result.Type != tt.expected {
			t.Errorf("MapReactEvent(%s): got %d, want %d", tt.reactType, result.Type, tt.expected)
		}
		if result.Raw != e {
			t.Errorf("MapReactEvent(%s): Raw not preserved", tt.reactType)
		}
		if result.Payload["key"] != "value" {
			t.Errorf("MapReactEvent(%s): Payload not preserved", tt.reactType)
		}
	}
}

func TestMapReactEvent_UnknownType(t *testing.T) {
	e := &react.AgentOutputEvent{
		Type: react.EventType("unknown_type"),
	}
	result := MapReactEvent(e)
	if result.Type != TuiEventLog {
		t.Errorf("unknown type should map to TuiEventLog, got %d", result.Type)
	}
}
