package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type AlertDialog struct {
	id      string
	title   string
	message string
	width   int
	height  int
}

func NewAlertDialog(id, title, message string) *AlertDialog {
	return &AlertDialog{
		id:      id,
		title:   title,
		message: message,
	}
}

func (a *AlertDialog) ID() string { return a.id }

func (a *AlertDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter", "esc", "q":
			return a, func() tea.Msg { return AlertDismissedMsg{DialogID: a.id} }
		}
	}
	return a, nil
}

func (a *AlertDialog) View() string {
	contentWidth := min(a.width-4, 60)
	if contentWidth < 20 {
		contentWidth = 20
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	msgStyle := lipgloss.NewStyle().Width(contentWidth - 4)
	hintStyle := lipgloss.NewStyle().Faint(true)

	content := fmt.Sprintf(
		"%s\n\n%s\n\n%s",
		titleStyle.Render(a.title),
		msgStyle.Render(a.message),
		hintStyle.Render("Press Enter or Esc to dismiss"),
	)

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(contentWidth)

	dialog := border.Render(content)
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (a *AlertDialog) SetSize(w, h int) {
	a.width = w
	a.height = h
}

type AlertDismissedMsg struct {
	DialogID string
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
