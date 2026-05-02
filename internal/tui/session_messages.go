package tui

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type persistedMessage struct {
	Role    string    `json:"role"`
	Content string    `json:"content"`
	Time    time.Time `json:"time"`
}

type persistedPart struct {
	Type    string    `json:"type"`
	Name    string    `json:"name,omitempty"`
	Content string    `json:"content"`
	Time    time.Time `json:"time"`
}

type persistedRunEvent struct {
	RunID   string    `json:"run_id"`
	Event   string    `json:"event"`
	Input   string    `json:"input,omitempty"`
	Success bool      `json:"success,omitempty"`
	Error   string    `json:"error,omitempty"`
	Time    time.Time `json:"time"`
}

func sessionDir(baseDir, sessionID string) string {
	return filepath.Join(baseDir, sessionID)
}

func sessionWorkspaceDir(baseDir, sessionID string) string {
	return filepath.Join(baseDir, sessionID, "workspace")
}

func saveSessionMessages(baseDir, sessionID string, messages []ChatMessage) error {
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
	for _, m := range messages {
		_ = enc.Encode(persistedMessage{Role: m.Role, Content: m.Content, Time: m.Time})
	}
	return nil
}

func appendSessionMessage(baseDir, sessionID string, msg ChatMessage) error {
	dir := sessionDir(baseDir, sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(dir, "messages.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(persistedMessage{Role: msg.Role, Content: msg.Content, Time: msg.Time})
}

func loadSessionMessages(baseDir, sessionID string) ([]ChatMessage, error) {
	path := filepath.Join(sessionDir(baseDir, sessionID), "messages.jsonl")
	return readJSONLMessages(path)
}

func readJSONLMessages(path string) ([]ChatMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var messages []ChatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var pm persistedMessage
		if err := json.Unmarshal(line, &pm); err != nil {
			continue
		}
		messages = append(messages, ChatMessage{Role: pm.Role, Content: pm.Content, Time: pm.Time})
	}
	return messages, scanner.Err()
}

func appendSessionPart(baseDir, sessionID string, part persistedPart) error {
	dir := sessionDir(baseDir, sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(dir, "parts.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(part)
}

func loadSessionParts(baseDir, sessionID string) ([]persistedPart, error) {
	path := filepath.Join(sessionDir(baseDir, sessionID), "parts.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var parts []persistedPart
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var part persistedPart
		if err := json.Unmarshal(line, &part); err != nil {
			continue
		}
		parts = append(parts, part)
	}
	return parts, scanner.Err()
}

func appendSessionRunEvent(baseDir, sessionID string, event persistedRunEvent) error {
	dir := sessionDir(baseDir, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(dir, "runs.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(event)
}

func loadSessionRunEvents(baseDir, sessionID string) ([]persistedRunEvent, error) {
	path := filepath.Join(sessionDir(baseDir, sessionID), "runs.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []persistedRunEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event persistedRunEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func saveSessionAIHistory(baseDir, sessionID string, history []*ai.MsgInfo) error {
	dir := sessionDir(baseDir, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(ai.NormalizeMsgInfoSlice(history), "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, "ai_history.json"), data, 0o644)
}

func loadSessionAIHistory(baseDir, sessionID string) ([]*ai.MsgInfo, error) {
	path := filepath.Join(sessionDir(baseDir, sessionID), "ai_history.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var history []*ai.MsgInfo
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}
	return ai.NormalizeMsgInfoSlice(history), nil
}

func loadSessionWorkspaceState(baseDir, sessionID string) (*builtin_tools.WorkspaceState, error) {
	path := filepath.Join(sessionWorkspaceDir(baseDir, sessionID), "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &builtin_tools.WorkspaceState{
				SessionID:          sessionID,
				LatestStepOutcomes: make(map[string]*builtin_tools.WorkspaceStepOutcomePointer),
				ChildAgents:        make(map[string]*builtin_tools.WorkspaceChildAgentPointer),
			}, nil
		}
		return nil, err
	}
	var state builtin_tools.WorkspaceState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.SessionID == "" {
		state.SessionID = sessionID
	}
	if state.LatestStepOutcomes == nil {
		state.LatestStepOutcomes = make(map[string]*builtin_tools.WorkspaceStepOutcomePointer)
	}
	if state.ChildAgents == nil {
		state.ChildAgents = make(map[string]*builtin_tools.WorkspaceChildAgentPointer)
	}
	return &state, nil
}

func saveSessionWorkspaceState(baseDir, sessionID string, state *builtin_tools.WorkspaceState) error {
	if state == nil {
		return nil
	}
	dir := sessionWorkspaceDir(baseDir, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if state.SessionID == "" {
		state.SessionID = sessionID
	}
	if state.LatestStepOutcomes == nil {
		state.LatestStepOutcomes = make(map[string]*builtin_tools.WorkspaceStepOutcomePointer)
	}
	if state.ChildAgents == nil {
		state.ChildAgents = make(map[string]*builtin_tools.WorkspaceChildAgentPointer)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644)
}

func mergeRecoveredPartMessages(messages []ChatMessage, parts []persistedPart) []ChatMessage {
	if len(parts) == 0 {
		return messages
	}
	latestMsgTime := time.Time{}
	for _, msg := range messages {
		if msg.Time.After(latestMsgTime) {
			latestMsgTime = msg.Time
		}
	}

	var recovered []ChatMessage
	for _, part := range parts {
		if !latestMsgTime.IsZero() && !part.Time.After(latestMsgTime) {
			continue
		}
		role := "system"
		content := part.Content
		switch part.Type {
		case "tool_start":
			role = "tool"
			content = toolMessageContent(part.Name, part.Content, "running...")
		case "tool_end":
			role = "tool"
			content = toolMessageContent(part.Name, "", part.Content)
		default:
			if part.Name != "" {
				content = part.Name + ": " + part.Content
			}
		}
		recovered = append(recovered, ChatMessage{
			Role:    role,
			Content: content,
			Time:    part.Time,
		})
	}
	if len(recovered) == 0 {
		return messages
	}

	out := append(append([]ChatMessage{}, messages...), recovered...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Time.Before(out[j].Time)
	})
	return out
}

func ensureSessionWorkspace(baseDir, sessionID string) error {
	return os.MkdirAll(sessionWorkspaceDir(baseDir, sessionID), 0755)
}
