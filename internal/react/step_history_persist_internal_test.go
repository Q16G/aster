package react

import (
	"encoding/json"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
)

func TestPersistStepTranscriptBlob_NilSafe(t *testing.T) {
	agent := &Agent{}
	agent.persistStepTranscriptBlob()
	if agent.lastStepTranscriptBlobRef != "" {
		t.Fatal("expected empty ref when v2Store is nil")
	}

	var nilAgent *Agent
	nilAgent.persistStepTranscriptBlob()
}

func TestPersistStepTranscriptBlob_EmptyHistory(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-empty")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	agent := &Agent{v2Store: store}
	agent.persistStepTranscriptBlob()
	if agent.lastStepTranscriptBlobRef != "" {
		t.Fatal("expected empty ref when stepHistory is empty")
	}
}

func TestPersistStepTranscriptBlob_WritesBlob(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-write")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	agent, err := NewReActAgent("test", &stubChatClientForHIL{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	agent.v2Store = store
	agent.stepHistory = []*ai.MsgInfo{
		ai.NewUserMsgInfo("tool output 1"),
		ai.NewAIMsgInfo("response"),
	}

	agent.persistStepTranscriptBlob()

	if agent.lastStepTranscriptBlobRef == "" {
		t.Fatal("expected non-empty blob ref")
	}
	if !strings.HasPrefix(agent.lastStepTranscriptBlobRef, "sha256:") {
		t.Fatalf("unexpected ref format: %s", agent.lastStepTranscriptBlobRef)
	}

	raw, err := store.ReadBlob(agent.lastStepTranscriptBlobRef)
	if err != nil {
		t.Fatalf("ReadBlob: %v", err)
	}
	var restored []*ai.MsgInfo
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(restored))
	}
}

func TestPersistInFlightStepHistory_NilSafe(t *testing.T) {
	agent := &Agent{}
	agent.persistInFlightStepHistory()

	var nilAgent *Agent
	nilAgent.persistInFlightStepHistory()
}

func TestPersistInFlightStepHistory_WritesAllBlobs(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-inflight")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	agent, err := NewReActAgent("test", &stubChatClientForHIL{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	agent.v2Store = store
	agent.history = []*ai.MsgInfo{ai.NewUserMsgInfo("hello")}
	agent.stepHistory = []*ai.MsgInfo{ai.NewAIMsgInfo("working")}

	agent.persistInFlightStepHistory()

	snap, err := store.LoadSnapshot()
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if strings.TrimSpace(snap.RuntimeStateBlobRef) == "" {
		t.Error("expected RuntimeStateBlobRef to be set")
	}
	if strings.TrimSpace(snap.StepHistoryBlobRef) == "" {
		t.Error("expected StepHistoryBlobRef to be set")
	}
	if strings.TrimSpace(snap.ConversationHistoryBlobRef) == "" {
		t.Error("expected ConversationHistoryBlobRef to be set")
	}

	raw, err := store.ReadBlob(snap.StepHistoryBlobRef)
	if err != nil {
		t.Fatalf("ReadBlob step_history: %v", err)
	}
	var stepMsgs []*ai.MsgInfo
	if err := json.Unmarshal(raw, &stepMsgs); err != nil {
		t.Fatalf("Unmarshal step_history: %v", err)
	}
	if len(stepMsgs) != 1 {
		t.Fatalf("expected 1 step message, got %d", len(stepMsgs))
	}
}

func TestSyncStepHistoryLayer_ClearsRefOnStepSwitch(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-sync")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	agent, err := NewReActAgent("test", &stubChatClientForHIL{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	agent.v2Store = store
	agent.stepHistory = []*ai.MsgInfo{ai.NewAIMsgInfo("step-A work")}
	agent.stepHistoryStepID = "step-A"
	agent.stepHistoryPhase = builtin_tools.AgentPhaseStep
	agent.stepHistoryPlanVer = 1

	agent.persistStepTranscriptBlob()
	blobRef := agent.lastStepTranscriptBlobRef
	if blobRef == "" {
		t.Fatal("expected blob to be written before step switch")
	}

	agent.syncStepHistoryLayer(builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhaseStep,
		CurrentStepID: "step-B",
		PlanVersion:   1,
	})

	if agent.lastStepTranscriptBlobRef != "" {
		t.Fatal("expected ref to be cleared on direct step switch (no replan)")
	}

	raw, err := store.ReadBlob(blobRef)
	if err != nil {
		t.Fatalf("blob should still be readable after ref cleared: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("blob content should be non-empty")
	}
}

