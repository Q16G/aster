package react

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"aster/internal/builtin_tools"
	"aster/internal/runtimelog"
)

// AsyncAgentRegistry tracks background sub-agents spawned with run_in_background.
// Thread-safe: multiple goroutines may update entries; the scheduler goroutine drains notifications.
type AsyncAgentRegistry struct {
	mu            sync.RWMutex
	agents        map[string]*AsyncAgentEntry
	notifications chan *AsyncAgentNotification
}

type AsyncAgentEntry struct {
	AgentID      string
	Status       string // "running" | "completed" | "failed"
	Instruction  string
	WorkspaceDir string
	Result       *builtin_tools.RunResult
	DoneCh    chan struct{}
	StartedAt time.Time
	delivered bool
	closed    bool
}

type AsyncAgentNotification struct {
	AgentID      string
	Status       string
	WorkspaceDir string
	Result       *builtin_tools.RunResult
}

func NewAsyncAgentRegistry() *AsyncAgentRegistry {
	return &AsyncAgentRegistry{
		agents:        make(map[string]*AsyncAgentEntry),
		notifications: make(chan *AsyncAgentNotification, 64),
	}
}

// Register adds a new running async agent.
func (r *AsyncAgentRegistry) Register(agentID, instruction, workspaceDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agentID] = &AsyncAgentEntry{
		AgentID:      agentID,
		Status:       "running",
		Instruction:  instruction,
		WorkspaceDir: workspaceDir,
		DoneCh:       make(chan struct{}),
		StartedAt:    time.Now(),
	}
}

// Complete marks an async agent as completed or failed and sends a notification.
// Safe to call multiple times; only the first call takes effect.
func (r *AsyncAgentRegistry) Complete(agentID string, result *builtin_tools.RunResult) {
	r.mu.Lock()
	entry, ok := r.agents[agentID]
	if !ok || entry.closed {
		r.mu.Unlock()
		return
	}
	if result != nil && result.Success {
		entry.Status = "completed"
	} else {
		entry.Status = "failed"
	}
	entry.Result = result
	entry.closed = true
	close(entry.DoneCh)
	status := entry.Status
	wsDir := entry.WorkspaceDir
	r.mu.Unlock()

	notif := &AsyncAgentNotification{
		AgentID:      agentID,
		Status:       status,
		WorkspaceDir: wsDir,
		Result:       result,
	}
	select {
	case r.notifications <- notif:
	default:
		runtimelog.LogJSON("warning", map[string]any{
			"event":    "async_agent_notification_dropped",
			"agent_id": agentID,
			"reason":   "channel full",
		})
	}
}

// RunningAgents returns a snapshot of all currently running async agents.
func (r *AsyncAgentRegistry) RunningAgents() []*AsyncAgentEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*AsyncAgentEntry
	for _, entry := range r.agents {
		if entry.Status == "running" {
			result = append(result, entry)
		}
	}
	return result
}

// Get returns the entry for a specific agent, or nil if not found.
func (r *AsyncAgentRegistry) Get(agentID string) *AsyncAgentEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[agentID]
}

// MarkDelivered marks a completed agent's notification as delivered to stepHistory.
func (r *AsyncAgentRegistry) MarkDelivered(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.agents[agentID]; ok {
		entry.delivered = true
	}
}

// HasRunning returns true if any agents are still running.
func (r *AsyncAgentRegistry) HasRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, entry := range r.agents {
		if entry.Status == "running" {
			return true
		}
	}
	return false
}

const maxAsyncNotificationRunes = 1024

// writeAsyncResultFile writes the full async agent result to a workspace file.
func writeAsyncResultFile(workspaceDir string, notif *AsyncAgentNotification) string {
	if workspaceDir == "" || notif == nil {
		return ""
	}
	resultFile := filepath.Join(workspaceDir, "async_result.json")
	data := map[string]any{
		"agent_id": notif.AgentID,
		"status":   notif.Status,
	}
	if notif.Result != nil {
		data["ok"] = notif.Result.Success
		data["result"] = notif.Result.Result
		data["error"] = notif.Result.Error
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(resultFile), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(resultFile, raw, 0o644); err != nil {
		return ""
	}
	return resultFile
}

// truncateRuneString truncates s to at most maxRunes runes, appending "..." if truncated.
func truncateRuneString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + fmt.Sprintf("\n...truncated (%d runes total)", len(runes))
}
