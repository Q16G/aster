package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// 下区 tile bar 的视觉常量（变体 C 移植自 concurrent-step-demo/variants/c-splitpane）。
//
// tileBarTileWidth = 单张 tile 显示宽度（rounded border 占 2 列已含在内，内容净宽=width-2）
// tileBarContentRows = 单 tile 内容行数（不含 border）：title + subtitle + status_line + latest_line
//
// MinHeight() 根据 items 数量在 TileBarModel 自报：
//   - 无 items：3（容器 border + "(no concurrent steps)" 占位）
//   - 有 items：9（容器 border 2 + 标题 1 + tile border 2 + 内容 4）
const (
	tileBarTileWidth   = 30
	tileBarContentRows = 4 // title / subtitle / status_line / latest_line
)

// tileSummary 是 tile bar 渲染单张 tile 所需的「派生」字段——由
// TileBarModel.SetSnapshot 调用方根据 ChildrenForStep(stepID) 派生填入，
// 避免渲染期再去查 store（数据同步策略：渲染期不拉 store，见 plan 文档 [v2-fix P7]）。
type tileSummary struct {
	Done       int
	Total      int
	LastTool   string // 反向遍历 ChildrenForStep 找到的最后一条 ToolPart.Name；空表示无 tool
	LastPending bool  // 最后一条 tool 是否仍 pending（用于 ⏳ 标记）
}

// renderTile 渲染单张 peer tile（变体 C 的核心视觉），返回多行字符串。
// 调用方负责 lipgloss.JoinHorizontal 把多张 tile 横向拼成 bar。
//
// 不在此函数内拉 store——所有派生字段（done/total/last tool）由调用方按 stepID
// 预先填进 tileSummary。这是 view 期间不触发 store 副作用的硬约定。
func renderTile(part InlineStepPart, summary tileSummary, focused bool) string {
	statusBadge := renderTileStatusBadge(part)
	bar := renderProgressBar(summary.Done, summary.Total, 8)

	lastLabel := "(idle)"
	if summary.LastTool != "" {
		lastLabel = summary.LastTool
		if summary.LastPending {
			lastLabel += " ⏳"
		}
	}

	title := fmt.Sprintf("⇶ [%s]", strings.TrimSpace(part.StepID))
	subtitle := truncOneLine(strings.TrimSpace(part.Description), tileBarTileWidth-6)
	statusLine := fmt.Sprintf("%s %d/%d", bar, summary.Done, summary.Total)
	latestLine := fmt.Sprintf("⏵ %s", lastLabel)

	borderColor := toolCompletedColor
	if focused {
		borderColor = toolBorderColor
		title = lipgloss.NewStyle().Bold(true).Foreground(toolBorderColor).Render(title)
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(tileBarTileWidth)

	var b strings.Builder
	b.WriteString(title + " " + statusBadge + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(subtitle) + "\n")
	b.WriteString(statusLine + "\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Render(latestLine))
	return style.Render(b.String())
}

// renderTileStatusBadge 把 InlineStepPart.Status 字符串映射到带颜色的 [running/done/failed] badge。
func renderTileStatusBadge(p InlineStepPart) string {
	elapsed := tileInlineStepElapsed(p)
	switch strings.ToLower(strings.TrimSpace(p.Status)) {
	case "running", "":
		if elapsed > 0 {
			return lipgloss.NewStyle().Foreground(toolBorderColor).Render(
				fmt.Sprintf("[running %s]", formatDuration(elapsed)))
		}
		return lipgloss.NewStyle().Foreground(toolBorderColor).Render("[running]")
	case "completed":
		if elapsed > 0 {
			return lipgloss.NewStyle().Foreground(toolCompletedColor).Render(
				fmt.Sprintf("[done %s]", formatDuration(elapsed)))
		}
		return lipgloss.NewStyle().Foreground(toolCompletedColor).Render("[done]")
	case "failed":
		return lipgloss.NewStyle().Foreground(toolErrorColor).Render("[failed]")
	case "cancelled", "canceled":
		return lipgloss.NewStyle().Foreground(toolErrorColor).Render("[cancelled]")
	}
	return ""
}

// tileInlineStepElapsed 类似 inlineStepElapsed（删旧 inline_step_card.go 前的同款），
// 这里保留 tile bar 自己一份避免 commit 1 阶段就强耦合 inline_step_card 删除。
func tileInlineStepElapsed(p InlineStepPart) time.Duration {
	if p.Duration > 0 {
		return p.Duration
	}
	if strings.EqualFold(p.Status, "running") && !p.StartedAt.IsZero() {
		return time.Since(p.StartedAt).Truncate(time.Millisecond * 100)
	}
	return 0
}

// renderProgressBar 渲染 done/total 的 ascii 进度条（width 字符宽）。
// 颜色根据进度比例：done=total → 完成色；否则 running 色。
func renderProgressBar(done, total, width int) string {
	if width <= 0 {
		return ""
	}
	if total <= 0 {
		return strings.Repeat("░", width)
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	col := toolBorderColor
	if done >= total {
		col = toolCompletedColor
	}
	return lipgloss.NewStyle().Foreground(col).Render(
		strings.Repeat("█", filled) + strings.Repeat("░", width-filled))
}

// truncOneLine 简单按 rune 截断（与现有 truncateDisplayWidth 区别：本函数只看 rune
// 数不考虑显示宽度——tile bar 主要英文 stepID + 短描述，rune 截断够用）。
func truncOneLine(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8RuneLen(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func utf8RuneLen(s string) int {
	return len([]rune(s))
}
