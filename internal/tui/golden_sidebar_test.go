package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func fixtureSidebarEmpty() SidebarModel {
	m := NewSidebarModel()
	m.SetSize(34, 24)
	m.SetSnapshot(SidebarSnapshot{
		// Default empty state: no provider configured. Exercises the
		// Getting Started section path and the empty MCP placeholder.
	})
	return m
}

func fixtureSidebarPopulated() SidebarModel {
	m := NewSidebarModel()
	m.SetSize(34, 24)
	m.SetSnapshot(SidebarSnapshot{
		AgentName:    "code-audit",
		ProviderName: "Anthropic",
		ModelID:      "claude-sonnet-4-6",

		RunStatus:    "running",
		TokenCount:   "12.3k",
		CostEstimate: "$0.04",

		InputTokens:      "8.0k",
		OutputTokens:     "4.3k",
		CacheReadTokens:  "2.1k",
		CacheWriteTokens: "1.2k",
		ReasoningTokens:  "0.5k",

		MCPServers: []MCPStatusEntry{
			{Name: "filesystem", Status: "connected", ToolCount: 7},
			{Name: "github", Status: "connecting"},
			{Name: "playwright", Status: "error"},
		},

		PlanItems: []PlanItemView{
			{Step: "recon repo", Status: "completed"},
			{Step: "analyze findings", Status: "in_progress"},
			{Step: "draft report", Status: "pending"},
		},

		ModifiedFiles: []string{"internal/tui/chat.go", "internal/tui/app.go"},

		ActiveSkills: []string{"git-atomic-commits"},
		ActiveMCPs:   []string{"filesystem"},

		HasProvider: true,

		Workdir: "/Users/me/go/sastx",
		Version: "v0.42.0",
	})
	return m
}

// TestGoldenSidebar locks SidebarModel.View() for two fixtures: an empty model
// (default state, surfaces the Getting Started + empty MCP path) and a fully
// populated snapshot exercising every section.
//
// Re-generate goldens with: go test ./internal/tui/ -run TestGoldenSidebar -update-golden
func TestGoldenSidebar(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)

	cases := []struct {
		name  string
		build func() SidebarModel
	}{
		{"sidebar_empty", fixtureSidebarEmpty},
		{"sidebar_populated", fixtureSidebarPopulated},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			m := c.build()
			diffGolden(t, c.name+".txt", m.View())
		})
	}
}
