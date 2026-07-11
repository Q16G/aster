package react

import (
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

// TestValidateStepDepsSameTopic（V4 二层硬边界）验证 plan[].depends_on 只允许同 topic 引用：
// 同 topic 依赖通过、跨 topic 依赖被拒（合并反馈）、缺 phase_id/未知依赖不误报。
func TestValidateStepDepsSameTopic(t *testing.T) {
	// 同 topic 依赖 + 无依赖 → 通过。
	okPlan := []*builtin_tools.PlanItem{
		{ID: "a1", PhaseID: "topic-a"},
		{ID: "a2", PhaseID: "topic-a", DependsOn: []string{"a1"}},
		{ID: "b1", PhaseID: "topic-b"},
	}
	if err := validateStepDepsSameTopic(okPlan); err != nil {
		t.Fatalf("同 topic 依赖不应报错，got %v", err)
	}

	// 跨 topic 依赖 → 拒绝，反馈含两端 step id。
	crossPlan := []*builtin_tools.PlanItem{
		{ID: "a1", PhaseID: "topic-a"},
		{ID: "b1", PhaseID: "topic-b", DependsOn: []string{"a1"}},
	}
	err := validateStepDepsSameTopic(crossPlan)
	if err == nil {
		t.Fatal("跨 topic step 依赖应被拒绝")
	}
	if !strings.Contains(err.Error(), "b1") || !strings.Contains(err.Error(), "a1") {
		t.Errorf("反馈应点名跨 topic 的两端 step，got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "phases[].depends_on") {
		t.Errorf("反馈应引导改用 topic 依赖，got %q", err.Error())
	}

	// 缺 phase_id / 未知依赖 → 本条不误报（交上游校验处理）。
	lenientPlan := []*builtin_tools.PlanItem{
		{ID: "x1", PhaseID: ""},                                  // 缺 phase_id
		{ID: "x2", PhaseID: "topic-a", DependsOn: []string{"x1"}}, // 依赖缺 phase_id 的 step
		{ID: "x3", PhaseID: "topic-a", DependsOn: []string{"ghost"}}, // 未知依赖
	}
	if err := validateStepDepsSameTopic(lenientPlan); err != nil {
		t.Fatalf("缺 phase_id / 未知依赖不应由 V4 报错（偏放行），got %v", err)
	}
}
