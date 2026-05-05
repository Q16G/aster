package tui

import "aster/internal/react"

type TuiEventType int

const (
	TuiEventAgentStream TuiEventType = iota
	TuiEventAgentResult
	TuiEventToolStart
	TuiEventToolEnd
	TuiEventToolUpdate
	TuiEventThink
	TuiEventIteration
	TuiEventStateChange
	TuiEventRetry
	TuiEventAgentEnter
	TuiEventAgentExit
	TuiEventTaskPlan
	TuiEventTaskItem
	TuiEventLog
	TuiEventStepFinish
	TuiEventHistoryCompacted
	TuiEventHumanRequest
	TuiEventStepSummaryResult
	TuiEventFinalAnswerResult
	// UI-specific events
	TuiEventToast
	TuiEventRouteChange
	TuiEventDialogPush
	TuiEventDialogPop
)

type TuiEvent struct {
	Type    TuiEventType
	Raw     *react.AgentOutputEvent
	Payload map[string]any
}

var reactToTuiMap = map[react.EventType]TuiEventType{
	react.EventTypeStream:           TuiEventAgentStream,
	react.EventTypeResult:           TuiEventAgentResult,
	react.EventTypeToolStart:        TuiEventToolStart,
	react.EventTypeToolEnd:          TuiEventToolEnd,
	react.EventTypeToolUpdate:       TuiEventToolUpdate,
	react.EventTypeThink:            TuiEventThink,
	react.EventTypeIteration:        TuiEventIteration,
	react.EventTypeStateChange:      TuiEventStateChange,
	react.EventTypeRetry:            TuiEventRetry,
	react.EventTypeAgentEnter:       TuiEventAgentEnter,
	react.EventTypeAgentExit:        TuiEventAgentExit,
	react.EventTypeTaskPlan:         TuiEventTaskPlan,
	react.EventTypeTaskItem:         TuiEventTaskItem,
	react.EventTypeLog:              TuiEventLog,
	react.EventTypeStepFinish:       TuiEventStepFinish,
	react.EventTypeHistoryCompacted: TuiEventHistoryCompacted,
	react.EventTypeHumanRequest:         TuiEventHumanRequest,
	react.EventTypeStepSummaryResult:    TuiEventStepSummaryResult,
	react.EventTypeFinalAnswerResult:    TuiEventFinalAnswerResult,
}

func MapReactEvent(e *react.AgentOutputEvent) TuiEvent {
	t, ok := reactToTuiMap[e.Type]
	if !ok {
		t = TuiEventLog
	}
	return TuiEvent{
		Type:    t,
		Raw:     e,
		Payload: e.Payload,
	}
}
