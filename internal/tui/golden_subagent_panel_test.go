package tui

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func fixtureSubAgentPanelEmpty() SubAgentPanel {
	p := NewSubAgentPanel()
	p.SetSize(subAgentPanelWidth, 12)
	p.SetSnapshot(nil)
	return p
}

func fixtureSubAgentPanelMixed() SubAgentPanel {
	p := NewSubAgentPanel()
	p.SetSize(subAgentPanelWidth, 14)
	p.SetSnapshot([]subAgentPanelItem{
		{
			CallID:      "call_aaa1234",
			Title:       "scan repo for TODOs",
			Description: "rg + filter",
			Status:      "completed",
			Elapsed:     5 * time.Second,
		},
		{
			CallID:      "call_bbb9999",
			Title:       "summarize README",
			Description: "read + condense",
			Status:      "running",
			Elapsed:     2 * time.Second,
			Running:     true,
		},
	})
	return p
}

// TestGoldenSubAgentPanel locks SubAgentPanel.View() for two fixtures: an empty
// panel (surfaces the "(none)" placeholder) and a mixed state with one
// completed + one running sub-agent (selection cursor on the first item).
//
// Re-generate goldens with: go test ./internal/tui/ -run TestGoldenSubAgentPanel -update-golden
func TestGoldenSubAgentPanel(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)

	cases := []struct {
		name  string
		build func() SubAgentPanel
	}{
		{"subagent_panel_empty", fixtureSubAgentPanelEmpty},
		{"subagent_panel_mixed", fixtureSubAgentPanelMixed},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			p := c.build()
			diffGolden(t, c.name+".txt", p.View())
		})
	}
}
