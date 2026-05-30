package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/mcp"
	"aster/internal/react/persistv2"
	tuiui "aster/internal/tui/ui"

	"github.com/google/uuid"
)

func (m *Model) ensureSession() bool {
	if m.currentSessionID != "" {
		if !m.sessionMaterialized {
			return m.materializeSession()
		}
		return true
	}
	if m.store == nil {
		return false
	}
	if !m.newSession() {
		return false
	}
	return m.materializeSession()
}

func (m *Model) newSession() bool {
	if m.store == nil {
		return false
	}
	if m.currentSessionID != "" && !m.sessionMaterialized {
		m.cleanupUnmaterializedSession()
	}
	m.persistCurrentSession()

	m.sessionMeta = SessionMeta{
		Theme:          m.themeProvider.Get().Name,
		PermissionMode: string(m.currentPermissionMode()),
	}
	if saved := m.localProvider.Get().PreferredPermissionMode; saved != "" {
		if mode, ok := parsePermissionModeArg(saved); ok {
			m.sessionMeta.PermissionMode = string(mode)
			m.setPermissionModeOnAgent(mode)
		}
	}
	m.currentSessionID = uuid.New().String()
	m.sessionMaterialized = false
	m.bindSessionToAgent()
	m.mcpLastLogged = nil

	if m.agentCtx != nil {
		m.agentCtx.InitialHistory = nil
		m.agentCtx.InitialState = builtin_tools.StateSnapshot{}
	}
	m.sessionUsage = ai.TokenUsage{}
	m.sessionCost = 0
	m.chat = NewChatModel()
	m.restoreToolVerbose()
	m.updateLayout()

	_ = ensureSessionWorkspace(m.store.BaseDir(), m.currentSessionID)
	m.applySessionRuntimeState()
	return true
}

func (m *Model) materializeSession() bool {
	if m.sessionMaterialized || m.store == nil || m.currentSessionID == "" {
		return m.sessionMaterialized
	}
	rec := &SessionRecord{
		ID:       m.currentSessionID,
		Status:   "active",
		Metadata: m.sessionMeta.String(),
	}
	m.populateSessionRecord(rec)
	if err := m.store.Create(rec); err != nil {
		return false
	}
	m.sessionMaterialized = true
	return true
}

func (m *Model) cleanupUnmaterializedSession() {
	if m.store == nil || m.currentSessionID == "" || m.sessionMaterialized {
		return
	}
	dir := filepath.Join(m.store.BaseDir(), m.currentSessionID)
	_ = os.RemoveAll(dir)
	m.currentSessionID = ""
	m.sessionMaterialized = false
}

func (m *Model) switchSession(idOrPrefix string) {
	if m.store == nil {
		return
	}
	if m.currentSessionID != "" && !m.sessionMaterialized {
		m.cleanupUnmaterializedSession()
	}
	m.persistCurrentSession()

	sessions, _ := m.store.List()
	for _, s := range sessions {
		if s.ID == idOrPrefix || strings.HasPrefix(s.ID, idOrPrefix) {
			m.currentSessionID = s.ID
			m.sessionMaterialized = true
			m.sessionMeta = parseSessionMeta(s.Metadata)
			m.mcpLastLogged = nil
			if strings.TrimSpace(s.Metadata) == "" {
				if ws, wsErr := loadSessionWorkspaceState(m.store.BaseDir(), s.ID); wsErr == nil && ws != nil {
					m.sessionMeta.ActiveSkillNames = builtin_tools.CloneStringSlice(ws.ActiveSkillNames)
					m.sessionMeta.ActiveMCPServers = builtin_tools.CloneStringSlice(ws.ActiveMCPServers)
				}
			}
			m.bindSessionToAgent()

			parts, err := loadSessionDisplayParts(m.store.BaseDir(), s.ID)
			m.chat = NewChatModel()
			m.restoreToolVerbose()
			m.updateLayout()
			if err == nil && len(parts) > 0 {
				m.chat.SetParts(parts)
			}

			if s.AgentName != "" && m.profileRegistry != nil {
				if def, ok := m.profileRegistry.Get(s.AgentName); ok {
					m.agentCtx.Definition = def
				}
			}

			m.restoreSessionProvider(s)

			if m.agentCtx != nil {
				history, histErr := loadSessionAIHistory(m.store.BaseDir(), s.ID)
				if histErr == nil {
					m.agentCtx.InitialHistory = history
					m.recalcUsageFromHistory(history)
				} else {
					m.agentCtx.InitialHistory = nil
					m.sessionUsage = ai.TokenUsage{}
					m.sessionCost = 0
				}
			}

			if runEvents, runErr := loadSessionRunEvents(m.store.BaseDir(), s.ID); runErr == nil {
				if note := summarizeRunRecovery(runEvents); note != "" {
					m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: note}})
				}
			}

			m.restoreSessionState()
			m.restorePendingInterruptFromV2()
			m.statusText = fmt.Sprintf("session: %s", s.ID[:8])
			m.refreshSidebarData()
			return
		}
	}
	m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "session not found: " + idOrPrefix}})
}

