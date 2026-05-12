package persistv2

import (
	"fmt"
	"strings"
	"time"
)

// ReduceSnapshot applies an event to a snapshot in-place.
//
// This reducer is intentionally minimal; higher-level runtime state remains in
// the engine. The snapshot here is mainly for session/turn/interrupt visibility
// and recovery UX.
func ReduceSnapshot(snap *Snapshot, ev *Event) error {
	if snap == nil || ev == nil {
		return nil
	}
	if snap.System != nil && snap.System.Degraded {
		// preserve degraded state
	} else if ev != nil && strings.TrimSpace(ev.SessionID) != "" {
		// no-op
	}

	if snap.FormatVersion == 0 {
		snap.FormatVersion = FormatVersion
	}
	if strings.TrimSpace(snap.SessionID) == "" && strings.TrimSpace(ev.SessionID) != "" {
		snap.SessionID = strings.TrimSpace(ev.SessionID)
	}

	switch strings.TrimSpace(ev.Type) {
	case "SESSION_CREATED":
		snap.SessionState = SessionStateIdle

	case "TURN_STARTED":
		snap.SessionState = SessionStateBusy
		snap.PendingInterrupt = nil
		snap.CurrentTurn = &Turn{
			TurnID:    strings.TrimSpace(ev.TurnID),
			GroupID:   strings.TrimSpace(ev.GroupID),
			Status:    TurnStatusRunning,
			Input:     payloadText(ev.Payload, "input"),
			StartedAt: ev.TimeUnixMs,
		}

	case "TURN_FINISHED":
		if snap.CurrentTurn == nil || strings.TrimSpace(snap.CurrentTurn.TurnID) != strings.TrimSpace(ev.TurnID) {
			// Ignore out-of-order finalize events.
			break
		}
		status := TurnStatus(payloadText(ev.Payload, "status"))
		switch status {
		case TurnStatusSucceeded, TurnStatusFailed, TurnStatusCancelled, TurnStatusInterrupted:
		default:
			status = TurnStatusSucceeded
		}
		snap.CurrentTurn.Status = status
		snap.CurrentTurn.Error = payloadText(ev.Payload, "error")
		snap.CurrentTurn.FinishedAt = ev.TimeUnixMs
		switch status {
		case TurnStatusInterrupted:
			snap.SessionState = SessionStateWaitingForHuman
		default:
			snap.SessionState = SessionStateIdle
		}

	case "TURN_ABORT_REQUESTED":
		// Does not change session state by itself; runtime will decide.

	case "INTERRUPT_RAISED":
		snap.SessionState = SessionStateWaitingForHuman
		snap.PendingInterrupt = &PendingInterrupt{
			InterruptID: strings.TrimSpace(ev.InterruptID),
			TurnID:      strings.TrimSpace(ev.TurnID),
			Question:    payloadText(ev.Payload, "question"),
			InputType:   payloadText(ev.Payload, "input_type"),
			Options:     payloadStringSlice(ev.Payload, "options"),
			Context:     payloadAnyMap(ev.Payload, "context"),
			ToolCallID:  payloadText(ev.Payload, "tool_call_id"),
			RaisedAt:    ev.TimeUnixMs,
		}
		if snap.CurrentTurn != nil && strings.TrimSpace(snap.CurrentTurn.TurnID) == strings.TrimSpace(ev.TurnID) {
			snap.CurrentTurn.Status = TurnStatusInterrupted
		}

	case "INTERRUPT_RESOLVED":
		if snap.PendingInterrupt == nil || strings.TrimSpace(snap.PendingInterrupt.InterruptID) != strings.TrimSpace(ev.InterruptID) {
			break
		}
		snap.PendingInterrupt.Answer = payloadText(ev.Payload, "answer")
		snap.PendingInterrupt.AnswerBlob = payloadText(ev.Payload, "answer_blob_ref")
		snap.PendingInterrupt.ResolvedAt = ev.TimeUnixMs
		snap.SessionState = SessionStateBusy

	case "INTERRUPT_CANCELLED":
		if snap.PendingInterrupt == nil || strings.TrimSpace(snap.PendingInterrupt.InterruptID) != strings.TrimSpace(ev.InterruptID) {
			break
		}
		snap.PendingInterrupt = nil
		snap.SessionState = SessionStateIdle

	default:
		// Unknown events do not affect the materialized view.
	}

	if ev.Seq > snap.LastSeq {
		snap.LastSeq = ev.Seq
	}
	snap.UpdatedAt = time.Now()
	return nil
}

func BuildSnapshotFromEvents(sessionID string, events []*Event, diag *SystemDiagnostics) (*Snapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is empty")
	}
	snap := &Snapshot{
		FormatVersion: FormatVersion,
		SessionID:     sessionID,
		SessionState:  SessionStateRecovering,
		System:        diag,
		UpdatedAt:     time.Now(),
	}
	for _, ev := range events {
		if ev == nil {
			continue
		}
		if err := ReduceSnapshot(snap, ev); err != nil {
			return nil, err
		}
	}
	if snap.SessionState == SessionStateRecovering {
		snap.SessionState = SessionStateIdle
	}
	return snap, nil
}

func payloadText(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func payloadAnyMap(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	if v, ok := payload[key]; ok {
		if m, ok := v.(map[string]any); ok && len(m) > 0 {
			return m
		}
	}
	return nil
}

func payloadStringSlice(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	raw, ok := payload[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, it := range v {
			s, _ := it.(string)
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}
