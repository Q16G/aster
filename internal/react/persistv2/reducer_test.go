package persistv2

import "testing"

func makeEvent(typ string, opts ...func(*Event)) *Event {
	ev := &Event{
		FormatVersion: FormatVersion,
		Type:          typ,
		SessionID:     "sess-1",
	}
	for _, o := range opts {
		o(ev)
	}
	return ev
}

func withTurn(id string) func(*Event) {
	return func(ev *Event) { ev.TurnID = id }
}

func withInterrupt(id string) func(*Event) {
	return func(ev *Event) { ev.InterruptID = id }
}

func withPayload(p map[string]any) func(*Event) {
	return func(ev *Event) { ev.Payload = p }
}

func withTime(ms int64) func(*Event) {
	return func(ev *Event) { ev.TimeUnixMs = ms }
}

// TC1: BuildSnapshotFromEvents 恢复 blob refs
// 真实序列: SESSION_CREATED → TURN_STARTED(user input) → INTERRUPT_RAISED(含 blob refs)
func TestReducer_BuildSnapshotFromEvents_RestoresBlobRefs(t *testing.T) {
	events := []*Event{
		makeEvent("SESSION_CREATED"),
		makeEvent("TURN_STARTED", withTurn("t1"), withPayload(map[string]any{"input": "hello"})),
		makeEvent("INTERRUPT_RAISED", withTurn("t1"), withInterrupt("int-1"), withPayload(map[string]any{
			"question":                       "confirm?",
			"runtime_state_blob_ref":         "sha256:runtime-abc",
			"step_history_blob_ref":          "sha256:history-def",
			"conversation_history_blob_ref":  "sha256:conv-ghi",
		})),
	}
	snap, err := BuildSnapshotFromEvents("sess-1", events, nil)
	if err != nil {
		t.Fatalf("BuildSnapshotFromEvents: %v", err)
	}
	if snap.RuntimeStateBlobRef != "sha256:runtime-abc" {
		t.Errorf("RuntimeStateBlobRef = %q, want %q", snap.RuntimeStateBlobRef, "sha256:runtime-abc")
	}
	if snap.StepHistoryBlobRef != "sha256:history-def" {
		t.Errorf("StepHistoryBlobRef = %q, want %q", snap.StepHistoryBlobRef, "sha256:history-def")
	}
	if snap.ConversationHistoryBlobRef != "sha256:conv-ghi" {
		t.Errorf("ConversationHistoryBlobRef = %q, want %q", snap.ConversationHistoryBlobRef, "sha256:conv-ghi")
	}
	if snap.SessionState != SessionStateWaitingForHuman {
		t.Errorf("SessionState = %q, want %q", snap.SessionState, SessionStateWaitingForHuman)
	}
	if snap.PendingInterrupt == nil || snap.PendingInterrupt.InterruptID != "int-1" {
		t.Errorf("PendingInterrupt unexpected: %#v", snap.PendingInterrupt)
	}
}

