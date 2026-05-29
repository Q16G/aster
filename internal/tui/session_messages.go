package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Type      string    `json:"type"`
	Name      string    `json:"name,omitempty"`
	CallID    string    `json:"call_id,omitempty"`
	AgentName string    `json:"agent_name,omitempty"`
	Content   string    `json:"content"`
	Time      time.Time `json:"time"`
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

func saveSessionDisplayParts(baseDir, sessionID string, parts []DisplayPart) error {
	dir := sessionDir(baseDir, sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.Create(filepath.Join(dir, "display_parts.jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, p := range parts {
		_ = enc.Encode(p)
	}
	return nil
}

func appendSessionDisplayPart(baseDir, sessionID string, part DisplayPart) error {
	dir := sessionDir(baseDir, sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(dir, "display_parts.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(part)
}

func loadSessionDisplayParts(baseDir, sessionID string) ([]DisplayPart, error) {
	path := filepath.Join(sessionDir(baseDir, sessionID), "display_parts.jsonl")
	parts, err := readJSONLDisplayParts(path)
	if err != nil {
		return nil, err
	}

	if parts == nil {
		// Fallback: try loading old messages.jsonl and migrate
		oldPath := filepath.Join(sessionDir(baseDir, sessionID), "messages.jsonl")
		oldMsgs, err := readJSONLOldMessages(oldPath)
		if err != nil || len(oldMsgs) == 0 {
			return nil, err
		}
		parts = migrateOldMessages(oldMsgs)
	}

	// Always merge recovery parts that are newer than the snapshot
	recoveryParts, _ := loadRecoveryParts(baseDir, sessionID)
	if len(recoveryParts) > 0 {
		parts = mergeRecoveryParts(parts, recoveryParts)
	}

	return parts, nil
}

func readJSONLDisplayParts(path string) ([]DisplayPart, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var parts []DisplayPart
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var part DisplayPart
		if err := json.Unmarshal(line, &part); err != nil {
			continue
		}
		parts = append(parts, part)
	}
	return parts, scanner.Err()
}

func readJSONLOldMessages(path string) ([]persistedMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var messages []persistedMessage
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
		messages = append(messages, pm)
	}
	return messages, scanner.Err()
}

func migrateOldMessages(messages []persistedMessage) []DisplayPart {
	var parts []DisplayPart
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			parts = append(parts, DisplayPart{Type: PartTypeUser, Time: msg.Time, User: &UserPart{Content: msg.Content}})
		case "assistant":
			parts = append(parts, DisplayPart{Type: PartTypeText, Time: msg.Time, Text: &TextPart{Content: msg.Content}})
		case "tool":
			parts = append(parts, DisplayPart{Type: PartTypeTool, Time: msg.Time, Tool: &ToolPart{Name: "unknown", Result: msg.Content, State: "completed"}})
		case "system":
			parts = append(parts, DisplayPart{Type: PartTypeSystem, Time: msg.Time, System: &SystemPart{Content: msg.Content}})
		case "plan":
			parts = append(parts, DisplayPart{Type: PartTypePlan, Time: msg.Time, Plan: &PlanPart{Explanation: msg.Content}})
		default:
			parts = append(parts, DisplayPart{Type: PartTypeSystem, Time: msg.Time, System: &SystemPart{Content: msg.Content}})
		}
	}
	return parts
}

