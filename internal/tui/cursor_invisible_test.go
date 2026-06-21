package tui

import (
	"testing"
	"time"
)

// 方案 C 红线（P6 修复后）：AddPart 不再无条件 setCursor 到新 part；
// partLineOffsets 用 sentinel -1；scrollToCursor 见 -1 时 noop。
// 防止「peer InlineStep 被强设 cursor + scrollToCursor 把 viewport 拽顶」回归。

// TestAddPart_InlineStep_DoesNotMoveCursor 红线：peer 的 InlineStepPart 被加入 store
// 后，cursor 不应跳到该 idx（旧版强 setCursor 导致 cursor 落在不可见 idx，后续
// enter 会误触发 ExpandableCard toggle，scrollToCursor 用 partLineOffsets[idx]=0
// 拽 viewport 到顶部）。
func TestAddPart_InlineStep_DoesNotMoveCursor(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	// 先加一个可见 root part 把 cursor 锚定在 idx=0
	m.AddPart(DisplayPart{
		Type: PartTypeUser,
		Time: time.Now(),
		User: &UserPart{Content: "hello"},
	})
	// 显式把 cursor 固定到 idx=0（user 显式 j 行为）
	m.FocusLastVisiblePart()
	if m.cursor != 0 {
		t.Fatalf("setup: cursor expected 0, got %d", m.cursor)
	}

	// peer 添加一个 InlineStepPart——不应改变 cursor
	idx := m.AddPart(DisplayPart{
		Type: PartTypeInlineStep,
		Time: time.Now(),
		InlineStep: &InlineStepPart{StepID: "p1", Status: "running", StartedAt: time.Now()},
	})
	if idx != 1 {
		t.Errorf("AddPart returned idx=%d, want 1", idx)
	}
	if m.cursor != 0 {
		t.Errorf("after AddPart(InlineStep) cursor=%d, want 0 (cursor must NOT track invisible part)", m.cursor)
	}
}

// TestScrollToCursor_SentinelNoop scrollToCursor 见 partLineOffsets[cursor]=-1 时不动
// viewport（旧版默认 0 会把 viewport 拽到顶部）。
func TestScrollToCursor_SentinelNoop(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "msg1"}})
	// 加入隐藏的 InlineStep
	m.AddPart(DisplayPart{
		Type: PartTypeInlineStep, Time: time.Now(),
		InlineStep: &InlineStepPart{StepID: "p1", Status: "running"},
	})

	// 手动把 cursor 设到不可见 part（模拟旧 bug 路径）
	m.cursor = 1
	// 验证 partLineOffsets 对应位置是 sentinel -1
	if got := m.partLineOffsets[1]; got != -1 {
		t.Fatalf("partLineOffsets[1]=%d, want -1 sentinel for hidden InlineStep", got)
	}

	// 让 viewport 滚到非顶部
	m.viewport.SetYOffset(5)
	beforeOffset := m.viewport.YOffset

	// scrollToCursor 见 sentinel -1 应该 noop
	m.scrollToCursor()
	if got := m.viewport.YOffset; got != beforeOffset {
		t.Errorf("scrollToCursor on sentinel-offset cursor changed YOffset: before=%d after=%d (want noop)", beforeOffset, got)
	}
}

// TestAutoFollow_PreservedAcrossInlineStepStart 红线：连发 3 个 InlineStep_start
// （peer spawn）后，autoFollowBottom 仍为 true，viewport 仍在底部——旧 bug 下 cursor
// 跳隐藏 idx + scrollToCursor 会反复打断 autoFollow。
func TestAutoFollow_PreservedAcrossInlineStepStart(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "go"}})
	// 加多个可见 part 让 viewport 处于「在底部」状态
	for i := 0; i < 3; i++ {
		m.AddPart(DisplayPart{
			Type: PartTypeText, Time: time.Now(),
			Text: &TextPart{Content: "line " + itoa(i), AgentName: ""},
		})
	}
	m.viewport.GotoBottom()
	m.autoFollowBottom = true

	// 连发 3 个 peer inline_step_start
	for i := 0; i < 3; i++ {
		m.AddPart(DisplayPart{
			Type: PartTypeInlineStep, Time: time.Now(),
			InlineStep: &InlineStepPart{StepID: "p" + itoa(i), Status: "running"},
		})
	}

	if !m.autoFollowBottom {
		t.Errorf("autoFollowBottom should remain true after inline_step_start bursts; got false")
	}
}

// TestAllocPartLineOffsets_SentinelInit allocPartLineOffsets 初始化全 -1。
func TestAllocPartLineOffsets_SentinelInit(t *testing.T) {
	out := allocPartLineOffsets(5)
	if len(out) != 5 {
		t.Fatalf("len=%d, want 5", len(out))
	}
	for i, v := range out {
		if v != -1 {
			t.Errorf("out[%d]=%d, want -1 sentinel", i, v)
		}
	}
	if got := allocPartLineOffsets(0); got == nil || len(got) != 0 {
		t.Errorf("alloc(0) should return empty slice, got %v", got)
	}
}

// TestFocusLastVisiblePart_FindsLastVisible FocusLastVisiblePart 跳到最后一个 mainVisible
// 的 part；最新 part 是 InlineStep（不可见）时跳到它之前的 user/text 等。
func TestFocusLastVisiblePart_FindsLastVisible(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.AddPart(DisplayPart{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "u"}})
	m.AddPart(DisplayPart{Type: PartTypeText, Time: time.Now(), Text: &TextPart{Content: "t1"}})
	m.AddPart(DisplayPart{
		Type: PartTypeInlineStep, Time: time.Now(),
		InlineStep: &InlineStepPart{StepID: "p1", Status: "running"},
	})

	m.FocusLastVisiblePart()
	// 期望 cursor 落在 idx=1（最后一个 text）而不是 idx=2（InlineStep 不可见）
	if m.cursor != 1 {
		t.Errorf("FocusLastVisiblePart cursor=%d, want 1 (last visible text, skip InlineStep)", m.cursor)
	}
}
