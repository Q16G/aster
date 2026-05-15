package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/mcp"
	"aster/internal/provider"
	tuiui "aster/internal/tui/ui"
)

var slashCommands = []tuiui.CommandEntry{
	{Name: "/agent", Description: "Switch agent profile"},
	{Name: "/provider", Description: "List or switch AI provider"},
	{Name: "/model", Description: "Switch model"},
	{Name: "/skill", Description: "Toggle skill"},
	{Name: "/mcp", Description: "Toggle MCP connection"},
	{Name: "/session", Description: "Session management"},
	{Name: "/new", Description: "New session"},
	{Name: "/clear", Description: "Clear chat history"},
	{Name: "/verbose", Description: "Toggle tool call detail"},
	{Name: "/mode", Description: "Switch bash permission mode (yolo/manual/ai)"},
	{Name: "/theme", Description: "Switch theme"},
	{Name: "/sidebar", Description: "Toggle sidebar (show/hide/auto)"},
	{Name: "/help", Description: "Show help"},
	{Name: "/exit", Description: "Quit application"},
}

const helpText = `Available commands:
  /agent [name]          — Switch agent profile (or open selector)
  /provider [name]       — List or switch AI provider
  /model [name]          — Open selector or switch model
  /skill [enable|disable] <name> — Toggle skill for current session
  /mcp [connect|disconnect] <name> — Toggle MCP for current session
  /session [new|list|switch|delete] — Session management / selector
  /clear                 — Clear chat history
  /verbose               — Toggle tool call detail level
  /mode [yolo|manual|ai] — Switch bash permission mode
  /theme                 — Toggle dark/light theme
  /sidebar [show|hide|auto] — Toggle or set sidebar mode
  /help                  — Show this help

Shortcuts:
  Tab            — Cycle focus: Input → Sidebar → Chat
  Escape         — Return focus to Input
  Ctrl+N         — New session
  Ctrl+O         — Open session selector
  Ctrl+K         — Open agent selector
  Ctrl+M         — Open model selector
  Ctrl+L         — Clear chat
  Ctrl+B         — Toggle sidebar
  Ctrl+C         — Cancel agent (running) / Quit (idle, double-press)

Scrolling:
  In AltScreen mode the terminal scrollback won't contain the full chat history.
  Use Tab to focus chat and PageUp/PageDown to scroll.
  Trackpad/mouse-wheel scrolling is enabled by default. If selection feels odd, try Shift+Drag to select text.`

func (m *Model) handleSlashCommand(cmd string) (tea.Model, tea.Cmd) {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return m, nil
	}

	switch parts[0] {
	case "/agent", "/agents":
		return m.cmdAgent(parts[1:])
	case "/provider", "/connect":
		return m.cmdProvider(parts[1:])
	case "/model", "/models":
		return m.cmdModel(parts[1:])
	case "/skill":
		return m.cmdSkill(parts[1:])
	case "/mcp", "/mcps":
		return m.cmdMCP(parts[1:])
	case "/new":
		return m.cmdSession([]string{"new"})
	case "/exit":
		m.persistCurrentSession()
		return m, tea.Quit
	case "/session":
		return m.cmdSession(parts[1:])
	case "/clear":
		m.chat = NewChatModel()
		m.restoreToolVerbose()
		m.updateLayout()
		m.persistCurrentSession()
		return m, nil
	case "/verbose":
		m.localProvider.ToggleToolVerbose()
		m.chat.SetToolVerbose(m.localProvider.Get().ToolVerbose)
		state := "compact"
		if m.localProvider.Get().ToolVerbose {
			state = "verbose"
		}
		m.statusText = fmt.Sprintf("tool display: %s", state)
		return m, nil
	case "/mode":
		return m.cmdMode(parts[1:])
	case "/sidebar":
		return m.cmdSidebar(parts[1:])
	case "/theme":
		if len(parts) > 1 {
			if parts[1] == "toggle" {
				m.themeProvider.Toggle()
			} else {
				m.themeProvider.SetByName(parts[1])
			}
			m.sessionMeta.Theme = m.themeProvider.Get().Name
			m.persistSessionMeta()
			m.statusText = fmt.Sprintf("theme: %s", m.themeProvider.Get().Name)
			return m, nil
		}
		return m.openThemeSelector()
	case "/help":
		helpDialog := tuiui.NewHelpDialog(tuiui.DefaultHelpSections())
		m.dialogStack.Push(helpDialog, nil)
		m.dialogStack.SetSize(m.width, m.height)
		return m, nil
	default:
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "unknown command: " + parts[0]}})
		return m, nil
	}
}

