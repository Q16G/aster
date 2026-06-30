package ai

import "context"

// RequestLimiter 限流单 Agent 实例族（同一 AgentFactory 范围内 root + 所有子 Agent）
// 的 outbound AI 请求并发。Acquire 阻塞获取槽位，支持 ctx 取消；Release 归还槽位。
//
// 实现由调用方提供（react.AgentFactory 持有 *AgentRequestPool 实现本接口）。
// ai 包不依赖具体实现，仅提供 ctx 注入/取出与统一入口处的 Acquire/Release 钩子。
type RequestLimiter interface {
	Acquire(ctx context.Context) error
	Release()
}

type requestLimiterCtxKey struct{}
type requestLimiterHoldKey struct{}

// WithRequestLimiter 把 limiter 挂进 ctx。ChatExWithOptions / ChatStreamWithOptions
// 自动从 ctx 取出并 Acquire/Release。调用方（react.AICallProxy /
// runStructuredOutputWithRetry）负责注入；nil limiter 直通不动 ctx。
func WithRequestLimiter(ctx context.Context, limiter RequestLimiter) context.Context {
	if ctx == nil || limiter == nil {
		return ctx
	}
	return context.WithValue(ctx, requestLimiterCtxKey{}, limiter)
}

// requestLimiterFromCtx 取出 limiter；未注入则返回 nil。
func requestLimiterFromCtx(ctx context.Context) RequestLimiter {
	if ctx == nil {
		return nil
	}
	v, _ := ctx.Value(requestLimiterCtxKey{}).(RequestLimiter)
	return v
}

// withLimiterHold 标记 ctx 已持有 limiter 槽（防嵌套调用重入死锁）。
// ChatExWithOptions / ChatStreamWithOptions 在 Acquire 成功后写入标记，并把带标记的
// ctx 传给底层 client；底层路径若回调到 ChatExWithOptions（典型是 history compaction
// 在 AICallProxy 内调 ai.ChatExWithOptions），limiter 检查到 hold 直通不重 Acquire。
func withLimiterHold(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestLimiterHoldKey{}, struct{}{})
}

// limiterHeld 检查 ctx 是否已持槽。
func limiterHeld(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Value(requestLimiterHoldKey{}).(struct{})
	return ok
}
