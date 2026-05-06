package react

import (
	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"context"
	"strings"
	"testing"
)

type noopFinalAnswerPromptClient struct{}

func (noopFinalAnswerPromptClient) Chat(context.Context, *ai.MsgInfo, ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (noopFinalAnswerPromptClient) ChatEx(context.Context, []*ai.MsgInfo, ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (noopFinalAnswerPromptClient) ChatText(context.Context, string, ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestBuildFinalAnswerPrompt_IncludesSummaryPolicyForSASTFindings(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		noopFinalAnswerPromptClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	agent.cfg.OutputContracts = map[string]*builtin_tools.OutputContract{
		"sast-findings": {
			Name:          "sast-findings",
			SummaryPolicy: "禁止压缩或省略 findings 列表；total_findings 和 severity_counts 必须原样保留。",
		},
	}
	agent.currentPublishContract = "sast-findings"
	agent.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "执行安全审计", Status: builtin_tools.PlanStepPending, OutputContractRef: "sast-findings"},
	}, "", false)
	agent.EnsureCurrentStep()

	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("输出标准安全审计报告")},
		"show_plan":      false,
		"plan":           agent.State().Plan,
		"plan_version":   1,
		"step_outcomes":  []any{},
		"warnings":       []string{},
		"unresolved":     []string{},
	})
	if err != nil {
		t.Fatalf("BuildFinalAnswerPrompt failed: %v", err)
	}

	if !strings.Contains(prompt, "output contract `sast-findings`") {
		t.Fatalf("expected prompt to mention summary policy contract, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "禁止压缩或省略 findings 列表") {
		t.Fatalf("expected prompt to inline summary policy detail, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "`sast-findings` 标准 Markdown 报告版式") {
		t.Fatalf("expected prompt to include sast-findings markdown report guidance, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "执行摘要") || !strings.Contains(prompt, "漏洞详情") {
		t.Fatalf("expected prompt to include markdown report sections, got:\n%s", prompt)
	}
}
