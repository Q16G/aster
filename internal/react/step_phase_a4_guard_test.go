package react

import (
	"context"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// a4GuardClient returns a single assistant message with text content and no
// tool calls, exercising the empty-tool-call path of runStepPhase.
type a4GuardClient struct{}

func (c *a4GuardClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *a4GuardClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return []*ai.ChatChoices{{
		Message:      ai.NewAIMsgInfo("我已经完成了这一步的工作。"),
		FinishReason: "stop",
	}}, nil
}

func (c *a4GuardClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestRunStepPhase_A4Guard_DefersWhenBackgroundRunning(t *testing.T) {
	client := &a4GuardClient{}
	agent, err := NewReActAgent("parent", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agent.asyncRegistry = NewAsyncAgentRegistry()
	agent.asyncRegistry.Register("bg", "long task", "/tmp/ws")

	agent.state.UpdatePlan([]*builtin_tools.PlanItem{{ID: "s1", Step: "do work", Status: builtin_tools.PlanStepPending}}, "", false)

	if err := agent.runStepPhase(context.Background(), 1, client, "", nil); err != nil {
		t.Fatalf("runStepPhase returned error: %v", err)
	}

	if !agent.awaitBackgroundRequested {
		t.Fatal("expected awaitBackgroundRequested to be set when a background sub-agent is running")
	}

	current := agent.state.Snapshot().CurrentStep()
	if current == nil {
		t.Fatal("expected current step to still exist")
	}
	if current.Status == builtin_tools.PlanStepCompleted {
		t.Fatalf("step must NOT be auto-completed while a background sub-agent is running, got status %q", current.Status)
	}
}

func TestRunStepPhase_A4Guard_AutoCompletesWhenNoBackground(t *testing.T) {
	client := &a4GuardClient{}
	agent, err := NewReActAgent("parent", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agent.asyncRegistry = NewAsyncAgentRegistry() // no running agents

	agent.state.UpdatePlan([]*builtin_tools.PlanItem{{ID: "s1", Step: "do work", Status: builtin_tools.PlanStepPending}}, "", false)

	if err := agent.runStepPhase(context.Background(), 1, client, "", nil); err != nil {
		t.Fatalf("runStepPhase returned error: %v", err)
	}

	if agent.awaitBackgroundRequested {
		t.Fatal("awaitBackgroundRequested must stay false with no running sub-agents")
	}

	current := agent.state.Snapshot().CurrentStep()
	// With no running sub-agent and assistant text present, the step is auto-completed,
	// which advances past s1 (CurrentStep becomes nil) or marks it completed.
	if current != nil && current.Status != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected step to be auto-completed, got status %q", current.Status)
	}
}