func (m *Model) restorePendingInterruptFromV2() {
	if m == nil || m.agentCtx == nil || m.currentSessionID == "" || m.agentCtx.SessionDir == "" {
		return
	}
	workspaceRoot := m.agentCtx.SessionDir + "/workspace"
	store, err := persistv2.Open(workspaceRoot, m.currentSessionID)
	if err != nil {
		return
	}
	snap, err := store.LoadSnapshot()
	if err != nil || snap == nil {
		return
	}
	if snap.SessionState != persistv2.SessionStateWaitingForHuman || snap.PendingInterrupt == nil {
		return
	}
	if strings.TrimSpace(snap.PendingInterrupt.InterruptID) == "" {
		return
	}
	// If already resolved, do not re-prompt.
	if snap.PendingInterrupt.ResolvedAt > 0 {
		return
	}

	m.pendingInterrupt = &builtin_tools.PendingInterrupt{
		InterruptID: strings.TrimSpace(snap.PendingInterrupt.InterruptID),
		Question:    strings.TrimSpace(snap.PendingInterrupt.Question),
		InputType:   strings.TrimSpace(snap.PendingInterrupt.InputType),
		Options:     builtin_tools.CloneStringSlice(snap.PendingInterrupt.Options),
		Context:     builtin_tools.CloneAnyMap(snap.PendingInterrupt.Context),
	}
	m.input.SetEnabled(false)
	m.dialogStack.Clear()
	prompt := tuiui.NewPromptDialog(m.pendingInterrupt.InterruptID, "Agent needs your input", m.pendingInterrupt.Question)
	if len(m.pendingInterrupt.Options) > 0 {
		prompt.WithOptions(m.pendingInterrupt.Options)
	}
	m.dialogStack.Push(prompt, nil)
	m.dialogStack.SetSize(m.width, m.height)
}

func (m *Model) bindSessionToAgent() {
	if m.agentCtx == nil || m.store == nil || m.currentSessionID == "" {
		return
	}
	m.agentCtx.SessionID = m.currentSessionID
	m.agentCtx.SessionDir = filepath.Join(m.store.BaseDir(), m.currentSessionID)
}

func (m *Model) persistCurrentSession() {
	if m.store == nil || m.currentSessionID == "" || !m.sessionMaterialized {
		return
	}
	saveSessionDisplayParts(m.store.BaseDir(), m.currentSessionID, m.chat.Parts())
	m.persistSessionSummary()
	m.persistSessionMeta()
}

func (m *Model) persistSessionSummary() {
	if m.store == nil || m.currentSessionID == "" || !m.sessionMaterialized {
		return
	}
	parts := m.chat.Parts()
	partCount := len(parts)
	lastContent := ""
	title := ""
	for _, p := range parts {
		if p.Type == PartTypeUser && p.User != nil && title == "" {
			title = p.User.Content
			if len(title) > 50 {
				title = title[:50]
			}
		}
		switch {
		case p.User != nil:
			lastContent = p.User.Content
		case p.Text != nil:
			lastContent = p.Text.Content
		case p.System != nil:
			lastContent = p.System.Content
		}
	}
	if len(lastContent) > 100 {
		lastContent = lastContent[:100]
	}

	_ = m.store.UpdateSummary(m.currentSessionID, partCount, lastContent)
	if title != "" {
		if rec, err := m.store.Get(m.currentSessionID); err == nil && rec.Title == "" {
			rec.Title = title
			m.populateSessionRecord(rec)
			_ = m.store.Update(rec)
		}
	}
}

