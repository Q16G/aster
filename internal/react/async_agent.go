package react

import (
	"context"
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
	// completed is a coalescing wake signal (cap=1). Complete() pushes a
	// non-blocking token so the scheduler loop can park on WaitForCompletion
	// and wake immediately when any background sub-agent finishes.
	completed chan struct{}
}

// AsyncAgent kind 区分两种后台任务：sub_agent 走 stepHistory 注入路径；
// remote_step 走 state.UpdateInlineStep 回写，不污染主 transcript。
// 空字符串视同 sub_agent，保持现状调用方零改动。
const (
	AsyncAgentKindSubAgent   = "sub_agent"
	AsyncAgentKindRemoteStep = "remote_step"
)

type AsyncAgentEntry struct {
	AgentID      string
	Kind         string // "" 或 AsyncAgentKindSubAgent / AsyncAgentKindRemoteStep
	Status       string // "running" | "completed" | "failed"
	Instruction  string
	WorkspaceDir string
	Result       *builtin_tools.RunResult
	StartedAt    time.Time
	delivered    bool
	closed       bool
}

type AsyncAgentNotification struct {
	AgentID      string
	Kind         string // 从 entry.Kind 复制；drain 按此分流
	Status       string
	WorkspaceDir string
	Result       *builtin_tools.RunResult
}

func NewAsyncAgentRegistry() *AsyncAgentRegistry {
	return &AsyncAgentRegistry{
		agents:        make(map[string]*AsyncAgentEntry),
		notifications: make(chan *AsyncAgentNotification, 64),
		completed:     make(chan struct{}, 1),
	}
}

// Register adds a new running async agent (sub_agent 路径，Kind 默认空).
// 保持现有调用方零改动；新引入的 remote_step 走 RegisterRemoteStep。
func (r *AsyncAgentRegistry) Register(agentID, instruction, workspaceDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[agentID] = &AsyncAgentEntry{
		AgentID:      agentID,
		Status:       "running",
		Instruction:  instruction,
		WorkspaceDir: workspaceDir,
		StartedAt:    time.Now(),
	}
}

// RegisterRemoteStep 注册一个 X2 远程 step。AgentID 复用 plan step ID，
// Kind = AsyncAgentKindRemoteStep。drain 路径据此分流到 state.UpdateInlineStep，
// 不灌 stepHistory（远程 step 的 transcript 由 step_fanout 落 blob，按指针读）。
func (r *AsyncAgentRegistry) RegisterRemoteStep(stepID, workspaceDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[stepID] = &AsyncAgentEntry{
		AgentID:      stepID,
		Kind:         AsyncAgentKindRemoteStep,
		Status:       "running",
		WorkspaceDir: workspaceDir,
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
	status := entry.Status
	wsDir := entry.WorkspaceDir
	kind := entry.Kind
	r.mu.Unlock()

	notif := &AsyncAgentNotification{
		AgentID:      agentID,
		Kind:         kind,
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

	// Coalescing kick: wake any scheduler loop parked on WaitForCompletion.
	// cap=1 + non-blocking means multiple concurrent completions need only one
	// token to wake the loop, which then drains all pending notifications.
	select {
	case r.completed <- struct{}{}:
	default:
	}
}

// WaitForCompletion blocks until any background sub-agent completes or ctx is
// cancelled. Returns immediately if no agents are currently running. No timeout
// is imposed: Complete() always fires via a defer/recover in the spawn goroutine
// (sub_agent_tool.go), so a finishing child always wakes the park; ctx.Done()
// covers user interruption and parent cancellation. Consumes at most one
// coalesced wake token per call; the scheduler loop drains the actual
// notifications on its next iteration.
func (r *AsyncAgentRegistry) WaitForCompletion(ctx context.Context) {
	if r == nil || !r.HasRunning() {
		return
	}
	select {
	case <-r.completed:
	case <-ctx.Done():
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

// RunningRemoteSteps 返回当前仍 running 的 remote_step 数。供 X2 fan-out
// 决策按 MaxParallelSteps 上限判断是否还可派发新远程 step（不影响 sub_agent 计数）。
func (r *AsyncAgentRegistry) RunningRemoteSteps() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, entry := range r.agents {
		if entry == nil {
			continue
		}
		if entry.Status == "running" && entry.Kind == AsyncAgentKindRemoteStep {
			count++
		}
	}
	return count
}

// HasRunningRemoteSteps O(1) 早退实现，语义等价 RunningRemoteSteps() > 0。
func (r *AsyncAgentRegistry) HasRunningRemoteSteps() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, entry := range r.agents {
		if entry == nil {
			continue
		}
		if entry.Status == "running" && entry.Kind == AsyncAgentKindRemoteStep {
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

// PurgeDelivered removes entries that are completed/failed AND whose notification
// has been delivered to stepHistory. This prevents the agents map from growing
// unbounded across many background sub-agent spawns.
func (r *AsyncAgentRegistry) PurgeDelivered() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	purged := 0
	for id, entry := range r.agents {
		if entry.closed && entry.delivered {
			delete(r.agents, id)
			purged++
		}
	}
	return purged
}

// Reset clears all entries and drains any remaining notifications.
// Should be called when the parent Agent's Execute() completes a turn to ensure
// no stale references are retained across turns.
func (r *AsyncAgentRegistry) Reset() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.agents = make(map[string]*AsyncAgentEntry)
	r.mu.Unlock()
	for {
		select {
		case <-r.notifications:
		default:
			// Drain the coalesced wake token too, so a stale completion from a
			// previous turn does not immediately un-park the next turn's wait.
			select {
			case <-r.completed:
			default:
			}
			return
		}
	}
}

// truncateRuneString truncates s to at most maxRunes runes, appending "..." if truncated.
func truncateRuneString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + fmt.Sprintf("\n...truncated (%d runes total)", len(runes))
}
