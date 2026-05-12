package persistv2

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore_SnapshotAtomicAndReplayTailTruncation(t *testing.T) {
	root := t.TempDir()
	sessionID := "11111111-1111-1111-1111-111111111111"

	store, err := Open(root, sessionID)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	snap, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}
	if snap == nil || snap.SessionID != sessionID {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}

	if err := store.SaveSnapshotAtomic(&Snapshot{
		SessionID:    sessionID,
		SessionState: SessionStateIdle,
	}); err != nil {
		t.Fatalf("SaveSnapshotAtomic failed: %v", err)
	}

	if _, err := store.AppendEvent(&Event{Type: "SESSION_CREATED"}); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}
	if _, err := store.AppendEvent(&Event{Type: "TURN_STARTED", TurnID: "turn-1", GroupID: "group-1", Payload: map[string]any{"input": "hi"}}); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Corrupt the tail (simulate partial write / truncation)
	eventsPath := store.EventsPath()
	raw, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events failed: %v", err)
	}
	raw = append(raw, []byte("{\"format_version\":2,\"seq\":999")...)
	if err := os.WriteFile(eventsPath, raw, 0o644); err != nil {
		t.Fatalf("write corrupted events failed: %v", err)
	}

	var got []*Event
	diag, err := store.ReplayEvents(func(ev *Event) error {
		c := *ev
		got = append(got, &c)
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayEvents failed: %v", err)
	}
	if diag == nil || !diag.Degraded || !diag.EventsTailTruncated {
		t.Fatalf("expected degraded diagnostics, got %#v", diag)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 valid events, got %d", len(got))
	}
	if got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("unexpected seqs: %#v", got)
	}

	// snapshot can be rebuilt from events we collected
	next, err := BuildSnapshotFromEvents(sessionID, got, diag)
	if err != nil {
		t.Fatalf("BuildSnapshotFromEvents failed: %v", err)
	}
	if next == nil || next.CurrentTurn == nil || next.CurrentTurn.TurnID != "turn-1" {
		t.Fatalf("unexpected rebuilt snapshot: %#v", next)
	}
	if next.CurrentTurn.GroupID != "group-1" {
		t.Fatalf("unexpected group id in rebuilt snapshot: %#v", next.CurrentTurn)
	}
	if next.System == nil || !next.System.Degraded {
		t.Fatalf("expected degraded system diagnostics in snapshot")
	}

	// blob io
	ref, err := store.WriteBlob([]byte("hello"))
	if err != nil {
		t.Fatalf("WriteBlob failed: %v", err)
	}
	if !strings.HasPrefix(ref, "sha256:") {
		t.Fatalf("unexpected blob ref: %q", ref)
	}
	data, err := store.ReadBlob(ref)
	if err != nil {
		t.Fatalf("ReadBlob failed: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected blob data: %q", string(data))
	}

	// ensure files are under workspace/sessions/<session_id>
	wantPrefix := filepath.Join(root, "workspace", "sessions", sessionID)
	if !strings.HasPrefix(store.SessionDir(), wantPrefix) {
		t.Fatalf("unexpected session dir: %s", store.SessionDir())
	}
}

func TestStore_AppendEvent_RepairsCorruptedTail(t *testing.T) {
	root := t.TempDir()
	sessionID := "22222222-2222-2222-2222-222222222222"

	store, err := Open(root, sessionID)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if _, err := store.AppendEvent(&Event{Type: "SESSION_CREATED"}); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Corrupt the tail (simulate crash mid-write). This should be repaired on the next append.
	eventsPath := store.EventsPath()
	f, err := os.OpenFile(eventsPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open events for corruption: %v", err)
	}
	if _, err := f.WriteString("{\"format_version\":2,\"seq\":999"); err != nil {
		_ = f.Close()
		t.Fatalf("write corruption: %v", err)
	}
	_ = f.Close()

	if _, err := store.AppendEvent(&Event{Type: "TURN_STARTED", TurnID: "turn-1"}); err != nil {
		t.Fatalf("AppendEvent (after corruption) failed: %v", err)
	}

	var got []*Event
	diag, err := store.ReplayEvents(func(ev *Event) error {
		c := *ev
		got = append(got, &c)
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayEvents failed: %v", err)
	}
	if diag != nil {
		t.Fatalf("expected repaired events to replay cleanly, got diag %#v", diag)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events after repair, got %d", len(got))
	}
	if got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("unexpected seqs after repair: %#v", got)
	}

	// Diagnostics should still be materialized into snapshot when we write a snapshot next.
	snap, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}
	if err := store.SaveSnapshotAtomic(snap); err != nil {
		t.Fatalf("SaveSnapshotAtomic failed: %v", err)
	}
	snap2, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("LoadSnapshot(2) failed: %v", err)
	}
	if snap2.System == nil || !snap2.System.Degraded || !snap2.System.EventsTailTruncated {
		t.Fatalf("expected degraded diagnostics to be materialized, got %#v", snap2.System)
	}
}

func TestStore_LoadSnapshot_RebuildsFromEventsOnParseError(t *testing.T) {
	root := t.TempDir()
	sessionID := "33333333-3333-3333-3333-333333333333"

	store, err := Open(root, sessionID)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	if _, err := store.AppendEvent(&Event{Type: "SESSION_CREATED"}); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Write an invalid snapshot.json to force a rebuild from events.
	if err := os.WriteFile(store.SnapshotPath(), []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write invalid snapshot: %v", err)
	}

	snap, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("LoadSnapshot should rebuild, got error: %v", err)
	}
	if snap == nil || snap.System == nil || !snap.System.Degraded {
		t.Fatalf("expected degraded rebuilt snapshot, got %#v", snap)
	}

	// Ensure we actually self-healed by persisting a valid JSON snapshot.
	raw, err := os.ReadFile(store.SnapshotPath())
	if err != nil {
		t.Fatalf("read healed snapshot: %v", err)
	}
	if !strings.Contains(string(raw), "\"system_diagnostics\"") {
		t.Fatalf("expected healed snapshot to contain system_diagnostics, got: %s", string(raw))
	}
}
