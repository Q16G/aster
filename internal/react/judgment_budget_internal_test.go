package react

import (
	"strings"
	"testing"
)

// TestJudgmentExplorationBudget_Sane 校验判定节点探索预算常量与降级提示的契约：
// 预算为正、宽限不小于 submit 重试空间、提示文案包含降级裁决与账本待复核指引。
func TestJudgmentExplorationBudget_Sane(t *testing.T) {
	if judgmentExplorationBudget <= 0 || judgmentGraceRounds <= 0 {
		t.Fatalf("budget/grace must be positive: budget=%d grace=%d", judgmentExplorationBudget, judgmentGraceRounds)
	}
	for _, want := range []string{"submit_plan", "digest-only", "ledger_add", "待复核"} {
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

// TestBuildSubmitReplanFunctionTool_MaintenanceDirectivesSchema 校验维护指令已进
// step_replan 输出 schema（可选字段、五类枚举）。
func TestBuildSubmitReplanFunctionTool_MaintenanceDirectivesSchema(t *testing.T) {
	tool := buildSubmitReplanFunctionTool()
	params := tool.Function.Parameters.(map[string]any)
	props := params["properties"].(map[string]any)
	md, ok := props["maintenance_directives"].(map[string]any)
	if !ok {
		t.Fatal("maintenance_directives missing from schema")
	}
	items := md["items"].(map[string]any)
	itemProps := items["properties"].(map[string]any)
	typeProp := itemProps["type"].(map[string]any)
	enum := typeProp["enum"].([]any)
	if len(enum) != 5 {
		t.Fatalf("expected 5 directive types, got %v", enum)
	}
	for _, required := range params["required"].([]string) {
		if required == "maintenance_directives" {
			t.Fatal("maintenance_directives must stay optional")
		}
	}
}
