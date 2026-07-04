package react

import (
	"strings"
	"testing"
)

// 共用的「两种合法形态」段必须出现在两条分支死锁报错里——目的是让 LLM 在
// needs_planning=true/false 之间反复翻转时看到全貌、按任务实际状态自决。
func assertShapeReminder(t *testing.T, errText string) {
	t.Helper()
	for _, marker := range []string{
		"submit_plan 的两种合法形态",
		"需要规划：needs_planning=true",
		"不需要规划：needs_planning=false",
	} {
		if !strings.Contains(errText, marker) {
			t.Errorf("missing shape reminder marker %q in error:\n%s", marker, errText)
		}
	}
}

// TestParseSubmitPlanArgs_PlanEmpty_GuidesBothBranches 校验
// needs_planning=true + plan 空时,报错既给出补 plan 的正路径,
// 也提示「翻成 needs_planning=false + direct_response」的反路径,
// 让模型能根据任务实际状态自决,不被锁死在「需要规划」分支。
func TestParseSubmitPlanArgs_PlanEmpty_GuidesBothBranches(t *testing.T) {
	_, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"explanation":        "复杂任务",
		"goal_understanding": "核心目标: 多模块审计",
		"plan":               []map[string]any{},
	}, true, nil, nil)
	if err == nil {
		t.Fatal("expected error when needs_planning=true but plan is empty")
	}
	text := err.Error()
	assertShapeReminder(t, text)
	for _, marker := range []string{
		"needs_planning=true 但 plan 为空",
		"补全 plan 字段",
		"或把 needs_planning 改为 false",
		"direct_response",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("missing per-error guidance marker %q in error:\n%s", marker, text)
		}
	}
}

// TestParseSubmitPlanArgs_DirectResponseEmpty_GuidesBothBranches 校验
// 这条 session 死锁的根因报错:needs_planning=false + direct_response 空。
// 必须给出补 direct_response 的正路径 和「翻成 needs_planning=true + plan」的反路径。
func TestParseSubmitPlanArgs_DirectResponseEmpty_GuidesBothBranches(t *testing.T) {
	_, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":  false,
		"explanation":     "无需规划",
		"direct_response": "",
	}, true, nil, nil)
	if err == nil {
		t.Fatal("expected error when needs_planning=false but direct_response is empty")
	}
	text := err.Error()
	assertShapeReminder(t, text)
	for _, marker := range []string{
		"needs_planning=false 但 direct_response 为空",
		"补全 direct_response",
		"或把 needs_planning 改为 true",
		"计划步骤写入 plan",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("missing per-error guidance marker %q in error:\n%s", marker, text)
		}
	}
}

// TestParseSubmitPlanArgs_GoalUnderstandingMissing_NoShapeReminder 校验
// 不涉及分支选择的错误(goal_understanding 必填)不带共用形态段——
// 形态段只对 needs_planning 分支二选一死锁场景有意义,
// 滥用会稀释具体指引的注意力。
func TestParseSubmitPlanArgs_GoalUnderstandingMissing_NoShapeReminder(t *testing.T) {
	_, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning": true,
		"explanation":    "需要规划",
		"plan": []map[string]any{
			{"id": "step-1", "step": "枚举模块 A 接口", "status": "pending", "depends_on": []string{}},
		},
		// goal_understanding 缺失
	}, true, nil, nil)
	if err == nil {
		t.Fatal("expected error when goal_understanding missing")
	}
	text := err.Error()
	if strings.Contains(text, "submit_plan 的两种合法形态") {
		t.Errorf("goal_understanding error must NOT carry shape reminder (it's a same-branch fix, not a flip-flop case):\n%s", text)
	}
	for _, marker := range []string{
		"goal_understanding 为空",
		"七要素",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("missing specific guidance marker %q in error:\n%s", marker, text)
		}
	}
}

// TestParseSubmitPlanArgs_PhasesMissing_NoShapeReminder 校验同理:
// phases 缺失也是同分支补字段,不应带形态段。
func TestParseSubmitPlanArgs_PhasesMissing_NoShapeReminder(t *testing.T) {
	_, err := parseSubmitPlanArgs(map[string]any{
		"needs_planning":     true,
		"explanation":        "复杂任务",
		"goal_understanding": "核心目标: 多模块审计",
		"plan": []map[string]any{
			{"id": "step-1", "step": "枚举模块 A 接口", "status": "pending", "phase_id": "phase-a", "depends_on": []string{}},
		},
		// phases 缺失
	}, true, nil, nil)
	if err == nil {
		t.Fatal("expected error when phases missing")
	}
	text := err.Error()
	if strings.Contains(text, "submit_plan 的两种合法形态") {
		t.Errorf("phases error must NOT carry shape reminder (it's a same-branch fix):\n%s", text)
	}
	if !strings.Contains(text, "phases 必填") {
		t.Errorf("missing specific guidance marker %q in error:\n%s", "phases 必填", text)
	}
}
