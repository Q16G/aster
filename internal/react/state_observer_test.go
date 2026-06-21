package react

import (
	"sync"
	"sync/atomic"
	"testing"

	"aster/internal/builtin_tools"
)

// recordingObserver 把每次回调追加到 changes 切片，调用方按需断言。
type recordingObserver struct {
	mu      sync.Mutex
	changes []PlanItemChange
}

func (r *recordingObserver) OnPlanItemChange(change PlanItemChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	clone := change
	r.changes = append(r.changes, clone)
}

func (r *recordingObserver) snapshot() []PlanItemChange {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]PlanItemChange, len(r.changes))
	copy(out, r.changes)
	return out
}

func newTrackerWithPlan(t *testing.T, items ...*builtin_tools.PlanItem) *StateTracker {
	t.Helper()
	tr := NewStateTracker()
	tr.UpdatePlan(append([]*builtin_tools.PlanItem(nil), items...), "init", true)
	return tr
}

// TestStateObserver_MarkCurrentInProgressEmitsMainActor 验证主路径 Pending→InProgress
// observer 收到 actor=main 单条变化。
func TestStateObserver_MarkCurrentInProgressEmitsMainActor(t *testing.T) {
	tr := newTrackerWithPlan(t,
		&builtin_tools.PlanItem{ID: "s1", Step: "first", Status: builtin_tools.PlanStepPending},
		&builtin_tools.PlanItem{ID: "s2", Step: "second", Status: builtin_tools.PlanStepPending},
	)
	tr.EnsureCurrentStep()

	obs := &recordingObserver{}
	tr.RegisterObserver(obs)

	tr.MarkCurrentStepInProgress()

	changes := obs.snapshot()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d (%+v)", len(changes), changes)
	}
	c := changes[0]
	if c.StepID != "s1" {
		t.Fatalf("StepID=%q, want s1", c.StepID)
	}
	if c.Actor != planItemActorMain {
		t.Fatalf("Actor=%q, want %q", c.Actor, planItemActorMain)
	}
	if c.PrevStatus != builtin_tools.PlanStepPending {
		t.Fatalf("PrevStatus=%q, want pending", c.PrevStatus)
	}
	if c.NextStatus != builtin_tools.PlanStepInProgress {
		t.Fatalf("NextStatus=%q, want in_progress", c.NextStatus)
	}
	if c.CurrentStepID != "s1" {
		t.Fatalf("CurrentStepID=%q, want s1", c.CurrentStepID)
	}
}

// TestStateObserver_MarkInlineInProgressEmitsPeerActor 验证 peer 路径的 actor 标签。
func TestStateObserver_MarkInlineInProgressEmitsPeerActor(t *testing.T) {
	tr := newTrackerWithPlan(t,
		&builtin_tools.PlanItem{ID: "p1", Step: "peer 1", Status: builtin_tools.PlanStepPending},
		&builtin_tools.PlanItem{ID: "p2", Step: "peer 2", Status: builtin_tools.PlanStepPending},
	)

	obs := &recordingObserver{}
	tr.RegisterObserver(obs)

	tr.MarkInlineStepInProgress("p2")

	changes := obs.snapshot()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if got := changes[0].Actor; got != planItemActorPeer {
		t.Fatalf("Actor=%q, want %q", got, planItemActorPeer)
	}
	if got := changes[0].StepID; got != "p2" {
		t.Fatalf("StepID=%q, want p2", got)
	}
}

// TestStateObserver_MarkInlineNonPendingIsNoop 已终态的 PlanItem 再次 Mark 不产 change。
func TestStateObserver_MarkInlineNonPendingIsNoop(t *testing.T) {
	tr := newTrackerWithPlan(t,
		&builtin_tools.PlanItem{ID: "x", Step: "x", Status: builtin_tools.PlanStepCompleted},
	)

	obs := &recordingObserver{}
	tr.RegisterObserver(obs)

	tr.MarkInlineStepInProgress("x")

	if got := len(obs.snapshot()); got != 0 {
		t.Fatalf("expected 0 changes for non-pending mark, got %d", got)
	}
}

// TestStateObserver_UpdateInlineStepEmitsTerminal 验证 peer 终态翻转 observer 看到。
func TestStateObserver_UpdateInlineStepEmitsTerminal(t *testing.T) {
	tr := newTrackerWithPlan(t,
		&builtin_tools.PlanItem{ID: "p1", Step: "peer", Status: builtin_tools.PlanStepPending},
	)
	tr.MarkInlineStepInProgress("p1")

	obs := &recordingObserver{}
	tr.RegisterObserver(obs)

	tr.UpdateInlineStep("p1", builtin_tools.CurrentStepUpdate{
		Status:  builtin_tools.PlanStepCompleted,
		Summary: "done",
	})

	changes := obs.snapshot()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d (%+v)", len(changes), changes)
	}
	c := changes[0]
	if c.NextStatus != builtin_tools.PlanStepCompleted {
		t.Fatalf("NextStatus=%q, want completed", c.NextStatus)
	}
	if c.Actor != planItemActorPeer {
		t.Fatalf("Actor=%q, want peer", c.Actor)
	}
	if c.Reason != "done" {
		t.Fatalf("Reason=%q, want done", c.Reason)
	}
}

