package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"aster/internal/builtin_tools"
	"aster/internal/mcp"
	tuicontext "aster/internal/tui/context"
)

func (m *Model) ensureSession() bool {
	if m.currentSessionID != "" {
		return true
	}
	if m.store == nil {
		return false
	}
	return m.newSession()
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

	m.sessionMeta = SessionMeta{Theme: m.themeProvider.Get().Name}
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

			messages, err := loadSessionMessages(m.store.BaseDir(), s.ID)
			if err != nil {
				m.chat = NewChatModel()
				messages = nil
			}
			if parts, partErr := loadSessionParts(m.store.BaseDir(), s.ID); partErr == nil {
				messages = mergeRecoveredPartMessages(messages, parts)
			}
			m.chat = NewChatModel()
			m.chat.SetMessages(messages)
			m.restoreToolVerbose()
			m.updateLayout()

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
				} else {
					m.agentCtx.InitialHistory = nil
				}
			}

			if runEvents, runErr := loadSessionRunEvents(m.store.BaseDir(), s.ID); runErr == nil {
				if note := summarizeRunRecovery(runEvents); note != "" {
					m.chat.AddMessage(ChatMessage{Role: "system", Content: note})
				}
			}

			m.restoreSessionState()
			m.statusText = fmt.Sprintf("session: %s", s.ID[:8])
			m.refreshSidebarData()
			return
		}
	}
	m.chat.AddMessage(ChatMessage{Role: "system", Content: "session not found: " + idOrPrefix})
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
	saveSessionMessages(m.store.BaseDir(), m.currentSessionID, m.chat.Messages())
	m.persistSessionSummary()
	m.persistSessionMeta()
}

func (m *Model) persistSessionSummary() {
	if m.store == nil || m.currentSessionID == "" {
		return
	}
	messages := m.chat.Messages()
	msgCount := len(messages)
	lastMsg := ""
	title := ""
	for _, msg := range messages {
		if msg.Role == "user" && title == "" {
			title = msg.Content
			if len(title) > 50 {
				title = title[:50]
			}
		}
		lastMsg = msg.Content
	}
	if len(lastMsg) > 100 {
		lastMsg = lastMsg[:100]
	}

	_ = m.store.UpdateSummary(m.currentSessionID, msgCount, lastMsg)
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
	m.localProvider.Update(func(p tuicontext.LocalPreferences) tuicontext.LocalPreferences {
		next := []string{modelID}
		for _, existing := range p.RecentModelIDs {
			if existing == modelID || strings.TrimSpace(existing) == "" {
				continue
			}
			next = append(next, existing)
			if len(next) >= 8 {
				break
			}
		}
		p.RecentModelIDs = next
		return p
	})
}

func (m *Model) appendMessage(msg ChatMessage) {
	if m.store == nil || m.currentSessionID == "" {
		return
	}
	_ = appendSessionMessage(m.store.BaseDir(), m.currentSessionID, msg)
}

func (m *Model) persistPart(partType, name, content string) {
	if m.store == nil || m.currentSessionID == "" {
		return
	}
	_ = appendSessionPart(m.store.BaseDir(), m.currentSessionID, persistedPart{
		Type:    partType,
		Name:    name,
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
	m.applySessionRuntimeState()
}

func (m *Model) restoreToolVerbose() {
	m.chat.SetToolVerbose(m.localProvider.Get().ToolVerbose)
}

func (m *Model) restoreSessionProvider(rec *SessionRecord) {
	if rec == nil || m.providerCfg == nil || m.appCfg == nil {
		return
	}
	if rec.ProviderName != "" {
		resolved := false
		cfgP := m.appCfg.Providers[rec.ProviderName]
		if bp, ok := GetBuiltinProvider(rec.ProviderName); ok {
			m.providerCfg.Name = rec.ProviderName
			m.providerCfg.BaseURL = bp.BaseURL
			cfgKey := ""
			if cfgP != nil {
				cfgKey = cfgP.APIKey
				if cfgP.BaseURL != "" {
					m.providerCfg.BaseURL = cfgP.BaseURL
				}
			}
			m.providerCfg.APIKey = resolveAPIKey(bp, cfgKey)
			resolved = true
		} else if cfgP != nil {
			m.providerCfg.Name = rec.ProviderName
			m.providerCfg.BaseURL = cfgP.BaseURL
			m.providerCfg.APIKey = cfgP.APIKey
			resolved = true
		}
		if resolved && m.agentCtx != nil && m.agentCtx.RebuildClient != nil {
			modelID := rec.ModelID
			if modelID == "" {
				modelID = m.providerCfg.ModelID
			}
			m.agentCtx.RebuildClient(m.providerCfg.BaseURL, m.providerCfg.APIKey, modelID)
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
