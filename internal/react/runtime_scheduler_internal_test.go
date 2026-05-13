package react

import (
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
)

type noopChatClientForScheduler struct{}

func (s *noopChatClientForScheduler) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (s *noopChatClientForScheduler) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (s *noopChatClientForScheduler) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestScheduler_FallbackDoesNotSwallowFinalAnswerError(t *testing.T) {
	agent, err := NewReActAgent("test", &noopChatClientForScheduler{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}
	// Intentionally do NOT configure workspaceRuntime: runFinalAnswerPhase should fail
	// while handling a plan-phase error.
	agent.workspaceRuntime = nil

	res, runErr := agent.runSchedulerLoop(context.Background(), nil, "", nil, 1)
	if runErr == nil {
		t.Fatalf("expected error, got result=%#v", res)
	}
	if res != nil {
		t.Fatalf("expected nil result on error, got %#v", res)
	}
	msg := runErr.Error()
	if !strings.Contains(msg, "input timeline is empty") {
		t.Fatalf("expected original phase error to be present, got: %s", msg)
	}
	if !strings.Contains(msg, "final_answer error") {
		t.Fatalf("expected final_answer error context, got: %s", msg)
	}
	if !strings.Contains(msg, "workspace runtime is nil") {
		t.Fatalf("expected final answer root cause, got: %s", msg)
	}
}
