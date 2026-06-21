package tui

import (
	"strings"
	"testing"
	"time"
)

// 变体 C tile bar 红线测试（plan v2 §测试覆盖）：
//   - TestTileBar_RendersAllInlineStepsThisTurn
//   - TestTileBar_CursorWrapping (MoveLeft/MoveRight 端点不折返)
//   - TestTileBar_EmptyStateNoTileBar
//   - TestTileBar_OverflowTruncation
//   - TestTileBar_SetCursorByStepID

// helper：在 ChatModel 内创建一个 InlineStepPart（已带 StepID/Description/Status）
func addInlineStep(m *ChatModel, stepID, desc, status string) {
	m.AddPart(DisplayPart{
		Type: PartTypeInlineStep,
		Time: time.Now(),
		InlineStep: &InlineStepPart{
			StepID:      stepID,
			Description: desc,
			Status:      status,
			StartedAt:   time.Now(),
		},
	})
}

func TestTileBar_RendersAllInlineStepsThisTurn(t *testing.T) {
	m := NewChatModel()
	m.SetSize(120, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})

	addInlineStep(&m, "p1", "first peer", "running")
	addInlineStep(&m, "p2", "second peer", "running")
	addInlineStep(&m, "p3", "third peer", "running")

	tile := NewTileBarModel()
	tile.SetSnapshot(m.store.InlineStepsThisTurn(), nil)
	tile.SetSize(120, tile.MinHeight())
	out := tile.View()

	for _, want := range []string{"p1", "p2", "p3"} {
		if !strings.Contains(out, want) {
			t.Errorf("tile bar should contain %q, got:\n%s", want, out)
		}
	}
}

func TestTileBar_CursorMoveNoWrap(t *testing.T) {
	m := NewChatModel()
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	addInlineStep(&m, "p1", "", "running")
	addInlineStep(&m, "p2", "", "running")
	addInlineStep(&m, "p3", "", "running")

	tile := NewTileBarModel()
	tile.SetSnapshot(m.store.InlineStepsThisTurn(), nil)

	if !tile.AtLeftEdge() {
		t.Error("cursor=0 should be AtLeftEdge")
	}
	tile.MoveLeft()
	if tile.Cursor() != 0 {
		t.Errorf("MoveLeft at edge should stay at 0, got %d", tile.Cursor())
	}
	tile.MoveRight()
	tile.MoveRight()
	if tile.Cursor() != 2 {
		t.Errorf("after 2 MoveRight cursor=%d, want 2", tile.Cursor())
	}
	if !tile.AtRightEdge() {
		t.Error("cursor=2 of 3 items should be AtRightEdge")
	}
	tile.MoveRight()
	if tile.Cursor() != 2 {
		t.Errorf("MoveRight at edge should stay at 2, got %d", tile.Cursor())
	}
}

func TestTileBar_EmptyStateMinHeightSmall(t *testing.T) {
	tile := NewTileBarModel()
	tile.SetSnapshot(nil, nil)
	if got := tile.MinHeight(); got != 3 {
		t.Errorf("empty MinHeight=%d, want 3", got)
	}
	if got := tile.Count(); got != 0 {
		t.Errorf("empty Count=%d, want 0", got)
	}
}

func TestTileBar_NonEmptyMinHeightNine(t *testing.T) {
	m := NewChatModel()
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	addInlineStep(&m, "p1", "", "running")

	tile := NewTileBarModel()
	tile.SetSnapshot(m.store.InlineStepsThisTurn(), nil)
	if got := tile.MinHeight(); got != 9 {
		t.Errorf("non-empty MinHeight=%d, want 9", got)
	}
}

// TestTileBar_OverflowTruncation 多 peer 时下区水平方向超宽：保证不 panic、依然渲染所有 tile。
func TestTileBar_OverflowTruncation(t *testing.T) {
	m := NewChatModel()
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	for i := 0; i < 10; i++ {
		addInlineStep(&m, "p"+itoa(i), "task "+itoa(i), "running")
	}

	tile := NewTileBarModel()
	tile.SetSnapshot(m.store.InlineStepsThisTurn(), nil)
	tile.SetSize(80, 9) // 远不够 10*30 = 300 列
	out := tile.View()

	// 不要 panic 即过；至少包含第一个 tile 内容
	if !strings.Contains(out, "p0") {
		t.Errorf("overflow render should still contain first tile p0; got:\n%s", out)
	}
}

func TestTileBar_SetCursorByStepID(t *testing.T) {
	m := NewChatModel()
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	addInlineStep(&m, "p1", "", "running")
	addInlineStep(&m, "p2", "", "running")
	addInlineStep(&m, "p3", "", "running")

	tile := NewTileBarModel()
	tile.SetSnapshot(m.store.InlineStepsThisTurn(), nil)
	tile.SetCursorByStepID("p2")
	if tile.Cursor() != 1 {
		t.Errorf("after SetCursorByStepID(p2) cursor=%d, want 1", tile.Cursor())
	}
	if got := tile.SelectedStepID(); got != "p2" {
		t.Errorf("SelectedStepID=%q, want p2", got)
	}
	// 不存在的 stepID 是 no-op，cursor 保持
	tile.SetCursorByStepID("nope")
	if tile.Cursor() != 1 {
		t.Errorf("SetCursorByStepID(nope) should be no-op, cursor=%d", tile.Cursor())
	}
}
