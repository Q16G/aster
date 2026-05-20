package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderSubAgentCard(sa *SubAgentPart, maxWidth int, expanded, selected bool) string {
	if sa == nil {
		return ""
	}

	icon := "⚡"
	title := sa.AgentName
	if title == "" {
		title = "sub_agent"
	}

	var statusStyle lipgloss.Style
	switch sa.Status {
	case "running":
		statusStyle = lipgloss.NewStyle().Foreground(toolBorderColor)
	case "failed":
		statusStyle = lipgloss.NewStyle().Foreground(toolErrorColor)
	default:
		statusStyle = lipgloss.NewStyle().Foreground(toolCompletedColor)
	}
	if selected {
		statusStyle = statusStyle.Bold(true)
	}

	if !expanded {
		summary := icon + " " + title + " · " + sa.Status
		if sa.Duration > 0 {
			summary += " [" + formatDuration(sa.Duration) + "]"
		}
		return statusStyle.Render(truncateDisplayWidth(summary, maxWidth))
	}

	borderColor := toolCompletedColor
	if sa.Status == "failed" {
		borderColor = toolErrorColor
	} else if sa.Status == "running" {
		borderColor = toolBorderColor
	}

	var content strings.Builder
	content.WriteString(icon + " " + title + "\n")
	content.WriteString("  Status: " + sa.Status)
	if sa.Duration > 0 {
		content.WriteString(" (" + formatDuration(sa.Duration) + ")")
	}
	if sa.WorkspaceRoot != "" {
		content.WriteString("\n  Workspace: " + sa.WorkspaceRoot)
	}
	if sa.Summary != "" {
		content.WriteString("\n  Summary: " + sa.Summary)
	}

	border := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(min(maxWidth-4, 80)).
		Padding(0, 1)
	return border.Render(content.String())
}
