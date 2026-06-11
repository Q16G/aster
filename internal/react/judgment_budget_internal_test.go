package react

import (
	"strings"
	"testing"
)

// TestJudgmentExplorationBudget_Sane 校验判定节点探索预算常量与降级提示的契约：
// 预算为正、宽限不小于 submit 重试空间、提示文案包含降级裁决与缺漏不静默丢弃指引。
func TestJudgmentExplorationBudget_Sane(t *testing.T) {
	if judgmentExplorationBudget <= 0 || judgmentGraceRounds <= 0 {
		t.Fatalf("budget/grace must be positive: budget=%d grace=%d", judgmentExplorationBudget, judgmentGraceRounds)
	}
	for _, want := range []string{"submit_plan", "digest-only", "不得静默丢弃"} {
		if !strings.Contains(stepReplanBudgetNotice, want) {
			t.Fatalf("step replan budget notice missing %q: %s", want, stepReplanBudgetNotice)
		}
	}
	for _, want := range []string{"submit_final_answer", "仅初步"} {
		if !strings.Contains(finalAnswerBudgetNotice, want) {
			t.Fatalf("final answer budget notice missing %q: %s", want, finalAnswerBudgetNotice)
		}
	}
}

// TestBuildSubmitReplanFunctionTool_NoMaintenanceDirectives 校验维护指令机制已废除：
// schema 不再含 maintenance_directives（共享区由 AI 直接维护，不走机械执行）。
func TestBuildSubmitReplanFunctionTool_NoMaintenanceDirectives(t *testing.T) {
	tool := buildSubmitReplanFunctionTool()
	params := tool.Function.Parameters.(map[string]any)
	props := params["properties"].(map[string]any)
	if _, ok := props["maintenance_directives"]; ok {
		t.Fatal("maintenance_directives must be removed from schema")
	}
}