func (m *Model) persistSessionMeta() {
	if m.store == nil || m.currentSessionID == "" || !m.sessionMaterialized {
		return
	}
	rec, err := m.store.Get(m.currentSessionID)
	if err != nil {
		return
	}
	rec.Metadata = m.sessionMeta.String()
	m.populateSessionRecord(rec)
	_ = m.store.Update(rec)
}

func (m *Model) updateSessionAgent(agentName string) {
	if m.store == nil || m.currentSessionID == "" || !m.sessionMaterialized {
		return
	}
	rec, err := m.store.Get(m.currentSessionID)
	if err != nil {
		return
	}
	rec.AgentName = agentName
	m.populateSessionRecord(rec)
	_ = m.store.Update(rec)
}

func (m *Model) populateSessionRecord(rec *SessionRecord) {
	if rec == nil {
		return
	}
	if m.agentCtx != nil && strings.TrimSpace(m.agentCtx.Definition.Name) != "" {
		rec.AgentName = strings.TrimSpace(m.agentCtx.Definition.Name)
	}
	rec.ProviderName = strings.TrimSpace(m.currentProviderName())
	if m.providerCfg != nil {
		rec.ModelID = strings.TrimSpace(m.providerCfg.ModelID)
	}
}

func (m *Model) rememberRecentModel(modelID string) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return
	}
	providerID := ""
	if m.providerCfg != nil {
		providerID = m.providerCfg.Name
	}
	m.localProvider.RememberModel(providerID, modelID)
}

func (m *Model) appendPart(part DisplayPart) {
	if m.store == nil || m.currentSessionID == "" {
		return
	}
	_ = appendSessionDisplayPart(m.store.BaseDir(), m.currentSessionID, part)
}

func (m *Model) persistPart(partType, name, content string) {
	m.persistPartWithCallID(partType, name, "", content)
}

func (m *Model) persistPartWithCallID(partType, name, callID, content string) {
	if m.store == nil || m.currentSessionID == "" {
		return
	}
	_ = appendSessionPart(m.store.BaseDir(), m.currentSessionID, persistedPart{
		Type:    partType,
		Name:    name,
		CallID:  callID,
		Content: content,
		Time:    time.Now(),
	})
}

func (m *Model) persistPartWithCallIDAndAgent(partType, name, callID, agentName, content string) {
	if m.store == nil || m.currentSessionID == "" {
		return
	}
	_ = appendSessionPart(m.store.BaseDir(), m.currentSessionID, persistedPart{
		Type:      partType,
		Name:      name,
		CallID:    callID,
		AgentName: agentName,
		Content:   content,
		Time:      time.Now(),
	})
}

func (m *Model) persistPartWithAgent(partType, name, agentName, content string) {
	if m.store == nil || m.currentSessionID == "" {
		return
	}
	_ = appendSessionPart(m.store.BaseDir(), m.currentSessionID, persistedPart{
		Type:      partType,
		Name:      name,
		AgentName: agentName,
		Content:   content,
		Time:      time.Now(),
	})
}

func (m *Model) toggleSessionSkill(name string, enabled bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if !enabled && m.isPreloadedSkill(name) {
		m.chat.AddPart(DisplayPart{
			Type: PartTypeSystem,
			Time: time.Now(),
			System: &SystemPart{
				Content: fmt.Sprintf("skill %q is pinned by current agent (preload_skills), cannot disable", name),
			},
		})
		m.statusText = fmt.Sprintf("skill pinned: %s", name)
		m.refreshSidebarData()
		return
	}
	if enabled {
		if !stringsContains(m.sessionMeta.ActiveSkillNames, name) {
			m.sessionMeta.ActiveSkillNames = append(m.sessionMeta.ActiveSkillNames, name)
		}
	} else {
		m.sessionMeta.ActiveSkillNames = stringsRemove(m.sessionMeta.ActiveSkillNames, name)
	}
	m.persistSessionMeta()
	m.applySessionRuntimeState()
	m.refreshSidebarData()
}

