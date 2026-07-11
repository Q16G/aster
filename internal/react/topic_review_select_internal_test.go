package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// TestNextReviewableTopic（Inc3 基石）验证局部 review 的 topic 选择：
// 静默且未在本静默点 review 过 → 选中并给出最新 step 边界；已 review 过/未静默/未解锁/终态 → 跳过。
func TestNextReviewableTopic(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "a1", PhaseID: "topic-a", Status: builtin_tools.PlanStepCompleted},
		{ID: "a2", PhaseID: "topic-a", Status: builtin_tools.PlanStepCompleted}, // a 静默，最新 a2
		{ID: "b1", PhaseID: "topic-b", Status: builtin_tools.PlanStepInProgress}, // b 未静默
	}
	phases := []*builtin_tools.PlanPhase{
		{ID: "topic-a", Status: builtin_tools.PlanPhasePending},
		{ID: "topic-b", Status: builtin_tools.PlanPhasePending},
	}

	// 无边界：topic-a 可 review，边界=a2。
	id, latest := nextReviewableTopic(plan, phases, map[string]string{})
	if id != "topic-a" || latest != "a2" {
		t.Fatalf("应选 topic-a 边界 a2，got (%q,%q)", id, latest)
	}

	// 边界已到 a2（该静默点已 review 过）→ 跳过，无可 review。
	id, _ = nextReviewableTopic(plan, phases, map[string]string{"topic-a": "a2"})
	if id != "" {
		t.Fatalf("topic-a 已 review 过该静默点，应无可 review，got %q", id)
	}

	// 边界停在 a1（a2 是新 terminal）→ 仍可 review topic-a。
	id, latest = nextReviewableTopic(plan, phases, map[string]string{"topic-a": "a1"})
	if id != "topic-a" || latest != "a2" {
		t.Fatalf("a2 为新 terminal，应可 review topic-a 到 a2，got (%q,%q)", id, latest)
	}

	// topic-a 终态（completed）→ 非 active → 不可 review。
	phasesDone := []*builtin_tools.PlanPhase{
		{ID: "topic-a", Status: builtin_tools.PlanPhaseCompleted},
		{ID: "topic-b", Status: builtin_tools.PlanPhasePending},
	}
	if id, _ := nextReviewableTopic(plan, phasesDone, map[string]string{}); id != "" {
		t.Fatalf("topic-a 已终态，不应可 review，got %q", id)
	}
}

// TestLatestStepIDOfTopic 验证取 plan 顺序中该 topic 的最后一个 step id。
func TestLatestStepIDOfTopic(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "a1", PhaseID: "topic-a"},
		{ID: "b1", PhaseID: "topic-b"},
		{ID: "a2", PhaseID: "topic-a"},
	}
	if got := latestStepIDOfTopic(plan, "topic-a"); got != "a2" {
		t.Errorf("topic-a 最新应为 a2，got %q", got)
	}
	if got := latestStepIDOfTopic(plan, "topic-none"); got != "" {
		t.Errorf("无该 topic 应返回空，got %q", got)
	}
}
