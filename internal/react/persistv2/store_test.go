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
