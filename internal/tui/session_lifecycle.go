package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/mcp"
)

func (m *Model) ensureSession() bool {
	if m.currentSessionID != "" {
		return true
	}
	if m.store == nil {
		return false
	}
	if m.restoreLatestSession() {
		return true
	}
	return m.newSession()
}

func (m *Model) restoreLatestSession() bool {
	if m.store == nil {
		return false
	}
	sessions, err := m.store.List()
	if err != nil || len(sessions) == 0 {
		return false
	}
	m.switchSession(sessions[0].ID)
	return m.currentSessionID != ""
}

func (m *Model) newSession() bool {
	if m.store == nil {
		return false
	}
	m.persistCurrentSession()

	agentName := ""
	if m.agentCtx != nil {
		agentName = m.agentCtx.Definition.Name
	}

	m.sessionMeta = SessionMeta{
		Theme:          m.themeProvider.Get().Name,
		PermissionMode: string(m.currentPermissionMode()),
	}
	rec := &SessionRecord{
		Title:     "",
		Status:    "active",
		AgentName: agentName,
		Metadata:  m.sessionMeta.String(),
	}
	m.populateSessionRecord(rec)
	if err := m.store.Create(rec); err != nil {
		return false
	}
	m.currentSessionID = rec.ID
	m.bindSessionToAgent()

	if m.agentCtx != nil {
		m.agentCtx.InitialHistory = nil
		m.agentCtx.InitialState = builtin_tools.StateSnapshot{}
	}
	m.sessionUsage = ai.TokenUsage{}
	m.sessionCost = 0
	m.chat = NewChatModel()
	m.restoreToolVerbose()
	m.updateLayout()

	_ = ensureSessionWorkspace(m.store.BaseDir(), rec.ID)
	m.applySessionRuntimeState()
	return true
}

func (m *Model) switchSession(idOrPrefix string) {
	if m.store == nil {
		return
	}
	m.persistCurrentSession()

	sessions, _ := m.store.List()
	for _, s := range sessions {
		if s.ID == idOrPrefix || strings.HasPrefix(s.ID, idOrPrefix) {
			m.currentSessionID = s.ID
			m.sessionMeta = parseSessionMeta(s.Metadata)
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
			m.statusText = fmt.Sprintf("session: %s", s.ID[:8])
			m.refreshSidebarData()
			return
		}
	}
	m.chat.AddPart(DisplayPart{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "session not found: " + idOrPrefix}})
}

func (m *Model) bindSessionToAgent() {
	if m.agentCtx == nil || m.store == nil || m.currentSessionID == "" {
		return
	}
	m.agentCtx.SessionID = m.currentSessionID
	m.agentCtx.SessionDir = filepath.Join(m.store.BaseDir(), m.currentSessionID)
}

func (m *Model) persistCurrentSession() {
	if m.store == nil || m.currentSessionID == "" {
		return
	}
	saveSessionDisplayParts(m.store.BaseDir(), m.currentSessionID, m.chat.Parts())
	m.persistSessionSummary()
	m.persistSessionMeta()
}

func (m *Model) persistSessionSummary() {
	if m.store == nil || m.currentSessionID == "" {
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
	if m.store == nil || m.currentSessionID == "" {
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
	if m.store == nil || m.currentSessionID == "" {
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

func (m *Model) toggleSessionSkill(name string, enabled bool) {
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
	if connect {
		if !stringsContains(m.sessionMeta.ActiveMCPServers, name) {
			m.sessionMeta.ActiveMCPServers = append(m.sessionMeta.ActiveMCPServers, name)
		}
	} else {
		m.sessionMeta.ActiveMCPServers = stringsRemove(m.sessionMeta.ActiveMCPServers, name)
	}
	m.persistSessionMeta()
	m.applySessionRuntimeState()
	m.refreshSidebarData()
}

func (m *Model) restoreSessionState() {
	m.themeProvider.SetByName(m.sessionMeta.Theme)
	if m.sessionMeta.PermissionMode != "" {
		if mode, ok := parsePermissionModeArg(m.sessionMeta.PermissionMode); ok {
			m.setPermissionMode(mode)
		}
	}
	m.applySessionRuntimeState()
}

func (m *Model) setPermissionMode(mode builtin_tools.PermissionMode) {
	if m.agentCtx != nil &&
		m.agentCtx.Definition.Policies.BashPermissionContext != nil &&
		m.agentCtx.Definition.Policies.BashPermissionContext.PermCtx != nil {
		m.agentCtx.Definition.Policies.BashPermissionContext.PermCtx.Mode = mode
	}
	m.sessionMeta.PermissionMode = string(mode)
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
		if state := m.appCfg.ResolveProviderState(rec.ProviderName, rec.ModelID, "", ""); state != nil {
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

	workspaceState, err := loadSessionWorkspaceState(m.store.BaseDir(), m.currentSessionID)
	if err != nil || workspaceState == nil {
		workspaceState = &builtin_tools.WorkspaceState{SessionID: m.currentSessionID}
	}

	snapshot := builtin_tools.StateSnapshot{
		ActiveSkillNames: builtin_tools.CloneStringSlice(m.sessionMeta.ActiveSkillNames),
		ActiveMCPServers: builtin_tools.CloneStringSlice(m.sessionMeta.ActiveMCPServers),
	}

	m.agentCtx.InitialState = snapshot

	workspaceState.SessionID = m.currentSessionID
	workspaceState.ActiveSkillNames = builtin_tools.CloneStringSlice(snapshot.ActiveSkillNames)
	workspaceState.ActiveMCPServers = builtin_tools.CloneStringSlice(snapshot.ActiveMCPServers)
	workspaceState.UpdatedAt = time.Now()
	_ = saveSessionWorkspaceState(m.store.BaseDir(), m.currentSessionID, workspaceState)

	m.reconcileSessionMCP(snapshot.ActiveMCPServers)
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
			_, _ = m.mcpManager.Disconnect(name)
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
