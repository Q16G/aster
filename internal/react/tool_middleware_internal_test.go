package react

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeExecTool 是链测试用的最小 Tool：返回预置输出/错误，可注入延迟（测超时）。
type fakeExecTool struct {
	name  string
	out   string
	err   error
	delay time.Duration
}

func (t *fakeExecTool) Name() string        { return t.name }
func (t *fakeExecTool) Description() string { return "fake tool for middleware tests" }
func (t *fakeExecTool) Parameters() any     { return map[string]any{} }
func (t *fakeExecTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	if t.delay > 0 {
		select {
		case <-time.After(t.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return t.out, t.err
}

func newMiddlewareTestAgent(t *testing.T) *Agent {
	t.Helper()
	return &Agent{
		cfg:              &AgentConfig{},
		workspaceRootDir: t.TempDir(),
	}
}

func execCallFor(tool Tool, args map[string]any) *toolExecCall {
	if args == nil {
		args = map[string]any{}
	}
	return &toolExecCall{
		CallID:   "call-1",
		ToolName: tool.Name(),
		Tool:     tool,
		Args:     args,
	}
}

// TestChainToolMiddlewaresOrder 验证洋葱层序：pre 由外向内、post 由内向外。
func TestChainToolMiddlewaresOrder(t *testing.T) {
	var trace []string
	mw := func(tag string) toolMiddleware {
		return func(next toolExecHandler) toolExecHandler {
			return func(ctx context.Context, call *toolExecCall) (*toolExecResult, error) {
				trace = append(trace, "pre:"+tag)
				res, err := next(ctx, call)
				trace = append(trace, "post:"+tag)
				return res, err
			}
		}
	}
	base := func(ctx context.Context, call *toolExecCall) (*toolExecResult, error) {
		trace = append(trace, "base")
		return &toolExecResult{Out: "ok"}, nil
	}
	h := chainToolMiddlewares(base, mw("outer"), nil, mw("inner"))
	res, err := h(context.Background(), execCallFor(&fakeExecTool{name: "t"}, nil))
	if err != nil || res == nil || res.Out != "ok" {
		t.Fatalf("chain 执行失败: res=%+v err=%v", res, err)
	}
	want := "pre:outer|pre:inner|base|post:inner|post:outer"
	if got := strings.Join(trace, "|"); got != want {
		t.Errorf("层序 = %s, want %s（nil 中间件应跳过）", got, want)
	}
}

// TestBaseToolExecHandler 验证：正常输出、业务错误进 res.Err/ErrText 且链自身
// error 恒为 nil、超时被包装为 timed out 错误。
func TestBaseToolExecHandler(t *testing.T) {
	a := newMiddlewareTestAgent(t)

	res, err := a.baseToolExecHandler(context.Background(), execCallFor(&fakeExecTool{name: "ok", out: "result"}, nil))
	if err != nil || res.Out != "result" || res.Err != nil {
		t.Errorf("正常执行: res=%+v err=%v", res, err)
	}

	bizErr := fmt.Errorf("boom")
	res, err = a.baseToolExecHandler(context.Background(), execCallFor(&fakeExecTool{name: "bad", err: bizErr}, nil))
	if err != nil {
		t.Errorf("业务错误不应成为链故障: %v", err)
	}
	if res.Err == nil || res.ErrText != "boom" {
		t.Errorf("业务错误应进 res: %+v", res)
	}

	slow := &fakeExecTool{name: "slow", delay: 300 * time.Millisecond}
	res, err = a.baseToolExecHandler(context.Background(), execCallFor(slow, map[string]any{"timeout_ms": 30}))
	if err != nil {
		t.Fatalf("超时不应成为链故障: %v", err)
	}
	if res.Err == nil || !strings.Contains(res.ErrText, "timed out after") {
		t.Errorf("超时应包装 timed out 错误, got %+v", res)
	}
}

// TestBaseToolExecHandlerAgentSkipsTimeout 验证 isAgent 免超时的现状语义。
func TestBaseToolExecHandlerAgentSkipsTimeout(t *testing.T) {
	a := newMiddlewareTestAgent(t)
	slow := &fakeExecTool{name: "agent", out: "done", delay: 120 * time.Millisecond}
	call := execCallFor(slow, map[string]any{"timeout_ms": 10})
	call.IsAgent = true
	res, err := a.baseToolExecHandler(context.Background(), call)
	if err != nil || res.Err != nil || res.Out != "done" {
		t.Errorf("isAgent 应免超时正常完成, got res=%+v err=%v", res, err)
	}
}

// TestToolOutputTruncateMiddleware 验证默认截断中间件：超限输出截断 + 全量落盘
// 路径回填；用户中间件在外层，post 段看到的是截断后的最终内容。
func TestToolOutputTruncateMiddleware(t *testing.T) {
	a := newMiddlewareTestAgent(t)
	huge := strings.Repeat("x", 60*1024) // 超 50KB 上限
	tool := &fakeExecTool{name: "big", out: huge}

	var outerSawLen int
	outer := func(next toolExecHandler) toolExecHandler {
		return func(ctx context.Context, call *toolExecCall) (*toolExecResult, error) {
			res, err := next(ctx, call)
			if res != nil {
				outerSawLen = len(res.Out)
			}
			return res, err
		}
	}
	h := chainToolMiddlewares(a.baseToolExecHandler, outer, a.toolOutputTruncateMiddleware, toolExecDurationMiddleware)
	res, err := h(context.Background(), execCallFor(tool, nil))
	if err != nil {
		t.Fatalf("chain 执行失败: %v", err)
	}
	if !res.OutTruncated || res.OutFullPath == "" {
		t.Fatalf("超限输出应截断并落盘全量, got trunc=%v path=%q", res.OutTruncated, res.OutFullPath)
	}
	if len(res.Out) >= len(huge) {
		t.Errorf("截断后输出不应等长原文")
	}
	if outerSawLen != len(res.Out) {
		t.Errorf("外层用户中间件 post 段应看到截断后内容: saw=%d final=%d", outerSawLen, len(res.Out))
	}
	if res.Duration <= 0 {
		t.Errorf("Duration 应由耗时中间件填充")
	}
}

// TestBuildToolExecChainUserMiddleware 验证 WithToolMiddleware 注册序 = 由外向内，
// 且默认链（截断/耗时）在用户中间件之内。
func TestBuildToolExecChainUserMiddleware(t *testing.T) {
	a := newMiddlewareTestAgent(t)
	var mu sync.Mutex
	var trace []string
	tag := func(name string) toolMiddleware {
		return func(next toolExecHandler) toolExecHandler {
			return func(ctx context.Context, call *toolExecCall) (*toolExecResult, error) {
				mu.Lock()
				trace = append(trace, name)
				mu.Unlock()
				return next(ctx, call)
			}
		}
	}
	a.cfg.toolMiddlewares = []toolMiddleware{tag("u1"), tag("u2")}
	h := a.buildToolExecChain()
	if _, err := h(context.Background(), execCallFor(&fakeExecTool{name: "t", out: "ok"}, nil)); err != nil {
		t.Fatalf("chain 执行失败: %v", err)
	}
	if got := strings.Join(trace, "|"); got != "u1|u2" {
		t.Errorf("用户中间件注册序应由外向内, got %s", got)
	}
}

// TestToolExecChainConcurrent 验证链在并发 goroutine 下的数据隔离（配合 -race）。
func TestToolExecChainConcurrent(t *testing.T) {
	a := newMiddlewareTestAgent(t)
	var counter int64
	var mu sync.Mutex
	count := func(next toolExecHandler) toolExecHandler {
		return func(ctx context.Context, call *toolExecCall) (*toolExecResult, error) {
			mu.Lock()
			counter++
			mu.Unlock()
			return next(ctx, call)
		}
	}
	a.cfg.toolMiddlewares = []toolMiddleware{count}
	h := a.buildToolExecChain()

	const n = 32
	var wg sync.WaitGroup
	results := make([]*toolExecResult, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tool := &fakeExecTool{name: fmt.Sprintf("t%d", i), out: fmt.Sprintf("out-%d", i)}
			res, err := h(context.Background(), execCallFor(tool, nil))
			if err != nil {
				t.Errorf("goroutine %d chain 失败: %v", i, err)
				return
			}
			results[i] = res
		}(i)
	}
	wg.Wait()
	if counter != n {
		t.Errorf("计数中间件应恰好执行 %d 次, got %d", n, counter)
	}
	for i, res := range results {
		if res == nil || res.Out != fmt.Sprintf("out-%d", i) {
			t.Errorf("goroutine %d 结果串扰: %+v", i, res)
		}
	}
}
