package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	tuicontext "aster/internal/tui/context"
)

type FooterModel struct {
	width        int
	workdir      string
	mcpTotal     int
	mcpConnected int
	statusText   string
	spinnerView  string
	focusHint    string
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

func (f FooterModel) View(th tuicontext.ThemeData) string {
	if f.width <= 0 {
		return ""
	}

	left := f.workdir
	if left == "" {
		left = "."
	}

	var rightParts []string
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

	gap := f.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + right

	style := lipgloss.NewStyle().
		Width(f.width).
		Foreground(th.StatusFg).
		Background(th.StatusBg).
		Padding(0, 1)

	return style.Render(line)
}
