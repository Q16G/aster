package tui

import (
	"fmt"
	"strings"
	"time"

	tuiui "aster/internal/tui/ui"
)

func sessionMatchesQuery(session *SessionRecord, query string) bool {
	if session == nil {
		return false
	}
	candidates := []string{
		session.ID,
		session.Title,
		session.AgentName,
		session.ProviderName,
		session.ModelID,
		session.LastMessage,
	}
	for _, candidate := range candidates {
		if strings.Contains(strings.ToLower(candidate), query) {
			return true
		}
	}
	return false
}

func shortSessionID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func selectorTruncate(s string, maxWidth int) string {
	if maxWidth <= 0 || len(s) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return s[:maxWidth]
	}
	return s[:maxWidth-1] + "…"
}

func formatRelativeTime(ts time.Time) string {
	if ts.IsZero() {
		return "unknown"
	}
	delta := time.Since(ts)
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta.Minutes()))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta.Hours()))
	case delta < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(delta.Hours()/24))
	default:
		return ts.Format("2006-01-02")
	}
}

func buildSessionSelectOptions(sessions []*SessionRecord, currentSessionID string) []tuiui.SelectOption {
	var options []tuiui.SelectOption
	for _, session := range sessions {
		if session == nil {
			continue
		}
		title := strings.TrimSpace(session.Title)
		if title == "" {
			title = "Untitled session"
		}

		agent := firstNonEmpty(strings.TrimSpace(session.AgentName), "agent?")
		model := firstNonEmpty(strings.TrimSpace(session.ModelID), "model?")
		desc := fmt.Sprintf("#%s · %s · %s · %d msgs · %s",
			shortSessionID(session.ID),
			agent, model,
			session.MessageCount,
			formatRelativeTime(session.UpdatedAt),
		)

		label := title
		if session.ID == currentSessionID {
			label += " [current]"
		}

		options = append(options, tuiui.SelectOption{
			Label:       label,
			Value:       session.ID,
			Description: desc,
		})
	}
	return options
}
