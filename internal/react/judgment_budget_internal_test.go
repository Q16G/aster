package react

import (
	"testing"
)

// TestBuildSubmitReplanFunctionTool_NoMaintenanceDirectives 校验维护指令机制已废除：
// schema 不再含 maintenance_directives（共享区由 AI 直接维护，不走机械执行）。
// 原本断言 judgmentExplorationBudget / judgmentGraceRounds / *BudgetNotice 文案
// 的契约已删除：思考预算上限已被移除，runaway 防线靠 ctx 取消 + MaxIterations 兜底。
func TestSubmitReplanTool_NoMaintenanceDirectives(t *testing.T) {
	tool := newSubmitReplanTool(nil, nil)
	params := tool.Parameters().(map[string]any)
	props := params["properties"].(map[string]any)
	if _, ok := props["maintenance_directives"]; ok {
		t.Fatal("maintenance_directives must be removed from schema")
	}
}
