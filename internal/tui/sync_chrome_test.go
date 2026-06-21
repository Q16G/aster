package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// 方案 A 红线（P1+P2 修复后）：syncChrome 出口兜底——任何 store 变化或 focus
// 不一致都在 Update wrapper 出口被收敛。

// TestSyncChrome_PeerAllDone_FocusFallsBack 红线 P2：peer 全完成 → tile bar
// 数据变空 → tileBarVisible=false → focus 仍 stuck FocusTileBar 应自动回退 FocusChat。
func TestSyncChrome_PeerAllDone_FocusFallsBack(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.width, m.height = 100, 40
	m.updateLayout()
	m.chat.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	// peer p1 进入 InlineStepsThisTurn
	m.chat.AddPart(DisplayPart{
		Type: PartTypeInlineStep, Time: time.Now(),
		InlineStep: &InlineStepPart{StepID: "p1", Status: "running"},
	})
	m.tileBar.SetSnapshot(m.chat.store.InlineStepsThisTurn(), nil)
	m.setFocus(FocusTileBar)
	if m.focus != FocusTileBar {
		t.Fatalf("setup: focus=%v, want FocusTileBar", m.focus)
	}

	// 模拟新 turn 开始（用户提交新消息）——InlineStepsThisTurn 清空
	m.chat.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "next"}})
	if m.chat.store.InlineStepsThisTurn() != nil {
		t.Fatalf("setup: new turn should clear InlineStepsThisTurn, got %v", m.chat.store.InlineStepsThisTurn())
	}

	// 触发 Update wrapper → syncChrome 调用
	updated, _ := m.Update(renderTickMsg{})
	m2 := updated.(Model)

	if m2.focus != FocusChat {
		t.Errorf("syncChrome should fall back FocusTileBar→FocusChat when tile bar invisible; got focus=%v", m2.focus)
	}
}

// TestSyncChrome_FocusStepDetail_DanglesAfterStepIDClear 红线 P2 类（同源 P5）：
// 详情态焦点若 ViewingStepID 被外部清空，syncChrome 兜底切 FocusChat。
func TestSyncChrome_FocusStepDetail_DanglesAfterStepIDClear(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.width, m.height = 100, 40
	m.updateLayout()
	m.chat.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	m.chat.AddPart(DisplayPart{
		Type: PartTypeInlineStep, Time: time.Now(),
		InlineStep: &InlineStepPart{StepID: "p1", Status: "running"},
	})
	m.chat.AddPart(DisplayPart{
		Type: PartTypeTool, Time: time.Now(),
		Tool: &ToolPart{Name: "bash", CallID: "c1", State: "completed", StepID: "p1"},
	})
	if !m.chat.EnterStepDetail("p1") {
		t.Fatal("EnterStepDetail(p1) should succeed")
	}
	m.setFocus(FocusStepDetail)
	// 模拟外部清空 ViewingStepID（不通过 ExitStepDetail）
	m.chat.viewingStepID = ""

	updated, _ := m.Update(renderTickMsg{})
	m2 := updated.(Model)

	if m2.focus != FocusChat {
		t.Errorf("syncChrome should fall back FocusStepDetail→FocusChat when ViewingStepID empty; got %v", m2.focus)
	}
}

// TestSyncChrome_RefreshesTileBarOnEveryUpdate 红线 P1：观察者类事件（仅 store 变化）
// 之后 tile bar 数据自动同步——即使 event handler 没显式 updateLayout。
func TestSyncChrome_RefreshesTileBarOnEveryUpdate(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.width, m.height = 100, 40
	m.updateLayout()
	m.chat.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})

	if m.tileBar.Count() != 0 {
		t.Fatalf("setup: tileBar.Count=%d, want 0", m.tileBar.Count())
	}

	// 直接 chat.AddPart 一个 InlineStep（绕开 event_handlers 路径，模拟某种漏调 updateLayout 的场景）
	m.chat.AddPart(DisplayPart{
		Type: PartTypeInlineStep, Time: time.Now(),
		InlineStep: &InlineStepPart{StepID: "p1", Status: "running"},
	})
	// tileBar 仍为旧空快照
	if m.tileBar.Count() != 0 {
		t.Fatalf("before Update: tileBar.Count=%d, want 0 (snapshot still stale)", m.tileBar.Count())
	}

	// 触发任意 Update 走 wrapper → syncChrome → refreshTileBar
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m2 := updated.(Model)

	if m2.tileBar.Count() != 1 {
		t.Errorf("after Update wrapper syncChrome should refresh tileBar; got Count=%d, want 1", m2.tileBar.Count())
	}
}
