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
		"是用户输入时间线，不是待办任务清单",
		"当前用户输入是否已得到合适响应",
		"当前输入的主要目标是建立对话、确认状态、获取说明或触发引导时，只要系统已给出恰当回应即视为完成",
		"不要把「等待用户输入」写成 `next_goal`",
		"`next_goal` 仅在确实需要 agent 继续执行时填写",
		"可直接交付的完整响应，高密度、不主动压缩",
		"账本 `## 不可解局限` 与输出字段 `warnings` 须逐条归置",
		"仅当用户明确要求全量明细或缺少明细会改变结论时才完整展开",
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
	if strings.Contains(prompt, "<PLAN_VERSION>") || strings.Contains(prompt, "<STEP_OUTCOMES>") {
		t.Fatalf("expected removed plan/outcome sections to stay absent, got:\n%s", prompt)
	}

	// coverage_checklist 作为一等归置数据源（来自 plan_item 卡片 / coverage_file 指针）。
	for _, needle := range []string{
		"coverage_checklist",
		"referenced_prior_coverage",
		"uncovered",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected final_answer to consume coverage_checklist (missing %q), got:\n%s", needle, prompt)
		}
	}
	// 不可解局限与 warnings 字段不在 summary-first 放松范围，不得静默丢弃。
	for _, needle := range []string{
		"摘要放松不适用",
		"不得静默丢弃",
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

	// 账本全文注入做总验收 + 不可解局限逐条归置。
	for _, needle := range []string{
		"<OPEN_ITEMS_LEDGER>",
		"总验收",
		"不可解局限",
		"权威载体",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected final_answer ledger injection (missing %q), got:\n%s", needle, prompt)
		}
	}
	// Fix6: no coverage-ledger residue.
	for _, banned := range []string{"coverage-ledger", "coverage_ledger"} {
		if strings.Contains(prompt, banned) {
			t.Fatalf("final_answer must not reference removed %q, got:\n%s", banned, prompt)
		}
	}

}