func loadRecoveryParts(baseDir, sessionID string) ([]persistedPart, error) {
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

func mergeRecoveryParts(existing []DisplayPart, recovery []persistedPart) []DisplayPart {
	if len(recovery) == 0 {
		return existing
	}
	latestTime := time.Time{}
	for _, p := range existing {
		if p.Time.After(latestTime) {
			latestTime = p.Time
		}
	}

	for _, rp := range recovery {
		if !latestTime.IsZero() && !rp.Time.After(latestTime) {
			continue
		}
		switch rp.Type {
		case "tool_start":
			existing = append(existing, DisplayPart{
				Type: PartTypeTool,
				Time: rp.Time,
				Tool: &ToolPart{Name: rp.Name, CallID: rp.CallID, Arguments: rp.Content, State: "running"},
			})
		case "tool_end":
			existing = recoveryUpdateTool(existing, rp, func(t *ToolPart) {
				t.Result = rp.Content
				if strings.HasPrefix(rp.Content, "error: ") {
					t.State = "error"
					t.Error = strings.TrimPrefix(rp.Content, "error: ")
				} else {
					t.State = "completed"
				}
			})
		case "tool_update":
			existing = recoveryUpdateTool(existing, rp, func(t *ToolPart) {
				if t.Result == "" {
					t.Result = rp.Content
				} else {
					t.Result += " " + rp.Content
				}
			})
		case "result", "stream":
			if rp.Content != "" {
				existing = append(existing, DisplayPart{
					Type: PartTypeText,
					Time: rp.Time,
					Text: &TextPart{Content: rp.Content, AgentName: rp.AgentName},
				})
			}
		case "task_plan":
			var plan PlanPart
			if json.Unmarshal([]byte(rp.Content), &plan) == nil && (len(plan.Items) > 0 || plan.Explanation != "") {
				existing = append(existing, DisplayPart{
					Type: PartTypePlan,
					Time: rp.Time,
					Plan: &plan,
				})
			} else if rp.Content != "" {
				existing = append(existing, DisplayPart{
					Type: PartTypePlan,
					Time: rp.Time,
					Plan: &PlanPart{AgentName: rp.AgentName, Explanation: rp.Content},
				})
			}
		case "task_item":
			if rp.Name != "" {
				merged := false
				for i := len(existing) - 1; i >= 0; i-- {
					if existing[i].Type == PartTypePlan && existing[i].Plan != nil &&
						existing[i].Plan.AgentName == rp.AgentName {
						found := false
						for j := range existing[i].Plan.Items {
							if existing[i].Plan.Items[j].ID != "" && existing[i].Plan.Items[j].ID == rp.Name {
								existing[i].Plan.Items[j].Status = rp.Content
								found = true
								break
							}
						}
						if !found {
							for j := range existing[i].Plan.Items {
								if existing[i].Plan.Items[j].Step == rp.Name {
									existing[i].Plan.Items[j].Status = rp.Content
									found = true
									break
								}
							}
						}
						if !found {
							existing[i].Plan.Items = append(existing[i].Plan.Items, PlanItemView{ID: rp.Name, Step: rp.Name, Status: rp.Content})
						}
						merged = true
						break
					}
				}
				if !merged {
					existing = append(existing, DisplayPart{
						Type: PartTypePlan,
						Time: rp.Time,
						Plan: &PlanPart{AgentName: rp.AgentName, Items: []PlanItemView{{ID: rp.Name, Step: rp.Name, Status: rp.Content}}},
					})
				}
			}
		case "step_summary":
			if rp.Content != "" {
				var sp StepSummaryPart
				if json.Unmarshal([]byte(rp.Content), &sp) == nil && (sp.ShortSummary != "" || sp.LongSummary != "") {
					existing = append(existing, DisplayPart{
						Type:        PartTypeStepSummary,
						Time:        rp.Time,
						StepSummary: &sp,
					})
				}
			}
		case "step_replan":
			if rp.Content != "" {
				var sr StepReplanPart
				if json.Unmarshal([]byte(rp.Content), &sr) == nil {
					existing = append(existing, DisplayPart{
						Type:       PartTypeStepReplan,
						Time:       rp.Time,
						StepReplan: &sr,
					})
				}
			}
		case "step_result":
			if rp.Content != "" {
				var sr StepResultPart
				if json.Unmarshal([]byte(rp.Content), &sr) == nil && (sr.DisplayResult != "" || sr.Summary != "" || sr.Error != "") {
					existing = append(existing, DisplayPart{
						Type:       PartTypeStepResult,
						Time:       rp.Time,
						StepResult: &sr,
					})
				}
			}
		case "final_answer":
			if rp.Content != "" {
				existing = append(existing, DisplayPart{
					Type: PartTypeFinalAnswer,
					Time: rp.Time,
					FinalAnswer: &FinalAnswerPart{
						Content: rp.Content,
						Source:  rp.Name,
					},
				})
			}
		case "sub_agent":
			if rp.Content != "" {
				var sa SubAgentPart
				if json.Unmarshal([]byte(rp.Content), &sa) == nil && sa.AgentName != "" {
					if sa.CallID == "" {
						sa.CallID = rp.CallID
					}
					existing = append(existing, DisplayPart{
						Type:     PartTypeSubAgent,
						Time:     rp.Time,
						SubAgent: &sa,
					})
				}
			}
		default:
			if rp.Content != "" {
				existing = append(existing, DisplayPart{
					Type:   PartTypeSystem,
					Time:   rp.Time,
					System: &SystemPart{Content: rp.Content},
				})
			}
		}
	}
	return existing
}

func recoveryUpdateTool(existing []DisplayPart, rp persistedPart, fn func(*ToolPart)) []DisplayPart {
	for i := len(existing) - 1; i >= 0; i-- {
		if existing[i].Type != PartTypeTool || existing[i].Tool == nil {
			continue
		}
		t := existing[i].Tool
		if rp.CallID != "" && t.CallID == rp.CallID {
			fn(t)
			return existing
		}
		if rp.CallID == "" && t.Name == rp.Name && t.State == "running" {
			fn(t)
			return existing
		}
	}
	newTool := &ToolPart{Name: rp.Name, CallID: rp.CallID, State: "completed"}
	fn(newTool)
	return append(existing, DisplayPart{Type: PartTypeTool, Time: rp.Time, Tool: newTool})
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

type asyncSubAgentResult struct {
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
	OK      bool   `json:"ok"`
	Result  string `json:"result"`
	Error   string `json:"error"`
}

// backfillCompletedSubAgents scans <session>/workspace/sub_agents/*/async_result.json
// and appends a notification message to the parent history for any terminal
// sub-agent result not already represented there. This recovers the case where a
// session was interrupted after a sub-agent finished but before the parent agent
// drained the async notification into its own history (see
// react.drainAsyncAgentNotifications). The injected message uses the same textual
// format as the live drain so the resumed parent sees identical context.
// Returns the augmented history and the number of injected entries.
func backfillCompletedSubAgents(baseDir, sessionID string, history []*ai.MsgInfo) ([]*ai.MsgInfo, int) {
	subAgentsDir := filepath.Join(sessionWorkspaceDir(baseDir, sessionID), "sub_agents")
	entries, err := os.ReadDir(subAgentsDir)
	if err != nil {
		return history, 0
	}

	present := make(map[string]struct{})
	for _, msg := range history {
		if msg == nil {
			continue
		}
		for _, id := range scanNotifiedAgentIDs(msgContentString(msg.Content)) {
			present[id] = struct{}{}
		}
	}

	injected := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		resultFile := filepath.Join(subAgentsDir, e.Name(), "async_result.json")
		data, rerr := os.ReadFile(resultFile)
		if rerr != nil {
			continue
		}
		var res asyncSubAgentResult
		if json.Unmarshal(data, &res) != nil {
			continue
		}
		agentID := strings.TrimSpace(res.AgentID)
		if agentID == "" {
			agentID = e.Name()
		}
		status := strings.TrimSpace(res.Status)
		if status == "" || status == "running" {
			continue
		}
		if _, ok := present[agentID]; ok {
			continue
		}

		summary := res.Result
		if summary == "" {
			summary = res.Error
		}
		summary = truncateRuneString(summary, 1024)

		workspace := filepath.Dir(resultFile)
		text := fmt.Sprintf(
			"[后台 Agent 完成通知]\nagent_id: %s\nstatus: %s\nworkspace: %s\nresult_file: %s",
			agentID, status, workspace, resultFile,
		)
		if summary != "" {
			text += fmt.Sprintf("\nresult_summary:\n%s", summary)
		}
		history = append(history, ai.NewUserMsgInfo(text))
		present[agentID] = struct{}{}
		injected++
	}
	return history, injected
}

// scanNotifiedAgentIDs extracts the agent_id values already recorded in a
// historical notification message body ("agent_id: <name>" lines).
func scanNotifiedAgentIDs(content string) []string {
	if content == "" {
		return nil
	}
	var ids []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "agent_id:"); ok {
			if id := strings.TrimSpace(rest); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func msgContentString(content any) string {
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", content)
}

func truncateRuneString(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "..."
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

func ensureSessionWorkspace(baseDir, sessionID string) error {
	return os.MkdirAll(sessionWorkspaceDir(baseDir, sessionID), 0755)
}