func (m *Model) cmdAgent(args []string) (tea.Model, tea.Cmd) {
	if len(args) > 0 {
		if m.profileRegistry != nil {
			if def, ok := m.profileRegistry.Get(args[0]); ok {
				m.agentCtx.Definition = def
				m.updateSessionAgent(def.Name)
				m.applySessionRuntimeState()
				m.refreshSidebarData()
				m.statusText = fmt.Sprintf("switched to %s", def.Name)
				return m, m.input.Focus()
			}
		}
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "unknown agent: " + args[0]}})
	} else {
		if m.profileRegistry != nil {
			m.showAgentSelector()
		}
	}
	return m, nil
}

func (m *Model) cmdProvider(args []string) (tea.Model, tea.Cmd) {
	if m.appCfg == nil || m.providerCfg == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "provider config not available"}})
		return m, nil
	}

	if len(args) == 0 {
		return m.openProviderSelector()
	}

	name := args[0]
	_, inRegistry := m.registry.GetProvider(name)
	_, inConfig := m.appCfg.Providers[name]
	if !inRegistry && !inConfig {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "unknown provider: " + name}})
		return m, nil
	}
	return m, func() tea.Msg { return ProviderSwitchMsg{Name: name} }
}

func (m *Model) openProviderSelector() (tea.Model, tea.Cmd) {
	if m.providerCfg == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "provider config not available"}})
		return m, nil
	}

	isCurrent := func(id string) bool {
		return m.providerCfg.Name == id
	}

	isConfigured := func(rp *provider.ProviderInfo) bool {
		cfgKey := ""
		if cfgProvider := m.appCfg.Providers[rp.ID]; cfgProvider != nil {
			cfgKey = cfgProvider.APIKey
		}
		if m.credStore != nil && m.credStore.Get(rp.ID) != "" {
			return true
		}
		return m.registry.ResolveAPIKey(rp.ID, cfgKey) != "" || len(rp.EnvVars) == 0
	}

	makeOption := func(rp *provider.ProviderInfo, configured bool) tuiui.SelectOption {
		icon := "○"
		if configured {
			icon = "✓"
		}
		desc := fmt.Sprintf("%d models", len(rp.Models))
		if cfgProvider := m.appCfg.Providers[rp.ID]; cfgProvider != nil && cfgProvider.DefaultModel != "" {
			desc = cfgProvider.DefaultModel
		}
		if isCurrent(rp.ID) {
			desc += " (current)"
		}
		if !configured && len(rp.EnvVars) > 0 {
			desc += fmt.Sprintf("  [set %s]", rp.EnvVars[0])
		}
		return tuiui.SelectOption{
			Label:       icon + " " + rp.Name,
			Value:       rp.ID,
			Description: desc,
		}
	}

	seen := make(map[string]bool)
	priority := m.appCfg.effectiveProviderPriority()
	providers := m.registry.ListProvidersSorted(priority, func(id string) bool {
		rp, ok := m.registry.GetProvider(id)
		if !ok {
			return false
		}
		return isConfigured(rp)
	})

	var configured, envAvailable, rest []tuiui.SelectOption
	for _, rp := range providers {
		seen[rp.ID] = true
		opt := makeOption(rp, isConfigured(rp))
		if isConfigured(rp) {
			configured = append(configured, opt)
		} else if m.registry.IsProviderAvailable(rp.ID) {
			envAvailable = append(envAvailable, opt)
		} else {
			rest = append(rest, opt)
		}
	}

	if m.appCfg != nil {
		for name, p := range m.appCfg.Providers {
			if seen[name] {
				continue
			}
			icon := "✓"
			if p.APIKey == "" && p.BaseURL == "" {
				icon = "○"
			}
			desc := p.DefaultModel
			if isCurrent(name) {
				desc += " (current)"
			}
			configured = append(configured, tuiui.SelectOption{
				Label:       icon + " " + name,
				Value:       name,
				Description: desc,
			})
		}
	}

	options := make([]tuiui.SelectOption, 0, len(configured)+len(envAvailable)+len(rest)+6)
	if len(configured) > 0 {
		options = append(options, tuiui.SelectOption{Label: "── Configured ──", Disabled: true})
		options = append(options, configured...)
	}
	if len(envAvailable) > 0 {
		options = append(options, tuiui.SelectOption{Label: "── Available (env) ──", Disabled: true})
		options = append(options, envAvailable...)
	}
	if len(rest) > 0 {
		options = append(options, tuiui.SelectOption{Label: "── All Providers ──", Disabled: true})
		options = append(options, rest...)
	}

	dialog := tuiui.NewSelectDialog("provider-select", "Select Provider", options)
	m.dialogStack.Push(dialog, nil)
	m.dialogStack.SetSize(m.width, m.height)
	return m, nil
}