// TestStateObserver_ConcurrentInlineMutationsNoLossNoDup 并发 mutator 不丢不重。
// 10 goroutine 各自 Mark 一个独立 stepID，observer 必须收到 10 条 InProgress 翻转，
// 每个 stepID 恰好一条。
func TestStateObserver_ConcurrentInlineMutationsNoLossNoDup(t *testing.T) {
	const N = 10
	items := make([]*builtin_tools.PlanItem, 0, N)
	for i := 0; i < N; i++ {
		items = append(items, &builtin_tools.PlanItem{
			ID:     stepIDFromIndex(i),
			Step:   "concurrent",
			Status: builtin_tools.PlanStepPending,
		})
	}
	tr := newTrackerWithPlan(t, items...)

	obs := &recordingObserver{}
	tr.RegisterObserver(obs)

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.MarkInlineStepInProgress(stepIDFromIndex(i))
		}()
	}
	wg.Wait()

	changes := obs.snapshot()
	if len(changes) != N {
		t.Fatalf("expected %d changes, got %d", N, len(changes))
	}
	seen := make(map[string]int, N)
	for _, c := range changes {
		seen[c.StepID]++
		if c.PrevStatus != builtin_tools.PlanStepPending {
			t.Errorf("step %s: PrevStatus=%q, want pending", c.StepID, c.PrevStatus)
		}
		if c.NextStatus != builtin_tools.PlanStepInProgress {
			t.Errorf("step %s: NextStatus=%q, want in_progress", c.StepID, c.NextStatus)
		}
		if c.Actor != planItemActorPeer {
			t.Errorf("step %s: Actor=%q, want peer", c.StepID, c.Actor)
		}
	}
	for i := 0; i < N; i++ {
		id := stepIDFromIndex(i)
		if got := seen[id]; got != 1 {
			t.Errorf("stepID %s observed %d times, want 1", id, got)
		}
	}
}

// TestStateObserver_RegisterIsThreadSafe 并发 RegisterObserver + 并发 mutator 不 race。
// 用 -race 跑能直接发现共享 t.observers 的未保护读写。
func TestStateObserver_RegisterIsThreadSafe(t *testing.T) {
	tr := newTrackerWithPlan(t,
		&builtin_tools.PlanItem{ID: "s1", Step: "s1", Status: builtin_tools.PlanStepPending},
	)

	var counter atomic.Int64
	observerCount := 16
	observers := make([]*counterObserver, observerCount)
	for i := range observers {
		observers[i] = &counterObserver{counter: &counter}
	}

	var wg sync.WaitGroup
	for i := range observers {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.RegisterObserver(observers[i])
		}()
	}
	wg.Wait()

	tr.MarkInlineStepInProgress("s1")

	if got := counter.Load(); got != int64(observerCount) {
		t.Fatalf("observer call count=%d, want %d", got, observerCount)
	}
}

// TestStateObserver_UpdatePlanEmitsSystemActor 全 plan 替换的 actor 标签 + 仅
// in_progress 新建条目被 reported 不影响 changelog（diff 维度全量产出）。
func TestStateObserver_UpdatePlanEmitsSystemActor(t *testing.T) {
	tr := NewStateTracker()

	obs := &recordingObserver{}
	tr.RegisterObserver(obs)

	tr.UpdatePlan([]*builtin_tools.PlanItem{
		{ID: "s1", Step: "a", Status: builtin_tools.PlanStepPending},
		{ID: "s2", Step: "b", Status: builtin_tools.PlanStepPending},
	}, "init", true)

	changes := obs.snapshot()
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes (two new items), got %d", len(changes))
	}
	for _, c := range changes {
		if c.Actor != planItemActorSystem {
			t.Errorf("Actor=%q, want system", c.Actor)
		}
		if c.Reason != "init" {
			t.Errorf("Reason=%q, want init", c.Reason)
		}
		if c.PrevStatus != "" {
			t.Errorf("PrevStatus=%q, want empty (new item)", c.PrevStatus)
		}
	}
}

type counterObserver struct {
	counter *atomic.Int64
}

func (c *counterObserver) OnPlanItemChange(_ PlanItemChange) {
	c.counter.Add(1)
}

func stepIDFromIndex(i int) string {
	return "step-" + string(rune('a'+i))
}
