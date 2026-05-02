package react_test

import (
	"encoding/json"
	"testing"

	"aster/internal/builtin_tools"
	. "aster/internal/react"
)

func TestSubAgentTool_Name(t *testing.T) {
	tool := NewSubAgentTool(nil, nil)
	if tool.Name() != builtin_tools.SubAgentToolName {
		t.Fatalf("expected name %q, got %q", builtin_tools.SubAgentToolName, tool.Name())
	}
}

func TestSubAgentTool_IsAgent(t *testing.T) {
	tool := NewSubAgentTool(nil, nil)
	if !tool.IsAgent() {
		t.Fatal("expected IsAgent() = true")
	}
	if !IsAgentTool(tool) {
		t.Fatal("expected IsAgentTool() = true")
	}
}

func TestSubAgentTool_Parameters_InstructionRequired(t *testing.T) {
	tool := NewSubAgentTool(nil, nil)
	params := tool.Parameters()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, ok := props["instruction"]; !ok {
		t.Fatal("expected instruction property")
	}
	if _, ok := props["tools"]; !ok {
		t.Fatal("expected tools property")
	}
	if _, ok := props["context"]; !ok {
		t.Fatal("expected context property")
	}

	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatal("expected required array")
	}
	found := false
	for _, r := range required {
		if r == "instruction" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("instruction should be in required")
	}
}

func TestSubAgentTool_Execute_NilParent(t *testing.T) {
	tool := NewSubAgentTool(nil, nil)
	_, err := tool.Execute(nil, map[string]any{"instruction": "test"})
	if err == nil {
		t.Fatal("expected error for nil parent")
	}
}

func TestSubAgentTool_Execute_MissingInstruction(t *testing.T) {
	agent, err := NewReActAgent("test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSubAgentTool(agent, factory)
	_, err = tool.Execute(nil, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing instruction")
	}
}

func TestSubAgentTool_DepthEnforcement(t *testing.T) {
	agent, err := NewReActAgent("test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)
	tool := NewSubAgentTool(agent, factory)

	ctx := builtin_tools.WithToolRuntime(nil, builtin_tools.ToolRuntimeInfo{
		Emitter:    NewDummyEmitter(),
		StackDepth: 3,
	})
	_, err = tool.Execute(ctx, map[string]any{"instruction": "do something"})
	if err == nil {
		t.Fatal("expected depth exceeded error")
	}
}

func TestSubAgentTool_RegisteredViaFactory(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
	)
	agent, err := factory.Build(AgentDefinition{
		Name:        "parent",
		Instruction: "test",
		ToolNames:   []string{"read_file"},
	})
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}

	tool, ok := agent.GetTool(builtin_tools.SubAgentToolName)
	if !ok || tool == nil {
		t.Fatal("sub_agent tool should be registered by factory")
	}
	if !IsAgentTool(tool) {
		t.Fatal("sub_agent should be recognized as AgentTool")
	}
}