func (m *Model) handleProviderPromptResult(msg tuiui.PromptResultMsg) (tea.Model, tea.Cmd) {
	if msg.Cancelled {
		m.pendingProvider = nil
		m.pendingPrompt = ""
		return m, nil
	}

	switch {
	case strings.HasPrefix(msg.DialogID, "provider-apikey:"):
		providerID := strings.TrimPrefix(msg.DialogID, "provider-apikey:")
		apiKey := strings.TrimSpace(msg.Value)
		if apiKey == "" {
			m.pendingProvider = nil
			m.pendingPrompt = ""
			return m, nil
		}

		if m.credStore != nil {
			if err := m.credStore.Set(providerID, apiKey); err != nil {
				m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("failed to save API key: %v", err)}})
				m.pendingProvider = nil
				return m, nil
			}
		}

		if err := SaveConfig(DefaultConfigPath(), func(cfg *AppConfig) {
			if cfg.Providers == nil {
				cfg.Providers = make(map[string]*ProviderConfig)
			}
			p := cfg.Providers[providerID]
			if p == nil {
				p = &ProviderConfig{}
				cfg.Providers[providerID] = p
			}
			if rp, ok := m.registry.GetProvider(providerID); ok {
				if p.BaseURL == "" {
					p.BaseURL = rp.BaseURL
				}
			}
		}); err != nil {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("failed to save provider config: %v", err)}})
		}

		if m.appCfg.Providers == nil {
			m.appCfg.Providers = make(map[string]*ProviderConfig)
		}
		p := m.appCfg.Providers[providerID]
		if p == nil {
			p = &ProviderConfig{}
			m.appCfg.Providers[providerID] = p
		}
		if rp, ok := m.registry.GetProvider(providerID); ok {
			if p.BaseURL == "" {
				p.BaseURL = rp.BaseURL
			}
		}

		m.pendingProvider = nil
		return m, func() tea.Msg { return ProviderSwitchMsg{Name: providerID} }
	}

	m.pendingProvider = nil
	return m, nil
}

func (m *Model) cmdModel(args []string) (tea.Model, tea.Cmd) {
	if m.providerCfg == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "provider config not available"}})
		return m, nil
	}

	if len(args) == 0 {
		return m, m.fetchAllProviderModelsCmd()
	}

	m.providerCfg.ModelID = args[0]
	if m.agentCtx != nil {
		m.agentCtx.Definition.ModelID = args[0]
	}
	m.rememberRecentModel(args[0])
	m.persistSessionMeta()
	m.statusText = fmt.Sprintf("model: %s", args[0])
	return m, nil
}

type providerFetchInfo struct {
	providerID string
	baseURL    string
	apiKey     string
}

