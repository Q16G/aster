package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
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
		sb.WriteString(sectionValueStyle.Render(truncateDisplayWidth(snap.AgentName, w-10)))
		sb.WriteString("\n")
	}
	if snap.ProviderName != "" {
		sb.WriteString(sectionDimStyle.Render("  Provider: "))
		sb.WriteString(sectionValueStyle.Render(truncateDisplayWidth(snap.ProviderName, w-12)))
		sb.WriteString("\n")
	}
	if snap.ModelID != "" {
		sb.WriteString(sectionDimStyle.Render("  Model: "))
		sb.WriteString(sectionValueStyle.Render(truncateDisplayWidth(snap.ModelID, w-10)))
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
		sb.WriteString(sectionWarnStyle.Render(truncateDisplayWidth(status, w-10)))
	} else {
		sb.WriteString(sectionValueStyle.Render(truncateDisplayWidth(status, w-10)))
	}
	sb.WriteString("\n")

	sb.WriteString(sectionDimStyle.Render("  Tokens: "))
	sb.WriteString(sectionValueStyle.Render(truncateDisplayWidth(snap.TokenCount, w-10)))
	sb.WriteString("\n")

	if snap.InputTokens != "" || snap.OutputTokens != "" {
		sb.WriteString(sectionDimStyle.Render("    in: "))
		in := snap.InputTokens
		if in == "" {
			in = "0"
		}
		sb.WriteString(sectionValueStyle.Render(in))
		sb.WriteString(sectionDimStyle.Render("  out: "))
		out := snap.OutputTokens
		if out == "" {
			out = "0"
		}
		sb.WriteString(sectionValueStyle.Render(out))
		sb.WriteString("\n")
	}
	if snap.CacheReadTokens != "" || snap.CacheWriteTokens != "" {
		sb.WriteString(sectionDimStyle.Render("    cached: "))
		cr := snap.CacheReadTokens
		if cr == "" {
			cr = "0"
		}
		sb.WriteString(sectionValueStyle.Render(cr))
		sb.WriteString(sectionDimStyle.Render("  written: "))
		cw := snap.CacheWriteTokens
		if cw == "" {
			cw = "0"
		}
		sb.WriteString(sectionValueStyle.Render(cw))
		sb.WriteString("\n")
	}
	if snap.ReasoningTokens != "" {
		sb.WriteString(sectionDimStyle.Render("    reasoning: "))
		sb.WriteString(sectionValueStyle.Render(snap.ReasoningTokens))
		sb.WriteString("\n")
	}

	sb.WriteString(sectionDimStyle.Render("  Cost: "))
	sb.WriteString(sectionValueStyle.Render(truncateDisplayWidth(snap.CostEstimate, w-8)))
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
			icon = "✗ "
			color = "9"
		}

		iconStyle := lipgloss.NewStyle().Foreground(color)
		detail := ""
		if s.Status == "connected" {
			detail = fmt.Sprintf(" (%d)", s.ToolCount)
		}
		iconW := runewidth.StringWidth(icon)
		nameMax := w - 2 - iconW - 1 - len(detail)
		if nameMax < 4 {
			nameMax = 4
		}
		name := truncateDisplayWidth(s.Name, nameMax)
		sb.WriteString("  " + iconStyle.Render(icon) + " " + name + detail + "\n")
	}
	sb.WriteString("\n")
}

// subtreeCompleted reports the total and completed count of all descendants
// of items[parentIdx] (items with depth > parent's depth, contiguously).
func subtreeCompleted(items []PlanItemView, parentIdx int) (total, done int) {
	parentDepth := items[parentIdx].Depth
	for j := parentIdx + 1; j < len(items); j++ {
		if items[j].Depth <= parentDepth {
			break
		}
		if items[j].Status == "cancelled" {
			continue
		}
		total++
		if items[j].Status == "completed" {
			done++
		}
	}
	return
}

func (m *SidebarModel) renderTodoSection(sb *strings.Builder, w int) {
	snap := m.snapshot
	if len(snap.PlanItems) == 0 {
		return
	}

	total := 0
	done := 0
	for _, item := range snap.PlanItems {
		if item.Status == "cancelled" {
			continue
		}
		total++
		if item.Status == "completed" {
			done++
		}
	}

	sb.WriteString(sectionHeader(fmt.Sprintf("Todo [%d/%d]", done, total)))
	sb.WriteString("\n")

	collapseAtDepth := -1
	for i, item := range snap.PlanItems {
		if collapseAtDepth >= 0 && item.Depth > collapseAtDepth {
			continue
		}
		collapseAtDepth = -1

		childTotal, childDone := subtreeCompleted(snap.PlanItems, i)
		collapsed := childTotal > 0 && childDone == childTotal

		icon := "○"
		style := sectionDimStyle
		switch item.Status {
		case "completed":
			icon = "✓ "
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		case "in_progress":
			icon = "▸ "
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
		case "failed":
			icon = "✗ "
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		case "cancelled":
			icon = "⊘ "
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		}

		indentSize := 2 + item.Depth*2
		indent := strings.Repeat(" ", indentSize)

		prefix := ""
		if item.AgentName != "" {
			prefix = "[" + item.AgentName + "] "
		}

		suffix := ""
		if collapsed {
			collapseAtDepth = item.Depth
			suffix = fmt.Sprintf(" (%d/%d)", childDone, childTotal)
		}

		iconW := runewidth.StringWidth(icon)
		maxStep := w - indentSize - iconW - 1 - len(prefix) - len(suffix)
		if maxStep < 4 {
			maxStep = 4
		}
		step := prefix + truncateDisplayWidth(item.Step, maxStep) + suffix
		sb.WriteString(style.Render(indent+icon+" "+step) + "\n")
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
		sb.WriteString("  " + truncateDisplayWidth(f, w-4) + "\n")
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
		sb.WriteString("  " + sectionActiveStyle.Render("● ") + truncateDisplayWidth(name, w-5) + "\n")
	}
	for _, name := range snap.ActiveMCPs {
		sb.WriteString("  " + sectionActiveStyle.Render("● ") + truncateDisplayWidth(name, w-11) + " " + sectionDimStyle.Render("(mcp)") + "\n")
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
		sb.WriteString(truncateDisplayWidth(snap.Workdir, w-8))
		sb.WriteString("\n")
	}
	if snap.Version != "" {
		sb.WriteString(sectionDimStyle.Render("  Ver: "))
		sb.WriteString(truncateDisplayWidth(snap.Version, w-8))
		sb.WriteString("\n")
	}
	if snap.UpdateAvailable != "" {
		updateStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		updateText := truncateDisplayWidth("  ⬆ "+snap.UpdateAvailable+" available (aster update)", w)
		sb.WriteString(updateStyle.Render(updateText))
		sb.WriteString("\n")
	}
}

