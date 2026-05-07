package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	tuicontext "aster/internal/tui/context"
)

type StatusCategory int

const (
	StatusIdle StatusCategory = iota
	StatusRunning
	StatusTool
	StatusIteration
	StatusError
	StatusRetry
	StatusUserAction
	StatusAgent
)

func classifyStatus(text string) StatusCategory {
	switch {
	case text == "" || text == "ready":
		return StatusIdle
	case text == "error" || text == "failed":
		return StatusError
	case strings.HasPrefix(text, "calling "):
		return StatusTool
	case strings.HasPrefix(text, "thinking") || text == "cancelling...":
		return StatusRunning
	case strings.HasPrefix(text, "iteration "):
		return StatusIteration
	case strings.HasPrefix(text, "agent: "):
		return StatusAgent
	case strings.HasPrefix(text, "retrying"):
		return StatusRetry
	default:
		return StatusUserAction
	}
}

type FooterModel struct {
	width         int
	workdir       string
	mcpTotal      int
	mcpConnected  int
	statusText    string
	spinnerView   string
	focusHint     string
	modeIndicator string
	sidebarShown  bool
}

func (f *FooterModel) SetWidth(w int) {
	f.width = w
}

func (f *FooterModel) SetWorkdir(dir string) {
	f.workdir = dir
}

func (f FooterModel) Workdir() string {
	return f.workdir
}

func (f *FooterModel) SetMCPStatus(total, connected int) {
	f.mcpTotal = total
	f.mcpConnected = connected
}

func (f *FooterModel) SetStatus(text, spinner, focus string) {
	f.statusText = text
	f.spinnerView = spinner
	f.focusHint = focus
}

func (f *FooterModel) SetModeIndicator(mode string) {
	f.modeIndicator = strings.ToUpper(mode)
}

func (f *FooterModel) SetSidebarShown(shown bool) {
	f.sidebarShown = shown
}