func (m *Model) configuredProviderIDs() []string {
	if m.appCfg == nil {
		if m.providerCfg != nil {
			return []string{m.providerCfg.Name}
		}
		return nil
	}

	priority := m.appCfg.effectiveProviderPriority()
	currentID := ""
	if m.providerCfg != nil {
		currentID = m.providerCfg.Name
	}

	seen := make(map[string]bool)
	var result []string

	if currentID != "" {
		result = append(result, currentID)
		seen[currentID] = true
	}

	for _, pid := range priority {
		if seen[pid] {
			continue
		}
		if _, ok := m.appCfg.Providers[pid]; ok {
			result = append(result, pid)
			seen[pid] = true
		}
	}

	for pid := range m.appCfg.Providers {
		if !seen[pid] {
			result = append(result, pid)
			seen[pid] = true
		}
	}

	return result
}

func (m *Model) fetchAllProviderModelsCmd() tea.Cmd {
	providerIDs := m.configuredProviderIDs()
	if len(providerIDs) == 0 {
		return func() tea.Msg {
			return ModelPickerFailedMsg{Err: fmt.Errorf("no providers configured")}
		}
	}

	registryModels := make(map[string][]ModelOption)
	var needsFetch []providerFetchInfo

	for _, pid := range providerIDs {
		if models := m.registry.ListModels(pid); len(models) > 0 {
			registryModels[pid] = registryModelsToOptions(pid, models)
		} else {
			state := m.appCfg.ResolveProviderState(pid, "", "", "", m.registry, m.credStore)
			if state != nil && state.BaseURL != "" {
				needsFetch = append(needsFetch, providerFetchInfo{
					providerID: pid,
					baseURL:    state.BaseURL,
					apiKey:     state.APIKey,
				})
			}
		}
	}

	if len(needsFetch) == 0 {
		result := registryModels
		return func() tea.Msg {
			return MultiProviderModelPickerLoadedMsg{ModelsByProvider: result}
		}
	}

	m.statusText = "loading models..."
	return func() tea.Msg {
		result := make(map[string][]ModelOption, len(registryModels)+len(needsFetch))
		for k, v := range registryModels {
			result[k] = v
		}

		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, fi := range needsFetch {
			wg.Add(1)
			go func(fi providerFetchInfo) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				models, err := ai.ListModels(ctx, fi.baseURL, fi.apiKey)
				if err != nil {
					return
				}
				opts := modelOptionsFromDescriptors(fi.providerID, models)
				mu.Lock()
				result[fi.providerID] = opts
				mu.Unlock()
			}(fi)
		}
		wg.Wait()
		return MultiProviderModelPickerLoadedMsg{ModelsByProvider: result}
	}
}

func (m *Model) cmdSkill(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		return m.openSkillSelector()
	}

	switch args[0] {
	case "list":
		return m.openSkillSelector()
	case "enable":
		if len(args) < 2 {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /skill enable <name>"}})
			return m, nil
		}
		if m.isPreloadedSkill(args[1]) {
			m.statusText = fmt.Sprintf("skill pinned: %s", args[1])
			return m, m.toastManager.Push(fmt.Sprintf("skill pinned by agent: %s", args[1]), tuiui.ToastInfo, 3*time.Second)
		}
		m.toggleSessionSkill(args[1], true)
		m.statusText = fmt.Sprintf("skill enabled: %s", args[1])
		return m, m.toastManager.Push(fmt.Sprintf("skill enabled: %s", args[1]), tuiui.ToastSuccess, 3*time.Second)
	case "disable":
		if len(args) < 2 {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /skill disable <name>"}})
			return m, nil
		}
		if m.isPreloadedSkill(args[1]) {
			m.toggleSessionSkill(args[1], false)
			return m, m.toastManager.Push(fmt.Sprintf("skill pinned by agent: %s", args[1]), tuiui.ToastWarning, 3*time.Second)
		}
		m.toggleSessionSkill(args[1], false)
		m.statusText = fmt.Sprintf("skill disabled: %s", args[1])
		return m, m.toastManager.Push(fmt.Sprintf("skill disabled: %s", args[1]), tuiui.ToastWarning, 3*time.Second)
	default:
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /skill [list|enable|disable] [name]"}})
	}
	return m, nil
}

