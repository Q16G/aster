package react

import (
	"context"
	"sync"
	"testing"

	"aster/internal/builtin_tools"
)

// frozenStepPromptCache 红线（P0 修复）：旧 a.frozenStepParts 单字段在多 peer 并发
// 派生 prompt 时存在 race + 缓存抖动。新设计 map[stepID+planVer] + RWMutex 后：
//   - 多 peer 同时 Get/Put 不再 race（-race 验证）
//   - peer A 和 peer B 不同 stepID 各自独立 entry，互不覆盖

// TestFrozenStepPromptCache_BasicGetPut 单写单读基本路径。
func TestFrozenStepPromptCache_BasicGetPut(t *testing.T) {
	c := newFrozenStepPromptCache()

	if _, ok := c.Get("s1", 1); ok {
		t.Error("empty cache Get should return ok=false")
	}

	parts := PromptParts{User: "u1"}
	c.Put("s1", 1, parts)

	got, ok := c.Get("s1", 1)
	if !ok || got == nil || got.User != "u1" {
		t.Errorf("Get after Put failed; ok=%v got=%v", ok, got)
	}

	// 不同 stepID 不同 planVer 不命中
	if _, ok := c.Get("s2", 1); ok {
		t.Error("different stepID should not hit")
	}
	if _, ok := c.Get("s1", 2); ok {
		t.Error("different planVer should not hit")
	}
}

// TestFrozenStepPromptCache_EmptyStepIDNoop stepID 空时 Get/Put 都是 no-op。
func TestFrozenStepPromptCache_EmptyStepIDNoop(t *testing.T) {
	c := newFrozenStepPromptCache()
	c.Put("", 1, PromptParts{User: "x"})
	c.Put("  ", 2, PromptParts{User: "y"})

	if c.Len() != 0 {
		t.Errorf("empty stepID Put should be no-op; got len=%d", c.Len())
	}
	if _, ok := c.Get("", 1); ok {
		t.Error("empty stepID Get should return ok=false")
	}
}

// TestFrozenStepPromptCache_Reset Reset 清空所有 entries。
func TestFrozenStepPromptCache_Reset(t *testing.T) {
	c := newFrozenStepPromptCache()
	c.Put("s1", 1, PromptParts{User: "a"})
	c.Put("s2", 1, PromptParts{User: "b"})
	if c.Len() != 2 {
		t.Fatalf("len before reset=%d", c.Len())
	}
	c.Reset()
	if c.Len() != 0 {
		t.Errorf("Reset should clear; len=%d", c.Len())
	}
	if _, ok := c.Get("s1", 1); ok {
		t.Error("Reset should drop s1 entry")
	}
}

// TestFrozenStepPromptCache_ConcurrentPutGet **P0 race 红线**：
// 多 goroutine 并发 Get/Put 不同 (stepID, planVer)，用 -race 跑必须不爆。
// 同时验证不同 peer 各自 entry 不串扰。
//
//	跑：go test -race -run TestFrozenStepPromptCache_ConcurrentPutGet ./internal/react/
func TestFrozenStepPromptCache_ConcurrentPutGet(t *testing.T) {
	c := newFrozenStepPromptCache()

	const goroutines = 10
	const iters = 200
	var wg sync.WaitGroup
	stepIDs := []string{"s1", "s2", "s3", "s4", "s5"}

	for g := 0; g < goroutines; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				stepID := stepIDs[g%len(stepIDs)]
				planVer := i % 3
				c.Put(stepID, planVer, PromptParts{User: stepID + "-" + itoa(planVer)})
				_, _ = c.Get(stepID, planVer)
			}
		}()
	}
	wg.Wait()

	// 抽样验证：写入过的 (stepID, planVer) 应该命中
	got, ok := c.Get("s1", 0)
	if !ok || got == nil {
		t.Errorf("expected hit on s1/0 after concurrent Put")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestThinkActPartsForStep_ConcurrentPeersIndependentCache **P0 集成红线**：
// 主路径 + 多 peer 同时进 thinkActPartsForStep，各自 cache 独立。
// 跑：go test -race -run TestThinkActPartsForStep_Concurrent... ./internal/react/
func TestThinkActPartsForStep_ConcurrentPeersIndependentCache(t *testing.T) {
	agent := newPeerSkillTestAgent(t)
	// 设 plan 让不同 stepID 都能解析
	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:       builtin_tools.AgentPhaseStep,
		Status:      builtin_tools.TaskStatusRunning,
		PlanVersion: 1,
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "main", Status: builtin_tools.PlanStepInProgress},
			{ID: "s2", Step: "peer 2", Status: builtin_tools.PlanStepInProgress},
			{ID: "s3", Step: "peer 3", Status: builtin_tools.PlanStepInProgress},
			{ID: "s4", Step: "peer 4", Status: builtin_tools.PlanStepInProgress},
		},
	})

	stepIDs := []string{"s1", "s2", "s3", "s4"}
	var wg sync.WaitGroup
	for _, sid := range stepIDs {
		sid := sid
		wg.Add(1)
		go func() {
			defer wg.Done()
			snap := agent.state.Snapshot()
			snap.CurrentStepID = sid
			// 跑 3 次（模拟 peer 多 iter）—— 第 2/3 次应该命中 cache
			for i := 0; i < 3; i++ {
				_ = agent.thinkActPartsForStep(context.Background(), "", snap)
			}
		}()
	}
	wg.Wait()

	// 4 个 stepID 各自一 entry
	if got := agent.frozenStepCache.Len(); got != 4 {
		t.Errorf("expected 4 cache entries (one per stepID); got %d", got)
	}
}
