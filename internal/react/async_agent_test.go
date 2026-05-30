package react

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"aster/internal/builtin_tools"
)

func TestAsyncAgentRegistry_WaitForCompletion_NoRunningReturnsImmediately(t *testing.T) {
	r := NewAsyncAgentRegistry()
	start := time.Now()
	r.WaitForCompletion(context.Background())
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("WaitForCompletion should return immediately with no running agents, took %s", elapsed)
	}
}

func TestAsyncAgentRegistry_WaitForCompletion_WakesOnComplete(t *testing.T) {
	r := NewAsyncAgentRegistry()
	r.Register("bg", "task", "/tmp/ws")

	go func() {
		time.Sleep(50 * time.Millisecond)
		r.Complete("bg", &builtin_tools.RunResult{Success: true, Result: "done"})
	}()

	// Backstop ctx so a regression cannot hang the test forever (no timer in prod path).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	r.WaitForCompletion(ctx)
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond {
		t.Fatalf("WaitForCompletion returned too early (%s), should wait for completion", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("WaitForCompletion did not wake on completion, took %s", elapsed)
	}
}

func TestAsyncAgentRegistry_WaitForCompletion_CtxCancelBranch(t *testing.T) {
	r := NewAsyncAgentRegistry()
	r.Register("bg", "task", "/tmp/ws") // never completes

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	r.WaitForCompletion(ctx)
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("WaitForCompletion did not wake on ctx cancel, took %s", elapsed)
	}
}

func TestAsyncAgentRegistry_WaitForCompletion_OneKickForManyCompletions(t *testing.T) {
	r := NewAsyncAgentRegistry()
	for i := 0; i < 5; i++ {
		r.Register(fmt.Sprintf("bg-%d", i), "task", "/tmp/ws")
	}

	// Complete several agents before anyone waits; the coalescing cap=1 channel
	// holds a single token, which is enough to wake one parked waiter.
	for i := 0; i < 5; i++ {
		r.Complete(fmt.Sprintf("bg-%d", i), &builtin_tools.RunResult{Success: true})
	}
	// drain notifications so they don't fill up (not strictly required here)
	for i := 0; i < 5; i++ {
		select {
		case <-r.notifications:
		default:
		}
	}

	start := time.Now()
	r.WaitForCompletion(context.Background())
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		// HasRunning is false now (all completed), so it should return immediately.
		t.Fatalf("WaitForCompletion should return promptly when nothing is running, took %s", elapsed)
	}
}

func TestAsyncAgentRegistry_ResetDrainsCompletedToken(t *testing.T) {
	r := NewAsyncAgentRegistry()
	r.Register("bg", "task", "/tmp/ws")
	r.Complete("bg", &builtin_tools.RunResult{Success: true})
	// completed token is now buffered

	r.Reset()

	select {
	case <-r.completed:
		t.Fatal("completed channel should be drained after Reset")
	default:
	}
}

func TestAsyncAgentRegistry_PurgeDelivered(t *testing.T) {
	r := NewAsyncAgentRegistry()

	r.Register("a1", "task1", "/tmp/ws1")
	r.Register("a2", "task2", "/tmp/ws2")
	r.Register("a3", "task3", "/tmp/ws3")

	r.Complete("a1", &builtin_tools.RunResult{Success: true, Result: "done1"})
	r.Complete("a2", &builtin_tools.RunResult{Success: false, Error: "fail2"})

	// drain notifications so they don't block
	<-r.notifications
	<-r.notifications

	r.MarkDelivered("a1")
	// a2 is completed but NOT delivered

	purged := r.PurgeDelivered()
	if purged != 1 {
		t.Fatalf("expected 1 purged, got %d", purged)
	}

	if r.Get("a1") != nil {
		t.Fatal("a1 should have been purged")
	}
	if r.Get("a2") == nil {
		t.Fatal("a2 should NOT be purged (not delivered)")
	}
	if r.Get("a3") == nil {
		t.Fatal("a3 should NOT be purged (still running)")
	}
}

func TestAsyncAgentRegistry_PurgeKeepsRunning(t *testing.T) {
	r := NewAsyncAgentRegistry()

	r.Register("r1", "running task", "/tmp/ws")
	r.Register("r2", "another running", "/tmp/ws2")

	purged := r.PurgeDelivered()
	if purged != 0 {
		t.Fatalf("expected 0 purged for running agents, got %d", purged)
	}
	if !r.HasRunning() {
		t.Fatal("should still have running agents")
	}
}

func TestAsyncAgentRegistry_Reset(t *testing.T) {
	r := NewAsyncAgentRegistry()

	r.Register("x1", "task", "/tmp/ws1")
	r.Register("x2", "task", "/tmp/ws2")
	r.Complete("x1", &builtin_tools.RunResult{Success: true, Result: "ok"})
	// notification is in channel

	r.Reset()

	if r.Get("x1") != nil {
		t.Fatal("x1 should be gone after Reset")
	}
	if r.Get("x2") != nil {
		t.Fatal("x2 should be gone after Reset")
	}
	if r.HasRunning() {
		t.Fatal("no agents should remain after Reset")
	}

	// channel should be drained
	select {
	case <-r.notifications:
		t.Fatal("channel should be empty after Reset")
	default:
	}
}

