package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type PromptResultMsg struct {
	DialogID  string
	Value     string
	Cancelled bool
}

type PromptDialog struct {
	id         string
	title      string
	question   string
	options    []string
	cursor     int
	input      textarea.Model
	singleLine textinput.Model
	masked     bool
	width      int
	height     int
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

func (p *PromptDialog) WithMasked() *PromptDialog {
	ti := textinput.New()
	ti.EchoMode = textinput.EchoPassword
	ti.Focus()
	p.singleLine = ti
	p.masked = true
	return p
}

func (p *PromptDialog) WithPlaceholder(s string) *PromptDialog {
	if p.masked {
		p.singleLine.Placeholder = s
	} else {
		p.input.Placeholder = s
	}
	return p
}

func (p *PromptDialog) ID() string { return p.id }

func (p *PromptDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		if len(p.options) > 0 {
			return p, nil
		}
		var cmd tea.Cmd
		if p.masked {
			p.singleLine, cmd = p.singleLine.Update(msg)
		} else {
			p.input, cmd = p.input.Update(msg)
		}
		return p, cmd
	}

	if key.String() == "esc" {
		return p, func() tea.Msg {
			return PromptResultMsg{DialogID: p.id, Cancelled: true}
		}
	}

	if len(p.options) > 0 {
		return p.updateOptions(key)
	}
	return p.updateTextInput(key)
}

func (p *PromptDialog) updateOptions(key tea.KeyMsg) (Dialog, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "down", "j":
		if p.cursor < len(p.options)-1 {
			p.cursor++
		}
	case "enter":
		value := p.options[p.cursor]
		return p, func() tea.Msg {
			return PromptResultMsg{DialogID: p.id, Value: value}
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key.String()[0] - '1')
		if idx >= 0 && idx < len(p.options) {
			value := p.options[idx]
			return p, func() tea.Msg {
				return PromptResultMsg{DialogID: p.id, Value: value}
			}
		}
	}
	return p, nil
}

func (p *PromptDialog) updateTextInput(key tea.KeyMsg) (Dialog, tea.Cmd) {
	if key.String() == "enter" {
		var value string
		if p.masked {
			value = strings.TrimSpace(p.singleLine.Value())
		} else {
			value = strings.TrimSpace(p.input.Value())
		}
		if value == "" {
			return p, nil
		}
		return p, func() tea.Msg {
			return PromptResultMsg{DialogID: p.id, Value: value}
		}
	}

	var cmd tea.Cmd
	if p.masked {
		p.singleLine, cmd = p.singleLine.Update(key)
	} else {
		p.input, cmd = p.input.Update(key)
	}
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
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	normalStyle := lipgloss.NewStyle().Faint(true)

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(p.title))
	sb.WriteString("\n\n")
	sb.WriteString(questionStyle.Render(p.question))

	if len(p.options) > 0 {
		sb.WriteString("\n\n")
		for i, opt := range p.options {
			if i == p.cursor {
				sb.WriteString(selectedStyle.Render(fmt.Sprintf("  ▸ %d. %s", i+1, opt)))
			} else {
				sb.WriteString(normalStyle.Render(fmt.Sprintf("    %d. %s", i+1, opt)))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
		sb.WriteString(hintStyle.Render("↑↓ to select • Enter to confirm • Esc to cancel"))
	} else {
		sb.WriteString("\n\n")
		if p.masked {
			sb.WriteString(p.singleLine.View())
		} else {
			sb.WriteString(p.input.View())
		}
		sb.WriteString("\n\n")
		sb.WriteString(hintStyle.Render("Enter to submit • Esc to cancel"))
	}

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
	inputWidth := min(w-10, 54)
	if p.masked {
		p.singleLine.Width = inputWidth
	} else {
		p.input.SetWidth(inputWidth)
	}
}
