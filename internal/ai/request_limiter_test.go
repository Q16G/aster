package ai

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingLimiter 是测试用 limiter：记录 Acquire 次数、当前持槽数、峰值。
type countingLimiter struct {
	sem        chan struct{}
	acquires   atomic.Int32
	current    atomic.Int32
	peak       atomic.Int32
	releaseCnt atomic.Int32
}

func newCountingLimiter(capacity int) *countingLimiter {
	if capacity < 1 {
		capacity = 1
	}
	return &countingLimiter{sem: make(chan struct{}, capacity)}
}

func (l *countingLimiter) Acquire(ctx context.Context) error {
	select {
	case l.sem <- struct{}{}:
		l.acquires.Add(1)
		cur := l.current.Add(1)
		for {
			p := l.peak.Load()
			if cur <= p || l.peak.CompareAndSwap(p, cur) {
				break
			}
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *countingLimiter) Release() {
	l.current.Add(-1)
	l.releaseCnt.Add(1)
	<-l.sem
}

// blockingClient 模拟一个真实 ChatClient：Execute 内 sleep delay，期间累计观察峰值。
type blockingClient struct {
	delay   time.Duration
	current atomic.Int32
	peak    atomic.Int32
	calls   atomic.Int32
}

func (c *blockingClient) Chat(ctx context.Context, info *MsgInfo, tools ...*FunctionTool) (string, error) {
	return "", nil
}
func (c *blockingClient) ChatText(ctx context.Context, text string, tools ...*FunctionTool) (string, error) {
	return "", nil
}
func (c *blockingClient) ChatEx(ctx context.Context, infos []*MsgInfo, tools ...*FunctionTool) ([]*ChatChoices, error) {
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
	return []*ChatChoices{{Index: 0, Message: NewAIMsgInfo("ok")}}, nil
}

// nestedClient 在 ChatEx 内再调一次 ai.ChatExWithOptions，模拟 history compaction 嵌套。
type nestedClient struct {
	inner ChatClient
	calls atomic.Int32
}

func (c *nestedClient) Chat(ctx context.Context, info *MsgInfo, tools ...*FunctionTool) (string, error) {
	return "", nil
}
func (c *nestedClient) ChatText(ctx context.Context, text string, tools ...*FunctionTool) (string, error) {
	return "", nil
}
func (c *nestedClient) ChatEx(ctx context.Context, infos []*MsgInfo, tools ...*FunctionTool) ([]*ChatChoices, error) {
	c.calls.Add(1)
	// 嵌套调用：limiter 已持槽，应直通不重 Acquire。
	if _, err := ChatExWithOptions(ctx, c.inner, infos, nil); err != nil {
		return nil, err
	}
	return []*ChatChoices{{Index: 0, Message: NewAIMsgInfo("nested")}}, nil
}

func TestWithRequestLimiter_AcquireBlocksAtCap(t *testing.T) {
	lim := newCountingLimiter(2)
	ctx := context.Background()
	// 先占满 2 槽
	if err := lim.Acquire(ctx); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := lim.Acquire(ctx); err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	// 第 3 路应阻塞 → 用短超时观察
	done := make(chan error, 1)
	tightCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	go func() { done <- lim.Acquire(tightCtx) }()
	if err := <-done; err == nil {
		t.Fatal("third acquire must block past timeout")
	}
	lim.Release()
	lim.Release()
}

func TestWithRequestLimiter_ChatExHonorsCap(t *testing.T) {
	lim := newCountingLimiter(2)
	cli := &blockingClient{delay: 40 * time.Millisecond}
	ctx := WithRequestLimiter(context.Background(), lim)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ChatExWithOptions(ctx, cli, nil, nil)
		}()
	}
	wg.Wait()
	if got := cli.peak.Load(); got > 2 {
		t.Fatalf("ChatEx concurrent peak=%d exceeds limiter cap=2", got)
	}
	if got := cli.peak.Load(); got < 2 {
		t.Fatalf("expected peak>=2 to confirm concurrency, got %d", got)
	}
	if got := cli.calls.Load(); got != 5 {
		t.Fatalf("expected 5 calls, got %d", got)
	}
}

