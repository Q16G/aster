package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type SidebarModel struct {
	focused  bool
	width    int
	height   int
	viewport viewport.Model
	snapshot SidebarSnapshot
}

func NewSidebarModel() SidebarModel {
	vp := viewport.New(0, 0)
	return SidebarModel{viewport: vp}
}

func (m *SidebarModel) SetSize(w, h int) {
	m.width = w
	m.height = h
	innerW := w - 3
	if innerW < 1 {
		innerW = 1
	}
	innerH := h - 2
	if innerH < 1 {
		innerH = 1
	}
	m.viewport.Width = innerW
	m.viewport.Height = innerH
	m.refreshView()
}

func (m *SidebarModel) SetFocused(focused bool) {
	m.focused = focused
}

func (m SidebarModel) IsFocused() bool {
	return m.focused
}

func (m *SidebarModel) SetSnapshot(snap SidebarSnapshot) {
	m.snapshot = snap
	m.refreshView()
}

func (m SidebarModel) Update(msg tea.Msg) (SidebarModel, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m SidebarModel) View() string {
	if m.width < 4 || m.height < 3 {
		return ""
	}

	borderColor := lipgloss.Color("240")
	if m.focused {
		borderColor = lipgloss.Color("62")
	}

	containerStyle := lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		Padding(0, 1)

	return containerStyle.Render(m.viewport.View())
}

func (m *SidebarModel) refreshView() {
	w := m.width - 3
	if w < 1 {
		w = 1
	}

	var sb strings.Builder

	m.renderIdentitySection(&sb, w)
	m.renderContextSection(&sb, w)
	m.renderMCPSection(&sb, w)
	m.renderTodoSection(&sb, w)
	m.renderFilesSection(&sb, w)
	m.renderSkillsSection(&sb, w)
	m.renderGettingStartedSection(&sb, w)
	m.renderWorkdirSection(&sb, w)

	m.viewport.SetContent(strings.TrimRight(sb.String(), "\n"))
}

