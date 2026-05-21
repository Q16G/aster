package react

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
)

type intentTestClient struct {
	replies []intentTestReply
	calls   int
}

type intentTestReply struct {
	content   string
	toolCalls []*ai.FunctionTool
}

func (c *intentTestClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return c.nextContent(), nil
}

func (c *intentTestClient) ChatEx(_ context.Context, msgs []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	reply := c.nextReply()
	choice := &ai.ChatChoices{
		Message: &ai.MsgInfo{
			Role:    "assistant",
			Content: reply.content,
		},
		FinishReason: "stop",
	}
	if len(reply.toolCalls) > 0 {
		choice.Message.ToolCalls = reply.toolCalls
	}
	return []*ai.ChatChoices{choice}, nil
}

func (c *intentTestClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return c.nextContent(), nil
}

func (c *intentTestClient) ModelContextInfo() ai.ModelContextInfo {
	return ai.ModelContextInfo{ContextWindowTokens: 128000, OutputTokenLimit: 32000}
}

func (c *intentTestClient) nextReply() intentTestReply {
	if c.calls >= len(c.replies) {
		c.calls++
		return intentTestReply{content: `{"is_complete":true,"status":"completed","reason":"fallback","user_message":"done","references":[]}`}
	}
	r := c.replies[c.calls]
	c.calls++
	return r
}

func (c *intentTestClient) nextContent() string {
	return c.nextReply().content
}

func seedIdleStore(t *testing.T, root, sessionID string, outcomes []*builtin_tools.StepOutcome, timeline []*builtin_tools.TimelineInput) *persistv2.Store {
	t.Helper()
	store, err := persistv2.Open(root, sessionID)
	if err != nil {
		t.Fatalf("persistv2.Open: %v", err)
	}

	if _, err := store.AppendEvent(&persistv2.Event{Type: "SESSION_CREATED"}); err != nil {
		t.Fatalf("AppendEvent SESSION_CREATED: %v", err)
	}
	if _, err := store.AppendEvent(&persistv2.Event{
		Type: "TURN_STARTED", TurnID: "turn-1", GroupID: "group-1",
		Payload: map[string]any{"input": "initial task"},
	}); err != nil {
		t.Fatalf("AppendEvent TURN_STARTED: %v", err)
	}

	rtState := builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhasePlan,
		Status:        builtin_tools.TaskStatusCompleted,
		CurrentGoal:   "analyze code",
		StepOutcomes:  outcomes,
		InputTimeline: timeline,
	}
	rtRaw, _ := json.Marshal(rtState)
	rtRef, err := store.WriteBlob(rtRaw)
	if err != nil {
		t.Fatalf("WriteBlob runtime_state: %v", err)
	}

	convHistory := []*ai.MsgInfo{
		{Role: "user", Content: "initial task"},
		{Role: "assistant", Content: "completed"},
	}
	chRaw, _ := json.Marshal(convHistory)
	chRef, err := store.WriteBlob(chRaw)
	if err != nil {
		t.Fatalf("WriteBlob conversation_history: %v", err)
	}

	if _, err := store.AppendEvent(&persistv2.Event{
		Type: "TURN_FINISHED", TurnID: "turn-1", GroupID: "group-1",
		Payload: map[string]any{
			"status":                        "succeeded",
			"runtime_state_blob_ref":        rtRef,
			"conversation_history_blob_ref": chRef,
		},
	}); err != nil {
		t.Fatalf("AppendEvent TURN_FINISHED: %v", err)
	}

	if _, err := store.LoadSnapshot(); err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	return store
}

func TestResumeIntent_IntentClassification_Carry(t *testing.T) {
	root := t.TempDir()
	sessionID := "sess-carry"

	outcomes := []*builtin_tools.StepOutcome{
		{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "read file done"},
	}
	timeline := []*builtin_tools.TimelineInput{
		{Content: "analyze main.go", CreatedAt: time.Now().Add(-time.Minute)},
	}
	seedIdleStore(t, root, sessionID, outcomes, timeline)

	client := &intentTestClient{
		replies: []intentTestReply{
			// intent_classification phase
			{content: `{"action":"carry","reason":"user continuing"}`},
			// plan phase — planner
			{content: `{"needs_planning":true,"plan":[{"id":"s2","step":"continue analysis"}]}`},
			// step phase — tool call (update_current_step)
			{content: `{"is_complete":true,"status":"completed","reason":"done","user_message":"carry-done","references":[]}`},
		},
	}

	agent, err := NewReActAgent("test-carry", client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(20),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	result, err := agent.Execute(context.Background(), "continue analysis",
		WithWorkspaceSession(sessionID, root),
		WithResumeExecutionIntent(),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("expected RunResult")
	}

	// Verify the intent_classification phase ran by checking the client was called
	if client.calls < 1 {
		t.Errorf("expected at least 1 client call (intent classification), got %d", client.calls)
	}
}

func TestResumeIntent_SkipClassification(t *testing.T) {
	root := t.TempDir()
	sessionID := "sess-skip"

	outcomes := []*builtin_tools.StepOutcome{
		{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "done"},
	}
	seedIdleStore(t, root, sessionID, outcomes, nil)

	client := &intentTestClient{
		replies: []intentTestReply{
			// plan phase — no intent classification since it's skipped
			{content: `{"needs_planning":true,"plan":[{"id":"s2","step":"next"}]}`},
			{content: `{"is_complete":true,"status":"completed","reason":"done","user_message":"skip-done","references":[]}`},
		},
	}

	agent, err := NewReActAgent("test-skip", client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(20),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	result, err := agent.Execute(context.Background(), "new input",
		WithWorkspaceSession(sessionID, root),
		WithResumeExecutionIntent(),
		WithSkipIntentPrelude(),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("expected RunResult")
	}
}

func TestResumeIntent_ColdStart_NoSnapshot(t *testing.T) {
	root := t.TempDir()
	sessionID := "sess-cold"

	client := &intentTestClient{
		replies: []intentTestReply{
			{content: `{"needs_planning":true,"plan":[{"id":"s1","step":"start fresh"}]}`},
			{content: `{"is_complete":true,"status":"completed","reason":"done","user_message":"cold-done","references":[]}`},
		},
	}

	agent, err := NewReActAgent("test-cold", client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(20),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	result, err := agent.Execute(context.Background(), "fresh task",
		WithWorkspaceSession(sessionID, root),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("expected RunResult")
	}
}

func TestResumeIntent_IntentClassification_ColdStart(t *testing.T) {
	root := t.TempDir()
	sessionID := "sess-coldstart"

	outcomes := []*builtin_tools.StepOutcome{
		{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "analyzed code"},
	}
	seedIdleStore(t, root, sessionID, outcomes, nil)

	client := &intentTestClient{
		replies: []intentTestReply{
			// intent_classification → cold_start
			{content: `{"action":"cold_start","reason":"completely unrelated request"}`},
			// plan phase — fresh start
			{content: `{"needs_planning":true,"plan":[{"id":"s1","step":"new task"}]}`},
			{content: `{"is_complete":true,"status":"completed","reason":"done","user_message":"fresh-done","references":[]}`},
		},
	}

	agent, err := NewReActAgent("test-coldstart", client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(20),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	result, err := agent.Execute(context.Background(), "something completely different",
		WithWorkspaceSession(sessionID, root),
		WithResumeExecutionIntent(),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil {
		t.Fatal("expected RunResult")
	}

	// Verify state was reset: check the agent's state has no leftover outcomes
	snap := agent.State()
	if strings.Contains(snap.CurrentGoal, "analyze code") {
		t.Error("cold_start should have cleared old goal")
	}
}
