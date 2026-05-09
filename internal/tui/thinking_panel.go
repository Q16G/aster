package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const timelinePanelMaxEntries = 8
const timelinePanelVisibleLines = 4

type timelineEntry struct {
	Time  time.Time
	Phase string
	Text  string
}

type ThinkingPanelModel struct {
	entries      []timelineEntry
	visible      bool
	phase        string
	contentDirty bool
	width        int
}

func NewThinkingPanelModel() ThinkingPanelModel {
	return ThinkingPanelModel{}
}

func (m *ThinkingPanelModel) Show(phase string) {
	if m == nil {
		return
	}
	m.visible = true
	m.phase = phase
	m.contentDirty = true
}

func (m *ThinkingPanelModel) Hide() {
	if m == nil {
		return
	}
	if m.visible {
		m.contentDirty = true
	}
	m.visible = false
	m.phase = ""
}

func (m *ThinkingPanelModel) Reset() {
	if m == nil {
		return
	}
	m.entries = m.entries[:0]
	m.visible = false
	m.phase = ""
	m.contentDirty = true
}

func (m *ThinkingPanelModel) PushEntry(phase, text string) {
	if m == nil {
		return
	}
	m.entries = append(m.entries, timelineEntry{
		Time:  time.Now(),
		Phase: phase,
		Text:  text,
	})
	if len(m.entries) > timelinePanelMaxEntries {
		m.entries = m.entries[len(m.entries)-timelinePanelMaxEntries:]
	}
	m.contentDirty = true
}

func (m *ThinkingPanelModel) UpdateLastEntry(text string) {
	if m == nil || len(m.entries) == 0 {
		return
	}
	m.entries[len(m.entries)-1].Text = text
	m.contentDirty = true
}

func (m *ThinkingPanelModel) Height() int {
	if m == nil || !m.visible {
		return 0
	}
	lines := len(m.entries)
	if lines > timelinePanelVisibleLines {
		lines = timelinePanelVisibleLines
	}
	if lines < 1 {
		lines = 1
	}
	return lines + 2
}

func (m *ThinkingPanelModel) IsDirty() bool {
	if m == nil {
		return false
	}
	return m.contentDirty
}

func (m *ThinkingPanelModel) FlushRender() {
	if m == nil {
		return
	}
	m.contentDirty = false
}

func (m *ThinkingPanelModel) SetWidth(w int) {
	if m == nil {
		return
	}
	m.width = w
}

func (m ThinkingPanelModel) View() string {
	if !m.visible {
		return ""
	}

	width := m.width
	if width < 10 {
		width = 10
	}

	phaseLabel := m.phase
	switch phaseLabel {
	case "step_summary":
		phaseLabel = "Step Summary"
	case "final_answer":
		phaseLabel = "Final Answer"
	}
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Bold(true).
		Render("▸ " + phaseLabel)

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))

	visible := m.entries
	if len(visible) > timelinePanelVisibleLines {
		visible = visible[len(visible)-timelinePanelVisibleLines:]
	}

	var lines []string
	for i, e := range visible {
		ts := e.Time.Format("15:04:05")
		isLast := i == len(visible)-1
		prefix := "  "
		if isLast {
			prefix = "▸ "
		}
		line := fmt.Sprintf("%s%s %s", prefix, ts, e.Text)
		if isLast {
			lines = append(lines, activeStyle.Render(line+"▌"))
		} else {
			lines = append(lines, dimStyle.Render(line))
		}
	}
	if len(lines) == 0 {
		lines = append(lines, dimStyle.Render("  waiting...▌"))
	}

	body := strings.Join(lines, "\n")

	border := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("8")).
		Width(width)

	return border.Render(header + "\n" + body)
}
