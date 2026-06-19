package react

import (
	"strings"

	"aster/internal/builtin_tools"
)

// inline_step 调度纯函数 + 集合辅助。
//
// 本文件提供 commit 8 真接桶并发时调度层会用的两个原语：
//   - selectInlineStepPeers: 在给定 ready 列表 + 现有占用 + 上限下，纯计算可派发 peer
//   - collectInlineStepIDs:  把 current + 可派发 peer 串成统一的 stepID 列表（commit 8
//     的 runStepsConcurrently 会按此列表起 goroutine）
//
// commit 7 阶段这两个函数尚无调度调用方——step_fanout.go 旧 X2 路径仍走 selectFanOutPeers，
// commit 8 把 runStepPhase 换成"current + peers 统一并发"模型时切到本文件函数并删除旧 fanout。

// selectInlineStepPeers 是 step_fanout.go:selectFanOutPeers 的迁移版本，语义不变：
// 在已有 running / current / 上限三层过滤下，从 ready 列表里挑出本轮要派发的 peer step ID。
//
//   - 跳过 current（主路径已处理）
//   - 跳过已在 registry 里的（正在跑或已完成未 purge）
//   - 派发数量上限：maxParallel - 1 - runningInline
//
// maxParallel < 2 或 slots <= 0 时返回 nil（no-op，退化为单步串行）。
//
// **degenerate case 设计**：maxParallel=1 时返回 nil → collectInlineStepIDs 只返回
// [current]，runStepsConcurrently 单 goroutine 跑——与多桶代码路径完全一致，不保留
// 任何 "if maxParallel < 2 { ... 走旧串行路径 }" 的分叉特殊化（参见
// [[feedback_no_atomic_ledger_tools]] 和 plan 文件 §fallback 不变质原则）。
func selectInlineStepPeers(maxParallel int, runningInline int, currentID string, ready []string, alreadyRegistered func(string) bool) []string {
	if maxParallel < 2 {
		return nil
	}
	slotsAvailable := maxParallel - 1 - runningInline
	if slotsAvailable <= 0 {
		return nil
	}
	currentID = strings.TrimSpace(currentID)
	out := make([]string, 0, slotsAvailable)
	for _, id := range ready {
		if slotsAvailable <= 0 {
			break
		}
		if id == currentID {
			continue
		}
		if alreadyRegistered != nil && alreadyRegistered(id) {
			continue
		}
		out = append(out, id)
		slotsAvailable--
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// collectInlineStepIDs 把本轮要并发跑的 stepID 集合化为 [current, peer1, peer2, ...]。
//
// 返回切片至少含 current（若存在），peer 顺序与 ReadyRunnablePlanStepIDs 输出一致。
// MaxParallelSteps < 2 或无可派发 peer 时仅返回 [current]；snapshot 无 current 时
// 返回 nil（退到 EnsureCurrentStep 兜底）。
//
// 调用方契约：scheduler 单 goroutine 调用——本函数读 a.asyncRegistry，写者在同一调度
// goroutine 内完成，不需要桶级锁。返回的 stepID 列表交 runStepsConcurrently 起 goroutine。
func (a *Agent) collectInlineStepIDs(snap builtin_tools.StateSnapshot) []string {
	if a == nil {
		return nil
	}
	currentID := strings.TrimSpace(snap.CurrentStepID)
	if currentID == "" {
		return nil
	}
	out := []string{currentID}

	if a.asyncRegistry == nil {
		return out
	}
	maxParallel := a.maxParallelSteps()
	if maxParallel < 2 {
		return out
	}
	peers := selectInlineStepPeers(
		maxParallel,
		a.asyncRegistry.RunningRemoteSteps(),
		currentID,
		builtin_tools.ReadyRunnablePlanStepIDs(snap.Plan),
		func(id string) bool { return a.asyncRegistry.Get(id) != nil },
	)
	out = append(out, peers...)
	return out
}
