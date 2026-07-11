package react

import (
	"strings"
	"testing"
)

// sumFieldTokens 计 priority 内字段 Text 的 token 之和。
func sumFieldTokens(fields ...*PreviewField) int {
	n := 0
	for _, f := range fields {
		n += countTokens(f.Text)
	}
	return n
}

// TestApplyInjectionBudget_NoOpWithinBudget 未超预算时不降级、全字段保正文。
func TestApplyInjectionBudget_NoOpWithinBudget(t *testing.T) {
	a := &Agent{}
	high := PreviewField{Text: "高优正文", Path: "/ws/a"}
	low := PreviewField{Text: "低优正文", Path: "/ws/b"}
	a.applyInjectionBudget([]injectionField{{field: &low}, {field: &high}}, 100000)
	if high.Truncated || low.Truncated {
		t.Fatalf("未超预算不应降级：low.Truncated=%v high.Truncated=%v", low.Truncated, high.Truncated)
	}
	if high.Text != "高优正文" || low.Text != "低优正文" {
		t.Fatalf("未超预算正文应原样保留")
	}
}

// TestApplyInjectionBudget_DegradeLowFirst 超预算时按 priority 低→高降级；高优保正文；
// 降级字段变纯指针且 NeedsPointer()=true；收敛后 Σ ≤ budget。
func TestApplyInjectionBudget_DegradeLowFirst(t *testing.T) {
	a := &Agent{}
	body := strings.Repeat("很长的中文正文内容行，", 40)
	low := PreviewField{Text: body, Path: "/ws/low.md"}
	mid := PreviewField{Text: body, Path: "/ws/mid.md"}
	high := PreviewField{Text: body, Path: "/ws/high.md"}

	// budget 设为约 2 个字段正文可容、3 个必超 → 逼降级最低优的 low（必要时含 mid）。
	one := countTokens(body)
	budget := one*2 + one/2

	a.applyInjectionBudget([]injectionField{{field: &low}, {field: &mid}, {field: &high}}, budget)

	if !low.Truncated {
		t.Fatalf("最低优字段应先降级为指针")
	}
	if !low.NeedsPointer() || low.PointerPath() != "/ws/low.md" {
		t.Fatalf("降级字段应 NeedsPointer 且 PointerPath 指真相源，got path=%q", low.PointerPath())
	}
	if !strings.Contains(low.Text, "/ws/low.md") {
		t.Fatalf("降级字段 Text 应为纯指针含真相源路径，got %q", low.Text)
	}
	if high.Truncated {
		t.Fatalf("最高优字段不应被降级（budget 足够容纳其余）")
	}
	if got := sumFieldTokens(&low, &mid, &high); got > budget {
		t.Fatalf("聚合封顶后 Σ=%d 应 ≤ budget=%d", got, budget)
	}
}

// TestApplyInjectionBudget_SpillMemoryField 无 Path 的内存字段降级前先 spill 取得 Path。
func TestApplyInjectionBudget_SpillMemoryField(t *testing.T) {
	a, _ := newPromptContextTestAgent(t, 5000)
	body := strings.Repeat("内存字段长正文行，", 60)
	mem := PreviewField{Text: body} // 无 Path（模拟 ≤单字段限但参与聚合超限的内存字段）
	a.applyInjectionBudget([]injectionField{{field: &mem, spillName: "warnings"}}, 1)
	if !mem.Truncated {
		t.Fatalf("内存字段应被降级")
	}
	if mem.Path == "" || !strings.Contains(mem.Text, mem.Path) {
		t.Fatalf("内存字段降级应先 spill 取得 Path 并指针化，got path=%q text=%q", mem.Path, tail(mem.Text, 80))
	}
}

// TestApplyInjectionBudget_ZeroBudgetNoOp budget<=0 不封顶。
func TestApplyInjectionBudget_ZeroBudgetNoOp(t *testing.T) {
	a := &Agent{}
	f := PreviewField{Text: "正文", Path: "/ws/x"}
	a.applyInjectionBudget([]injectionField{{field: &f}}, 0)
	if f.Truncated {
		t.Fatalf("budget<=0 不应降级")
	}
}

// fieldPtrs 取 injectionField 列表里的字段指针集合，便于断言包含关系。
func fieldPtrs(fields []injectionField) map[*PreviewField]bool {
	m := make(map[*PreviewField]bool, len(fields))
	for _, f := range fields {
		m[f.field] = true
	}
	return m
}

// TestPlanInjectionBudgetFields_OnlyInjectedFields（M2 回归）验证 plan 阶段预算只对本回合
// 确实会注入的字段记账：regenGoal 时排除 GoalUnderstanding、非顶层/无 workspace 时排除
// TaskContextBoard——否则高估总量、过度降级 InputTimeline/Plan。
func TestPlanInjectionBudgetFields_OnlyInjectedFields(t *testing.T) {
	pc := &PromptContext{}

	// 常规回合：全字段纳入，顺序低→高（Topics、TaskContextBoard、GU、InputTimeline、Plan）。
	full := planInjectionBudgetFields(pc, false, true)
	if len(full) != 5 {
		t.Fatalf("常规回合应纳入 5 字段，got %d", len(full))
	}
	if full[0].field != &pc.Topics || full[len(full)-1].field != &pc.Plan {
		t.Fatalf("优先级序应 Topics 先降级、Plan 最后降级")
	}

	// regenGoal：GoalUnderstanding 本回合不注入 → 不计入预算。
	regen := fieldPtrs(planInjectionBudgetFields(pc, true, true))
	if regen[&pc.GoalUnderstanding] {
		t.Errorf("regenGoal 回合 GoalUnderstanding 不注入，不应计入预算")
	}
	if !regen[&pc.TaskContextBoard] || !regen[&pc.Plan] {
		t.Errorf("regenGoal 回合仍应保留 TaskContextBoard/Plan")
	}

	// 子 Agent / 无 workspace：TaskContextBoard 本回合不注入 → 不计入预算。
	noBoard := fieldPtrs(planInjectionBudgetFields(pc, false, false))
	if noBoard[&pc.TaskContextBoard] {
		t.Errorf("injectsTaskBoard=false 时 TaskContextBoard 不注入，不应计入预算")
	}
	if !noBoard[&pc.GoalUnderstanding] {
		t.Errorf("injectsTaskBoard=false 仍应保留 GoalUnderstanding")
	}
}
