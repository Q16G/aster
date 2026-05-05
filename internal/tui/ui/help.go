package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type HelpSection struct {
	Title string
	Items []HelpItem
}

type HelpItem struct {
	Key         string
	Description string
}

type HelpDialog struct {
	id       string
	sections []HelpSection
	viewport viewport.Model
	width    int
	height   int
	ready    bool
}

func NewHelpDialog(sections []HelpSection) *HelpDialog {
	return &HelpDialog{
		id:       "help",
		sections: sections,
	}
}

func (h *HelpDialog) ID() string { return h.id }

func (h *HelpDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "q":
			return h, func() tea.Msg { return AlertDismissedMsg{DialogID: h.id} }
		}
	}

	var cmd tea.Cmd
	h.viewport, cmd = h.viewport.Update(msg)
	return h, cmd
}

func (h *HelpDialog) View() string {
	if !h.ready {
		h.buildContent()
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	hintStyle := lipgloss.NewStyle().Faint(true)

	content := fmt.Sprintf(
		"%s\n%s\n%s",
		titleStyle.Render(" Help "),
		h.viewport.View(),
		hintStyle.Render(" Esc to close • ↑/↓ to scroll"),
	)

	dialog := border.Render(content)
	return lipgloss.Place(h.width, h.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (h *HelpDialog) buildContent() {
	w := min(h.width-6, 60)
	viewH := min(h.height-8, 30)
	if viewH < 5 {
		viewH = 5
	}

	h.viewport = viewport.New(w, viewH)
	h.viewport.Style = lipgloss.NewStyle()

	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Width(16)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))

	var sb strings.Builder
	for i, section := range h.sections {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(sectionStyle.Render(section.Title))
		sb.WriteString("\n")
		for _, item := range section.Items {
			sb.WriteString(fmt.Sprintf("  %s%s\n",
				keyStyle.Render(item.Key),
				descStyle.Render(item.Description),
			))
		}
	}

	h.viewport.SetContent(sb.String())
	h.ready = true
}

func (h *HelpDialog) SetSize(w, h2 int) {
	h.width = w
	h.height = h2
	h.ready = false
}

func DefaultHelpSections() []HelpSection {
	return []HelpSection{
		{
			Title: "Navigation",
			Items: []HelpItem{
				{"Tab", "Cycle focus: Input → Sidebar → Chat"},
				{"Esc", "Return to input"},
				{"Ctrl+L", "Clear chat display"},
			},
		},
		{
			Title: "Sessions",
			Items: []HelpItem{
				{"Ctrl+N", "New session"},
				{"Ctrl+O", "Open session list"},
			},
		},
		{
			Title: "Agent & Model",
			Items: []HelpItem{
				{"Ctrl+K", "Switch agent profile"},
				{"Ctrl+M", "Switch model"},
			},
		},
		{
			Title: "Slash Commands",
			Items: []HelpItem{
				{"/agent", "Switch agent profile"},
				{"/model", "Switch model"},
				{"/provider", "Switch AI provider"},
				{"/skill", "Toggle skills"},
				{"/mcp", "Toggle MCP servers"},
				{"/session", "Session management"},
				{"/theme", "Toggle dark/light theme"},
				{"/verbose", "Toggle tool output verbosity"},
				{"/mode", "Switch bash permission mode"},
				{"/clear", "Clear chat"},
				{"/help", "Show this help"},
			},
		},
		{
			Title: "Other",
			Items: []HelpItem{
				{"Ctrl+C", "Cancel agent / Quit (2x)"},
			},
		},
	}
}
