package react

import (
	"context"

	"aster/internal/ai"
)

// AgentRequestPool 实现 ai.RequestLimiter：单 AgentFactory 范围内所有 Agent 实例
// （root + 所有 sub_agent / skill fork child）共享同一信号量限流 outbound AI 请求。
//
// **为什么是 factory 持有**：所有 Agent 经 AgentFactory.Build 构造，pool 随 factory
// 走是进程内天然全局，零额外贯穿。容量在 NewAgentFactory 时按 maxParallelSteps 一次性
// 确定，pool 生命周期与 factory 绑定。
//
// **限流粒度**：每个 outbound AI 请求经 ai.ChatExWithOptions / ChatStreamWithOptions
// 时占 1 槽；history compaction 的嵌套调用通过 ctx 的 hold 标记直通不重 Acquire。
type AgentRequestPool struct {
	sem chan struct{}
}

var _ ai.RequestLimiter = (*AgentRequestPool)(nil)

func newAgentRequestPool(capacity int) *AgentRequestPool {
	if capacity < 1 {
		capacity = 1
	}
	return &AgentRequestPool{sem: make(chan struct{}, capacity)}
}

// Capacity 返回 pool 容量；测试与日志可读。
func (p *AgentRequestPool) Capacity() int {
	if p == nil || p.sem == nil {
		return 0
	}
	return cap(p.sem)
}

// Acquire 阻塞获取一个并发槽位，支持 ctx 取消。
// 返回非 nil 时**绝对不能** defer Release——会消费别的 goroutine 在等的槽。
// nil pool 直通：兼容未经 factory.Build 构造的 Agent（主要是单测）。
func (p *AgentRequestPool) Acquire(ctx context.Context) error {
	if p == nil || p.sem == nil {
		return nil
	}
	select {
	case p.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release 归还槽位；**只能在 Acquire 成功后调用**。
func (p *AgentRequestPool) Release() {
	if p == nil || p.sem == nil {
		return
	}
	<-p.sem
}
