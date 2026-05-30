package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const subAgentPanelWidth = 36

// subAgentPanelItem is one row in the right-hand sub-agent panel.
type subAgentPanelItem struct {
	CallID      string
	Title       string
	Description string
	Status      string
	Elapsed     time.Duration
	Running     bool
}

// SubAgentPanel is the right-side column listing every spawned sub-agent. Up/down
// move the selection; the Model drives drill-in via Selected().
type SubAgentPanel struct {
	items   []subAgentPanelItem
	cursor  int
	focused bool
	width   int
	height  int
}

func NewSubAgentPanel() SubAgentPanel { return SubAgentPanel{} }

func (m *SubAgentPanel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *SubAgentPanel) SetFocused(focused bool) { m.focused = focused }

func (m SubAgentPanel) IsFocused() bool { return m.focused }

func (m SubAgentPanel) Count() int { return len(m.items) }

// SetSnapshot replaces the rendered items, clamping the cursor into range so a
// finished/removed sub-agent never leaves the selection dangling.
func (m *SubAgentPanel) SetSnapshot(items []subAgentPanelItem) {
	m.items = items
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *SubAgentPanel) MoveUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *SubAgentPanel) MoveDown() {
	if m.cursor < len(m.items)-1 {
		m.cursor++
	}
}

func (m SubAgentPanel) Selected() (subAgentPanelItem, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return subAgentPanelItem{}, false
	}
	return m.items[m.cursor], true
}

func (m SubAgentPanel) View() string {
	if m.width < 4 || m.height < 3 {
		return ""
	}

	borderColor := lipgloss.Color("240")
	if m.focused {
		borderColor = lipgloss.Color("62")
	}

	innerW := m.width - 3
	if innerW < 1 {
		innerW = 1
	}

	var sb strings.Builder
	sb.WriteString(sectionHeader("子 Agent"))
	sb.WriteString("\n")

	if len(m.items) == 0 {
		sb.WriteString(sectionDimStyle.Render("  (none)"))
	}

	for i, it := range m.items {
		icon := "○"
		color := lipgloss.Color("8")
		switch it.Status {
		case "running":
			icon = "◐"
			color = "11"
		case "failed", "error":
			icon = "✗"
			color = "9"
		default:
			icon = "●"
			color = "10"
		}

		selected := i == m.cursor
		prefix := "  "
		if selected {
			prefix = "▸ "
		}

		head := prefix + icon + " " + it.Title
		if it.Elapsed > 0 {
			head += " [" + formatDuration(it.Elapsed) + "]"
		}

		headStyle := lipgloss.NewStyle().Foreground(color)
		if selected {
			headStyle = headStyle.Bold(true)
			if m.focused {
				headStyle = lipgloss.NewStyle().Bold(true).
					Foreground(lipgloss.Color("15")).
					Background(lipgloss.Color("62"))
			}
		}
		sb.WriteString(headStyle.Render(truncateDisplayWidth(head, innerW)))
		sb.WriteString("\n")

		if it.Description != "" {
			desc := "    " + truncateDisplayWidth(it.Description, innerW-4)
			sb.WriteString(sectionDimStyle.Render(desc))
			sb.WriteString("\n")
		}
	}

	containerStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		Padding(0, 1)
	return containerStyle.Render(strings.TrimRight(sb.String(), "\n"))
}
