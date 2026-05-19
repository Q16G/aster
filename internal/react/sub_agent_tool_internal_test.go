package react

import (
	"context"
	"slices"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type stubClient struct{}

func (s *stubClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}
func (s *stubClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}
func (s *stubClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestResolveChildToolNames_FiltersPolicyManagedTools(t *testing.T) {
	bashCfg := &BashToolConfig{
		PermCtx: &builtin_tools.BashPermissionContext{
			Mode:        builtin_tools.PermissionModeYOLO,
			ProjectPath: "/tmp/test",
		},
	}

	parent, err := NewReActAgent("parent", &stubClient{},
		WithEmitter(NewDummyEmitter()),
		WithBashTool(bashCfg),
		WithTools(builtin_tools.NewReadFileTool()),
	)
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}

	registry := NewDefaultToolRegistry()
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(registry),
	)

	sub := NewSubAgentTool(parent, factory)

	tests := []struct {
		name      string
		requested []string
		wantIn    []string
		wantOut   []string
	}{
		{
			name:      "bash filtered from explicit request",
			requested: []string{"bash", "read_file"},
			wantIn:    []string{"read_file"},
			wantOut:   []string{"bash"},
		},
		{
			name:      "all policy-managed tools filtered",
			requested: []string{"bash", "sub_agent", "update_current_step", "task_status", "human_confirm", "skill", "read_file"},
			wantIn:    []string{"read_file"},
			wantOut:   []string{"bash", "sub_agent", "update_current_step", "task_status", "human_confirm", "skill"},
		},
		{
			name:      "registry tools pass through",
			requested: []string{"read_file", "list_files", "rg"},
			wantIn:    []string{"read_file", "list_files", "rg"},
			wantOut:   nil,
		},
		{
			name:      "empty request inherits domain tools and excludes platform tools",
			requested: nil,
			wantIn:    []string{"read_file"},
			wantOut:   []string{"bash", "sub_agent", "update_current_step", "task_status", "human_confirm"},
		},
		{
			name:      "unknown tools filtered by parent+registry check",
			requested: []string{"bash", "nonexistent_tool", "read_file"},
			wantIn:    []string{"read_file"},
			wantOut:   []string{"bash", "nonexistent_tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sub.resolveChildToolNames(tt.requested)
			for _, want := range tt.wantIn {
				if !slices.Contains(got, want) {
					t.Errorf("expected %q in result %v", want, got)
				}
			}
			for _, reject := range tt.wantOut {
				if slices.Contains(got, reject) {
					t.Errorf("expected %q NOT in result %v", reject, got)
				}
			}
		})
	}
}

func TestParentDomainToolNames_ExcludesInheritanceBlocked(t *testing.T) {
	bashCfg := &BashToolConfig{
		PermCtx: &builtin_tools.BashPermissionContext{
			Mode:        builtin_tools.PermissionModeYOLO,
			ProjectPath: "/tmp/test",
		},
	}

	parent, err := NewReActAgent("parent", &stubClient{},
		WithEmitter(NewDummyEmitter()),
		WithBashTool(bashCfg),
	)
	if err != nil {
		t.Fatalf("new parent: %v", err)
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
	)

	sub := NewSubAgentTool(parent, factory)
	names := sub.parentDomainToolNames()

	for _, blocked := range []string{
		builtin_tools.BashToolName,
		builtin_tools.SubAgentToolName,
		builtin_tools.UpdateCurrentStepToolName,
		builtin_tools.TaskStatusQueryToolName,
		builtin_tools.HumanConfirmToolName,
		builtin_tools.SkillToolName,
		builtin_tools.LoadSkillsToolName,
		builtin_tools.ListSkillsToolName,
		builtin_tools.DeleteSkillToolName,
	} {
		if slices.Contains(names, blocked) {
			t.Errorf("parentDomainToolNames should not contain %q, got %v", blocked, names)
		}
	}
}
