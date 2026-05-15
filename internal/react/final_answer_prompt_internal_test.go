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

	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("请输出最终答案")},
		"show_plan":      false,
		"plan":           plan,
		"plan_version":   1,
		"step_outcomes":  collectAllStepContextViews(plan, stepOutcomes),
		"warnings":       []string{},
		"unresolved":     []string{},
	})
	if err != nil {
		t.Fatalf("BuildFinalAnswerPrompt failed: %v", err)
	}

	if !strings.Contains(prompt, `"result":"result-only-payload"`) {
		t.Fatalf("expected prompt to retain result in STEP_OUTCOMES, got:\n%s", prompt)
	}
}