var (
	sectionHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	sectionDimStyle    = lipgloss.NewStyle().Faint(true)
	sectionValueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	sectionActiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	sectionWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

func sectionHeader(title string) string {
	return sectionHeaderStyle.Render("▸ " + title)
}

func (m *SidebarModel) renderIdentitySection(sb *strings.Builder, w int) {
	sb.WriteString(sectionHeader("Session"))
	sb.WriteString("\n")

	snap := m.snapshot

	if snap.AgentName != "" {
		sb.WriteString(sectionDimStyle.Render("  Agent: "))
		sb.WriteString(sectionValueStyle.Render(truncSidebar(snap.AgentName, w-10)))
		sb.WriteString("\n")
	}
	if snap.ProviderName != "" {
		sb.WriteString(sectionDimStyle.Render("  Provider: "))
		sb.WriteString(sectionValueStyle.Render(truncSidebar(snap.ProviderName, w-12)))
		sb.WriteString("\n")
	}
	if snap.ModelID != "" {
		sb.WriteString(sectionDimStyle.Render("  Model: "))
		sb.WriteString(sectionValueStyle.Render(truncSidebar(snap.ModelID, w-10)))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
}

func (m *SidebarModel) renderContextSection(sb *strings.Builder, w int) {
	sb.WriteString(sectionHeader("Context"))
	sb.WriteString("\n")

	snap := m.snapshot

	sb.WriteString(sectionDimStyle.Render("  Status: "))
	status := snap.RunStatus
	if status == "" {
		status = "idle"
	}
	if status == "running" {
		sb.WriteString(sectionWarnStyle.Render(status))
	} else {
		sb.WriteString(sectionValueStyle.Render(status))
	}
	sb.WriteString("\n")

	sb.WriteString(sectionDimStyle.Render("  Tokens: "))
	sb.WriteString(sectionValueStyle.Render(snap.TokenCount))
	sb.WriteString("\n")

	sb.WriteString(sectionDimStyle.Render("  Cost: "))
	sb.WriteString(sectionValueStyle.Render(snap.CostEstimate))
	sb.WriteString("\n")
	sb.WriteString("\n")
}

func (m *SidebarModel) renderMCPSection(sb *strings.Builder, w int) {
	snap := m.snapshot
	if len(snap.MCPServers) == 0 {
		sb.WriteString(sectionHeader("MCP"))
		sb.WriteString("\n")
		sb.WriteString(sectionDimStyle.Render("  (no servers)"))
		sb.WriteString("\n\n")
		return
	}

	connected := 0
	for _, s := range snap.MCPServers {
		if s.Status == "connected" {
			connected++
		}
	}

	sb.WriteString(sectionHeader(fmt.Sprintf("MCP (%d/%d)", connected, len(snap.MCPServers))))
	sb.WriteString("\n")

	for _, s := range snap.MCPServers {
		icon := "○"
		color := lipgloss.Color("8")
		switch s.Status {
		case "connected":
			icon = "●"
			color = "10"
		case "connecting":
			icon = "◐"
			color = "11"
		case "error":
			icon = "✗"
			color = "9"
		}

		iconStyle := lipgloss.NewStyle().Foreground(color)
		name := truncSidebar(s.Name, w-8)
		detail := ""
		if s.Status == "connected" {
			detail = fmt.Sprintf(" (%d)", s.ToolCount)
		}
		sb.WriteString("  " + iconStyle.Render(icon) + " " + name + detail + "\n")
	}
	sb.WriteString("\n")
}

func (m *SidebarModel) renderTodoSection(sb *strings.Builder, w int) {
	snap := m.snapshot
	if len(snap.PlanItems) == 0 {
		return
	}

	total := len(snap.PlanItems)
	done := 0
	for _, item := range snap.PlanItems {
		if item.Status == "completed" {
			done++
		}
	}

	sb.WriteString(sectionHeader(fmt.Sprintf("Todo [%d/%d]", done, total)))
	sb.WriteString("\n")

	for _, item := range snap.PlanItems {
		icon := "○"
		style := sectionDimStyle
		switch item.Status {
		case "completed":
			icon = "✓"
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		case "in_progress":
			icon = "▸"
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
		case "failed":
			icon = "✗"
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		}
		step := truncSidebar(item.Step, w-6)
		sb.WriteString(style.Render("  "+icon+" "+step) + "\n")
	}
	sb.WriteString("\n")
}

func (m *SidebarModel) renderFilesSection(sb *strings.Builder, w int) {
	snap := m.snapshot
	if len(snap.ModifiedFiles) == 0 {
		return
	}

	sb.WriteString(sectionHeader(fmt.Sprintf("Files (%d)", len(snap.ModifiedFiles))))
	sb.WriteString("\n")

	maxShow := 15
	for i, f := range snap.ModifiedFiles {
		if i >= maxShow {
			sb.WriteString(sectionDimStyle.Render(fmt.Sprintf("  ... +%d more", len(snap.ModifiedFiles)-maxShow)))
			sb.WriteString("\n")
			break
		}
		sb.WriteString("  " + truncSidebar(f, w-4) + "\n")
	}
	sb.WriteString("\n")
}

func (m *SidebarModel) renderSkillsSection(sb *strings.Builder, w int) {
	snap := m.snapshot
	if len(snap.ActiveSkills) == 0 && len(snap.ActiveMCPs) == 0 {
		return
	}

	sb.WriteString(sectionHeader("Active"))
	sb.WriteString("\n")

	for _, name := range snap.ActiveSkills {
		sb.WriteString("  " + sectionActiveStyle.Render("● ") + truncSidebar(name, w-6) + "\n")
	}
	for _, name := range snap.ActiveMCPs {
		sb.WriteString("  " + sectionActiveStyle.Render("● ") + truncSidebar(name, w-6) + " " + sectionDimStyle.Render("(mcp)") + "\n")
	}
	sb.WriteString("\n")
}

func (m *SidebarModel) renderGettingStartedSection(sb *strings.Builder, w int) {
	snap := m.snapshot
	if snap.HasProvider || snap.DismissedGettingStarted {
		return
	}

	sb.WriteString(sectionHeader("Getting Started"))
	sb.WriteString("\n")
	sb.WriteString(sectionWarnStyle.Render("  No provider configured."))
	sb.WriteString("\n")
	sb.WriteString(sectionDimStyle.Render("  Use /provider to set up."))
	sb.WriteString("\n\n")
}

func (m *SidebarModel) renderWorkdirSection(sb *strings.Builder, w int) {
	snap := m.snapshot
	if snap.Workdir == "" && snap.Version == "" {
		return
	}

	sb.WriteString(sectionHeader("Info"))
	sb.WriteString("\n")
	if snap.Workdir != "" {
		sb.WriteString(sectionDimStyle.Render("  Dir: "))
		sb.WriteString(truncSidebar(snap.Workdir, w-8))
		sb.WriteString("\n")
	}
	if snap.Version != "" {
		sb.WriteString(sectionDimStyle.Render("  Ver: "))
		sb.WriteString(snap.Version)
		sb.WriteString("\n")
	}
}

func truncSidebar(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if len(s) <= maxWidth {
		return s
	}
	if maxWidth <= 2 {
		return s[:maxWidth]
	}
	return s[:maxWidth-2] + ".."
}