func (m *Model) toggleSessionMCP(name string, connect bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	agentDefaults := m.agentDefaultMCPSet()
	_, isAgentDefault := agentDefaults[name]

	if connect {
		m.sessionMeta.DisabledMCPServers = stringsRemove(m.sessionMeta.DisabledMCPServers, name)
		// Only persist "user active" when the MCP is NOT part of the current agent defaults.
		// Agent defaults are derived dynamically from the profile and can be overridden via disabled_mcp_servers.
		if !isAgentDefault {
			if !stringsContains(m.sessionMeta.ActiveMCPServers, name) {
				m.sessionMeta.ActiveMCPServers = append(m.sessionMeta.ActiveMCPServers, name)
			}
		}
	} else {
		m.sessionMeta.ActiveMCPServers = stringsRemove(m.sessionMeta.ActiveMCPServers, name)
		if !stringsContains(m.sessionMeta.DisabledMCPServers, name) {
			m.sessionMeta.DisabledMCPServers = append(m.sessionMeta.DisabledMCPServers, name)
		}
	}
	m.persistSessionMeta()
	m.applySessionRuntimeState()
	m.refreshSidebarData()
}

func (m *Model) restoreSessionState() {
	m.themeProvider.SetByName(m.sessionMeta.Theme)
	if m.sessionMeta.PermissionMode != "" {
		if mode, ok := parsePermissionModeArg(m.sessionMeta.PermissionMode); ok {
			m.setPermissionModeOnAgent(mode)
		}
	}
	m.applySessionRuntimeState()
}

func (m *Model) setPermissionModeOnAgent(mode builtin_tools.PermissionMode) {
	if m.agentCtx != nil &&
		m.agentCtx.Definition.Policies.BashPermissionContext != nil &&
		m.agentCtx.Definition.Policies.BashPermissionContext.PermCtx != nil {
		m.agentCtx.Definition.Policies.BashPermissionContext.PermCtx.Mode = mode
	}
}

func (m *Model) setPermissionMode(mode builtin_tools.PermissionMode) {
	m.setPermissionModeOnAgent(mode)
	m.sessionMeta.PermissionMode = string(mode)
	m.localProvider.SetPreferredPermissionMode(string(mode))
	m.persistSessionMeta()
}

func (m *Model) currentPermissionMode() builtin_tools.PermissionMode {
	if m.agentCtx != nil &&
		m.agentCtx.Definition.Policies.BashPermissionContext != nil &&
		m.agentCtx.Definition.Policies.BashPermissionContext.PermCtx != nil {
		return m.agentCtx.Definition.Policies.BashPermissionContext.PermCtx.Mode
	}
	return builtin_tools.PermissionModeManual
}

func (m *Model) restoreToolVerbose() {
	m.chat.SetToolVerbose(m.localProvider.Get().ToolVerbose)
}

func (m *Model) restoreSessionProvider(rec *SessionRecord) {
	if rec == nil || m.providerCfg == nil || m.appCfg == nil {
		return
	}
	if rec.ProviderName != "" {
		if state := m.appCfg.ResolveProviderState(rec.ProviderName, rec.ModelID, "", "", m.registry, m.credStore); state != nil {
			*m.providerCfg = *state
			if m.agentCtx != nil {
				m.agentCtx.Definition.ModelID = m.providerCfg.ModelID
			}
			if m.agentCtx != nil && m.agentCtx.RebuildClient != nil {
				m.agentCtx.RebuildClient(m.providerCfg)
			}
		}
	}
	if rec.ModelID != "" {
		m.providerCfg.ModelID = rec.ModelID
		if m.agentCtx != nil {
			m.agentCtx.Definition.ModelID = rec.ModelID
		}
	}
}

func (m *Model) applySessionRuntimeState() {
	if m.store == nil || m.currentSessionID == "" || m.agentCtx == nil {
		return
	}

	// newSession/switchSession rebuild m.chat via NewChatModel(), which resets
	// rootAgentName to "". Restore it here so the main timeline's per-agent
	// filtering keeps recognizing the root agent's parts (otherwise every
	// non-empty-named root part is dropped and the main agent shows nothing).
	m.chat.rootAgentName = m.agentCtx.Definition.Name

	workspaceState, err := loadSessionWorkspaceState(m.store.BaseDir(), m.currentSessionID)
	if err != nil || workspaceState == nil {
		workspaceState = &builtin_tools.WorkspaceState{SessionID: m.currentSessionID}
	}

	snapshot := builtin_tools.StateSnapshot{
		ActiveSkillNames: builtin_tools.CloneStringSlice(m.effectiveActiveSkillNames()),
		ActiveMCPServers: builtin_tools.CloneStringSlice(m.desiredMCPNames()),
	}

	m.agentCtx.InitialState = snapshot

	workspaceState.SessionID = m.currentSessionID
	workspaceState.ActiveSkillNames = builtin_tools.CloneStringSlice(snapshot.ActiveSkillNames)
	workspaceState.ActiveMCPServers = builtin_tools.CloneStringSlice(snapshot.ActiveMCPServers)
	workspaceState.UpdatedAt = time.Now()
	_ = saveSessionWorkspaceState(m.store.BaseDir(), m.currentSessionID, workspaceState)

	m.reconcileSessionMCP(snapshot.ActiveMCPServers)
}

