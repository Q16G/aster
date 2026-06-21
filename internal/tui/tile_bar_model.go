package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TileBarModel 是变体 C（Split Pane Horizontal）的下区 peer tile bar。
//
// 设计抉择（参见 plan 文档 v2 §「TileBarModel 不带 viewport」）：
//   - **不**带 viewport.Model（与 SidebarModel 不同，与 SubAgentPanel 一致）：下区
//     横向排列、≤5 个 peer，不需要滚动。viewport 是 over-engineering。
//   - **不**在 View() 内拉 PartsStore：所有派生字段（done/total/last tool）由调用方
//     SetSnapshot 前预计算，避免渲染期触发副作用。PartsStore 无 mutex（单 goroutine
//     既定 invariant），TileBarModel 的状态只在主循环 Model.Update 同步。
//
// MinHeight() 自报需要的高度（不用 app.go 硬编码常量），SetSize 根据 MinHeight
// 在双 viewport 上下分屏中给下区留位。
type TileBarModel struct {
	items    []InlineStepPart       // 当前 turn 内的 InlineStepPart 快照
	summary  map[string]tileSummary // stepID → 派生 summary（done/total/last tool）
	cursor   int                    // focused tile 下标
	focused  bool                   // tile bar 是否拿到键盘焦点
	width    int
	height   int
}

func NewTileBarModel() TileBarModel { return TileBarModel{} }

func (m *TileBarModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *TileBarModel) SetFocused(focused bool) { m.focused = focused }

func (m TileBarModel) IsFocused() bool { return m.focused }

func (m TileBarModel) Count() int { return len(m.items) }

// SetSnapshot 把当前 turn 的 InlineStepPart 列表 + 每个 stepID 的派生 summary 注入。
// 调用方（app.go Model.Update）每次状态可能变化时调一次——典型时机：observer
// 事件（task_item / inline_step_start/end）、tool_start/end、SetSize、SetParts。
//
// cursor 在新切片范围内 clamp，避免「已完成 step 被裁掉后 cursor dangling」。
func (m *TileBarModel) SetSnapshot(items []InlineStepPart, summary map[string]tileSummary) {
	m.items = items
	m.summary = summary
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// MoveLeft / MoveRight 在 tile 列表里切 cursor，端点不折返（端点折返由 app.go
// Update 路由处理：cursor==0 时 h 应当让焦点跳回 chat，cursor==末尾 时 l 跳到 input）。
func (m *TileBarModel) MoveLeft() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *TileBarModel) MoveRight() {
	if m.cursor < len(m.items)-1 {
		m.cursor++
	}
}

// AtLeftEdge / AtRightEdge 给 app.go Update 路由判断端点折返用。
func (m TileBarModel) AtLeftEdge() bool  { return m.cursor <= 0 }
func (m TileBarModel) AtRightEdge() bool { return m.cursor >= len(m.items)-1 }

// SelectedStepID 返回当前 focused tile 的 stepID；无 tile 时返回 ""。
func (m TileBarModel) SelectedStepID() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	return strings.TrimSpace(m.items[m.cursor].StepID)
}

// Cursor 返回当前 cursor 下标（测试用）。
func (m TileBarModel) Cursor() int { return m.cursor }

// SetCursorByStepID 把 cursor 同步到指定 stepID 对应的 tile（detail 模式 h/l 切 peer
// 时用——viewingStepID 变化后调本方法让 cursor 跟上）。
// 找不到时 no-op。
func (m *TileBarModel) SetCursorByStepID(stepID string) {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return
	}
	for i, it := range m.items {
		if strings.TrimSpace(it.StepID) == stepID {
			m.cursor = i
			return
		}
	}
}

// MinHeight 自报需要的最小高度。app.go SetSize 据此动态给下区留位，不用硬编码。
//
// 空 items：3 行（容器 border 2 + "(no concurrent steps)" 占位 1）
// 有 items：9 行（容器 border 2 + 标题 1 + tile 自身 border 2 + 内容 4）
func (m TileBarModel) MinHeight() int {
	if len(m.items) == 0 {
		return 3
	}
	return 2 + 1 + 2 + tileBarContentRows // = 9
}

// View 渲染整个下区。横向拼接所有 tile，前面加 "⇶ Concurrent Steps (N)" 标题。
//
// 空 items 时返回带占位文字的容器，不返回空字符串——避免 app.go layout 出现
// 突变（高度切换的视觉抖动）。
func (m TileBarModel) View() string {
	if m.width < 10 || m.height < 3 {
		return ""
	}

	borderColor := lipgloss.Color("240")
	if m.focused {
		borderColor = lipgloss.Color("62")
	}

	containerStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)

	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")).Render(
		"⇶ Concurrent Steps") + lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(
		" ("+itoa(len(m.items))+")")

	if len(m.items) == 0 {
		body := title + "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true).Render(
			"  (none)")
		return containerStyle.Render(body)
	}

	tiles := make([]string, 0, len(m.items))
	for i, it := range m.items {
		summary := m.summary[strings.TrimSpace(it.StepID)]
		tiles = append(tiles, renderTile(it, summary, i == m.cursor && m.focused))
	}
	tileRow := lipgloss.JoinHorizontal(lipgloss.Top, tiles...)
	return containerStyle.Render(title + "\n" + tileRow)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
