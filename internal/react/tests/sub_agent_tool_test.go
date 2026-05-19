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

// TestSubAgentTool_BashViaPolicy verifies that bash is available on a child
// agent when configured via Policies (not ToolNames). "bash" must NOT appear
// in ToolNames — it is not in the ToolRegistry and would cause a build error.
func TestSubAgentTool_BashViaPolicy(t *testing.T) {
	bashCfg := &BashToolConfig{
		PermCtx: &builtin_tools.BashPermissionContext{
			Mode:        builtin_tools.PermissionModeYOLO,
			ProjectPath: "/tmp/test",
		},
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
	)

	child, err := factory.Build(AgentDefinition{
		Name:        "child-bash-test",
		Instruction: "test bash forwarding",
		ToolNames:   []string{"read_file"},
		Policies: AgentPolicies{
			MaxIterations:        10,
			AllowBash:            true,
			BashPermissionContext: bashCfg,
		},
	})
	if err != nil {
		t.Fatalf("factory.Build failed: %v", err)
	}

	if _, ok := child.GetTool(builtin_tools.BashToolName); !ok {
		t.Fatal("child should have bash via policy")
	}
	if _, ok := child.GetTool("read_file"); !ok {
		t.Fatal("child should have read_file from registry")
	}
}

// TestSubAgentTool_ChildInheritsBash verifies that a sub-agent built by a
// parent with bash can also use bash. We can't easily call Execute (it runs
// the AI loop), so we verify the precondition: when the parent has BashTool
// configured, factory.Build with the same policy produces a child that has bash.
func TestSubAgentTool_ChildInheritsBash(t *testing.T) {
	bashCfg := &BashToolConfig{
		PermCtx: &builtin_tools.BashPermissionContext{
			Mode:        builtin_tools.PermissionModeYOLO,
			ProjectPath: "/tmp/test",
		},
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
	)

	parent, err := factory.Build(AgentDefinition{
		Name:        "parent-bash",
		Instruction: "parent with bash",
		ToolNames:   []string{"read_file"},
		Policies: AgentPolicies{
			AllowBash:            true,
			BashPermissionContext: bashCfg,
		},
	})
	if err != nil {
		t.Fatalf("build parent: %v", err)
	}
	if _, ok := parent.GetTool(builtin_tools.BashToolName); !ok {
		t.Fatal("parent should have bash")
	}

	// Build a child WITHOUT bash in Policies — should NOT have bash.
	childNoBash, err := factory.Build(AgentDefinition{
		Name:        "child-no-bash",
		Instruction: "child without bash policy",
		ToolNames:   []string{"read_file"},
		Policies:    AgentPolicies{MaxIterations: 10},
	})
	if err != nil {
		t.Fatalf("build child-no-bash: %v", err)
	}
	if _, ok := childNoBash.GetTool(builtin_tools.BashToolName); ok {
		t.Fatal("child without bash policy should NOT have bash")
	}

	// Build a child WITH bash in Policies (as SubAgentTool.Execute does) — should have bash.
	childWithBash, err := factory.Build(AgentDefinition{
		Name:        "child-with-bash",
		Instruction: "child with inherited bash policy",
		ToolNames:   []string{"read_file"},
		Policies: AgentPolicies{
			MaxIterations:        10,
			AllowBash:            true,
			BashPermissionContext: bashCfg,
		},
	})
	if err != nil {
		t.Fatalf("build child-with-bash: %v", err)
	}
	if _, ok := childWithBash.GetTool(builtin_tools.BashToolName); !ok {
		t.Fatal("child with bash policy should have bash")
	}
}

// TestSubAgentTool_FactoryBuildWithBashInToolNames_NoPanic verifies that
// calling factory.Build with "bash" in ToolNames does not panic or error,
// as long as AllowBash is set via Policies (the registry-based resolution
// is expected to fail for policy-managed names, which resolveChildToolNames
// strips before reaching the factory).
func TestSubAgentTool_FactoryBuildWithBashInToolNames_NoPanic(t *testing.T) {
	bashCfg := &BashToolConfig{
		PermCtx: &builtin_tools.BashPermissionContext{
			Mode:        builtin_tools.PermissionModeYOLO,
			ProjectPath: "/tmp/test",
		},
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
	)

	// Only registry-resolvable tools in ToolNames; bash via policy.
	child, err := factory.Build(AgentDefinition{
		Name:        "child-clean",
		Instruction: "test",
		ToolNames:   []string{"read_file"},
		Policies: AgentPolicies{
			MaxIterations:        10,
			AllowBash:            true,
			BashPermissionContext: bashCfg,
		},
	})
	if err != nil {
		t.Fatalf("build should succeed: %v", err)
	}

	if _, ok := child.GetTool(builtin_tools.BashToolName); !ok {
		t.Fatal("child should have bash from policy")
	}
	if _, ok := child.GetTool("read_file"); !ok {
		t.Fatal("child should have read_file from registry")
	}

	// With "bash" in ToolNames (not in registry) -> should error
	// because resolveChildToolNames is the caller's responsibility.
	_, err = factory.Build(AgentDefinition{
		Name:        "child-bad",
		Instruction: "test",
		ToolNames:   []string{"bash", "read_file"},
		Policies: AgentPolicies{
			MaxIterations:        10,
			AllowBash:            true,
			BashPermissionContext: bashCfg,
		},
	})
	if err == nil {
		t.Fatal("factory.Build with 'bash' in ToolNames should fail (not in registry)")
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
