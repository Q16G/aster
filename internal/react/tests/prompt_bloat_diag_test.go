package react_test

import (
	"context"
	"fmt"
	"testing"

	"aster/internal/builtin_tools"
	. "aster/internal/react"
	"aster/internal/service"
)

func TestPromptBloat_AgentBrowserImpact(t *testing.T) {
	svc := service.NewSkillServiceWithMemory()
	count, err := svc.ImportEmbeddedSkills(context.Background())
	if err != nil {
		t.Fatalf("ImportEmbeddedSkills failed: %v", err)
	}
	t.Logf("imported %d embedded skills", count)

	ctx := context.Background()

	// --- Injected section size comparison ---
	auditSkills := []string{"security-code-analysis", "result-with-file"}
	browserSkills := []string{"security-code-analysis", "result-with-file", "agent-browser"}

	auditSection, err := svc.BuildInjectedSkillsSection(ctx, nil, auditSkills)
	if err != nil {
		t.Fatalf("BuildInjectedSkillsSection (audit) failed: %v", err)
	}
	browserSection, err := svc.BuildInjectedSkillsSection(ctx, nil, browserSkills)
	if err != nil {
		t.Fatalf("BuildInjectedSkillsSection (browser) failed: %v", err)
	}

	t.Logf("=== Injected Section Size ===")
	t.Logf("Audit only:        %d bytes", len(auditSection))
	t.Logf("Audit + browser:   %d bytes", len(browserSection))
	t.Logf("Delta:             +%d bytes (+%.1f%%)",
		len(browserSection)-len(auditSection),
		float64(len(browserSection)-len(auditSection))/float64(len(auditSection)+1)*100)

	// --- Full prompt size comparison ---
	makeAgent := func(name string, activeSkills []string) *Agent {
		provider := SkillsPromptProviderFunc(
			func(_ context.Context, _ string, _ builtin_tools.StateSnapshot) (*SkillsPromptContext, error) {
				table, _ := svc.BuildSkillsTableWithStatus(ctx, "all", nil, activeSkills)
				injected, _ := svc.BuildInjectedSkillsSection(ctx, nil, activeSkills)
				return &SkillsPromptContext{Table: table, Injected: injected}, nil
			},
		)
		agent, err := NewReActAgent(
			name,
			&stubChatClient{},
			WithEmitter(NewDummyEmitter()),
			WithInstruction("你是安全审计 Agent"),
			WithSkillsPromptProvider(provider),
		)
		if err != nil {
			t.Fatalf("NewReActAgent %s failed: %v", name, err)
		}
		agent.ReplaceState(builtin_tools.StateSnapshot{
			Phase:            builtin_tools.AgentPhaseStep,
			Status:           builtin_tools.TaskStatusRunning,
			CurrentGoal:      "审计项目代码",
			CurrentStepID:    "step-1",
			ActiveSkillNames: activeSkills,
			Plan: []*builtin_tools.PlanItem{
				{ID: "step-1", Step: "审计代码", Status: builtin_tools.PlanStepInProgress},
			},
		})
		return agent
	}

	auditAgent := makeAgent("audit-only", auditSkills)
	browserAgent := makeAgent("audit-with-browser", browserSkills)

	auditPrompt := auditAgent.BuildThinkActPrompt(ctx, "", nil)
	browserPrompt := browserAgent.BuildThinkActPrompt(ctx, "", nil)

	estTokens := func(s string) int { return len(s) * 10 / 35 }

	t.Logf("")
	t.Logf("=== Full ThinkAct Prompt Size ===")
	t.Logf("Audit only:        %d bytes (~%d tokens)", len(auditPrompt), estTokens(auditPrompt))
	t.Logf("Audit + browser:   %d bytes (~%d tokens)", len(browserPrompt), estTokens(browserPrompt))
	delta := len(browserPrompt) - len(auditPrompt)
	t.Logf("Delta:             +%d bytes (+%d tokens, +%.1f%%)",
		delta, estTokens(fmt.Sprintf("%*s", delta, "")),
		float64(delta)/float64(len(auditPrompt)+1)*100)

	// --- Also measure agent-browser alone ---
	browserOnly, _ := svc.BuildInjectedSkillsSection(ctx, nil, []string{"agent-browser"})
	t.Logf("")
	t.Logf("=== agent-browser Skill Alone ===")
	t.Logf("Injected size:     %d bytes (~%d tokens)", len(browserOnly), estTokens(browserOnly))
}