// TC2: 真实事件顺序回放（resolve 链路）
// 真实落盘序列:
//
//	SESSION_CREATED → TURN_STARTED(t1, input="do stuff") → INTERRUPT_RAISED(t1) →
//	TURN_FINISHED(t1, interrupted) → TURN_STARTED(t2, no input) → INTERRUPT_RESOLVED(int-1) →
//	TURN_FINISHED(t2, succeeded)
//
// 关键验证点:
//  1. WAITING 时刻 blob refs 可用
//  2. TURN_STARTED(resolve, no input) 不清空 PendingInterrupt
//  3. INTERRUPT_RESOLVED 真正生效（Answer/ResolvedAt 被写入）
//  4. 最终 turn 结束后 blob refs 清零
func TestReducer_ResolveChain_BlobRefsLifecycle(t *testing.T) {
	interruptPayload := map[string]any{
		"question":                       "proceed?",
		"runtime_state_blob_ref":         "sha256:rt-1",
		"step_history_blob_ref":          "sha256:sh-1",
		"conversation_history_blob_ref":  "sha256:conv-1",
	}

	waitingEvents := []*Event{
		makeEvent("SESSION_CREATED"),
		makeEvent("TURN_STARTED", withTurn("t1"), withPayload(map[string]any{"input": "do stuff"})),
		makeEvent("INTERRUPT_RAISED", withTurn("t1"), withInterrupt("int-1"), withPayload(interruptPayload), withTime(1000)),
		makeEvent("TURN_FINISHED", withTurn("t1"), withPayload(map[string]any{"status": "interrupted"})),
	}

	// checkpoint 1: WAITING state — blob refs and PendingInterrupt must be present
	snapWait, err := BuildSnapshotFromEvents("sess-1", waitingEvents, nil)
	if err != nil {
		t.Fatalf("BuildSnapshotFromEvents (wait): %v", err)
	}
	if snapWait.RuntimeStateBlobRef != "sha256:rt-1" {
		t.Errorf("at WAITING: RuntimeStateBlobRef = %q, want %q", snapWait.RuntimeStateBlobRef, "sha256:rt-1")
	}
	if snapWait.StepHistoryBlobRef != "sha256:sh-1" {
		t.Errorf("at WAITING: StepHistoryBlobRef = %q, want %q", snapWait.StepHistoryBlobRef, "sha256:sh-1")
	}
	if snapWait.ConversationHistoryBlobRef != "sha256:conv-1" {
		t.Errorf("at WAITING: ConversationHistoryBlobRef = %q, want %q", snapWait.ConversationHistoryBlobRef, "sha256:conv-1")
	}
	if snapWait.PendingInterrupt == nil {
		t.Fatal("at WAITING: PendingInterrupt should not be nil")
	}

	// checkpoint 2: after TURN_STARTED(resolve, no input), PendingInterrupt must survive
	postStartEvents := append(waitingEvents,
		makeEvent("TURN_STARTED", withTurn("t2")),
	)
	snapPostStart, err := BuildSnapshotFromEvents("sess-1", postStartEvents, nil)
	if err != nil {
		t.Fatalf("BuildSnapshotFromEvents (post-start): %v", err)
	}
	if snapPostStart.PendingInterrupt == nil {
		t.Fatal("after TURN_STARTED(resolve): PendingInterrupt must survive for INTERRUPT_RESOLVED")
	}
	if snapPostStart.RuntimeStateBlobRef != "sha256:rt-1" {
		t.Errorf("after TURN_STARTED(resolve): RuntimeStateBlobRef = %q, want %q", snapPostStart.RuntimeStateBlobRef, "sha256:rt-1")
	}

	// checkpoint 3: full resolve chain — INTERRUPT_RESOLVED must actually take effect
	fullEvents := append(postStartEvents,
		makeEvent("INTERRUPT_RESOLVED", withInterrupt("int-1"), withPayload(map[string]any{
			"answer": "yes",
		}), withTime(2000)),
		makeEvent("TURN_FINISHED", withTurn("t2"), withPayload(map[string]any{"status": "succeeded"})),
	)

	snapFinal, err := BuildSnapshotFromEvents("sess-1", fullEvents, nil)
	if err != nil {
		t.Fatalf("BuildSnapshotFromEvents (final): %v", err)
	}
	// INTERRUPT_RESOLVED must have written Answer and ResolvedAt
	if snapFinal.PendingInterrupt == nil {
		t.Fatal("at FINAL: PendingInterrupt should still exist (cleared only by next user-input turn)")
	}
	if snapFinal.PendingInterrupt.Answer != "yes" {
		t.Errorf("at FINAL: PendingInterrupt.Answer = %q, want %q", snapFinal.PendingInterrupt.Answer, "yes")
	}
	if snapFinal.PendingInterrupt.ResolvedAt != 2000 {
		t.Errorf("at FINAL: PendingInterrupt.ResolvedAt = %d, want 2000", snapFinal.PendingInterrupt.ResolvedAt)
	}
	// blob refs must be cleared by TURN_FINISHED(succeeded)
	if snapFinal.RuntimeStateBlobRef != "" {
		t.Errorf("at FINAL: RuntimeStateBlobRef = %q, want empty", snapFinal.RuntimeStateBlobRef)
	}
	if snapFinal.StepHistoryBlobRef != "" {
		t.Errorf("at FINAL: StepHistoryBlobRef = %q, want empty", snapFinal.StepHistoryBlobRef)
	}
	if snapFinal.ConversationHistoryBlobRef != "" {
		t.Errorf("at FINAL: ConversationHistoryBlobRef = %q, want empty", snapFinal.ConversationHistoryBlobRef)
	}
	if snapFinal.SessionState != SessionStateIdle {
		t.Errorf("at FINAL: SessionState = %q, want %q", snapFinal.SessionState, SessionStateIdle)
	}
}

