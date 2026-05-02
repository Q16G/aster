package react

import (
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/service"
)

type skillRuntimeStubChatClient struct{}

func (s *skillRuntimeStubChatClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (s *skillRuntimeStubChatClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (s *skillRuntimeStubChatClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (s *skillRuntimeStubChatClient) ModelContextInfo() ai.ModelContextInfo {
	return ai.ModelContextInfo{}
}

func TestBuildThinkActPrompt_UsesDynamicSkillsTableAndInjectedSkills(t *testing.T) {
	agent, err := NewReActAgent(
		"skill-prompt-test",
		&skillRuntimeStubChatClient{},
		WithEmitter(NewDummyEmitter()),
		WithInstruction("你是技能测试代理"),
		WithSkillsPromptProvider(SkillsPromptProviderFunc(func(_ context.Context, _ string, snapshot builtin_tools.StateSnapshot) (*SkillsPromptContext, error) {
			table := "| name | desc | trigger_keywords | status |\n| --- | --- | --- | --- |\n| data-flow | 数据流分析 | flow | available |"
			if len(snapshot.ActiveSkillNames) > 0 {
				table = "| name | desc | trigger_keywords | status |\n| --- | --- | --- | --- |\n| data-flow | 数据流分析 | flow | loaded |"
			}
			if len(snapshot.ActiveSkillNames) == 0 {
				return &SkillsPromptContext{Table: table}, nil
			}
			return &SkillsPromptContext{
				Table:    table,
				Injected: "#### data-flow\n- description: 数据流分析\n- version: 1.0.0\n\nfollow flows",
			}, nil
		})),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:            builtin_tools.AgentPhaseStep,
		Status:           builtin_tools.TaskStatusRunning,
		CurrentGoal:      "继续分析",
		ActiveSkillNames: []string{"data-flow"},
	})

	prompt := agent.BuildThinkActPrompt("", nil)
	for _, expected := range []string{
		"### 5.3 Skills 索引",
		"| data-flow | 数据流分析 | flow | loaded |",
		"### 5.3b Injected Skills",
		"#### data-flow",
		"follow flows",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", expected, prompt)
		}
	}
}

func TestHandleSkillToolStateSync_LoadAndDeletePersistActiveSkillNames(t *testing.T) {
	agent, err := NewReActAgent(
		"skill-runtime-test",
		&skillRuntimeStubChatClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("ses-skill-runtime", rootDir, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime failed: %v", err)
	}
	agent.workspaceRuntime = runtime
	agent.workspaceSessionID = "ses-skill-runtime"
	agent.workspaceRootDir = rootDir

	agent.handleSkillToolStateSync(
		builtin_tools.LoadSkillsToolName,
		map[string]any{"names": []any{"data-flow", "syntaxflow-syntax-guide"}},
		`{"ok":true,"count":2,"skills":[{"name":"data-flow"},{"name":"syntaxflow-syntax-guide"},{"name":"data-flow"}]}`,
		"",
	)

	snapshot := agent.State()
	if got := strings.Join(snapshot.ActiveSkillNames, ","); got != "data-flow,syntaxflow-syntax-guide" {
		t.Fatalf("unexpected active skills after load: %q", got)
	}

	workspaceState, err := runtime.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("LoadWorkspaceState failed: %v", err)
	}
	if got := strings.Join(workspaceState.ActiveSkillNames, ","); got != "data-flow,syntaxflow-syntax-guide" {
		t.Fatalf("unexpected persisted active skills after load: %q", got)
	}

	agent.handleSkillToolStateSync(
		builtin_tools.DeleteSkillToolName,
		map[string]any{"name": "data-flow"},
		`{"ok":true,"name":"data-flow"}`,
		"",
	)

	snapshot = agent.State()
	if got := strings.Join(snapshot.ActiveSkillNames, ","); got != "syntaxflow-syntax-guide" {
		t.Fatalf("unexpected active skills after delete: %q", got)
	}

	workspaceState, err = runtime.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("LoadWorkspaceState(after delete) failed: %v", err)
	}
	if got := strings.Join(workspaceState.ActiveSkillNames, ","); got != "syntaxflow-syntax-guide" {
		t.Fatalf("unexpected persisted active skills after delete: %q", got)
	}
}

func TestNewSkillsPromptProviderFromCatalog_BuildsTableAndInjectedSection(t *testing.T) {
	svc := service.NewSkillServiceWithMemory()
	enabled := true
	if err := svc.ImportSkill(context.Background(), &service.MCPSkill{
		Name:         "data-flow",
		Description:  "数据流分析",
		Instructions: "follow flows",
		Version:      "1.0.0",
		Enabled:      &enabled,
		Agent:        "skill-runtime-test",
		WhenToUse:    "flow",
	}); err != nil {
		t.Fatalf("ImportSkill failed: %v", err)
	}

	provider := NewSkillsPromptProviderFromCatalog(svc)
	if provider == nil {
		t.Fatalf("expected provider")
	}

	ctx, err := provider.BuildSkillsPrompt(context.Background(), "skill-runtime-test", builtin_tools.StateSnapshot{
		ActiveSkillNames: []string{"data-flow"},
	})
	if err != nil {
		t.Fatalf("BuildSkillsPrompt failed: %v", err)
	}
	if ctx == nil {
		t.Fatalf("expected prompt context")
	}
	if !strings.Contains(ctx.Table, "| data-flow | 数据流分析 | flow | inline | loaded |") {
		t.Fatalf("unexpected skills table: %s", ctx.Table)
	}
	if !strings.Contains(ctx.Injected, "follow flows") {
		t.Fatalf("unexpected injected skills section: %s", ctx.Injected)
	}
}
