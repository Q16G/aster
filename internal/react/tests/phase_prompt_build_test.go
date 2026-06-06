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
		"用户可直接消费的总结",
		"无需把全部内部账本原样展开",
		"只有用户明确要求全量明细时",
	}
	for _, needle := range mustContain {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", needle, prompt)
		}
	}
	mustNotContain := []string{
		"同一把意图尺子",
		"step_replan 原则 6",
		"逐轴强制消费",
	}
	for _, needle := range mustNotContain {
		if strings.Contains(prompt, needle) {
			t.Fatalf("expected prompt to stop depending on cross-prompt wording %q, got:\n%s", needle, prompt)
		}
	}
	if strings.Contains(prompt, "<PLAN_VERSION>") || strings.Contains(prompt, "<PLAN>") {
		t.Fatalf("expected prompt to omit plan section when show_plan=false, got:\n%s", prompt)
	}

	// Fix3 (ungated): coverage_checklist consumed as a first-class归置 data source.
	for _, needle := range []string{
		"result.coverage_checklist",
		"referenced_prior_coverage",
		"uncovered",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected final_answer to consume coverage_checklist (missing %q), got:\n%s", needle, prompt)
		}
	}
	// Fix2 (ungated): summary-first放松不含 warnings; schema keeps 不得静默丢弃.
	for _, needle := range []string{
		"不在放松范围",
		"每条须在 user_message 中有对应归置，不得静默丢弃",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected final_answer to keep warnings归置 invariants (missing %q), got:\n%s", needle, prompt)
		}
	}
}

func TestBuildFinalAnswerPrompt_ReadsOpenItemsLedgerWhenWorkspace(t *testing.T) {
	agent, err := NewReActAgent(
		"prompt-agent",
		&executeModelTestClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	const shared = "/ws/shared"
	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":               "running",
		"state_error":          "",
		"input_timeline":       []*ai.MsgInfo{ai.NewUserMsgInfo("你好")},
		"show_plan":            false,
		"plan":                 []any{},
		"plan_version":         1,
		"step_outcomes":        []any{},
		"warnings":             []string{},
		"workspace_shared_dir": shared,
	})
	if err != nil {
		t.Fatalf("buildFinalAnswerPrompt (workspace) failed: %v", err)
	}

	// Fix1/Fix2: read open_items.md做总验收 + 不可解局限逐条归置.
	for _, needle := range []string{
		shared + "/open_items.md",
		"总验收",
		"## 不可解局限",
		"以账本为准",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected final_answer to read open_items ledger (missing %q), got:\n%s", needle, prompt)
		}
	}
	// Fix6: no coverage-ledger residue.
	for _, banned := range []string{"coverage-ledger", "coverage_ledger"} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("final_answer must not reference removed %q, got:\n%s", banned, prompt)
		}
	}

	noWorkspace, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         "running",
		"state_error":    "",
		"input_timeline": []*ai.MsgInfo{ai.NewUserMsgInfo("你好")},
		"show_plan":      false,
		"plan":           []any{},
		"plan_version":   1,
		"step_outcomes":  []any{},
		"warnings":       []string{},
	})
	if err != nil {
		t.Fatalf("buildFinalAnswerPrompt (no workspace) failed: %v", err)
	}
	if strings.Contains(noWorkspace, "open_items.md") {
		t.Fatalf("final_answer without workspace must not render open_items ledger, got:\n%s", noWorkspace)
	}
}
