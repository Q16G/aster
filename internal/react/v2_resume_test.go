package react

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
)

func TestSoftResetWithContext_Normal(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-soft-normal")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	outcomes := []*builtin_tools.StepOutcome{
		{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "done1"},
		{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "done2"},
	}
	timeline := []*builtin_tools.TimelineInput{
		{Content: "first input", CreatedAt: time.Now().Add(-2 * time.Minute)},
		{Content: "second input", CreatedAt: time.Now().Add(-time.Minute)},
	}
	rtState := builtin_tools.StateSnapshot{
		StepOutcomes:  outcomes,
		InputTimeline: timeline,
	}
	rtRaw, _ := json.Marshal(rtState)
	rtRef, err := store.WriteBlob(rtRaw)
	if err != nil {
		t.Fatalf("WriteBlob runtime_state: %v", err)
	}

	convHistory := []*ai.MsgInfo{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}
	chRaw, _ := json.Marshal(convHistory)
	chRef, err := store.WriteBlob(chRaw)
	if err != nil {
		t.Fatalf("WriteBlob conversation_history: %v", err)
	}

	snap := &persistv2.Snapshot{
		RuntimeStateBlobRef:        rtRef,
		ConversationHistoryBlobRef: chRef,
	}

	client := &intentTestClient{}
	agent, aerr := NewReActAgent("test-soft", client, WithEmitter(NewDummyEmitter()))
	if aerr != nil {
		t.Fatalf("NewReActAgent: %v", aerr)
	}

	agent.softResetWithContext(context.Background(), client, store, snap)

	state := agent.State()
	if len(state.StepOutcomes) != 2 {
		t.Errorf("StepOutcomes len = %d, want 2", len(state.StepOutcomes))
	}
	if len(state.InputTimeline) != 2 {
		t.Errorf("InputTimeline len = %d, want 2", len(state.InputTimeline))
	}
	if state.Phase != builtin_tools.AgentPhasePlan {
		t.Errorf("Phase = %q, want plan", state.Phase)
	}
	if len(agent.history) != 2 {
		t.Errorf("history len = %d, want 2", len(agent.history))
	}
}

func TestSoftResetWithContext_BlobReadFail(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-soft-fail")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	snap := &persistv2.Snapshot{
		RuntimeStateBlobRef: "sha256:nonexistent",
	}

	client := &intentTestClient{}
	agent, aerr := NewReActAgent("test-soft-fail", client, WithEmitter(NewDummyEmitter()))
	if aerr != nil {
		t.Fatalf("NewReActAgent: %v", aerr)
	}

	agent.softResetWithContext(context.Background(), client, store, snap)

	state := agent.State()
	if len(state.StepOutcomes) != 0 {
		t.Errorf("StepOutcomes should be empty after blob fail, got %d", len(state.StepOutcomes))
	}
	if state.Phase != builtin_tools.AgentPhasePlan {
		t.Errorf("Phase = %q, want plan", state.Phase)
	}
}

func TestSoftResetWithContext_ReducerSkipped(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-soft-skip")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	outcomes := []*builtin_tools.StepOutcome{
		{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "a"},
		{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "b"},
	}
	rtState := builtin_tools.StateSnapshot{StepOutcomes: outcomes}
	rtRaw, _ := json.Marshal(rtState)
	rtRef, err := store.WriteBlob(rtRaw)
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	snap := &persistv2.Snapshot{RuntimeStateBlobRef: rtRef}

	client := &intentTestClient{}
	agent, aerr := NewReActAgent("test-soft-noreduce", client, WithEmitter(NewDummyEmitter()))
	if aerr != nil {
		t.Fatalf("NewReActAgent: %v", aerr)
	}

	agent.softResetWithContext(context.Background(), client, store, snap)

	if client.calls != 0 {
		t.Errorf("client.calls = %d, want 0 (reducer should not trigger for small outcomes)", client.calls)
	}
	state := agent.State()
	if len(state.StepOutcomes) != 2 {
		t.Errorf("StepOutcomes len = %d, want 2", len(state.StepOutcomes))
	}
}

func TestSoftResetWithContext_HistoryRestored(t *testing.T) {
	root := t.TempDir()
	store, err := persistv2.Open(root, "sess-soft-hist")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	convHistory := []*ai.MsgInfo{
		{Role: "user", Content: "task1"},
		{Role: "assistant", Content: "done1"},
		{Role: "user", Content: "task2"},
	}
	chRaw, _ := json.Marshal(convHistory)
	chRef, err := store.WriteBlob(chRaw)
	if err != nil {
		t.Fatalf("WriteBlob: %v", err)
	}

	snap := &persistv2.Snapshot{
		ConversationHistoryBlobRef: chRef,
	}

	client := &intentTestClient{}
	agent, aerr := NewReActAgent("test-soft-hist", client, WithEmitter(NewDummyEmitter()))
	if aerr != nil {
		t.Fatalf("NewReActAgent: %v", aerr)
	}

	agent.softResetWithContext(context.Background(), client, store, snap)

	if len(agent.history) != 3 {
		t.Errorf("history len = %d, want 3", len(agent.history))
	}
	if agent.history[0].Role != "user" || agent.history[0].Content != "task1" {
		t.Errorf("history[0] = %+v, want user/task1", agent.history[0])
	}
}
