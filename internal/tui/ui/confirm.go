package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ConfirmResult int

const (
	ConfirmYes ConfirmResult = iota
	ConfirmNo
	ConfirmCancel
)

type ConfirmResultMsg struct {
	DialogID string
	Result   ConfirmResult
}

type ConfirmDialog struct {
	id       string
	title    string
	message  string
	yesLabel string
	noLabel  string
	focused  ConfirmResult
	width    int
	height   int
}

func NewConfirmDialog(id, title, message string) *ConfirmDialog {
	return &ConfirmDialog{
		id:       id,
		title:    title,
		message:  message,
		yesLabel: "Yes",
		noLabel:  "No",
		focused:  ConfirmNo,
	}
}

func (c *ConfirmDialog) WithLabels(yes, no string) *ConfirmDialog {
	c.yesLabel = yes
	c.noLabel = no
	return c
}

func (c *ConfirmDialog) ID() string { return c.id }

func (c *ConfirmDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "left", "h":
			c.focused = ConfirmYes
		case "right", "l":
			c.focused = ConfirmNo
		case "tab":
			if c.focused == ConfirmYes {
				c.focused = ConfirmNo
			} else {
				c.focused = ConfirmYes
			}
		case "y":
			return c, func() tea.Msg {
				return ConfirmResultMsg{DialogID: c.id, Result: ConfirmYes}
			}
		case "n":
			return c, func() tea.Msg {
				return ConfirmResultMsg{DialogID: c.id, Result: ConfirmNo}
			}
		case "enter":
			return c, func() tea.Msg {
				return ConfirmResultMsg{DialogID: c.id, Result: c.focused}
			}
		case "esc":
			return c, func() tea.Msg {
				return ConfirmResultMsg{DialogID: c.id, Result: ConfirmCancel}
			}
		}
	}
	return c, nil
}

func (c *ConfirmDialog) View() string {
	contentWidth := min(c.width-4, 50)
	if contentWidth < 20 {
		contentWidth = 20
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	msgStyle := lipgloss.NewStyle().Width(contentWidth - 4)

	activeBtn := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("62")).
		Padding(0, 2)

	inactiveBtn := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 2)

	var yesBtn, noBtn string
	if c.focused == ConfirmYes {
		yesBtn = activeBtn.Render(c.yesLabel)
		noBtn = inactiveBtn.Render(c.noLabel)
	} else {
		yesBtn = inactiveBtn.Render(c.yesLabel)
		noBtn = activeBtn.Render(c.noLabel)
	}

	buttons := lipgloss.JoinHorizontal(lipgloss.Center, yesBtn, "  ", noBtn)

	content := fmt.Sprintf(
		"%s\n\n%s\n\n%s",
		titleStyle.Render(c.title),
		msgStyle.Render(c.message),
		buttons,
	)

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(contentWidth)

	dialog := border.Render(content)
	return lipgloss.Place(c.width, c.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (c *ConfirmDialog) SetSize(w, h int) {
	c.width = w
	c.height = h
}
