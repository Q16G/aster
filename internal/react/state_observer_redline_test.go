package react

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"aster/internal/builtin_tools"
)

// 红线测试组：覆盖 session 288b1031 暴露的三个症状：
//   - peer Mark InProgress 必须触发 task_item emit（TUI 右侧 todo 翻 in_progress）
//   - peer Mark InProgress 必须触发 inline_step_start emit（peer 卡片就位）
//   - peer Mark InProgress 必须触发 step_<id>.md 骨架创建
//   - peer auto-complete 必须触发 inline_step_end emit + task_item completed emit
//
// 测试不再依赖任何调用方的"手 emit / 手 scaffold"——只走 state observer。

type capturedEvents struct {
	mu     sync.Mutex
	events []*AgentOutputEvent
}

func (c *capturedEvents) all() []*AgentOutputEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*AgentOutputEvent, len(c.events))
	copy(out, c.events)
	return out
}

func (c *capturedEvents) byType(eventType EventType) []*AgentOutputEvent {
	var out []*AgentOutputEvent
	for _, e := range c.all() {
		if e != nil && e.Type == eventType {
			out = append(out, e)
		}
	}
	return out
}

func newCapturingEmitter() (*Emitter, *capturedEvents) {
	cap := &capturedEvents{}
	em := NewEmitter("agent-id", "agent-name", func(e *AgentOutputEvent) error {
		cap.mu.Lock()
		defer cap.mu.Unlock()
		cap.events = append(cap.events, e)
		return nil
	})
	return em, cap
}

// newAgentForObserverTest 复刻 NewReActAgent 关键 wiring：
// state + emitter + workspaceRuntime + RegisterObserver(emitter+workspace)。
// 不走完整 ReAct 构造（绕开 ai.ChatClient 等）。
func newAgentForObserverTest(t *testing.T) (*Agent, *capturedEvents, string) {
	t.Helper()
	tmpDir := t.TempDir()
	rt, err := newLocalWorkspaceRuntime("test-session", tmpDir, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	if seeder, ok := rt.(interface{ EnsureSharedScaffold() error }); ok {
		if err := seeder.EnsureSharedScaffold(); err != nil {
			t.Fatalf("EnsureSharedScaffold: %v", err)
		}
	}
	em, cap := newCapturingEmitter()
	a := &Agent{
		state:            NewStateTracker(),
		emitter:          em,
		workspaceRuntime: rt,
		workspaceRootDir: tmpDir,
	}
	a.state.RegisterObserver(newEmitterStateObserver(a))
	a.state.RegisterObserver(newWorkspaceStateObserver(a))
	return a, cap, tmpDir
}

// TestObserver_PeerInProgressEmitsTaskItemAndCard 红线：peer 翻 InProgress 必须 emit
// task_item + inline_step_start。修复前 spawnInlinePeer 只 emit inline_step_start，
// task_item 漏 emit 导致 TUI todo 看不到 in_progress（session 288b1031 症状 3）。
func TestObserver_PeerInProgressEmitsTaskItemAndCard(t *testing.T) {
	a, cap, _ := newAgentForObserverTest(t)
	a.state.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "p1", Step: "main", Status: builtin_tools.PlanStepPending},
		{ID: "p2", Step: "peer 任务", Status: builtin_tools.PlanStepPending},
	}, "init", true)

	a.state.MarkInlineStepInProgress("p2")

	taskItemEvents := cap.byType(EventTypeTaskItem)
	var p2TaskItem *AgentOutputEvent
	for _, e := range taskItemEvents {
		if id, _ := e.Payload["id"].(string); id == "p2" {
			p2TaskItem = e
			break
		}
	}
	if p2TaskItem == nil {
		t.Fatalf("expected task_item event for p2 (total task_items=%d)", len(taskItemEvents))
	}
	if status, _ := p2TaskItem.Payload["status"].(builtin_tools.PlanStepStatus); status != builtin_tools.PlanStepInProgress {
		t.Errorf("p2 task_item status=%v, want in_progress", p2TaskItem.Payload["status"])
	}

	cardEvents := cap.byType(EventTypeInlineStepStart)
	if len(cardEvents) != 1 {
		t.Fatalf("expected 1 inline_step_start event, got %d", len(cardEvents))
	}
	if id, _ := cardEvents[0].Payload["step_id"].(string); id != "p2" {
		t.Errorf("inline_step_start step_id=%q, want p2", id)
	}
}

