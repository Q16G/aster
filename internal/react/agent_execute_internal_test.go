package react

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
)

type noopChatClientForExecute struct{}

func (s *noopChatClientForExecute) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (s *noopChatClientForExecute) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (s *noopChatClientForExecute) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestExecute_InterruptCancel_AppendsTurnFinished(t *testing.T) {
	root := t.TempDir()
	sessionID := "sess-1"

	// Seed an interrupted session that is WAITING_FOR_HUMAN.
	store, err := persistv2.Open(root, sessionID)
	if err != nil {
		t.Fatalf("persistv2.Open failed: %v", err)
	}
	if _, err := store.AppendEvent(&persistv2.Event{Type: "SESSION_CREATED"}); err != nil {
		t.Fatalf("AppendEvent SESSION_CREATED failed: %v", err)
	}
	if _, err := store.AppendEvent(&persistv2.Event{
		Type:    "TURN_STARTED",
		TurnID:  "turn-1",
		GroupID: "group-1",
		Payload: map[string]any{"input": "run task"},
	}); err != nil {
		t.Fatalf("AppendEvent TURN_STARTED failed: %v", err)
	}

	// The resume path expects referenced blobs to exist and be valid JSON.
	rtRaw, _ := json.Marshal(builtin_tools.StateSnapshot{})
	rtRef, err := store.WriteBlob(rtRaw)
	if err != nil {
		t.Fatalf("WriteBlob runtime_state failed: %v", err)
	}
	shRaw, _ := json.Marshal([]*ai.MsgInfo{})
	shRef, err := store.WriteBlob(shRaw)
	if err != nil {
		t.Fatalf("WriteBlob step_history failed: %v", err)
	}

	if _, err := store.AppendEvent(&persistv2.Event{
		Type:        "INTERRUPT_RAISED",
		TurnID:      "turn-1",
		GroupID:     "group-1",
		InterruptID: "int-1",
		Payload: map[string]any{
			"question":               "confirm?",
			"input_type":             "text",
			"runtime_state_blob_ref": rtRef,
			"step_history_blob_ref":  shRef,
		},
	}); err != nil {
		t.Fatalf("AppendEvent INTERRUPT_RAISED failed: %v", err)
	}
	if _, err := store.AppendEvent(&persistv2.Event{
		Type:    "TURN_FINISHED",
		TurnID:  "turn-1",
		GroupID: "group-1",
		Payload: map[string]any{"status": "interrupted"},
	}); err != nil {
		t.Fatalf("AppendEvent TURN_FINISHED(interrupted) failed: %v", err)
	}
	// Materialize snapshot.json so Execute() can find PendingInterrupt.
	if _, err := store.LoadSnapshot(); err != nil {
		t.Fatalf("LoadSnapshot seed failed: %v", err)
	}

	agent, err := NewReActAgent("test", &noopChatClientForExecute{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runID := "turn-2"
	result, execErr := agent.Execute(
		context.Background(),
		"",
		WithWorkspaceSession(sessionID, root),
		WithExecuteRunID(runID),
		WithInterruptCancel("int-1", "user_cancelled"),
	)
	if execErr != nil {
		t.Fatalf("Execute returned error: %v", execErr)
	}
	if result == nil {
		t.Fatal("expected RunResult")
	}
	if got := strings.TrimSpace(result.TurnStatus); got != string(persistv2.TurnStatusCancelled) {
		t.Fatalf("expected TurnStatus=cancelled, got %q", got)
	}

	// Verify event log has a TURN_FINISHED for this cancel turn.
	var (
		foundStart  bool
		foundFinish bool
	)
	diag, rerr := store.ReplayEvents(func(ev *persistv2.Event) error {
		if ev == nil {
			return nil
		}
		if strings.TrimSpace(ev.TurnID) != runID {
			return nil
		}
		switch strings.TrimSpace(ev.Type) {
		case "TURN_STARTED":
			foundStart = true
		case "TURN_FINISHED":
			status, _ := ev.Payload["status"].(string)
			if strings.TrimSpace(status) == "cancelled" {
				foundFinish = true
			}
		}
		return nil
	})
	if rerr != nil {
		t.Fatalf("ReplayEvents failed: %v", rerr)
	}
	_ = diag
	if !foundStart {
		t.Fatalf("expected TURN_STARTED for %s", runID)
	}
	if !foundFinish {
		t.Fatalf("expected TURN_FINISHED(cancelled) for %s", runID)
	}

	// Verify snapshot view is consistent.
	snap, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("LoadSnapshot after cancel failed: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot")
	}
	if snap.CurrentTurn == nil || strings.TrimSpace(snap.CurrentTurn.TurnID) != runID {
		t.Fatalf("expected CurrentTurn=%s, got %#v", runID, snap.CurrentTurn)
	}
	if snap.CurrentTurn.Status != persistv2.TurnStatusCancelled {
		t.Fatalf("expected CurrentTurn.Status=cancelled, got %q", snap.CurrentTurn.Status)
	}
	if snap.SessionState != persistv2.SessionStateIdle {
		t.Fatalf("expected SessionState=IDLE after cancel, got %q", snap.SessionState)
	}
}