func TestSyncStepHistoryLayer_PersistsOnLeaveStepPhase(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-leave")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	agent, err := NewReActAgent("test", &stubChatClientForHIL{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	agent.v2Store = store
	agent.stepHistory = []*ai.MsgInfo{ai.NewAIMsgInfo("step work")}
	agent.stepHistoryStepID = "step-X"
	agent.stepHistoryPhase = builtin_tools.AgentPhaseStep
	agent.stepHistoryPlanVer = 1

	agent.syncStepHistoryLayer(builtin_tools.StateSnapshot{
		Phase:       builtin_tools.AgentPhaseStepReplan,
		PlanVersion: 1,
	})

	if agent.lastStepTranscriptBlobRef == "" {
		t.Fatal("expected transcript blob ref after leaving step phase")
	}
	if agent.stepHistoryStepID != "" {
		t.Fatal("expected stepHistoryStepID to be cleared")
	}
}

func TestSyncStepHistoryLayer_NoPersisteOnEnterStepAttach(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-attach")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	agent, err := NewReActAgent("test", &stubChatClientForHIL{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	agent.v2Store = store
	agent.stepHistoryStepID = ""
	agent.stepHistoryPhase = ""
	agent.stepHistoryPlanVer = 0

	agent.syncStepHistoryLayer(builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhaseStep,
		CurrentStepID: "step-A",
		PlanVersion:   1,
	})

	if agent.lastStepTranscriptBlobRef != "" {
		t.Fatal("enter_step_attach should not persist (no previous transcript)")
	}
}

func TestResumeCondition_BroadenedToAnyBlobRef(t *testing.T) {
	snap := &persistv2.Snapshot{
		StepHistoryBlobRef: "sha256:abc123",
	}
	hasResumable := strings.TrimSpace(snap.RuntimeStateBlobRef) != "" ||
		strings.TrimSpace(snap.StepHistoryBlobRef) != "" ||
		strings.TrimSpace(snap.ConversationHistoryBlobRef) != ""
	if !hasResumable {
		t.Fatal("expected hasResumable=true when StepHistoryBlobRef is set")
	}

	snap2 := &persistv2.Snapshot{
		ConversationHistoryBlobRef: "sha256:def456",
	}
	hasResumable2 := strings.TrimSpace(snap2.RuntimeStateBlobRef) != "" ||
		strings.TrimSpace(snap2.StepHistoryBlobRef) != "" ||
		strings.TrimSpace(snap2.ConversationHistoryBlobRef) != ""
	if !hasResumable2 {
		t.Fatal("expected hasResumable=true when ConversationHistoryBlobRef is set")
	}

	snap3 := &persistv2.Snapshot{}
	hasResumable3 := strings.TrimSpace(snap3.RuntimeStateBlobRef) != "" ||
		strings.TrimSpace(snap3.StepHistoryBlobRef) != "" ||
		strings.TrimSpace(snap3.ConversationHistoryBlobRef) != ""
	if hasResumable3 {
		t.Fatal("expected hasResumable=false when all blob refs are empty")
	}
}

func TestStepContextRecord_TranscriptBlobRefField(t *testing.T) {
	record := &builtin_tools.StepContextRecord{
		ContextKey:        "ns:1:step-1",
		StepID:            "step-1",
		TranscriptBlobRef: "sha256:abc123",
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"transcript_blob_ref":"sha256:abc123"`) {
		t.Fatalf("expected transcript_blob_ref in JSON, got: %s", string(raw))
	}

	var restored builtin_tools.StepContextRecord
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if restored.TranscriptBlobRef != "sha256:abc123" {
		t.Fatalf("expected restored ref, got %q", restored.TranscriptBlobRef)
	}
}

func TestStepContextRecord_TranscriptBlobRefOmitEmpty(t *testing.T) {
	record := &builtin_tools.StepContextRecord{
		ContextKey: "ns:1:step-1",
		StepID:     "step-1",
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), "transcript_blob_ref") {
		t.Fatalf("expected omitempty to exclude empty transcript_blob_ref, got: %s", string(raw))
	}
}
