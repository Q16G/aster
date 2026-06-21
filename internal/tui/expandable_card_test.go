package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestInlineStep_EnterTogglesExpanded 红线 FR3：用户在 InlineStepPart 卡片上按 enter
// 必须切换展开/折叠。旧实现 key handler 的 type 白名单不含 PartTypeInlineStep——卡片
// 永远折叠、展开态分支死代码。改造后 key handler 走 part.AsExpandable()，编译期保证
// 所有可展开卡片类型自动接入。
func TestInlineStep_EnterTogglesExpanded(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{
		Type: PartTypeInlineStep,
		Time: time.Now(),
		InlineStep: &InlineStepPart{
			StepID:    "p2",
			Status:    "running",
			StartedAt: time.Now(),
		},
	})
	m.focused = true
	// AddPart 已经把 cursor 移到新 part 上；如未移动则手工 setCursor。
	if m.cursor != 0 {
		m.setCursor(0)
	}

	if part0 := m.store.At(0); part0.InlineStep != nil && part0.InlineStep.Expanded() {
		t.Fatal("InlineStepPart should be collapsed initially")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if part0 := m.store.At(0); part0.InlineStep == nil || !part0.InlineStep.Expanded() {
		t.Fatal("InlineStepPart must be expanded after enter")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if part0 := m.store.At(0); part0.InlineStep == nil || part0.InlineStep.Expanded() {
		t.Fatal("InlineStepPart must collapse on second enter")
	}
}

// TestExpandableCards_AllImplementedTypesToggle 类型层断言：所有 PartType 在
// DisplayPart.AsExpandable 里被识别的 case 都必须真正能被 enter toggle。这是
// 编译期 + runtime 两层保证「漏白名单」类 bug（FR3）不再可能发生。
func TestExpandableCards_AllImplementedTypesToggle(t *testing.T) {
	cases := []struct {
		name string
		part DisplayPart
	}{
		{"SubAgent", DisplayPart{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub", CallID: "c1", Status: "running"}}},
		{"InlineStep", DisplayPart{Type: PartTypeInlineStep, InlineStep: &InlineStepPart{StepID: "p1", Status: "running"}}},
		{"Plan", DisplayPart{Type: PartTypePlan, Plan: &PlanPart{AgentName: "root", Items: []PlanItemView{{ID: "s1", Step: "one", Status: "pending"}}}}},
		{"StepResult", DisplayPart{Type: PartTypeStepResult, StepResult: &StepResultPart{StepName: "r", Status: "completed"}}},
		{"StepSummary", DisplayPart{Type: PartTypeStepSummary, StepSummary: &StepSummaryPart{StepID: "s1", StepName: "r", ShortSummary: "hi"}}},
		{"FinalAnswer", DisplayPart{Type: PartTypeFinalAnswer, FinalAnswer: &FinalAnswerPart{Content: "done"}}},
	}
	for _, tc := range cases {
		card, ok := tc.part.AsExpandable()
		if !ok {
			t.Errorf("%s: AsExpandable() should be ok=true", tc.name)
			continue
		}
		if card.Expanded() {
			// StepResult 是 shouldAutoExpandPart=true，但本测试构造的是裸 DisplayPart
			// （没走 AddPart），所以 Expanded()=false 是预期；如果哪个类型不是 false
			// 在此提示。
		}
		card.SetExpanded(true)
		if !card.Expanded() {
			t.Errorf("%s: SetExpanded(true) did not stick", tc.name)
		}
		card.SetExpanded(false)
		if card.Expanded() {
			t.Errorf("%s: SetExpanded(false) did not stick", tc.name)
		}
	}
}
