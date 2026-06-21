package react

import (
	"strings"

	"aster/internal/builtin_tools"
)

// emitterStateObserver 桥接 StateTracker.PlanItemChange → emitter 事件。
//
// 行为契约（替代散落各处的 emitTaskItemDiffs / EmitJSON 调用）：
//
//   - 任意 actor 的状态翻转 → emit EventTypeTaskItem（TUI 右侧 todo 列表订阅）；
//     新建 PlanItem（PrevStatus 空）只在 NextStatus=InProgress 或匹配 CurrentStepID
//     时 emit——保留旧 emitTaskItemDiffs 的「全 plan 创建只 emit 当前选中条目」语义，
//     避免 UpdatePlan 一次广播 N 个 pending task_item 把 TUI 列表抖一遍。
//
//   - actor=peer 且 Pending→InProgress：附加 emit EventTypeInlineStepStart
//     （TUI 卡片订阅），承担旧 spawnInlinePeer 内手 emit 的职责。
//
//   - actor=peer 且 → 终态（Completed/Failed/Skipped）：附加 emit
//     EventTypeInlineStepEnd，承担旧 async_agent_drain.handleInlineStepNotification
//     的手 emit 职责。cancelled 状态由 cancelRunningInlineSteps 直接 emit
//     （不经状态变化）——本 observer 不接管。
//
// agent 字段保存活引用：observer 不在 PlanItemChange 里读 workspaceRootDir 等
// 运行时态字段（避免 PlanItemChange 结构膨胀），改在回调时从 agent 现取——
// agent 字段在 NewReActAgent / Execute 期间稳定，observer 不持锁读 agent 的瞬态字段。
type emitterStateObserver struct {
	agent *Agent
}

// newEmitterStateObserver 构造一个绑定到指定 Agent 的 emitter observer。
// 由 NewReActAgent 在 emitter 注入后调用并 RegisterObserver。
func newEmitterStateObserver(agent *Agent) *emitterStateObserver {
	if agent == nil {
		return nil
	}
	return &emitterStateObserver{agent: agent}
}

func (o *emitterStateObserver) OnPlanItemChange(change PlanItemChange) {
	if o == nil || o.agent == nil {
		return
	}
	emitter := o.agent.emitter
	if emitter == nil {
		return
	}

	o.emitTaskItem(emitter, change)
	o.emitPeerCardEvents(emitter, change)
}

// emitTaskItem 守门：新建条目仅在 NextStatus=InProgress 或匹配 CurrentStepID 时 emit。
func (o *emitterStateObserver) emitTaskItem(emitter *Emitter, change PlanItemChange) {
	if change.Item == nil {
		return
	}
	if strings.TrimSpace(string(change.PrevStatus)) == "" {
		isInProgress := change.NextStatus == builtin_tools.PlanStepInProgress
		matchesCurrent := change.CurrentStepID != "" && strings.TrimSpace(change.StepID) == change.CurrentStepID
		if !isInProgress && !matchesCurrent {
			return
		}
	}
	emitter.EmitTaskItem(change.Item, change.PrevStatus, change.Index, change.Reason)
}

// emitPeerCardEvents 在 peer 路径上额外 emit inline_step_start/end。
func (o *emitterStateObserver) emitPeerCardEvents(emitter *Emitter, change PlanItemChange) {
	if change.Actor != planItemActorPeer {
		return
	}
	stepID := strings.TrimSpace(change.StepID)
	if stepID == "" {
		return
	}

	if change.PrevStatus == builtin_tools.PlanStepPending && change.NextStatus == builtin_tools.PlanStepInProgress {
		emitter.EmitJSON(EventTypeInlineStepStart, stepID, map[string]any{
			"agent_id":  stepID,
			"step_id":   stepID,
			"workspace": o.agent.workspaceRootDir,
		})
		return
	}

	if isPlanStepTerminal(change.NextStatus) {
		emitter.EmitJSON(EventTypeInlineStepEnd, stepID, map[string]any{
			"agent_id": stepID,
			"step_id":  stepID,
			"status":   peerTerminalCardStatus(change.NextStatus),
			"summary":  change.Reason,
		})
	}
}

func isPlanStepTerminal(status builtin_tools.PlanStepStatus) bool {
	switch status {
	case builtin_tools.PlanStepCompleted,
		builtin_tools.PlanStepFailed,
		builtin_tools.PlanStepSkipped:
		return true
	}
	return false
}

// peerTerminalCardStatus 映射 PlanStepStatus → inline_step 卡片状态字符串。
// 保持与旧 handleInlineStepNotification 的卡片状态对齐：completed/failed/skipped。
func peerTerminalCardStatus(status builtin_tools.PlanStepStatus) string {
	switch status {
	case builtin_tools.PlanStepCompleted:
		return "completed"
	case builtin_tools.PlanStepFailed:
		return "failed"
	case builtin_tools.PlanStepSkipped:
		return "skipped"
	}
	return strings.TrimSpace(string(status))
}
