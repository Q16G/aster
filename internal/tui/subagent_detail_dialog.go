package tui

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	tuiui "aster/internal/tui/ui"
)

const subAgentDetailDialogID = "subagent-detail"

// SubAgentDetailDialog is a read-only, scrollable modal showing the transcript
// of a single sub-agent (its plan / tool / stream / result parts), filtered out
// of the interleaved main timeline. It implements tuiui.Dialog and reuses
// AlertDismissedMsg on close so the existing dialog-stack pop handler applies.
type SubAgentDetailDialog struct {
	title    string
	content  string
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

func NewSubAgentDetailDialog(title, content string) *SubAgentDetailDialog {
	return &SubAgentDetailDialog{title: title, content: content}
}

func (d *SubAgentDetailDialog) ID() string { return subAgentDetailDialogID }

func (d *SubAgentDetailDialog) Update(msg tea.Msg) (tuiui.Dialog, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "q":
			return d, func() tea.Msg { return tuiui.AlertDismissedMsg{DialogID: d.ID()} }
		}
	}
	var cmd tea.Cmd
	d.viewport, cmd = d.viewport.Update(msg)
	return d, cmd
}

func (d *SubAgentDetailDialog) View() string {
	if !d.ready {
		d.build()
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	hintStyle := lipgloss.NewStyle().Faint(true)

	content := titleStyle.Render(" "+d.title+" ") + "\n" +
		d.viewport.View() + "\n" +
		hintStyle.Render(" Esc/q 关闭 • ↑/↓ 滚动")

	return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center, border.Render(content))
}

func (d *SubAgentDetailDialog) build() {
	w := min(d.width-6, 96)
	if w < 20 {
		w = 20
	}
	viewH := min(d.height-8, 36)
	if viewH < 5 {
		viewH = 5
	}
	d.viewport = viewport.New(w, viewH)
	d.viewport.Style = lipgloss.NewStyle()
	d.viewport.SetContent(d.content)
	d.ready = true
}

func (d *SubAgentDetailDialog) SetSize(w, h int) {
	d.width = w
	d.height = h
	d.ready = false
}
