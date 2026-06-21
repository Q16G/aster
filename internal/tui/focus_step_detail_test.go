package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// 方案 B 红线（P5 修复后）：FocusStepDetail 是独立焦点档，不走 chat.Update——
// 详情态下任何按键（特别是 enter）都不应触发 chat 的 ExpandableCard.SetExpanded。
//
// 旧实现 BUG：FocusChat 案例下用 `if ViewingStepID() != ""` 在 case 内分叉，
// 但只拦截 h/l/esc——enter 漏过去，把 split 模式下 cursor 指向的卡片（StepResult/
// Plan/etc.）默默翻状态。新设计让详情态走独立 Update case，物理隔离。

// helper：构造一个 Model + chat 里有 InlineStep + 用户输入 Enter 进入详情态
func setupStepDetailModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(ModelDeps{})
	m.width, m.height = 100, 40
	m.updateLayout()

	// 加 user + 一个 StepResultPart（IsExpanded=true，PartTypeStepResult 自动展开）
	m.chat.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	m.chat.AddPart(DisplayPart{
		Type: PartTypeStepResult, Time: time.Now(),
		StepResult: &StepResultPart{StepName: "important step", Status: "completed", DisplayResult: "result"},
	})
	// 加一个 InlineStep + 子 part（让 EnterStepDetail 能成功）
	m.chat.AddPart(DisplayPart{
		Type: PartTypeInlineStep, Time: time.Now(),
		InlineStep: &InlineStepPart{StepID: "p1", Status: "running"},
	})
	m.chat.AddPart(DisplayPart{
		Type: PartTypeTool, Time: time.Now(),
		Tool: &ToolPart{Name: "bash", CallID: "c1", State: "completed", Result: "x", StepID: "p1"},
	})
	// 把 chat cursor 显式落在 StepResultPart（idx=1）模拟用户曾选中过
	m.chat.cursor = 1
	return &m
}

// TestFocusStepDetail_EnterDoesNotToggleExpandable 红线 P5：详情态下按 Enter
// 不应翻转 split 模式下 cursor 指向的 StepResult 卡片的展开状态。
func TestFocusStepDetail_EnterDoesNotToggleExpandable(t *testing.T) {
	model := setupStepDetailModel(t)

	// 记录 StepResult 进入详情前的展开状态
	stepResult := model.chat.store.parts[1].StepResult
	if stepResult == nil {
		t.Fatal("setup: parts[1] should be StepResult")
	}
	beforeExpanded := stepResult.Expanded()

	// 进入详情态：模拟 FocusTileBar Enter 路径
	model.tileBar.SetSnapshot(model.chat.store.InlineStepsThisTurn(), nil)
	model.setFocus(FocusTileBar)
	if !model.chat.EnterStepDetail("p1") {
		t.Fatal("EnterStepDetail(p1) should succeed")
	}
	model.setFocus(FocusStepDetail)
	if model.focus != FocusStepDetail {
		t.Fatalf("focus should be FocusStepDetail, got %v", model.focus)
	}

	// 详情态下按 Enter——不能触发任何 chat 的 ExpandableCard toggle
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(Model)

	afterExpanded := m2.chat.store.parts[1].StepResult.Expanded()
	if afterExpanded != beforeExpanded {
		t.Errorf("StepResult.Expanded() flipped during FocusStepDetail Enter: before=%v after=%v (P5 regression)",
			beforeExpanded, afterExpanded)
	}
	// 验证仍在详情态（Enter 在 detail 是 noop，不切焦点不退出）
	if m2.focus != FocusStepDetail {
		t.Errorf("focus changed away from FocusStepDetail on Enter; got %v", m2.focus)
	}
	if m2.chat.ViewingStepID() == "" {
		t.Errorf("ViewingStepID cleared on Enter; should remain detail mode")
	}
}

// TestFocusStepDetail_UnknownKeyNoop 详情态收到不识别按键（如字母 'a'）应 noop——
// 不会触发任何意外副作用。
func TestFocusStepDetail_UnknownKeyNoop(t *testing.T) {
	model := setupStepDetailModel(t)
	model.tileBar.SetSnapshot(model.chat.store.InlineStepsThisTurn(), nil)
	model.setFocus(FocusTileBar)
	if !model.chat.EnterStepDetail("p1") {
		t.Fatal("EnterStepDetail(p1) should succeed")
	}
	model.setFocus(FocusStepDetail)

	beforeFocus := model.focus
	beforeStepID := model.chat.ViewingStepID()

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m2 := updated.(Model)

	if m2.focus != beforeFocus {
		t.Errorf("unknown key changed focus: before=%v after=%v", beforeFocus, m2.focus)
	}
	if m2.chat.ViewingStepID() != beforeStepID {
		t.Errorf("unknown key changed ViewingStepID")
	}
}

// TestFocusStepDetail_EscReturnsToChat 详情态 Esc 退出回 FocusChat + 清 ViewingStepID。
func TestFocusStepDetail_EscReturnsToChat(t *testing.T) {
	model := setupStepDetailModel(t)
	model.tileBar.SetSnapshot(model.chat.store.InlineStepsThisTurn(), nil)
	model.setFocus(FocusTileBar)
	if !model.chat.EnterStepDetail("p1") {
		t.Fatal("EnterStepDetail(p1) should succeed")
	}
	model.setFocus(FocusStepDetail)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)

	if m2.focus != FocusChat {
		t.Errorf("after Esc focus=%v, want FocusChat", m2.focus)
	}
	if m2.chat.ViewingStepID() != "" {
		t.Errorf("after Esc ViewingStepID=%q, want \"\"", m2.chat.ViewingStepID())
	}
}