func truncateTail(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	var b strings.Builder
	width := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if width+rw > maxWidth-1 {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	return b.String() + "…"
}

func truncateHead(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	runes := []rune(s)
	width := 0
	start := len(runes)
	for i := len(runes) - 1; i >= 0; i-- {
		rw := runewidth.RuneWidth(runes[i])
		if width+rw > maxWidth-1 {
			break
		}
		width += rw
		start = i
	}
	return "…" + string(runes[start:])
}

func (f FooterModel) statusColor(th tuicontext.ThemeData) lipgloss.Color {
	switch classifyStatus(f.statusText) {
	case StatusIdle:
		return th.Success
	case StatusRunning:
		return th.Warning
	case StatusTool:
		return th.ToolAccent
	case StatusIteration, StatusAgent:
		return th.AssistantAccent
	case StatusError:
		return th.Error
	case StatusRetry:
		return th.Warning
	case StatusUserAction:
		return th.UserAccent
	default:
		return th.TextMuted
	}
}

func (f FooterModel) renderModeBadge(th tuicontext.ThemeData, compact bool) string {
	if f.modeIndicator == "" {
		return ""
	}

	var bg, fg lipgloss.Color
	switch f.modeIndicator {
	case "YOLO":
		bg, fg = th.Warning, th.Background
	case "AI":
		bg, fg = th.AssistantAccent, th.Background
	default:
		bg, fg = th.BackgroundElement, th.TextMuted
	}

	label := f.modeIndicator
	if compact {
		label = string([]rune(f.modeIndicator)[0:1])
	}

	style := lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Bold(true).
		Padding(0, 1)

	return style.Render(label)
}

func (f FooterModel) renderStatus(th tuicontext.ThemeData) string {
	var parts []string
	if f.spinnerView != "" {
		parts = append(parts, f.spinnerView)
	}
	if f.statusText != "" {
		parts = append(parts, f.statusText)
	}
	if len(parts) == 0 {
		return ""
	}

	text := strings.Join(parts, " ")
	style := lipgloss.NewStyle().
		Foreground(f.statusColor(th)).
		Background(th.StatusBg)

	return style.Render(text)
}

func (f FooterModel) renderMeta(th tuicontext.ThemeData, showMCP, showFocus bool) string {
	var parts []string

	if showMCP && f.mcpTotal > 0 && !f.sidebarShown {
		var mcpText string
		var mcpColor lipgloss.Color
		if f.mcpConnected < f.mcpTotal {
			mcpText = fmt.Sprintf("MCP:%d/%d", f.mcpConnected, f.mcpTotal)
			mcpColor = th.Warning
		} else {
			mcpText = fmt.Sprintf("MCP:%d", f.mcpTotal)
			mcpColor = th.TextMuted
		}
		mcpStyle := lipgloss.NewStyle().
			Foreground(mcpColor).
			Background(th.StatusBg)
		parts = append(parts, mcpStyle.Render(mcpText))
	}

	if showFocus && f.focusHint != "" {
		focusStyle := lipgloss.NewStyle().
			Foreground(th.TextMuted).
			Background(th.StatusBg)
		parts = append(parts, focusStyle.Render(f.focusHint))
	}

	return strings.Join(parts, " ")
}

func (f FooterModel) View(th tuicontext.ThemeData) string {
	if f.width <= 0 {
		return ""
	}

	contentWidth := f.width - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	// Extreme narrow: only status text
	if contentWidth < 25 {
		status := f.statusText
		if status == "" {
			status = "ready"
		}
		line := truncateTail(status, contentWidth)
		style := lipgloss.NewStyle().
			Width(f.width).
			Foreground(f.statusColor(th)).
			Background(th.StatusBg).
			Padding(0, 1)
		return style.Render(line)
	}

	compact := contentWidth < 40
	showFocus := contentWidth >= 55
	showMCP := contentWidth >= 50

	badge := f.renderModeBadge(th, compact)
	badgeWidth := lipgloss.Width(badge)

	status := f.renderStatus(th)
	statusWidth := lipgloss.Width(status)

	meta := f.renderMeta(th, showMCP, showFocus)
	metaWidth := lipgloss.Width(meta)

	// Calculate workdir budget
	separators := 0
	if badgeWidth > 0 {
		separators++
	}
	if statusWidth > 0 && (badgeWidth > 0 || metaWidth > 0) {
		separators++
	}
	if metaWidth > 0 && statusWidth > 0 {
		separators++
	}

	usedWidth := badgeWidth + statusWidth + metaWidth + separators
	workdirBudget := contentWidth - usedWidth

	// If status doesn't fit, truncate it
	if workdirBudget < 0 {
		available := contentWidth - badgeWidth - metaWidth - separators
		if available < 4 {
			// Drop meta entirely
			meta = ""
			metaWidth = 0
			available = contentWidth - badgeWidth - 1
		}
		if available > 0 {
			plainStatus := f.statusText
			if f.spinnerView != "" && f.statusText != "" {
				plainStatus = f.spinnerView + " " + f.statusText
			} else if f.spinnerView != "" {
				plainStatus = f.spinnerView
			}
			truncated := truncateTail(plainStatus, available)
			statusStyle := lipgloss.NewStyle().
				Foreground(f.statusColor(th)).
				Background(th.StatusBg)
			status = statusStyle.Render(truncated)
			statusWidth = lipgloss.Width(status)
		} else {
			status = ""
			statusWidth = 0
		}
		workdirBudget = contentWidth - badgeWidth - statusWidth - metaWidth - separators
	}

	// Build workdir segment
	workdir := ""
	if workdirBudget > 3 {
		dir := f.workdir
		if dir == "" {
			dir = "."
		}
		truncated := truncateHead(dir, workdirBudget)
		wdStyle := lipgloss.NewStyle().
			Foreground(th.TextMuted).
			Background(th.StatusBg)
		workdir = wdStyle.Render(truncated)
	}
	workdirWidth := lipgloss.Width(workdir)

	// Assemble line: badge + workdir + flexible gap + status + meta
	var leftParts []string
	if badge != "" {
		leftParts = append(leftParts, badge)
	}
	if workdir != "" {
		leftParts = append(leftParts, workdir)
	}
	leftSide := strings.Join(leftParts, " ")
	leftWidth := lipgloss.Width(leftSide)

	var rightParts []string
	if status != "" {
		rightParts = append(rightParts, status)
	}
	if meta != "" {
		rightParts = append(rightParts, meta)
	}
	rightSide := strings.Join(rightParts, " ")
	rightWidth := lipgloss.Width(rightSide)

	gapWidth := contentWidth - leftWidth - rightWidth
	if gapWidth < 1 && leftWidth > 0 && rightWidth > 0 {
		gapWidth = 1
	}
	if gapWidth < 0 {
		gapWidth = 0
	}

	_ = workdirWidth

	gapStyle := lipgloss.NewStyle().Background(th.StatusBg)
	gap := ""
	if gapWidth > 0 {
		gap = gapStyle.Render(strings.Repeat(" ", gapWidth))
	}

	line := leftSide + gap + rightSide

	outerStyle := lipgloss.NewStyle().
		Width(f.width).
		Background(th.StatusBg).
		Padding(0, 1)

	return outerStyle.Render(line)
}