func (m *Model) allowedSkillNames() map[string]struct{} {
	if m.agentCtx == nil || len(m.agentCtx.Definition.SkillNames) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(m.agentCtx.Definition.SkillNames))
	for _, name := range m.agentCtx.Definition.SkillNames {
		name = strings.TrimSpace(name)
		if name != "" {
			set[name] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func (m *Model) skillList() (tea.Model, tea.Cmd) {
	if m.skillService == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "skill service not available"}})
		return m, nil
	}
	skills, _ := m.skillService.ListSkills(context.Background(), nil)
	if len(skills) == 0 {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "(no skills)"}})
		return m, nil
	}

	allowed := m.allowedSkillNames()
	activeSet := m.effectiveActiveSkillSet()
	var sb strings.Builder
	sb.WriteString("Skills:\n")
	for _, s := range skills {
		if len(allowed) > 0 {
			if _, ok := allowed[s.Name]; !ok {
				continue
			}
		}
		icon := "○"
		if activeSet != nil {
			if _, ok := activeSet[s.Name]; ok {
				icon = "●"
			}
		}
		if icon == "○" && m.isPreloadedSkill(s.Name) {
			icon = "●"
		}
		sb.WriteString(fmt.Sprintf("  %s %s", icon, s.Name))
		if s.Description != "" {
			sb.WriteString(fmt.Sprintf("  — %s", s.Description))
		}
		sb.WriteString("\n")
	}
	m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: sb.String()}})
	return m, nil
}

func (m *Model) openSkillSelector() (tea.Model, tea.Cmd) {
	if m.skillService == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "skill service not available"}})
		return m, nil
	}
	skills, _ := m.skillService.ListSkills(context.Background(), nil)
	if len(skills) == 0 {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "(no skills)"}})
		return m, nil
	}
	allowed := m.allowedSkillNames()
	activeSet := m.effectiveActiveSkillSet()
	options := make([]tuiui.SelectOption, 0, len(skills))
	for _, s := range skills {
		if len(allowed) > 0 {
			if _, ok := allowed[s.Name]; !ok {
				continue
			}
		}
		icon := "○"
		if activeSet != nil {
			if _, ok := activeSet[s.Name]; ok {
				icon = "●"
			}
		}
		if icon == "○" && m.isPreloadedSkill(s.Name) {
			icon = "●"
		}
		options = append(options, tuiui.SelectOption{
			Label:       icon + " " + s.Name,
			Value:       s.Name,
			Description: s.Description,
		})
	}
	dialog := tuiui.NewSelectDialog("skill-select", "Skills", options)
	m.dialogStack.Push(dialog, nil)
	m.dialogStack.SetSize(m.width, m.height)
	return m, nil
}

func (m *Model) cmdMCP(args []string) (tea.Model, tea.Cmd) {
	if m.mcpManager == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "MCP manager not available"}})
		return m, nil
	}

	if len(args) == 0 {
		return m.openMCPSelector()
	}

	switch args[0] {
	case "list":
		return m.openMCPSelector()
	case "connect":
		if len(args) < 2 {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /mcp connect <name>"}})
			return m, nil
		}
		m.toggleSessionMCP(args[1], true)
		m.statusText = fmt.Sprintf("MCP connecting: %s", args[1])
	case "disconnect":
		if len(args) < 2 {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /mcp disconnect <name>"}})
			return m, nil
		}
		m.toggleSessionMCP(args[1], false)
		m.statusText = fmt.Sprintf("MCP disconnected: %s", args[1])
	default:
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /mcp [list|connect|disconnect] [name]"}})
	}
	return m, nil
}

func (m *Model) mcpList() (tea.Model, tea.Cmd) {
	entries := m.mcpManager.ServerEntries()
	if len(entries) == 0 {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "(no MCP servers)"}})
		return m, nil
	}

	var sb strings.Builder
	sb.WriteString("MCP Servers:\n")
	for _, e := range entries {
		active := stringsContains(m.sessionMeta.ActiveMCPServers, e.Name)
		icon := "○"
		if active && e.Status == mcp.MCPStatusConnected {
			icon = "●"
		} else if e.Status == mcp.MCPStatusError {
			icon = "✗"
		}
		sb.WriteString(fmt.Sprintf("  %s %s  [%s]", icon, e.Name, e.Status))
		if e.ToolCount > 0 {
			sb.WriteString(fmt.Sprintf("  (%d tools)", e.ToolCount))
		}
		sb.WriteString("\n")
	}
	m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: sb.String()}})
	return m, nil
}

