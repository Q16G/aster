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
	bp, ok := GetBuiltinProvider(m.providerCfg.Name)
	if !ok {
		return m.providerCfg.BaseURL == ""
	}
	return bp.APIKeyEnvVar != "" && resolveAPIKey(bp, "") == ""
}

func (m *Model) startProviderOnboarding() (tea.Model, tea.Cmd) {
	return m.openProviderSelector()
}

func (m *Model) handleProviderSelected(providerID string) (tea.Model, tea.Cmd) {
	bp, isBuiltin := GetBuiltinProvider(providerID)
	if !isBuiltin {
		return m, func() tea.Msg { return ProviderSwitchMsg{Name: providerID} }
	}

	cfgProvider := m.appCfg.Providers[providerID]
	apiKey := resolveAPIKey(bp, "")
	if cfgProvider != nil {
		apiKey = resolveAPIKey(bp, cfgProvider.APIKey)
	}

	if apiKey != "" || bp.APIKeyEnvVar == "" {
		return m, func() tea.Msg { return ProviderSwitchMsg{Name: providerID} }
	}

	m.pendingProvider = &pendingProviderSetup{ProviderID: providerID}
	prompt := fmt.Sprintf("Enter API key for %s", bp.Name)
	if bp.APIKeyEnvVar != "" {
		prompt += fmt.Sprintf(" (or set %s)", bp.APIKeyEnvVar)
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
	bp, isBuiltin := GetBuiltinProvider(providerID)

	var models []string
	if isBuiltin {
		models = bp.Models
	}

	if len(models) == 0 {
		if m.pendingPrompt != "" {
			prompt := m.pendingPrompt
			m.pendingPrompt = ""
			return m, func() tea.Msg { return UserSubmitMsg{Text: prompt} }
		}
		return m, nil
	}

	options := make([]tuiui.SelectOption, 0, len(models))
	for _, mid := range models {
		desc := ""
		if m.providerCfg.ModelID == mid {
			desc = "(current)"
		}
		options = append(options, tuiui.SelectOption{
			Label:       mid,
			Value:       mid,
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
