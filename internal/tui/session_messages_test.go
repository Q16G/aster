package tui

import (
	"path/filepath"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

func TestSessionArtifactsRoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	sessionID := "ses-test"

	msgs := []ChatMessage{
		{Role: "user", Content: "hello", Time: time.Unix(10, 0)},
		{Role: "assistant", Content: "world", Time: time.Unix(20, 0)},
	}
	if err := saveSessionMessages(baseDir, sessionID, msgs); err != nil {
		t.Fatalf("saveSessionMessages failed: %v", err)
	}
	if err := appendSessionPart(baseDir, sessionID, persistedPart{
		Type:    "tool_end",
		Name:    "list_files",
		Content: "ok",
		Time:    time.Unix(30, 0),
	}); err != nil {
		t.Fatalf("appendSessionPart failed: %v", err)
	}
	if err := appendSessionRunEvent(baseDir, sessionID, persistedRunEvent{
		RunID: "run-1", Event: "started", Input: "hello", Time: time.Unix(11, 0),
	}); err != nil {
		t.Fatalf("appendSessionRunEvent(start) failed: %v", err)
	}
	if err := appendSessionRunEvent(baseDir, sessionID, persistedRunEvent{
		RunID: "run-1", Event: "finished", Success: true, Time: time.Unix(40, 0),
	}); err != nil {
		t.Fatalf("appendSessionRunEvent(finish) failed: %v", err)
	}

	history := []*ai.MsgInfo{
		ai.NewSystemMsgInfo("sys"),
		ai.NewUserMsgInfo("hello"),
		ai.NewAIMsgInfo("world"),
	}
	if err := saveSessionAIHistory(baseDir, sessionID, history); err != nil {
		t.Fatalf("saveSessionAIHistory failed: %v", err)
	}

	state := &builtin_tools.WorkspaceState{
		SessionID:        sessionID,
		ActiveSkillNames: []string{"skill-a"},
		ActiveMCPServers: []string{"mcp-a"},
	}
	if err := saveSessionWorkspaceState(baseDir, sessionID, state); err != nil {
		t.Fatalf("saveSessionWorkspaceState failed: %v", err)
	}

	loadedMsgs, err := loadSessionMessages(baseDir, sessionID)
	if err != nil {
		t.Fatalf("loadSessionMessages failed: %v", err)
	}
	if len(loadedMsgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(loadedMsgs))
	}

	loadedParts, err := loadSessionParts(baseDir, sessionID)
	if err != nil {
		t.Fatalf("loadSessionParts failed: %v", err)
	}
	if len(loadedParts) != 1 || loadedParts[0].Name != "list_files" {
		t.Fatalf("unexpected parts: %+v", loadedParts)
	}

	loadedRuns, err := loadSessionRunEvents(baseDir, sessionID)
	if err != nil {
		t.Fatalf("loadSessionRunEvents failed: %v", err)
	}
	if len(loadedRuns) != 2 || loadedRuns[1].Event != "finished" {
		t.Fatalf("unexpected runs: %+v", loadedRuns)
	}

	loadedHistory, err := loadSessionAIHistory(baseDir, sessionID)
	if err != nil {
		t.Fatalf("loadSessionAIHistory failed: %v", err)
	}
	if len(loadedHistory) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(loadedHistory))
	}

	loadedState, err := loadSessionWorkspaceState(baseDir, sessionID)
	if err != nil {
		t.Fatalf("loadSessionWorkspaceState failed: %v", err)
	}
	if len(loadedState.ActiveSkillNames) != 1 || loadedState.ActiveSkillNames[0] != "skill-a" {
		t.Fatalf("unexpected workspace state: %+v", loadedState)
	}
	if len(loadedState.ActiveMCPServers) != 1 || loadedState.ActiveMCPServers[0] != "mcp-a" {
		t.Fatalf("unexpected workspace mcp state: %+v", loadedState)
	}

	recovered := mergeRecoveredPartMessages(loadedMsgs, loadedParts)
	if len(recovered) != 3 {
		t.Fatalf("expected merged recovered message, got %d", len(recovered))
	}
	if recovered[2].Role != "tool" {
		t.Fatalf("unexpected recovered message: %+v", recovered[2])
	}

	if _, err := loadSessionWorkspaceState(baseDir, "missing"); err != nil {
		t.Fatalf("loadSessionWorkspaceState(missing) failed: %v", err)
	}
	if _, err := loadSessionAIHistory(baseDir, "missing"); err != nil {
		t.Fatalf("loadSessionAIHistory(missing) failed: %v", err)
	}

	if got := filepath.Join(baseDir, sessionID, "workspace", "state.json"); got == "" {
		t.Fatal("expected workspace path")
	}
}
