package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type CommandEntry struct {
	Name        string
	Description string
}

type CommandPickerResultMsg struct {
	Command   string
	Cancelled bool
}

type CommandPickerModel struct {
	commands []CommandEntry
	filtered []int
	cursor   int
	filter   string
	width    int
}

const pickerMaxVisible = 8

func NewCommandPickerModel(commands []CommandEntry, width int) *CommandPickerModel {
	filtered := make([]int, len(commands))
	for i := range commands {
		filtered[i] = i
	}
	return &CommandPickerModel{
		commands: commands,
		filtered: filtered,
		width:    width,
	}
}

func (p *CommandPickerModel) SetFilter(query string) {
	p.filter = query
	p.applyFilter()
}

func (p *CommandPickerModel) SetWidth(w int) {
	p.width = w
}

func (p *CommandPickerModel) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch key.String() {
	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}
		return nil
	case "down", "ctrl+n":
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
		return nil
	case "enter", "tab":
		if len(p.filtered) > 0 && p.cursor < len(p.filtered) {
			idx := p.filtered[p.cursor]
			cmd := p.commands[idx].Name
			return func() tea.Msg {
				return CommandPickerResultMsg{Command: cmd}
			}
		}
		return nil
	case "esc":
		return func() tea.Msg {
			return CommandPickerResultMsg{Cancelled: true}
		}
	}
	return nil
}

func (p *CommandPickerModel) View() string {
	if len(p.filtered) == 0 {
		style := lipgloss.NewStyle().Faint(true).Padding(0, 1)
		return style.Render("No matching commands")
	}

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("62"))
	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))
	descStyle := lipgloss.NewStyle().
		Faint(true)

	visible := len(p.filtered)
	if visible > pickerMaxVisible {
		visible = pickerMaxVisible
	}

	start := 0
	if p.cursor >= visible {
		start = p.cursor - visible + 1
	}
	end := start + visible
	if end > len(p.filtered) {
		end = len(p.filtered)
	}

	var lines []string
	for i := start; i < end; i++ {
		idx := p.filtered[i]
		entry := p.commands[idx]
		prefix := "  "
		style := normalStyle
		if i == p.cursor {
			prefix = "> "
			style = selectedStyle
		}
		line := style.Render(prefix + entry.Name)
		if entry.Description != "" {
			line += " " + descStyle.Render(entry.Description)
		}
		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Width(p.width - 2)

	return boxStyle.Render(content)
}

func (p *CommandPickerModel) applyFilter() {
	query := strings.ToLower(strings.TrimPrefix(p.filter, "/"))
	if query == "" {
		p.filtered = make([]int, len(p.commands))
		for i := range p.commands {
			p.filtered[i] = i
		}
	} else {
		p.filtered = p.filtered[:0]
		for i, cmd := range p.commands {
			if strings.Contains(strings.ToLower(cmd.Name), query) ||
				strings.Contains(strings.ToLower(cmd.Description), query) {
				p.filtered = append(p.filtered, i)
			}
		}
	}
	if p.cursor >= len(p.filtered) {
		if len(p.filtered) > 0 {
			p.cursor = len(p.filtered) - 1
		} else {
			p.cursor = 0
		}
	}
}