func (m *Model) openMCPSelector() (tea.Model, tea.Cmd) {
	if m.mcpManager == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "MCP manager not available"}})
		return m, nil
	}
	entries := m.mcpManager.ServerEntries()
	if len(entries) == 0 {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "(no MCP servers)"}})
		return m, nil
	}
	options := make([]tuiui.SelectOption, len(entries))
	for i, e := range entries {
		active := stringsContains(m.sessionMeta.ActiveMCPServers, e.Name)
		icon := "○"
		if active && e.Status == mcp.MCPStatusConnected {
			icon = "●"
		} else if e.Status == mcp.MCPStatusError {
			icon = "✗"
		}
		desc := string(e.Status)
		if e.ToolCount > 0 {
			desc += fmt.Sprintf(" (%d tools)", e.ToolCount)
		}
		options[i] = tuiui.SelectOption{
			Label:       icon + " " + e.Name,
			Value:       e.Name,
			Description: desc,
		}
	}
	dialog := tuiui.NewSelectDialog("mcp-select", "MCP Servers", options)
	m.dialogStack.Push(dialog, nil)
	m.dialogStack.SetSize(m.width, m.height)
	return m, nil
}

func (m *Model) cmdSession(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		return m.openSessionSelector()
	}

	switch args[0] {
	case "new":
		if !m.newSession() {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "failed to create session"}})
			return m, nil
		}
		m.statusText = fmt.Sprintf("new session: %s", shortSessionID(m.currentSessionID))
		return m, tea.Batch(m.input.Focus(), m.refreshSidebarCmd())
	case "list":
		return m.openSessionSelector()
	case "switch":
		if len(args) < 2 {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /session switch <id>"}})
			return m, nil
		}
		m.switchSession(args[1])
		return m, m.input.Focus()
	case "delete":
		if len(args) < 2 {
			m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /session delete <id>"}})
			return m, nil
		}
		return m.sessionDelete(args[1])
	default:
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /session [new|list|switch|delete]"}})
		return m, nil
	}
}

func (m *Model) sessionList() (tea.Model, tea.Cmd) {
	if m.store == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "session store not available"}})
		return m, nil
	}
	sessions, err := m.store.List()
	if err != nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("list sessions error: %v", err)}})
		return m, nil
	}
	if len(sessions) == 0 {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "(no sessions)"}})
		return m, nil
	}

	var sb strings.Builder
	sb.WriteString("Sessions:\n")
	for _, s := range sessions {
		marker := "  "
		if s.ID == m.currentSessionID {
			marker = "* "
		}
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		sb.WriteString(fmt.Sprintf("%s%s  %s  [%s]  msgs:%d\n",
			marker, s.ID[:8], title, s.AgentName, s.MessageCount))
	}
	m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: sb.String()}})
	return m, nil
}

func (m *Model) openSessionSelector() (tea.Model, tea.Cmd) {
	if m.store == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "session store not available"}})
		return m, nil
	}
	sessions, err := m.store.List()
	if err != nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("list sessions error: %v", err)}})
		return m, nil
	}
	options := buildSessionSelectOptions(sessions, m.currentSessionID)
	dialog := tuiui.NewSelectDialog("session-select", "Switch Session", options)
	dialog.FullWidth = true
	m.dialogStack.Push(dialog, nil)
	m.dialogStack.SetSize(m.width, m.height)
	m.statusText = "select session"
	return m, nil
}

func (m *Model) showAgentSelector() {
	profiles := m.profileRegistry.List()
	options := make([]tuiui.SelectOption, len(profiles))
	for i, p := range profiles {
		options[i] = tuiui.SelectOption{Label: p.Name, Value: p.Name, Description: p.Role}
	}
	dialog := tuiui.NewSelectDialog("agent-select", "Select Agent", options)
	m.dialogStack.Push(dialog, nil)
	m.dialogStack.SetSize(m.width, m.height)
}

