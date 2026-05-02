package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"aster/internal/mcp"
	"aster/internal/service"
)

type SidebarTab int

const (
	TabSkills SidebarTab = iota
	TabMCP
	tabCount
)

var tabLabels = [tabCount]string{"Skills", "MCP"}

type SidebarModel struct {
	activeTab SidebarTab
	focused   bool
	cursor    int
	width     int
	height    int

	skills           []*service.Skill
	mcpEntries       []*mcp.MCPServerEntry
	activeSkillNames map[string]bool
	activeMCPServers map[string]bool
}

func NewSidebarModel() SidebarModel {
	return SidebarModel{}
}

func (m *SidebarModel) SetSize(w, h int) {
	m.width = w
	m.height = h
}

func (m *SidebarModel) SetFocused(focused bool) {
	m.focused = focused
}

func (m SidebarModel) IsFocused() bool {
	return m.focused
}

func (m *SidebarModel) SetSkills(skills []*service.Skill) {
	m.skills = skills
	m.clampCursor()
}

func (m *SidebarModel) SetMCPEntries(entries []*mcp.MCPServerEntry) {
	m.mcpEntries = entries
	m.clampCursor()
}

func (m *SidebarModel) SetActiveSkillNames(names map[string]bool) {
	m.activeSkillNames = names
}

func (m *SidebarModel) SetActiveMCPServers(names map[string]bool) {
	m.activeMCPServers = names
}

func (m *SidebarModel) clampCursor() {
	max := m.itemCount() - 1
	if max < 0 {
		max = 0
	}
	if m.cursor > max {
		m.cursor = max
	}
}

func (m SidebarModel) itemCount() int {
	switch m.activeTab {
	case TabSkills:
		return len(m.skills)
	case TabMCP:
		return len(m.mcpEntries)
	}
	return 0
}

func (m SidebarModel) Update(msg tea.Msg) (SidebarModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			max := m.itemCount() - 1
			if max < 0 {
				max = 0
			}
			if m.cursor < max {
				m.cursor++
			}
		case "1":
			m.activeTab = TabSkills
			m.cursor = 0
		case "2":
			m.activeTab = TabMCP
			m.cursor = 0
		case "enter":
			return m, m.handleEnter()
		}
	}
	return m, nil
}

func (m SidebarModel) handleEnter() tea.Cmd {
	switch m.activeTab {
	case TabSkills:
		if m.cursor < len(m.skills) {
			skill := m.skills[m.cursor]
			active := m.activeSkillNames[skill.Name]
			return func() tea.Msg {
				return SkillToggleMsg{Name: skill.Name, Enabled: !active}
			}
		}
	case TabMCP:
		if m.cursor < len(m.mcpEntries) {
			entry := m.mcpEntries[m.cursor]
			active := m.activeMCPServers[entry.Name]
			name := entry.Name
			return func() tea.Msg {
				return MCPToggleMsg{Name: name, Connect: !active}
			}
		}
	}
	return nil
}

func (m SidebarModel) View() string {
	if m.width < 4 || m.height < 3 {
		return ""
	}

	contentWidth := m.width - 3

	borderColor := lipgloss.Color("240")
	if m.focused {
		borderColor = lipgloss.Color("62")
	}

	tabLine := m.renderTabs(contentWidth)

	contentHeight := m.height - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	var content string
	switch m.activeTab {
	case TabSkills:
		content = m.renderSkills(contentWidth, contentHeight)
	case TabMCP:
		content = m.renderMCP(contentWidth, contentHeight)
	}

	body := lipgloss.JoinVertical(lipgloss.Left, tabLine, content)

	containerStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		Padding(0, 1)

	return containerStyle.Render(body)
}

func (m SidebarModel) renderTabs(maxWidth int) string {
	var parts []string
	for i := SidebarTab(0); i < tabCount; i++ {
		label := tabLabels[i]
		if i == m.activeTab {
			style := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62"))
			parts = append(parts, style.Render(label))
		} else {
			style := lipgloss.NewStyle().Faint(true)
			parts = append(parts, style.Render(label))
		}
	}
	line := strings.Join(parts, " ")
	if len(line) > maxWidth && maxWidth > 0 {
		line = line[:maxWidth]
	}
	return line
}

func (m SidebarModel) renderSkills(w, h int) string {
	if len(m.skills) == 0 {
		return lipgloss.NewStyle().Faint(true).Render("(no skills)")
	}

	var lines []string
	for i, s := range m.skills {
		if i >= h {
			break
		}
		name := s.Name
		if len(name) > w-6 && w > 8 {
			name = name[:w-8] + ".."
		}

		active := m.activeSkillNames[s.Name]
		icon := "○"
		if active {
			icon = "●"
		}

		prefix := "  "
		style := lipgloss.NewStyle()
		if m.focused && i == m.cursor {
			prefix = "> "
			style = style.Bold(true).Foreground(lipgloss.Color("10"))
		}

		enabledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		if active {
			enabledStyle = enabledStyle.Foreground(lipgloss.Color("10"))
		}

		lines = append(lines, style.Render(prefix)+enabledStyle.Render(icon)+" "+style.Render(name))
	}
	return strings.Join(lines, "\n")
}

func (m SidebarModel) renderMCP(w, h int) string {
	if len(m.mcpEntries) == 0 {
		return lipgloss.NewStyle().Faint(true).Render("(no MCP servers)")
	}

	var lines []string
	for i, e := range m.mcpEntries {
		if i >= h {
			break
		}
		name := e.Name
		if len(name) > w-6 && w > 8 {
			name = name[:w-8] + ".."
		}

		active := m.activeMCPServers[e.Name]
		var statusIcon string
		var statusColor lipgloss.Color
		if active && e.Status == mcp.MCPStatusConnected {
			statusIcon = "●"
			statusColor = "10"
		} else if active && e.Status == mcp.MCPStatusConnecting {
			statusIcon = "◐"
			statusColor = "11"
		} else if e.Status == mcp.MCPStatusError {
			statusIcon = "✗"
			statusColor = "9"
		} else {
			statusIcon = "○"
			statusColor = "8"
		}

		prefix := "  "
		style := lipgloss.NewStyle()
		if m.focused && i == m.cursor {
			prefix = "> "
			style = style.Bold(true).Foreground(lipgloss.Color("10"))
		}

		iconStyle := lipgloss.NewStyle().Foreground(statusColor)
		detail := ""
		if e.Status == mcp.MCPStatusConnected {
			detail = fmt.Sprintf(" (%d)", e.ToolCount)
		}

		lines = append(lines, style.Render(prefix)+iconStyle.Render(statusIcon)+" "+style.Render(name+detail))
	}
	return strings.Join(lines, "\n")
}
