package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// subAgentElapsed returns the sub-agent's elapsed time: the recorded Duration
// once finished, or live time since StartedAt while still running.
func subAgentElapsed(sa *SubAgentPart) time.Duration {
	if sa.Duration > 0 {
		return sa.Duration
	}
	if sa.Status == "running" && !sa.StartedAt.IsZero() {
		return time.Since(sa.StartedAt)
	}
	return 0
}

// Kind 常量：与 react.AsyncAgentKind* 对齐（不直接 import 避免循环）。
const (
	subAgentPartKindSubAgent   = "sub_agent"
	subAgentPartKindRemoteStep = "remote_step"
)

func renderSubAgentCard(sa *SubAgentPart, maxWidth int, expanded, selected bool) string {
	if sa == nil {
		return ""
	}

	// 按 Kind 区分卡片样式：sub_agent=⚡，remote_step=⇶（fan-out 多线箭头）。
	// 默认空字符串视同 sub_agent，保持现状向后兼容。
	icon := "⚡"
	titleFallback := "sub_agent"
	if sa.Kind == subAgentPartKindRemoteStep {
		icon = "⇶"
		titleFallback = "remote_step"
	}
	title := sa.AgentName
	if title == "" {
		title = titleFallback
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
		summary := icon + " " + title
		if sa.Description != "" {
			summary += "  " + truncateOneLine(sa.Description, 60)
		}
		summary += " · " + sa.Status
		if d := subAgentElapsed(sa); d > 0 {
			summary += " [" + formatDuration(d) + "]"
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
	if sa.Description != "" {
		content.WriteString("  Task: " + sa.Description + "\n")
	}
	content.WriteString("  Status: " + sa.Status)
	if d := subAgentElapsed(sa); d > 0 {
		content.WriteString(" (" + formatDuration(d) + ")")
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