func TestAsyncAgentRegistry_PurgeSkipsDroppedNotification(t *testing.T) {
	r := NewAsyncAgentRegistry()

	// fill the notification channel to capacity (buffer=64)
	for i := 0; i < 64; i++ {
		id := fmt.Sprintf("fill-%d", i)
		r.Register(id, "filler", "/tmp")
		r.Complete(id, &builtin_tools.RunResult{Success: true})
	}

	// now register + complete an agent whose notification will be dropped
	r.Register("dropped", "task", "/tmp/ws")
	r.Complete("dropped", &builtin_tools.RunResult{Success: true})
	// notification dropped because channel is full
	// entry is closed=true, but delivered=false (MarkDelivered never called)

	purged := r.PurgeDelivered()
	if purged != 0 {
		t.Fatalf("expected 0 purged (none delivered), got %d", purged)
	}
	if r.Get("dropped") == nil {
		t.Fatal("dropped-notification entry should survive PurgeDelivered")
	}

	// Reset should still clean it up
	r.Reset()
	if r.Get("dropped") != nil {
		t.Fatal("dropped-notification entry should be gone after Reset")
	}
}

func TestAsyncAgentRegistry_ResetClearsRunning(t *testing.T) {
	r := NewAsyncAgentRegistry()
	r.Register("r1", "task1", "/tmp/ws1")
	r.Register("r2", "task2", "/tmp/ws2")

	if !r.HasRunning() {
		t.Fatal("should have running agents before Reset")
	}

	r.Reset()

	if r.HasRunning() {
		t.Fatal("should have no running agents after Reset")
	}
	if r.Get("r1") != nil || r.Get("r2") != nil {
		t.Fatal("running entries should be gone after Reset")
	}
}

func TestAsyncAgentRegistry_ResetThenRegister(t *testing.T) {
	r := NewAsyncAgentRegistry()
	r.Register("old", "old task", "/tmp/old")
	r.Complete("old", &builtin_tools.RunResult{Success: true})

	r.Reset()

	// registry should still work after reset
	r.Register("new", "new task", "/tmp/new")
	if r.Get("new") == nil {
		t.Fatal("should be able to register after Reset")
	}
	if !r.HasRunning() {
		t.Fatal("new agent should be running")
	}

	r.Complete("new", &builtin_tools.RunResult{Success: true, Result: "done"})
	<-r.notifications
	r.MarkDelivered("new")

	purged := r.PurgeDelivered()
	if purged != 1 {
		t.Fatalf("expected 1 purged after re-register cycle, got %d", purged)
	}
}

func TestAsyncAgentRegistry_DrainSkipsStaleNotification(t *testing.T) {
	r := NewAsyncAgentRegistry()
	r.Register("stale", "old task", "/tmp/old")
	r.Complete("stale", &builtin_tools.RunResult{Success: true, Result: "old result"})
	// notification is in channel

	// simulate turn boundary: Reset clears map but notification stays in channel
	// (race window: Complete sent notification after Reset's drain finished)
	r.Reset()

	// re-inject the stale notification to simulate the race
	r.notifications <- &AsyncAgentNotification{
		AgentID: "stale",
		Status:  "completed",
		Result:  &builtin_tools.RunResult{Success: true, Result: "stale"},
	}

	// build a minimal Agent to call drainAsyncAgentNotifications
	agent := &Agent{asyncRegistry: r}
	agent.drainAsyncAgentNotifications()

	// stepHistory should be empty — stale notification should be skipped
	if len(agent.stepHistory) != 0 {
		t.Fatalf("expected 0 stepHistory entries for stale notification, got %d", len(agent.stepHistory))
	}
}

func TestAsyncAgentRegistry_ConcurrentCompleteAndPurge(t *testing.T) {
	r := NewAsyncAgentRegistry()
	const n = 100

	for i := 0; i < n; i++ {
		r.Register(fmt.Sprintf("agent-%d", i), "task", "/tmp")
	}

	var wg sync.WaitGroup

	// goroutines completing agents concurrently
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			r.Complete(fmt.Sprintf("agent-%d", id), &builtin_tools.RunResult{Success: true})
		}(i)
	}
	wg.Wait()

	// drain all notifications and mark delivered
	drained := 0
drain:
	for {
		select {
		case notif := <-r.notifications:
			r.MarkDelivered(notif.AgentID)
			drained++
		default:
			break drain
		}
	}

	// some notifications may have been dropped if channel was full
	// purge should only remove delivered ones
	purged := r.PurgeDelivered()
	if purged != drained {
		t.Fatalf("expected %d purged (= drained), got %d", drained, purged)
	}

	// remaining entries (dropped notifications) should be cleaned by Reset
	r.Reset()
	if r.HasRunning() {
		t.Fatal("should have no agents after Reset")
	}
}
