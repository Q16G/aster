package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var asterLogo = []string{
	"  █████╗ ███████╗████████╗███████╗██████╗ ",
	" ██╔══██╗██╔════╝╚══██╔══╝██╔════╝██╔══██╗",
	" ███████║███████╗   ██║   █████╗  ██████╔╝",
	" ██╔══██║╚════██║   ██║   ██╔══╝  ██╔══██╗",
	" ██║  ██║███████║   ██║   ███████╗██║  ██║",
	" ╚═╝  ╚═╝╚══════╝   ╚═╝   ╚══════╝╚═╝  ╚═╝",
}

func (m Model) renderHomeView(width, height int) string {
	th := m.themeProvider.Get()

	logoStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.FocusBorderColor)

	subtitleStyle := lipgloss.NewStyle().
		Faint(true)

	hintStyle := lipgloss.NewStyle().
		Faint(true)

	logo := ""
	for _, line := range asterLogo {
		logo += logoStyle.Render(line) + "\n"
	}

	hints := []string{
		"Type a message to begin",
		"",
		hintStyle.Render("Ctrl+K agent  Ctrl+M model  Ctrl+N new session"),
		hintStyle.Render("/help for commands"),
	}

	content := lipgloss.JoinVertical(
		lipgloss.Center,
		logo,
		subtitleStyle.Render("Terminal workspace for long-running agent work"),
		"",
		strings.Join(hints, "\n"),
	)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) currentProviderName() string {
	if m.appCfg == nil || m.providerCfg == nil {
		return ""
	}
	for name, provider := range m.appCfg.Providers {
		if provider == nil {
			continue
		}
		if strings.TrimSpace(provider.BaseURL) == strings.TrimSpace(m.providerCfg.BaseURL) {
			return name
		}
	}
	return ""
}