// TC3: INTERRUPT_CANCELLED 清理 blob refs
// 真实落盘序列:
//
//	SESSION_CREATED → TURN_STARTED(t1, input) → INTERRUPT_RAISED(t1) →
//	TURN_FINISHED(t1, interrupted) → TURN_STARTED(t2, no input) → INTERRUPT_CANCELLED(int-1) →
//	TURN_FINISHED(t2, cancelled)
func TestReducer_InterruptCancelled_ClearsBlobRefs(t *testing.T) {
	events := []*Event{
		makeEvent("SESSION_CREATED"),
		makeEvent("TURN_STARTED", withTurn("t1"), withPayload(map[string]any{"input": "run task"})),
		makeEvent("INTERRUPT_RAISED", withTurn("t1"), withInterrupt("int-1"), withPayload(map[string]any{
			"question":                       "confirm?",
			"runtime_state_blob_ref":         "sha256:rt-x",
			"step_history_blob_ref":          "sha256:sh-x",
			"conversation_history_blob_ref":  "sha256:conv-x",
		})),
		makeEvent("TURN_FINISHED", withTurn("t1"), withPayload(map[string]any{"status": "interrupted"})),
		makeEvent("TURN_STARTED", withTurn("t2")),
		makeEvent("INTERRUPT_CANCELLED", withInterrupt("int-1")),
		makeEvent("TURN_FINISHED", withTurn("t2"), withPayload(map[string]any{"status": "cancelled"})),
	}

	snap, err := BuildSnapshotFromEvents("sess-1", events, nil)
	if err != nil {
		t.Fatalf("BuildSnapshotFromEvents: %v", err)
	}
	if snap.RuntimeStateBlobRef != "" {
		t.Errorf("after CANCELLED: RuntimeStateBlobRef = %q, want empty", snap.RuntimeStateBlobRef)
	}
	if snap.StepHistoryBlobRef != "" {
		t.Errorf("after CANCELLED: StepHistoryBlobRef = %q, want empty", snap.StepHistoryBlobRef)
	}
	if snap.ConversationHistoryBlobRef != "" {
		t.Errorf("after CANCELLED: ConversationHistoryBlobRef = %q, want empty", snap.ConversationHistoryBlobRef)
	}
	if snap.PendingInterrupt != nil {
		t.Errorf("after CANCELLED: PendingInterrupt should be nil, got %#v", snap.PendingInterrupt)
	}
	if snap.SessionState != SessionStateIdle {
		t.Errorf("SessionState = %q, want %q", snap.SessionState, SessionStateIdle)
	}
}

// TC4: TURN_FINISHED 非 interrupted 清理 blob refs
// 真实落盘序列:
//
//	SESSION_CREATED → TURN_STARTED(t1, input) → INTERRUPT_RAISED(t1) →
//	TURN_FINISHED(t1, interrupted) → TURN_STARTED(t2, no input) → INTERRUPT_RESOLVED(int-1) →
//	TURN_FINISHED(t2, succeeded)
func TestReducer_TurnFinishedNonInterrupted_ClearsBlobRefs(t *testing.T) {
	events := []*Event{
		makeEvent("SESSION_CREATED"),
		makeEvent("TURN_STARTED", withTurn("t1"), withPayload(map[string]any{"input": "do it"})),
		makeEvent("INTERRUPT_RAISED", withTurn("t1"), withInterrupt("int-1"), withPayload(map[string]any{
			"question":                       "ok?",
			"runtime_state_blob_ref":         "sha256:rt-y",
			"step_history_blob_ref":          "sha256:sh-y",
			"conversation_history_blob_ref":  "sha256:conv-y",
		})),
		makeEvent("TURN_FINISHED", withTurn("t1"), withPayload(map[string]any{"status": "interrupted"})),
		makeEvent("TURN_STARTED", withTurn("t2")),
		makeEvent("INTERRUPT_RESOLVED", withInterrupt("int-1"), withPayload(map[string]any{"answer": "yes"})),
		makeEvent("TURN_FINISHED", withTurn("t2"), withPayload(map[string]any{"status": "succeeded"})),
	}

	snap, err := BuildSnapshotFromEvents("sess-1", events, nil)
	if err != nil {
		t.Fatalf("BuildSnapshotFromEvents: %v", err)
	}
	if snap.RuntimeStateBlobRef != "" {
		t.Errorf("after succeeded TURN_FINISHED: RuntimeStateBlobRef = %q, want empty", snap.RuntimeStateBlobRef)
	}
	if snap.StepHistoryBlobRef != "" {
		t.Errorf("after succeeded TURN_FINISHED: StepHistoryBlobRef = %q, want empty", snap.StepHistoryBlobRef)
	}
	if snap.ConversationHistoryBlobRef != "" {
		t.Errorf("after succeeded TURN_FINISHED: ConversationHistoryBlobRef = %q, want empty", snap.ConversationHistoryBlobRef)
	}
	if snap.SessionState != SessionStateIdle {
		t.Errorf("SessionState = %q, want %q", snap.SessionState, SessionStateIdle)
	}
}