// TestObserver_PeerInProgressCreatesStepFileScaffold 红线：peer 翻 InProgress 必须
// 自动创建 step_<id>.md。修复前 spawnInlinePeer 不调 ensureStepFileScaffold（仅主
// 路径 runStepPhase 调），导致 step_p2.md 缺失（session 288b1031 症状 2）。
func TestObserver_PeerInProgressCreatesStepFileScaffold(t *testing.T) {
	a, _, tmpDir := newAgentForObserverTest(t)
	a.state.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "p1", Step: "main", Status: builtin_tools.PlanStepPending},
		{ID: "p2", Step: "peer 任务", Status: builtin_tools.PlanStepPending},
	}, "init", true)

	a.state.MarkInlineStepInProgress("p2")

	wantPath := filepath.Join(tmpDir, "shared", "step_p2.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected scaffold file %q to exist: %v", wantPath, err)
	}
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read scaffold: %v", err)
	}
	if len(data) == 0 {
		t.Errorf("scaffold file is empty, want skeleton")
	}
}

// TestObserver_MainInProgressCreatesStepFileScaffold 主路径同 peer 一致的不变量：
// runStepPhase 删去显式 ensureStepFileScaffold 调用后，仍必须通过 observer 创建。
func TestObserver_MainInProgressCreatesStepFileScaffold(t *testing.T) {
	a, _, tmpDir := newAgentForObserverTest(t)
	a.state.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "s1", Step: "main step", Status: builtin_tools.PlanStepPending},
	}, "init", true)
	a.state.EnsureCurrentStep()

	a.state.MarkCurrentStepInProgress()

	wantPath := filepath.Join(tmpDir, "shared", "step_s1.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected scaffold file %q to exist: %v", wantPath, err)
	}
}

// TestObserver_PeerAutoCompleteEmitsTerminal peer auto-complete 必须 emit
// task_item completed + inline_step_end，不依赖下一轮 step_replan 兜底 diff。
func TestObserver_PeerAutoCompleteEmitsTerminal(t *testing.T) {
	a, cap, _ := newAgentForObserverTest(t)
	a.state.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "p2", Step: "peer 任务", Status: builtin_tools.PlanStepPending},
	}, "init", true)
	a.state.MarkInlineStepInProgress("p2")

	// 清空 init/in_progress 事件，让本测试聚焦终态翻转。
	cap.mu.Lock()
	cap.events = nil
	cap.mu.Unlock()

	a.state.UpdateInlineStep("p2", builtin_tools.CurrentStepUpdate{
		Status:  builtin_tools.PlanStepCompleted,
		Summary: "peer done",
	})

	taskItemEvents := cap.byType(EventTypeTaskItem)
	if len(taskItemEvents) == 0 {
		t.Fatalf("expected task_item for completed transition, got none")
	}
	var completed bool
	for _, e := range taskItemEvents {
		if id, _ := e.Payload["id"].(string); id == "p2" {
			if status, _ := e.Payload["status"].(builtin_tools.PlanStepStatus); status == builtin_tools.PlanStepCompleted {
				completed = true
			}
		}
	}
	if !completed {
		t.Errorf("expected p2 task_item status=completed, got events=%+v", taskItemEvents)
	}

	endEvents := cap.byType(EventTypeInlineStepEnd)
	if len(endEvents) != 1 {
		t.Fatalf("expected 1 inline_step_end, got %d", len(endEvents))
	}
	if status, _ := endEvents[0].Payload["status"].(string); status != "completed" {
		t.Errorf("inline_step_end status=%q, want completed", status)
	}
}

// TestObserver_MainPathDoesNotEmitInlineStepCard 主路径翻 InProgress 不应 emit
// inline_step_start——卡片事件是 peer 路径专属（actor=peer 才双轨）。
func TestObserver_MainPathDoesNotEmitInlineStepCard(t *testing.T) {
	a, cap, _ := newAgentForObserverTest(t)
	a.state.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "s1", Step: "main", Status: builtin_tools.PlanStepPending},
	}, "init", true)
	a.state.EnsureCurrentStep()

	a.state.MarkCurrentStepInProgress()

	if got := len(cap.byType(EventTypeInlineStepStart)); got != 0 {
		t.Fatalf("main path should not emit inline_step_start, got %d", got)
	}
}
