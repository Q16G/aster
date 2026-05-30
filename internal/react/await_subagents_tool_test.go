package react

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAwaitSubAgentsTool_NoRunningReturnsNoOp(t *testing.T) {
	agent, err := NewReActAgent("parent", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agent.asyncRegistry = NewAsyncAgentRegistry()

	tool := NewAwaitSubAgentsTool(agent)
	out, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["status"] != "no_running_subagents" {
		t.Fatalf("expected no_running_subagents, got %v", payload["status"])
	}
	if agent.awaitBackgroundRequested {
		t.Fatal("awaitBackgroundRequested must stay false when nothing is running")
	}
}

func TestAwaitSubAgentsTool_SetsFlagWhenRunning(t *testing.T) {
	agent, err := NewReActAgent("parent", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agent.asyncRegistry = NewAsyncAgentRegistry()
	agent.asyncRegistry.Register("bg", "task", "/tmp/ws")

	tool := NewAwaitSubAgentsTool(agent)
	out, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["status"] != "awaiting_background" {
		t.Fatalf("expected awaiting_background, got %v", payload["status"])
	}
	if !agent.awaitBackgroundRequested {
		t.Fatal("awaitBackgroundRequested must be set when a sub-agent is running")
	}
}

func TestAwaitSubAgentsTool_NilRegistryNoOp(t *testing.T) {
	agent, err := NewReActAgent("parent", &stubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agent.asyncRegistry = nil

	tool := NewAwaitSubAgentsTool(agent)
	out, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["status"] != "no_running_subagents" {
		t.Fatalf("expected no_running_subagents with nil registry, got %v", payload["status"])
	}
}