// TC5: TURN_FINISHED(succeeded) with blob refs in payload preserves them in snapshot
func TestReducer_TurnFinishedSucceeded_PreservesBlobRefs(t *testing.T) {
	events := []*Event{
		makeEvent("SESSION_CREATED"),
		makeEvent("TURN_STARTED", withTurn("t1"), withPayload(map[string]any{"input": "task"})),
		makeEvent("TURN_FINISHED", withTurn("t1"), withPayload(map[string]any{
			"status":                        "succeeded",
			"runtime_state_blob_ref":        "sha256:rt-ok",
			"conversation_history_blob_ref": "sha256:conv-ok",
		})),
	}

	snap, err := BuildSnapshotFromEvents("sess-1", events, nil)
	if err != nil {
		t.Fatalf("BuildSnapshotFromEvents: %v", err)
	}
	if snap.RuntimeStateBlobRef != "sha256:rt-ok" {
		t.Errorf("RuntimeStateBlobRef = %q, want sha256:rt-ok", snap.RuntimeStateBlobRef)
	}
	if snap.ConversationHistoryBlobRef != "sha256:conv-ok" {
		t.Errorf("ConversationHistoryBlobRef = %q, want sha256:conv-ok", snap.ConversationHistoryBlobRef)
	}
	if snap.StepHistoryBlobRef != "" {
		t.Errorf("StepHistoryBlobRef = %q, want empty", snap.StepHistoryBlobRef)
	}
	if snap.SessionState != SessionStateIdle {
		t.Errorf("SessionState = %q, want %q", snap.SessionState, SessionStateIdle)
	}
}

// TC6: TURN_STARTED with real user input clears stale PendingInterrupt from prior session
func TestReducer_TurnStartedWithInput_ClearsStalePendingInterrupt(t *testing.T) {
	events := []*Event{
		makeEvent("SESSION_CREATED"),
		makeEvent("TURN_STARTED", withTurn("t1"), withPayload(map[string]any{"input": "first"})),
		makeEvent("INTERRUPT_RAISED", withTurn("t1"), withInterrupt("int-1"), withPayload(map[string]any{
			"question":               "confirm?",
			"runtime_state_blob_ref": "sha256:rt-stale",
			"step_history_blob_ref":  "sha256:sh-stale",
		})),
		makeEvent("TURN_FINISHED", withTurn("t1"), withPayload(map[string]any{"status": "interrupted"})),
		makeEvent("TURN_STARTED", withTurn("t2")),
		makeEvent("INTERRUPT_RESOLVED", withInterrupt("int-1"), withPayload(map[string]any{"answer": "ok"})),
		makeEvent("TURN_FINISHED", withTurn("t2"), withPayload(map[string]any{"status": "succeeded"})),
		// New user input turn — should clear the resolved PendingInterrupt
		makeEvent("TURN_STARTED", withTurn("t3"), withPayload(map[string]any{"input": "new question"})),
	}

	snap, err := BuildSnapshotFromEvents("sess-1", events, nil)
	if err != nil {
		t.Fatalf("BuildSnapshotFromEvents: %v", err)
	}
	if snap.PendingInterrupt != nil {
		t.Errorf("after new user-input TURN_STARTED: PendingInterrupt should be nil, got %#v", snap.PendingInterrupt)
	}
	if snap.CurrentTurn == nil || snap.CurrentTurn.TurnID != "t3" {
		t.Errorf("CurrentTurn should be t3, got %#v", snap.CurrentTurn)
	}
}
