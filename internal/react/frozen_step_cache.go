package react

import (
	"strings"
	"sync"
)

// frozenStepPromptCache 是「按 (stepID, planVer) 缓存 think_act 入口 prompt」的并发
// 安全数据结构。
//
// **存在原因**：同一 (stepID, planVer) 的 think_act prompt 在 step 内字节恒定（首条
// user message 冻结，让消息前缀的移动缓存断点全程命中——见 [[project_prompt_split_plan]]
// 与 commit dacc4251 系列）。
//
// **为什么是 map 而非单字段**：多 peer 并发场景下，主路径 (current=s1) + peer s2/s3
// 同时调 thinkActPartsForStep。旧版 `frozenStepParts *PromptParts` 单字段无锁，存在：
//   1. **race**：goroutine 同时读写指针 → -race 必爆
//   2. **抖动**：peer A 写 s2 cache 把 peer B 的 s3 entry 顶掉 → 下次进 cache miss + rebuild
//      → 模型 API 缓存断点失效，prompt 缓存命中率归零
//
// **设计哲学一致性**：延续「stepID 一等公民」原则——cache 按 (stepID, planVer) 复合
// 键分桶，每个 peer 独立 cache entry 互不串扰；与 streamKey / thinkBufKey 同款模式。
//
// **锁粒度**：sync.RWMutex 而非 Mutex——Get 是热路径（每 think_act 入口都调），
// 多 reader 不阻塞；Put 只在新 (stepID, planVer) 出现时写一次。
type frozenStepPromptCache struct {
	mu      sync.RWMutex
	entries map[frozenStepKey]*PromptParts
}

type frozenStepKey struct {
	stepID  string
	planVer int
}

func newFrozenStepPromptCache() *frozenStepPromptCache {
	return &frozenStepPromptCache{
		entries: make(map[frozenStepKey]*PromptParts),
	}
}

// Get 返回缓存的 prompt。stepID 为空或未命中时返回 (nil, false)。
// 调用方拿到的是 *PromptParts，**只读**——不可 mutate（其他 goroutine 同时持有）。
func (c *frozenStepPromptCache) Get(stepID string, planVer int) (*PromptParts, bool) {
	if c == nil {
		return nil, false
	}
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.entries[frozenStepKey{stepID: stepID, planVer: planVer}]
	return p, ok
}

// Put 写入 (stepID, planVer) → parts 缓存。stepID 空时 no-op。
// 复制 parts 值后取地址——entries 持有的是独立拷贝，调用方继续 mutate 原 parts 不影响 cache。
func (c *frozenStepPromptCache) Put(stepID string, planVer int, parts PromptParts) {
	if c == nil {
		return
	}
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return
	}
	frozen := parts
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[frozenStepKey]*PromptParts)
	}
	c.entries[frozenStepKey{stepID: stepID, planVer: planVer}] = &frozen
}

// Reset 清空所有 entries。用于 plan 切换 / 新 turn 等需要彻底失效缓存的场景
// （agent_execute.go 入口、Replan 等位置调用）。
func (c *frozenStepPromptCache) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[frozenStepKey]*PromptParts)
}

// Len 返回当前缓存条目数（测试用）。
func (c *frozenStepPromptCache) Len() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}