func (m *Model) sessionDelete(idPrefix string) (tea.Model, tea.Cmd) {
	if m.store == nil {
		return m, nil
	}
	sessions, _ := m.store.List()
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, idPrefix) {
			if s.ID == m.currentSessionID {
				m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "cannot delete current session"}})
				return m, nil
			}
			if err := m.store.Delete(s.ID); err != nil {
				m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("delete error: %v", err)}})
			} else {
				m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: fmt.Sprintf("deleted session %s", s.ID[:8])}})
				m.refreshSidebarData()
			}
			return m, nil
		}
	}
	m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "session not found: " + idPrefix}})
	return m, nil
}

func (m *Model) openThemeSelector() (tea.Model, tea.Cmd) {
	names := m.themeProvider.List()
	current := m.themeProvider.Get().Name
	options := make([]tuiui.SelectOption, len(names))
	for i, name := range names {
		desc := ""
		if name == current {
			desc = "(current)"
		}
		options[i] = tuiui.SelectOption{Label: name, Value: name, Description: desc}
	}
	dialog := tuiui.NewSelectDialog("theme-select", "Select Theme", options)
	m.dialogStack.Push(dialog, nil)
	m.dialogStack.SetSize(m.width, m.height)
	m.statusText = "select theme"
	return m, nil
}

func (m *Model) cmdMode(args []string) (tea.Model, tea.Cmd) {
	if m.agentCtx == nil {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "agent context not available"}})
		return m, nil
	}
	if len(args) == 0 {
		return m.openModeSelector()
	}
	mode, ok := parsePermissionModeArg(args[0])
	if !ok {
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /mode [yolo|manual|ai]"}})
		return m, nil
	}
	return m.applyPermissionMode(mode)
}

func (m *Model) openModeSelector() (tea.Model, tea.Cmd) {
	current := string(m.currentPermissionMode())
	modes := []struct {
		value string
		label string
		desc  string
	}{
		{"YOLO", "yolo", "auto-approve all"},
		{"MANUAL", "manual", "always confirm"},
		{"AI", "ai", "risk-based (confirm high risk only)"},
	}
	options := make([]tuiui.SelectOption, len(modes))
	for i, md := range modes {
		desc := md.desc
		if md.value == current {
			desc += " (current)"
		}
		options[i] = tuiui.SelectOption{Label: md.label, Value: md.value, Description: desc}
	}
	dialog := tuiui.NewSelectDialog("mode-select", "Bash Permission Mode", options)
	m.dialogStack.Push(dialog, nil)
	m.dialogStack.SetSize(m.width, m.height)
	m.statusText = "select mode"
	return m, nil
}

func (m *Model) applyPermissionMode(mode builtin_tools.PermissionMode) (tea.Model, tea.Cmd) {
	m.setPermissionMode(mode)
	m.statusText = fmt.Sprintf("bash mode: %s", mode)
	toastLevel := tuiui.ToastSuccess
	if mode == builtin_tools.PermissionModeYOLO {
		toastLevel = tuiui.ToastWarning
	}
	return m, m.toastManager.Push(fmt.Sprintf("bash mode: %s", mode), toastLevel, 3*time.Second)
}

func (m *Model) cmdSidebar(args []string) (tea.Model, tea.Cmd) {
	if len(args) == 0 {
		m.toggleSidebar()
		return m, nil
	}
	mode := strings.ToLower(strings.TrimSpace(args[0]))
	switch mode {
	case "show", "hide", "auto":
		pref := m.localProvider.Get()
		pref.SidebarMode = mode
		m.localProvider.Set(pref)
		m.updateLayout()
		m.refreshSidebarData()
		m.statusText = fmt.Sprintf("sidebar: %s", mode)
	default:
		m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "usage: /sidebar [show|hide|auto]"}})
	}
	return m, nil
}

func parsePermissionModeArg(arg string) (builtin_tools.PermissionMode, bool) {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "yolo":
		return builtin_tools.PermissionModeYOLO, true
	case "manual":
		return builtin_tools.PermissionModeManual, true
	case "ai":
		return builtin_tools.PermissionModeAI, true
	default:
		return "", false
	}
}
