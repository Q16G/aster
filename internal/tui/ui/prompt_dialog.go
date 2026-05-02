package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type PromptResultMsg struct {
	DialogID  string
	Value     string
	Cancelled bool
}

type PromptDialog struct {
	id          string
	title       string
	question    string
	options     []string
	input       textarea.Model
	width       int
	height      int
}

func NewPromptDialog(id, title, question string) *PromptDialog {
	ta := textarea.New()
	ta.Placeholder = "Type your answer..."
	ta.SetHeight(3)
	ta.Focus()

	return &PromptDialog{
		id:       id,
		title:    title,
		question: question,
		input:    ta,
	}
}

func (p *PromptDialog) WithOptions(opts []string) *PromptDialog {
	p.options = opts
	return p
}

func (p *PromptDialog) ID() string { return p.id }

func (p *PromptDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			value := strings.TrimSpace(p.input.Value())
			if value == "" {
				return p, nil
			}
			return p, func() tea.Msg {
				return PromptResultMsg{DialogID: p.id, Value: value}
			}
		case "esc":
			return p, func() tea.Msg {
				return PromptResultMsg{DialogID: p.id, Cancelled: true}
			}
		}
	}

	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	return p, cmd
}

func (p *PromptDialog) View() string {
	contentWidth := min(p.width-4, 60)
	if contentWidth < 20 {
		contentWidth = 20
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	questionStyle := lipgloss.NewStyle().Width(contentWidth - 4)
	hintStyle := lipgloss.NewStyle().Faint(true)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(p.title))
	sb.WriteString("\n\n")
	sb.WriteString(questionStyle.Render(p.question))

	if len(p.options) > 0 {
		sb.WriteString("\n\n")
		for i, opt := range p.options {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, opt))
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(p.input.View())
	sb.WriteString("\n\n")
	sb.WriteString(hintStyle.Render("Enter to submit • Esc to cancel"))

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("14")).
		Padding(1, 2).
		Width(contentWidth)

	dialog := border.Render(sb.String())
	return lipgloss.Place(p.width, p.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (p *PromptDialog) SetSize(w, h int) {
	p.width = w
	p.height = h
	p.input.SetWidth(min(w-10, 54))
}
