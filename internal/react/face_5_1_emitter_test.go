package react

import (
	"context"
	"sync"
	"testing"
	"time"

	"aster/internal/builtin_tools"
)

// remoteStepEventRecorder 收集所有 EventTypeInlineStepStart/BgEnd 事件。
func remoteStepEventRecorder() (*Emitter, func() (map[string]string, map[string]string)) {
	var mu sync.Mutex
	starts := map[string]string{}
	ends := map[string]string{}
	em := NewEmitter("", "", func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		id, _ := e.Payload["agent_id"].(string)
		mu.Lock()
		defer mu.Unlock()
		switch e.Type {
		case EventTypeInlineStepStart:
			stepText, _ := e.Payload["step_text"].(string)
			starts[id] = stepText
		case EventTypeInlineStepEnd:
			status, _ := e.Payload["status"].(string)
			ends[id] = status
		}
		return nil
	})
	return em, func() (map[string]string, map[string]string) {
		mu.Lock()
		defer mu.Unlock()
		s := make(map[string]string, len(starts))
		e := make(map[string]string, len(ends))
		for k, v := range starts {
			s[k] = v
		}
		for k, v := range ends {
			e[k] = v
		}
		return s, e
	}
}

// drainAgentWithEmitter 构造 drain 测试 agent + 注入 emitter，让事件被记录。
// inline_step_start/end 现在走 state observer（commit 2 之后），所以构造 Agent 后
// 必须 RegisterObserver(newEmitterStateObserver(a))，否则 UpdateInlineStep 翻终态
// 不会触发 inline_step_end emit。
func drainAgentWithEmitter(t *testing.T, em *Emitter) *Agent {
	t.Helper()
	r := NewAsyncAgentRegistry()
	state := NewStateTracker()
	state.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "a", Step: "a", Status: builtin_tools.PlanStepCompleted},
		{ID: "b", Step: "主路径 b", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
		{ID: "c", Step: "远程 c", Status: builtin_tools.PlanStepPending, DependsOn: []string{"a"}},
	}, "init", true)
	state.EnsureCurrentStep()
	a := &Agent{asyncRegistry: r, state: state, emitter: em}
	a.state.RegisterObserver(newEmitterStateObserver(a))
	return a
}

func TestDrain_RemoteStepEmitsBgEndOnComplete(t *testing.T) {
	em, snapshot := remoteStepEventRecorder()
	a := drainAgentWithEmitter(t, em)

	a.asyncRegistry.RegisterInlineStep("c", "")
	a.asyncRegistry.Complete("c", &builtin_tools.RunResult{Success: true, Result: "remote done"})

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	a.drainAsyncAgentNotifications(context.Background())

	_, ends := snapshot()
	if got := ends["c"]; got != "completed" {
		t.Fatalf("expected ends[c]=completed, got %q (full=%v)", got, ends)
	}
}

func TestDrain_RemoteStepEmitsBgEndOnFailed(t *testing.T) {
	em, snapshot := remoteStepEventRecorder()
	a := drainAgentWithEmitter(t, em)

	a.asyncRegistry.RegisterInlineStep("c", "")
	a.asyncRegistry.Complete("c", &builtin_tools.RunResult{Success: false, Error: "boom"})

	deadline := time.After(500 * time.Millisecond)
	for len(a.asyncRegistry.notifications) == 0 {
		select {
		case <-deadline:
			t.Fatal("timeout")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	a.drainAsyncAgentNotifications(context.Background())

	_, ends := snapshot()
	if got := ends["c"]; got != "failed" {
		t.Fatalf("expected ends[c]=failed, got %q (full=%v)", got, ends)
	}
}

func TestCancelRunningInlineSteps_EmitsBgEndCancelled(t *testing.T) {
	em, snapshot := remoteStepEventRecorder()
	a := drainAgentWithEmitter(t, em)

	a.asyncRegistry.RegisterInlineStep("c", "")
	a.asyncRegistry.RegisterInlineStep("d", "")

	a.cancelRunningInlineSteps()

	_, ends := snapshot()
	if got := ends["c"]; got != "cancelled" {
		t.Fatalf("expected ends[c]=cancelled, got %q (full=%v)", got, ends)
	}
	if got := ends["d"]; got != "cancelled" {
		t.Fatalf("expected ends[d]=cancelled, got %q (full=%v)", got, ends)
	}
}

func TestCancelRunningInlineSteps_IgnoresSubAgentEntries(t *testing.T) {
	em, snapshot := remoteStepEventRecorder()
	a := drainAgentWithEmitter(t, em)

	a.asyncRegistry.Register("sub-1", "x", "")  // sub_agent kind=""
	a.asyncRegistry.RegisterInlineStep("c", "") // remote_step kind

	a.cancelRunningInlineSteps()

	_, ends := snapshot()
	if _, found := ends["sub-1"]; found {
		t.Fatalf("cancelRunningInlineSteps should NOT emit for sub_agent entries, got %v", ends)
	}
	if got := ends["c"]; got != "cancelled" {
		t.Fatalf("expected remote_step c cancelled, got %q", got)
	}
}

// TestCancelRunningSubAgents_IgnoresRemoteStepEntries：对称回归——
// cancelRunningSubAgents 看到 Kind=remote_step 时不应发 SubAgentBgEnd 事件，
// 避免 TUI 把远程 step 卡当 sub_agent 卡渲染、串台。
func TestCancelRunningSubAgents_IgnoresRemoteStepEntries(t *testing.T) {
	em, snapshot := remoteStepEventRecorder()
	// 同时收集 SubAgentBgEnd 事件，验证不发到 remote_step
	subEnds := map[string]string{}
	combinedEm := NewEmitter("", "", func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		id, _ := e.Payload["agent_id"].(string)
		status, _ := e.Payload["status"].(string)
		switch e.Type {
		case EventTypeSubAgentBgEnd:
			subEnds[id] = status
		case EventTypeInlineStepStart, EventTypeInlineStepEnd:
			// 走原 recorder 钩子（这里测试 cancel 路径，主要看 SubAgentBgEnd）
		}
		return nil
	})
	_ = em // 复用 helper 但本测试主要看 combinedEm
	a := drainAgentWithEmitter(t, combinedEm)

	a.asyncRegistry.Register("sub-1", "x", "")  // sub_agent
	a.asyncRegistry.RegisterInlineStep("c", "") // remote_step

	a.cancelRunningSubAgents()

	if got := subEnds["sub-1"]; got != "cancelled" {
		t.Fatalf("expected sub-1 cancelled via SubAgentBgEnd, got %q", got)
	}
	if _, found := subEnds["c"]; found {
		t.Fatalf("cancelRunningSubAgents should NOT emit SubAgentBgEnd for remote_step c")
	}
	// 副 snapshot 仅用于让 helper 不报未用
	_ = snapshot
}

// Note：spawnRemoteStep 发 BgStart 单测留 follow-up——需要 stub agentFactory
// 让 spawn 同步段全过；与面 3.1 spawn 成功集成测试同处境。
// 端到端 TUI 验证（face 5.1 验证步 2）能覆盖 BgStart 路径正确性。
