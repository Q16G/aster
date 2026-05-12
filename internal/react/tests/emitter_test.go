package react_test

import (
	. "aster/internal/react"
	"testing"
)

func TestEmitter_AssignsSeqIDAndTimestamp(t *testing.T) {
	var gotSeqID uint64
	var gotTimestamp int64

	emitter := NewEmitter("demo-session", "demo-agent", func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		gotSeqID = e.SeqID
		gotTimestamp = e.Timestamp
		return nil
	})

	if err := emitter.Emit(&AgentOutputEvent{Type: EventTypeLog, NodeID: "log"}); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}

	if gotSeqID != 1 {
		t.Fatalf("SeqID not assigned or unexpected: got=%d want=%d", gotSeqID, 1)
	}
	// UnixMilli in 2026 should be ~1e12; this guards against accidentally using Unix seconds.
	if gotTimestamp <= 1e11 {
		t.Fatalf("Timestamp not assigned in milliseconds: got=%d", gotTimestamp)
	}
}

func TestEmitter_PushProcessorKeepsSharedSeqID(t *testing.T) {
	var seqIDs []uint64

	base := func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		seqIDs = append(seqIDs, e.SeqID)
		return nil
	}

	emitter := NewEmitter("demo-session", "demo-agent", base)
	emitterWithProc := emitter.PushProcessor(func(e *AgentOutputEvent) *AgentOutputEvent { return e })

	_ = emitter.Emit(&AgentOutputEvent{Type: EventTypeLog, NodeID: "log"})
	_ = emitterWithProc.Emit(&AgentOutputEvent{Type: EventTypeLog, NodeID: "log"})
	_ = emitter.Emit(&AgentOutputEvent{Type: EventTypeLog, NodeID: "log"})

	if len(seqIDs) != 3 {
		t.Fatalf("unexpected emitted events: got=%d want=%d", len(seqIDs), 3)
	}
	for i := 0; i < len(seqIDs); i++ {
		want := uint64(i + 1)
		if seqIDs[i] != want {
			t.Fatalf("SeqID not monotonic across shared emitter: got=%v want=%v", seqIDs, []uint64{1, 2, 3})
		}
	}
}

func TestEmitter_AssignsRecordUniqueEventID(t *testing.T) {
	var eventIDs []string

	emitter := NewEmitter("demo-session", "demo-agent", func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		eventIDs = append(eventIDs, e.EventID)
		return nil
	})

	_ = emitter.Emit(&AgentOutputEvent{Type: EventTypeLog, NodeID: "log"})
	_ = emitter.Emit(&AgentOutputEvent{Type: EventTypeLog, NodeID: "log"})

	if len(eventIDs) != 2 {
		t.Fatalf("unexpected emitted events: got=%d want=%d", len(eventIDs), 2)
	}
	if eventIDs[0] == "" || eventIDs[1] == "" {
		t.Fatalf("expected non-empty event IDs, got=%v", eventIDs)
	}
	if eventIDs[0] == eventIDs[1] {
		t.Fatalf("expected distinct event IDs, got=%v", eventIDs)
	}
	if eventIDs[0] != "demo-session:1" || eventIDs[1] != "demo-session:2" {
		t.Fatalf("unexpected event ID assignment: got=%v want=%v", eventIDs, []string{"demo-session:1", "demo-session:2"})
	}
}
