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

func TestBuildFinalAnswerPrompt_PreservesResultFromSharedStepView(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		noopFinalAnswerPromptClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	plan := []*builtin_tools.PlanItem{
		{ID: "step-1", Step: "生成结果"},
	}
	stepOutcomes := []*builtin_tools.StepOutcome{
		{
			StepID:       "step-1",
			Status:       builtin_tools.StepOutcomeCompleted,
			ShortSummary: "摘要很短",
			Result:       "result-only-payload",
		},
	}

	// 烘焙形态：产出小字段随 plan_item 注入（A 阶段终态烘焙后），细节经指针回读。
	plan[0].BakeOutcome(stepOutcomes[0])
	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("请输出最终答案")},
		"plan_items":     ProjectPlanItemCards(plan, ""),
		"warnings":       []string{},
	})
	if err != nil {
		t.Fatalf("BuildFinalAnswerPrompt failed: %v", err)
	}

	if !strings.Contains(prompt, `"short_summary":"摘要很短"`) {
		t.Fatalf("expected prompt to carry baked short_summary in PLAN_ITEMS, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "<PLAN_ITEMS>") || !strings.Contains(prompt, "<OPEN_ITEMS_LEDGER>") {
		t.Fatalf("expected new injection sections, got:\n%s", prompt)
	}
}
