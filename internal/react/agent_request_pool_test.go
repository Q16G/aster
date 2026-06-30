package react

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"aster/internal/ai"
)

func TestAgentRequestPool_Capacity(t *testing.T) {
	if got := newAgentRequestPool(5).Capacity(); got != 5 {
		t.Fatalf("expected cap=5, got %d", got)
	}
	if got := newAgentRequestPool(0).Capacity(); got != 1 {
		t.Fatalf("cap=0 must floor to 1, got %d", got)
	}
	if got := newAgentRequestPool(-3).Capacity(); got != 1 {
		t.Fatalf("cap<0 must floor to 1, got %d", got)
	}
	var nilPool *AgentRequestPool
	if got := nilPool.Capacity(); got != 0 {
		t.Fatalf("nil pool Capacity must be 0, got %d", got)
	}
}

func TestAgentRequestPool_AcquireRespectsCtxCancel(t *testing.T) {
	p := newAgentRequestPool(1)
	if err := p.Acquire(context.Background()); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Acquire(ctx) }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("second acquire must return ctx.Err() after cancel")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second acquire did not unblock after cancel")
	}
	p.Release()
}

func TestAgentRequestPool_NilPoolPassThrough(t *testing.T) {
	var p *AgentRequestPool
	if err := p.Acquire(context.Background()); err != nil {
		t.Fatalf("nil pool Acquire must return nil err, got %v", err)
	}
	// nil pool Release 不 panic
	p.Release()
}

func TestAgentRequestPool_ImplementsRequestLimiter(t *testing.T) {
	var _ ai.RequestLimiter = (*AgentRequestPool)(nil)
}

// TestAIRequestPool_CapsAcrossConcurrentAICalls 端到端级联回归：
// pool cap=3，5 个 goroutine 各自把 limiter 注入 ctx 后调 ai.ChatExWithOptions；
// mock client 内 atomic 计数 + CAS 维护峰值；断言 outbound peak ≤ 3。
//
// 这条用例验证「无论同时有多少个 inline peer / 子 Agent 在跑，所有 outbound AI 请求
// 共享 factory 全局 pool，HTTP 请求总并发 ≤ MaxParallelSteps」。
func TestAIRequestPool_CapsAcrossConcurrentAICalls(t *testing.T) {
	pool := newAgentRequestPool(3)

	current := atomic.Int32{}
	peak := atomic.Int32{}
	calls := atomic.Int32{}

	cli := &poolPeakChatClient{
		delay:   30 * time.Millisecond,
		current: &current,
		peak:    &peak,
		calls:   &calls,
	}

	ctx := ai.WithRequestLimiter(context.Background(), pool)
	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ai.ChatExWithOptions(ctx, cli, nil, nil)
		}()
	}
	wg.Wait()

	if got := peak.Load(); got > 3 {
		t.Fatalf("AI request peak=%d exceeds pool cap=3", got)
	}
	if got := peak.Load(); got < 2 {
		t.Fatalf("expected concurrent peak>=2 to confirm parallelism, got %d", got)
	}
	if got := calls.Load(); int(got) != n {
		t.Fatalf("expected %d AI calls, got %d", n, got)
	}
}

// TestAIRequestPool_NoSlotLeakOnCtxCancel：pool cap=1，第 1 路占槽且 mock client
// 内 sleep 较长；第 2 路 Acquire 被阻塞；cancel 父 ctx → 第 2 路返回 err；
// 等第 1 路自然完成后断言 pool 槽位归 0（无泄漏）。
func TestAIRequestPool_NoSlotLeakOnCtxCancel(t *testing.T) {
	pool := newAgentRequestPool(1)
	current := atomic.Int32{}
	peak := atomic.Int32{}
	calls := atomic.Int32{}
	cli := &poolPeakChatClient{
		delay:   200 * time.Millisecond,
		current: &current,
		peak:    &peak,
		calls:   &calls,
	}

	parentCtx := ai.WithRequestLimiter(context.Background(), pool)

	// 第 1 路占槽并执行
	firstDone := make(chan struct{})
	go func() {
		_, _ = ai.ChatExWithOptions(parentCtx, cli, nil, nil)
		close(firstDone)
	}()
	time.Sleep(10 * time.Millisecond)

	// 第 2 路 Acquire 被阻塞；cancel 后立刻返回 err
	cancelCtx, cancel := context.WithCancel(parentCtx)
	secondDone := make(chan error, 1)
	go func() {
		_, err := ai.ChatExWithOptions(cancelCtx, cli, nil, nil)
		secondDone <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-secondDone:
		if err == nil {
			t.Fatal("second call must return ctx.Err() after cancel")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second call did not unblock after ctx cancel")
	}
	<-firstDone

	// 槽位应归 0
	if got := pool.Capacity() - len(pool.sem); got != pool.Capacity() {
		t.Fatalf("pool slot leak detected: %d/%d slots in use after both finish", len(pool.sem), pool.Capacity())
	}
}

// poolPeakChatClient 是测试用 ai.ChatClient：累计当前持槽与峰值。
type poolPeakChatClient struct {
	delay   time.Duration
	current *atomic.Int32
	peak    *atomic.Int32
	calls   *atomic.Int32
}

func (c *poolPeakChatClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}
func (c *poolPeakChatClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}
func (c *poolPeakChatClient) ChatEx(ctx context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.calls.Add(1)
	cur := c.current.Add(1)
	defer c.current.Add(-1)
	for {
		p := c.peak.Load()
		if cur <= p || c.peak.CompareAndSwap(p, cur) {
			break
		}
	}
	select {
	case <-time.After(c.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []*ai.ChatChoices{{Index: 0, Message: ai.NewAIMsgInfo("ok")}}, nil
}
