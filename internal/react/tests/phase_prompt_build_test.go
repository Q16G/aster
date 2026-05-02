package react_test

import (
	. "aster/internal/react"
	"strings"
	"testing"

	"aster/internal/ai"
)

func TestBuildFinalAnswerPrompt_EmphasizesInputTimelineCompletion(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		&executeModelTestClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("你好")},
		"show_plan":      false,
		"plan":           []any{},
		"plan_version":   1,
		"step_outcomes":  []any{},
		"warnings":       []string{},
		"unresolved":     []string{},
	})
	if err != nil {
		t.Fatalf("buildFinalAnswerPrompt failed: %v", err)
	}

	mustContain := []string{
		"`INPUT_TIMELINE` 是用户输入时间线，不是待办任务清单。",
		"完成性判断必须优先回答：当前用户输入是否已经得到合适响应",
		"若当前用户输入的主要目标是建立对话、确认状态、获取说明、了解能力或触发下一步引导，而不是要求系统立即展开新的内部执行，则只要系统已经给出恰当回应，这一轮就应视为**已完成对当前输入的响应**。",
		"不要把“等待用户输入”写成新的执行目标",
		"只有在确实需要 agent 继续执行时填写；不要把“等待用户输入”写进 `next_goal`。",
	}
	for _, needle := range mustContain {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", needle, prompt)
		}
	}
	if strings.Contains(prompt, "<PLAN_VERSION>") || strings.Contains(prompt, "<PLAN>") {
		t.Fatalf("expected prompt to omit plan section when show_plan=false, got:\n%s", prompt)
	}
}
