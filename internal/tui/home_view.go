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

	subtitleStyle := lipgloss.NewStyle().Faint(true)
	hintStyle := lipgloss.NewStyle().Faint(true)
	ctaStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)

	logo := ""
	for _, line := range asterLogo {
		logo += logoStyle.Render(line) + "\n"
	}

	var sections []string
	sections = append(sections, logo)

	if m.isFirstTimeUser() {
		sections = append(sections,
			subtitleStyle.Render("Terminal workspace for long-running agent work"),
			"",
			ctaStyle.Render("No provider configured"),
			hintStyle.Render("Use /provider to connect an AI provider"),
			"",
			m.renderProviderHints(hintStyle),
		)
	} else {
		sections = append(sections,
			subtitleStyle.Render("Terminal workspace for long-running agent work"),
			"",
			m.renderDynamicHint(hintStyle),
		)

		if !m.localProvider.Get().TipsHidden {
			sections = append(sections,
				"",
				m.renderTips(hintStyle),
			)
		}
	}

	content := lipgloss.JoinVertical(lipgloss.Center, sections...)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m Model) renderDynamicHint(hintStyle lipgloss.Style) string {
	hint := "Type a message to begin"
	if m.providerCfg != nil && m.providerCfg.ModelID != "" {
		hint += "  (" + m.providerCfg.Name + "/" + m.providerCfg.ModelID + ")"
	}
	return hintStyle.Render(hint)
}

func (m Model) renderTips(hintStyle lipgloss.Style) string {
	tips := []string{
		hintStyle.Render("Ctrl+K agent  Ctrl+M model  Ctrl+N new session  Ctrl+B sidebar"),
		hintStyle.Render("/help for commands"),
	}
	return strings.Join(tips, "\n")
}

func (m Model) renderProviderHints(hintStyle lipgloss.Style) string {
	var available []string
	for _, rp := range m.registry.ListProviders() {
		if m.registry.IsProviderAvailable(rp.ID) {
			available = append(available, rp.Name)
		}
	}
	if len(available) == 0 {
		return hintStyle.Render("Supported: OpenAI, Anthropic, DeepSeek, Ollama, ...")
	}
	return hintStyle.Render("Detected: " + strings.Join(available, ", "))
}

func (m Model) currentProviderName() string {
	if m.providerCfg == nil {
		return ""
	}
	return m.providerCfg.Name
}