func (m *Model) effectiveActiveSkillNames() []string {
	if m == nil {
		return nil
	}
	sessionSkills := builtin_tools.CloneStringSlice(m.sessionMeta.ActiveSkillNames)
	var preload []string
	if m.agentCtx != nil {
		preload = builtin_tools.CloneStringSlice(m.agentCtx.Definition.PreloadSkills)
	}
	return mergeDistinctNames(preload, sessionSkills)
}

func (m *Model) effectiveActiveSkillSet() map[string]struct{} {
	names := m.effectiveActiveSkillNames()
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		set[name] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func (m *Model) agentDefaultMCPNames() []string {
	if m == nil || m.agentCtx == nil || len(m.agentCtx.Definition.MCPServers) == 0 {
		return nil
	}
	out := make([]string, 0, len(m.agentCtx.Definition.MCPServers))
	seen := make(map[string]struct{}, len(m.agentCtx.Definition.MCPServers))
	for _, sc := range m.agentCtx.Definition.MCPServers {
		if sc == nil {
			continue
		}
		name := strings.TrimSpace(sc.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *Model) agentDefaultMCPSet() map[string]struct{} {
	names := m.agentDefaultMCPNames()
	if len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return set
}

func (m *Model) desiredMCPNames() []string {
	if m == nil {
		return nil
	}

	desired := mergeDistinctNames(m.agentDefaultMCPNames(), m.sessionMeta.ActiveMCPServers)
	if len(desired) == 0 {
		return nil
	}

	if len(m.sessionMeta.DisabledMCPServers) == 0 {
		return desired
	}
	disabled := make(map[string]struct{}, len(m.sessionMeta.DisabledMCPServers))
	for _, raw := range m.sessionMeta.DisabledMCPServers {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		disabled[name] = struct{}{}
	}
	if len(disabled) == 0 {
		return desired
	}

	filtered := make([]string, 0, len(desired))
	for _, name := range desired {
		if _, ok := disabled[name]; ok {
			continue
		}
		filtered = append(filtered, name)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func (m *Model) isPreloadedSkill(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || m == nil || m.agentCtx == nil {
		return false
	}
	for _, n := range m.agentCtx.Definition.PreloadSkills {
		if strings.EqualFold(strings.TrimSpace(n), name) {
			return true
		}
	}
	return false
}

func mergeDistinctNames(primary []string, secondary []string) []string {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	out := make([]string, 0, len(primary)+len(secondary))
	for _, raw := range append(primary, secondary...) {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *Model) reconcileSessionMCP(desired []string) {
	if m.mcpManager == nil {
		return
	}

	desiredSet := make(map[string]struct{}, len(desired))
	for _, name := range desired {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		desiredSet[name] = struct{}{}
	}

	for _, entry := range m.mcpManager.ServerEntries() {
		if entry == nil {
			continue
		}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		if _, want := desiredSet[name]; want {
			if entry.Status != mcp.MCPStatusConnected {
				go func(serverName string) {
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel()
					_, _ = m.mcpManager.Connect(ctx, serverName)
				}(name)
			}
			continue
		}
		if entry.Config != nil && entry.Config.Resident {
			continue
		}
		if entry.Status == mcp.MCPStatusConnected {
			// Off the Bubble Tea Update goroutine: Disconnect closes a stdio
			// subprocess and synchronously notifies status, which must not block
			// the message loop.
			go func(serverName string) {
				_, _ = m.mcpManager.Disconnect(serverName)
			}(name)
		}
	}
}

func summarizeRunRecovery(events []persistedRunEvent) string {
	if len(events) == 0 {
		return ""
	}
	last := events[len(events)-1]
	if last.Event == "started" {
		return fmt.Sprintf("restored run %s from %s (no finished record)", last.RunID, last.Time.Format(time.RFC3339))
	}
	if last.Event == "finished" && strings.TrimSpace(last.Error) != "" {
		return fmt.Sprintf("last run %s ended with error: %s", last.RunID, last.Error)
	}
	return ""
}
