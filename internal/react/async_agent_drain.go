package react

import (
	"context"
	"fmt"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// drainAsyncAgentNotifications reads all pending completed/failed async agent notifications
// and dispatches them by Kind:
//   - sub_agent（默认）：注入 stepHistory（原路径，模型可见）
//   - remote_step（X2）：调 state.UpdateRemotePlanItem 回写 PlanItem 并立即重扫 ready 二次 fan-out
//
// ctx 承自 scheduler 顶层（最终 TUI ctx，不 derive 不超时），供 handleRemoteStepNotification
// 内 spawnRemoteStep 复用。Must only be called from the scheduler goroutine (single-writer invariant).
func (a *Agent) drainAsyncAgentNotifications(ctx context.Context) {
	if a == nil || a.asyncRegistry == nil {
		return
	}
	for {
		select {
		case notif := <-a.asyncRegistry.notifications:
			if a.asyncRegistry.Get(notif.AgentID) == nil {
				continue
			}
			switch notif.Kind {
			case AsyncAgentKindRemoteStep:
				a.handleRemoteStepNotification(ctx, notif)
			default: // "" 或 AsyncAgentKindSubAgent
				a.handleSubAgentNotification(notif)
			}

		default:
			a.asyncRegistry.PurgeDelivered()
			return
		}
	}
}

// handleSubAgentNotification 处理 sub_agent 完成通知：原路径，注入 stepHistory。
// 行为与面 2 之前完全一致——为分流抽出的方法，仅作组织调整。
func (a *Agent) handleSubAgentNotification(notif *AsyncAgentNotification) {
	resultFile := writeAsyncResultFile(notif.WorkspaceDir, notif)

	summary := ""
	if notif.Result != nil {
		summary = notif.Result.Result
		if notif.Result.Error != "" && summary == "" {
			summary = notif.Result.Error
		}
	}
	summary = truncateRuneString(summary, maxAsyncNotificationRunes)

	text := fmt.Sprintf(
		"[后台 Agent 完成通知]\nagent_id: %s\nstatus: %s\nworkspace: %s",
		notif.AgentID, notif.Status, notif.WorkspaceDir,
	)
	if resultFile != "" {
		text += fmt.Sprintf("\nresult_file: %s", resultFile)
	}
	if summary != "" {
		text += fmt.Sprintf("\nresult_summary:\n%s", summary)
	}

	notifMsg := ai.NewUserMsgInfo(text)
	a.stepHistory = append(a.stepHistory, notifMsg)
	a.asyncRegistry.MarkDelivered(notif.AgentID)
	// The panel card is closed from the child goroutine's defer
	// (sub_agent_tool.go executeAsync) the instant it settles, so drain
	// no longer emits EventTypeSubAgentBgEnd — it only injects the
	// completion into stepHistory for the model.
}

// handleRemoteStepNotification 处理 X2 远程 step 完成通知。
// 与 sub_agent 路径的差异：
//   - 不写 async_result.json（远程 step 的 transcript 由 step_fanout 落 blob）
//   - 不注入 stepHistory（主路径 transcript 不被远程 step 污染）
//   - 直接调 state.UpdateRemotePlanItem 把 Status + Summary 回写 PlanItem
//   - 回写后立即重扫 ready 触发二次 fan-out（X2 滚动派发的核心）
//
// 面 2 仅做最小回写（Status + Summary + Error）；完整结构化字段
// （StatusSummary / ShortSummary / KeyFacts / etc）留给面 3.3 spawnRemoteStep
// 通过新增通道传完整 CurrentStepUpdate。
func (a *Agent) handleRemoteStepNotification(ctx context.Context, notif *AsyncAgentNotification) {
	if a.state == nil {
		a.asyncRegistry.MarkDelivered(notif.AgentID)
		return
	}

	// 从 notif.Status（"completed"/"failed"）映射 PlanStepStatus。
	// 兜底视为 Failed——绝不把 PlanItem 退回非终态（与 UpdateRemotePlanItem 守卫呼应）。
	status := builtin_tools.PlanStepFailed
	if notif.Status == "completed" {
		status = builtin_tools.PlanStepCompleted
	}

	summary := ""
	errMsg := ""
	transcriptRef := ""
	if notif.Result != nil {
		summary = notif.Result.Result
		errMsg = notif.Result.Error
		transcriptRef = notif.Result.TranscriptBlobRef
		if summary == "" && errMsg != "" {
			summary = errMsg
		}
	}
	summary = truncateRuneString(summary, maxAsyncNotificationRunes)
	errMsg = truncateRuneString(errMsg, maxAsyncNotificationRunes)

	a.state.UpdateRemotePlanItem(notif.AgentID, builtin_tools.CurrentStepUpdate{
		Status:            status,
		Summary:           summary,
		Error:             errMsg,
		TranscriptBlobRef: transcriptRef,
	})

	// 发 BgEnd 事件让 TUI 卡片翻终态。与 sub_agent 的 emitSubAgentCardEnd 对照——
	// 在 fanOutReadyPeers 之前发，确保用户先看到该卡片终态、再看到新 spawn 的卡片，
	// 时序与 plan state 推进顺序对齐。
	if a.emitter != nil {
		cardStatus := "completed"
		if status == builtin_tools.PlanStepFailed {
			cardStatus = "failed"
		}
		a.emitter.EmitJSON(EventTypeRemoteStepBgEnd, notif.AgentID, map[string]any{
			"agent_id": notif.AgentID,
			"status":   cardStatus,
			"summary":  summary,
		})
	}

	// X2 滚动派发核心：回写完 PlanItem 后立即扫 ready，把刚被解锁的下一波 step
	// 直接 spawn 出去，无需等到下一 iter 入口。fanOutReadyPeers 自带 slot/registered/
	// agentFactory 三层早退，drain 侧无需防御。
	//
	// 顺序不变量（不要交换以下两行）：
	//   1. fanOutReadyPeers 必须在 MarkDelivered 之前调用。
	//      此时 notif.AgentID 对应 entry 仍在 registry（closed=true, delivered=false），
	//      selectFanOutPeers 的 alreadyRegistered 判定看到 Get(notif.AgentID) != nil 会跳过
	//      ——防止同 ID 被误派发为新远程 step（理论不会命中，因为 ready 已排除终态，
	//      但保留这层兜底防御）。
	//   2. MarkDelivered 后续 drain 的 PurgeDelivered 才会清掉 entry。
	a.fanOutReadyPeers(ctx, a.state.Snapshot())

	a.asyncRegistry.MarkDelivered(notif.AgentID)
}

// awaitAllBackgroundSubAgents blocks until every running background task
// has completed (含 sub_agent 与 X2 remote_step——HasRunning 不分 kind），
// draining each completion as it arrives. drain 内部按 kind 分流：
//   - sub_agent：注入 stepHistory（模型可见）
//   - remote_step：state.UpdateRemotePlanItem 回写，不污染 stepHistory
//
// If ctx is cancelled it stops early; callers on cancel paths should follow up
// with cancelRunningSubAgents 处理 sub_agent 卡片；remote_step 走 spawnRemoteStep
// 自身 defer 的 Complete(failed) 兜底，不在此处显式取消（面 5 引入 RemoteStepEnd event 时再统一）。
// Must only be called from the scheduler goroutine (single-writer invariant).
func (a *Agent) awaitAllBackgroundSubAgents(ctx context.Context) {
	if a == nil || a.asyncRegistry == nil {
		return
	}
	for a.asyncRegistry.HasRunning() {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		a.asyncRegistry.WaitForCompletion(ctx)
		a.drainAsyncAgentNotifications(ctx)
	}
	// Final drain: pick up any completion that landed but was not yet processed.
	a.drainAsyncAgentNotifications(ctx)
}

// emitSubAgentCardEnd closes the durable sub-agent panel card opened by the
// matching EventTypeSubAgentBgStart, describing the child's final status from
// its RunResult. It is emitted from the child goroutine's defer (sub_agent_tool
// executeAsync) the instant the child settles, so the card flips without waiting
// for the parent scheduler to drain. Safe to call off the scheduler goroutine:
// the emitter sink (EventBridge p.Send) is concurrency-safe.
func (a *Agent) emitSubAgentCardEnd(childName string, result *builtin_tools.RunResult) {
	if a == nil || a.emitter == nil {
		return
	}
	status := "completed"
	summary := ""
	if result == nil || !result.Success {
		status = "failed"
	}
	if result != nil {
		if summary = result.Result; summary == "" {
			summary = result.Error
		}
	}
	a.emitter.EmitJSON(EventTypeSubAgentBgEnd, childName, map[string]any{
		"agent_id": childName,
		"status":   status,
		"summary":  truncateRuneString(summary, maxAsyncNotificationRunes),
	})
}

// cancelRunningSubAgents emits a cancelled EventTypeSubAgentBgEnd for every
// **sub_agent** registry entry still marked running, so the TUI panel cards do not
// stay stuck on "running" when the parent turn aborts (ctx-cancel / forced failure)
// before the children settle.
//
// 显式过滤 Kind=remote_step：那些是 X2 远程 step 卡片，使用不同的 event 类型
// （EventTypeRemoteStepBgEnd），由 cancelRunningRemoteSteps 配对处理。这里发
// EventTypeSubAgentBgEnd 会让 TUI 把远程 step 卡当 sub_agent 卡渲染、串台。
// Must only be called from the scheduler goroutine (single-writer invariant).
func (a *Agent) cancelRunningSubAgents() {
	if a == nil || a.asyncRegistry == nil || a.emitter == nil {
		return
	}
	for _, entry := range a.asyncRegistry.RunningAgents() {
		if entry == nil {
			continue
		}
		if entry.Kind == AsyncAgentKindRemoteStep {
			continue
		}
		a.emitter.EmitJSON(EventTypeSubAgentBgEnd, entry.AgentID, map[string]any{
			"agent_id": entry.AgentID,
			"status":   "cancelled",
		})
	}
}

// cancelRunningRemoteSteps emits a cancelled EventTypeRemoteStepBgEnd for every
// remote_step registry entry still marked running. Pairs with cancelRunningSubAgents
// in the same scheduler cancel/teardown call sites.
//
// 与 cancelRunningSubAgents 对称：只处理 Kind=remote_step。sub_agent 卡片通过同位置
// 的 cancelRunningSubAgents 调用清理。Must only be called from the scheduler goroutine.
func (a *Agent) cancelRunningRemoteSteps() {
	if a == nil || a.asyncRegistry == nil || a.emitter == nil {
		return
	}
	for _, entry := range a.asyncRegistry.RunningAgents() {
		if entry == nil {
			continue
		}
		if entry.Kind != AsyncAgentKindRemoteStep {
			continue
		}
		a.emitter.EmitJSON(EventTypeRemoteStepBgEnd, entry.AgentID, map[string]any{
			"agent_id": entry.AgentID,
			"status":   "cancelled",
		})
	}
}
