package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

func TestSessionArtifactsRoundTrip(t *testing.T) {
	baseDir := t.TempDir()
	sessionID := "ses-test"

	parts := []DisplayPart{
		{Type: PartTypeUser, Time: time.Unix(10, 0), User: &UserPart{Content: "hello"}},
		{Type: PartTypeText, Time: time.Unix(20, 0), Text: &TextPart{Content: "world"}},
	}
	if err := saveSessionDisplayParts(baseDir, sessionID, parts); err != nil {
		t.Fatalf("saveSessionDisplayParts failed: %v", err)
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

	loadedParts, err := loadSessionDisplayParts(baseDir, sessionID)
	if err != nil {
		t.Fatalf("loadSessionDisplayParts failed: %v", err)
	}
	// 2 saved display parts + 1 recovery part (tool_end at t=30, newer than t=20)
	if len(loadedParts) != 3 {
		t.Fatalf("expected 3 parts (2 saved + 1 recovered), got %d", len(loadedParts))
	}
	if loadedParts[0].Type != PartTypeUser || loadedParts[0].User.Content != "hello" {
		t.Fatalf("unexpected first part: %+v", loadedParts[0])
	}
	if loadedParts[2].Type != PartTypeTool || loadedParts[2].Tool.Name != "list_files" {
		t.Fatalf("unexpected recovered part: %+v", loadedParts[2])
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

func TestOldMessagesMigration(t *testing.T) {
	baseDir := t.TempDir()
	sessionID := "ses-migrate"

	// Write old-format messages.jsonl
	oldMsgs := []persistedMessage{
		{Role: "user", Content: "hello", Time: time.Unix(10, 0)},
		{Role: "assistant", Content: "world", Time: time.Unix(20, 0)},
	}
	if err := saveOldMessages(baseDir, sessionID, oldMsgs); err != nil {
		t.Fatalf("saveOldMessages failed: %v", err)
	}

	// loadSessionDisplayParts should migrate old format
	parts, err := loadSessionDisplayParts(baseDir, sessionID)
	if err != nil {
		t.Fatalf("loadSessionDisplayParts failed: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 migrated parts, got %d", len(parts))
	}
	if parts[0].Type != PartTypeUser || parts[0].User.Content != "hello" {
		t.Fatalf("unexpected migrated part[0]: %+v", parts[0])
	}
	if parts[1].Type != PartTypeText || parts[1].Text.Content != "world" {
		t.Fatalf("unexpected migrated part[1]: %+v", parts[1])
	}
}

func saveOldMessages(baseDir, sessionID string, msgs []persistedMessage) error {
	dir := sessionDir(baseDir, sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "messages.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, m := range msgs {
		_ = enc.Encode(m)
	}
	return nil
}

func TestMergeRecoveryParts(t *testing.T) {
	existing := []DisplayPart{
		{Type: PartTypeUser, Time: time.Unix(10, 0), User: &UserPart{Content: "hello"}},
	}
	recovery := []persistedPart{
		{Type: "tool_end", Name: "bash", Content: "done", Time: time.Unix(20, 0)},
		{Type: "tool_start", Name: "rg", Content: "search", Time: time.Unix(5, 0)}, // before existing, should be skipped
	}
	merged := mergeRecoveryParts(existing, recovery)
	if len(merged) != 2 {
		t.Fatalf("expected 2 parts after merge, got %d", len(merged))
	}
	if merged[1].Type != PartTypeTool || merged[1].Tool.Name != "bash" {
		t.Fatalf("unexpected merged part: %+v", merged[1])
	}
}