func TestWithRequestLimiter_ChatStreamHonorsCap(t *testing.T) {
	lim := newCountingLimiter(1)
	cli := &streamingBlockingClient{delay: 20 * time.Millisecond}
	ctx := WithRequestLimiter(context.Background(), lim)
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ChatStreamWithOptions(ctx, cli, nil, nil, func(*StreamDelta, bool) error { return nil })
		}()
	}
	wg.Wait()
	if got := cli.peak.Load(); got > 1 {
		t.Fatalf("ChatStream peak=%d exceeds cap=1", got)
	}
	if got := cli.calls.Load(); got != 3 {
		t.Fatalf("expected 3 stream calls, got %d", got)
	}
}

func TestWithRequestLimiter_ReentrancyByHoldMark(t *testing.T) {
	lim := newCountingLimiter(1)
	inner := &blockingClient{delay: 5 * time.Millisecond}
	outer := &nestedClient{inner: inner}
	ctx := WithRequestLimiter(context.Background(), lim)

	_, err := ChatExWithOptions(ctx, outer, nil, nil)
	if err != nil {
		t.Fatalf("nested ChatEx: %v", err)
	}
	// 外层 Acquire 1 次；嵌套调用应直通不重 Acquire。
	if got := lim.acquires.Load(); got != 1 {
		t.Fatalf("expected exactly 1 acquire (hold mark prevents re-entry), got %d", got)
	}
	if got := lim.releaseCnt.Load(); got != 1 {
		t.Fatalf("expected exactly 1 release, got %d", got)
	}
	if got := outer.calls.Load(); got != 1 {
		t.Fatalf("expected outer 1 call, got %d", got)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("expected inner 1 call, got %d", got)
	}
}

func TestWithRequestLimiter_CtxCancelReleasesAcquire(t *testing.T) {
	lim := newCountingLimiter(1)
	cli := &blockingClient{delay: 200 * time.Millisecond}
	parentCtx := WithRequestLimiter(context.Background(), lim)

	// 第 1 路占槽并持续执行
	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		close(started)
		_, _ = ChatExWithOptions(parentCtx, cli, nil, nil)
		close(finished)
	}()
	<-started
	time.Sleep(10 * time.Millisecond)

	// 第 2 路 Acquire 被阻塞；cancel ctx 应立即返回 err
	cancelCtx, cancel := context.WithCancel(parentCtx)
	done := make(chan error, 1)
	go func() {
		_, err := ChatExWithOptions(cancelCtx, cli, nil, nil)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("second ChatExWithOptions must return ctx.Err() after cancel")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second ChatExWithOptions did not unblock after ctx cancel")
	}
	<-finished
	// pool 应回归 0
	if got := lim.current.Load(); got != 0 {
		t.Fatalf("pool current=%d after both finish, expected 0", got)
	}
}

func TestWithRequestLimiter_NilLimiterPassThrough(t *testing.T) {
	cli := &blockingClient{delay: 1 * time.Millisecond}
	// 不注入 limiter
	_, err := ChatExWithOptions(context.Background(), cli, nil, nil)
	if err != nil {
		t.Fatalf("nil limiter passthrough: %v", err)
	}
	if got := cli.calls.Load(); got != 1 {
		t.Fatalf("expected 1 call, got %d", got)
	}
}

// streamingBlockingClient 在 blockingClient 基础上多一个 ChatStream 方法。
type streamingBlockingClient struct {
	delay   time.Duration
	current atomic.Int32
	peak    atomic.Int32
	calls   atomic.Int32
}

func (c *streamingBlockingClient) Chat(ctx context.Context, info *MsgInfo, tools ...*FunctionTool) (string, error) {
	return "", nil
}
func (c *streamingBlockingClient) ChatText(ctx context.Context, text string, tools ...*FunctionTool) (string, error) {
	return "", nil
}
func (c *streamingBlockingClient) ChatEx(ctx context.Context, infos []*MsgInfo, tools ...*FunctionTool) ([]*ChatChoices, error) {
	return nil, nil
}
func (c *streamingBlockingClient) ChatStream(ctx context.Context, infos []*MsgInfo, handler StreamHandler, tools ...*FunctionTool) error {
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
		return ctx.Err()
	}
	return nil
}
