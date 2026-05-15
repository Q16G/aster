package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	tuiui "aster/internal/tui/ui"
)

func (m Model) isFirstTimeUser() bool {
	if m.providerCfg == nil {
		return true
	}
	if m.providerCfg.APIKey != "" {
		return false
	}
	envVar := m.registry.ProviderEnvVar(m.providerCfg.Name)
	if envVar == "" {
		return m.providerCfg.BaseURL == ""
	}
	return m.registry.ResolveAPIKey(m.providerCfg.Name, "") == ""
}

func (m *Model) startProviderOnboarding() (tea.Model, tea.Cmd) {
	return m.openProviderSelector()
}

func (m *Model) handleProviderSelected(providerID string) (tea.Model, tea.Cmd) {
	rp, inRegistry := m.registry.GetProvider(providerID)

	cfgProvider := m.appCfg.Providers[providerID]
	cfgKey := ""
	if cfgProvider != nil {
		cfgKey = cfgProvider.APIKey
	}
	apiKey := m.registry.ResolveAPIKey(providerID, cfgKey)
	envVar := m.registry.ProviderEnvVar(providerID)

	if !inRegistry && cfgProvider == nil {
		return m, func() tea.Msg { return ProviderSwitchMsg{Name: providerID} }
	}

	if apiKey != "" || envVar == "" {
		return m, func() tea.Msg { return ProviderSwitchMsg{Name: providerID} }
	}

	pName := providerID
	if rp != nil {
		pName = rp.Name
	}
	m.pendingProvider = &pendingProviderSetup{ProviderID: providerID}
	prompt := fmt.Sprintf("Enter API key for %s", pName)
	if envVar != "" {
		prompt += fmt.Sprintf(" (or set %s)", envVar)
	}
	dialog := tuiui.NewPromptDialog("provider-apikey:"+providerID, prompt, "")
	m.dialogStack.Push(dialog, nil)
	m.dialogStack.SetSize(m.width, m.height)
	return m, nil
}

func (m *Model) openModelSelectorAfterProviderSetup() (tea.Model, tea.Cmd) {
	if m.providerCfg == nil {
		return m, nil
	}

	providerID := m.providerCfg.Name
	regModels := m.registry.ListModels(providerID)

	if len(regModels) == 0 {
		if m.pendingPrompt != "" {
			prompt := m.pendingPrompt
			m.pendingPrompt = ""
			return m, func() tea.Msg { return UserSubmitMsg{Text: prompt} }
		}
		return m, nil
	}

	options := make([]tuiui.SelectOption, 0, len(regModels))
	for _, mi := range regModels {
		desc := ""
		if m.providerCfg.ModelID == mi.ID {
			desc = "(current)"
		}
		options = append(options, tuiui.SelectOption{
			Label:       mi.Name,
			Value:       mi.ID,
			Description: desc,
		})
	}

	dialog := tuiui.NewSelectDialog("onboarding-model-select", "Select Model", options)
	m.dialogStack.Push(dialog, nil)
	m.dialogStack.SetSize(m.width, m.height)
	return m, nil
}

func (m *Model) tryProviderGuardedSubmit(text string) (tea.Model, tea.Cmd) {
	if !m.isFirstTimeUser() {
		return m, nil
	}

	m.pendingPrompt = text
	m.chat.AddPart(DisplayPart{
		Type:   PartTypeSystem,
		Time:   time.Now(),
		System: &SystemPart{Content: "No provider configured. Opening provider setup..."},
	})
	return m.startProviderOnboarding()
}

func (m *Model) isProviderConfigField(id string) bool {
	return strings.HasPrefix(id, "provider-apikey:")
}
