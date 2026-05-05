package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	tuicontext "aster/internal/tui/context"
)

type FooterModel struct {
	width         int
	workdir       string
	mcpTotal      int
	mcpConnected  int
	statusText    string
	spinnerView   string
	focusHint     string
	modeIndicator string
}

func (f *FooterModel) SetWidth(w int) {
	f.width = w
}

func (f *FooterModel) SetWorkdir(dir string) {
	f.workdir = dir
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
	f.modeIndicator = "[" + strings.ToUpper(mode) + "]"
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

func (f FooterModel) View(th tuicontext.ThemeData) string {
	if f.width <= 0 {
		return ""
	}

	left := f.workdir
	if left == "" {
		left = "."
	}

	var rightParts []string
	if f.modeIndicator != "" {
		rightParts = append(rightParts, f.modeIndicator)
	}
	if f.statusText != "" {
		rightParts = append(rightParts, f.statusText)
	}
	if f.spinnerView != "" {
		rightParts = append(rightParts, f.spinnerView)
	}
	if f.focusHint != "" {
		rightParts = append(rightParts, f.focusHint)
	}
	if f.mcpTotal > 0 {
		rightParts = append(rightParts, fmt.Sprintf("MCP:%d/%d", f.mcpConnected, f.mcpTotal))
	}
	right := strings.Join(rightParts, "  ")

	contentWidth := f.width - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	var line string
	rightWidth := runewidth.StringWidth(right)
	switch {
	case right == "":
		line = truncateHead(left, contentWidth)
	case rightWidth >= contentWidth:
		line = truncateTail(right, contentWidth)
	default:
		leftWidth := contentWidth - rightWidth - 1
		if leftWidth <= 0 {
			line = truncateTail(right, contentWidth)
			break
		}
		leftPart := truncateHead(left, leftWidth)
		gapWidth := contentWidth - runewidth.StringWidth(leftPart) - rightWidth
		if gapWidth < 1 {
			gapWidth = 1
		}
		line = leftPart + strings.Repeat(" ", gapWidth) + right
	}

	style := lipgloss.NewStyle().
		Width(f.width).
		Foreground(th.StatusFg).
		Background(th.StatusBg).
		Padding(0, 1)

	return style.Render(line)
}
