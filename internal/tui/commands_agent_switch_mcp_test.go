package tui

import (
	"path/filepath"
	"testing"

	"aster/internal/mcp"
	"aster/internal/react"
)

func TestCmdAgent_SyncsMCP_WithDisableOverride(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSessionStore(filepath.Join(tmpDir, "data.db"), filepath.Join(tmpDir, "sessions"))
	if err != nil {
		t.Fatalf("NewSessionStore failed: %v", err)
	}
	defer store.Close()

	rec := &SessionRecord{AgentName: "agent-b"}
	if err := store.Create(rec); err != nil {
		t.Fatalf("create session failed: %v", err)
	}

	defA := react.AgentDefinition{
		Name:       "agent-a",
		MCPServers: []*mcp.MCPServerConfig{{Name: "mcp-a"}},
	}
	defB := react.AgentDefinition{Name: "agent-b"}
	reg := NewProfileRegistry()
	reg.Register(defA)
	reg.Register(defB)

	ctx := &AgentExecContext{Definition: defB}
	m := NewModel(ModelDeps{Store: store, AgentCtx: ctx, ProfileRegistry: reg})
	m.currentSessionID = rec.ID

	// Switch to agent-a: mcp-a should be desired by default.
	_, _ = m.cmdAgent([]string{"agent-a"})
	if !stringsContains(m.agentCtx.InitialState.ActiveMCPServers, "mcp-a") {
		t.Fatalf("expected mcp-a to be active by default, got %v", m.agentCtx.InitialState.ActiveMCPServers)
	}

	// Disable mcp-a: should add to DisabledMCPServers and remove from desired.
	m.toggleSessionMCP("mcp-a", false)
	if !stringsContains(m.sessionMeta.DisabledMCPServers, "mcp-a") {
		t.Fatalf("expected mcp-a to be disabled, got %v", m.sessionMeta.DisabledMCPServers)
	}
	if stringsContains(m.agentCtx.InitialState.ActiveMCPServers, "mcp-a") {
		t.Fatalf("expected mcp-a to be removed from desired after disable, got %v", m.agentCtx.InitialState.ActiveMCPServers)
	}

	// Switch away and back: disable override should persist in the session.
	_, _ = m.cmdAgent([]string{"agent-b"})
	_, _ = m.cmdAgent([]string{"agent-a"})
	if stringsContains(m.agentCtx.InitialState.ActiveMCPServers, "mcp-a") {
		t.Fatalf("expected mcp-a to remain disabled after switching agents, got %v", m.agentCtx.InitialState.ActiveMCPServers)
	}

	// Re-enable: should remove from DisabledMCPServers and become desired again.
	m.toggleSessionMCP("mcp-a", true)
	if stringsContains(m.sessionMeta.DisabledMCPServers, "mcp-a") {
		t.Fatalf("expected mcp-a to be removed from disabled list, got %v", m.sessionMeta.DisabledMCPServers)
	}
	if !stringsContains(m.agentCtx.InitialState.ActiveMCPServers, "mcp-a") {
		t.Fatalf("expected mcp-a to be active again after re-enable, got %v", m.agentCtx.InitialState.ActiveMCPServers)
	}
}
