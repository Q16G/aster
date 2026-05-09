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
	agent.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepCompleted,
		Result: `{"total_findings":3}`,
	})

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

func TestLookupFinalAnswerOutputContract_ExplicitPublishContractWithoutOutcome(t *testing.T) {
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
			SummaryPolicy: "keep all findings",
		},
	}
	agent.currentPublishContract = "sast-findings"
	agent.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "audit", Status: builtin_tools.PlanStepPending, OutputContractRef: "sast-findings"},
	}, "", false)
	agent.EnsureCurrentStep()

	snap := agent.State()
	contract := agent.lookupFinalAnswerOutputContract(snap)
	if contract == nil {
		t.Fatal("explicit publishContract must resolve even without step outcomes")
	}
	if contract.Name != "sast-findings" {
		t.Fatalf("expected sast-findings, got %q", contract.Name)
	}
}

func TestBuildFinalAnswerPrompt_MultiContractPicksLastEligible(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		noopFinalAnswerPromptClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	agent.cfg.OutputContracts = map[string]*builtin_tools.OutputContract{
		"contract-a": {
			Name:          "contract-a",
			SummaryPolicy: "policy-a-detail",
		},
		"contract-b": {
			Name:          "contract-b",
			SummaryPolicy: "policy-b-detail",
		},
	}
	agent.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "step-1", Step: "first step", Status: builtin_tools.PlanStepPending, OutputContractRef: "contract-a"},
		{ID: "step-2", Step: "second step", Status: builtin_tools.PlanStepPending, OutputContractRef: "contract-b"},
	}, "", false)

	// step-1: ensure → complete → summary clears CurrentStepID
	agent.EnsureCurrentStep()
	agent.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepCompleted,
		Result: `{"from":"step-1"}`,
	})
	agent.state.ApplyStepSummary("step-1", stepSummaryUpdate{ShortSummary: "done"})

	// step-2: ensure → complete → summary clears CurrentStepID
	agent.EnsureCurrentStep()
	agent.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
		Status: builtin_tools.PlanStepCompleted,
		Result: `{"from":"step-2"}`,
	})
	agent.state.ApplyStepSummary("step-2", stepSummaryUpdate{ShortSummary: "done"})

	snap := agent.State()
	contract := agent.lookupFinalAnswerOutputContract(snap)
	if contract == nil {
		t.Fatal("expected a contract, got nil")
	}
	if contract.Name != "contract-b" {
		t.Fatalf("expected contract-b (last eligible), got %q", contract.Name)
	}

	result, ok, _ := latestNonEmptyStepResultWithPlan(snap.StepOutcomes, snap.Plan, "")
	if !ok {
		t.Fatal("expected a result from latestNonEmptyStepResultWithPlan")
	}
	if result != `{"from":"step-2"}` {
		t.Fatalf("expected result from step-2, got %q", result)
	}
}
