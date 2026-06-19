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
//   - inline_step（commit 7-9 重构后的并发 step）：调 state.UpdateInlineStep 回写 PlanItem 终态
//
// inline_step goroutine 已在 spawnInlinePeer 内部完成 PlanItem 推进（auto-complete 或
// 工具分发驱动）；drain 路径的 UpdateInlineStep 仅作为「失败兜底」——goroutine 异常 / panic /
// ctx 取消时把 PlanItem 翻 Failed，确保 PlanItem 不滞留 InProgress。
//
// 不再触发二次 fan-out（commit 8c2 已删 fanOutReadyPeers 调用）——scheduler 下一 iter 重入
// runStepPhase 时会通过 collectInlineStepIDs 自然重扫 ready，把新解锁的 peer spawn 出去。
//
// Must only be called from the scheduler goroutine (single-writer invariant).
func (a *Agent) drainAsyncAgentNotifications(ctx context.Context) {
	if a == nil || a.asyncRegistry == nil {
		return
	}
	_ = ctx
	for {
		select {
		case notif := <-a.asyncRegistry.notifications:
			if a.asyncRegistry.Get(notif.AgentID) == nil {
				continue
			}
			switch notif.Kind {
			case AsyncAgentKindInlineStep:
				a.handleInlineStepNotification(notif)
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

// handleInlineStepNotification 处理 inline step 完成通知。
// inline goroutine 已在自己 think_act 循环里推进 PlanItem（auto-complete 或工具分发），
// 本函数仅作为「失败兜底」+ 终态事件 emit：
//   - PlanItem 仍是 Pending/InProgress → UpdateInlineStep 翻 Failed 防滞留
//   - PlanItem 已终态 → UpdateInlineStep 终态守卫 no-op，无副作用
//   - emit EventTypeInlineStepEnd 让 TUI 卡片翻终态
//
// **不再触发二次 fan-out**：scheduler 下一 iter 自然走 runStepPhase →
// collectInlineStepIDs 重扫 ready，把新解锁的 peer spawn 出去。
func (a *Agent) handleInlineStepNotification(notif *AsyncAgentNotification) {
	if a.state == nil {
		a.asyncRegistry.MarkDelivered(notif.AgentID)
		return
	}

	// 从 notif.Status（"completed"/"failed"）映射 PlanStepStatus。
	// 兜底视为 Failed——绝不把 PlanItem 退回非终态（与 UpdateInlineStep 守卫呼应）。
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

	a.state.UpdateInlineStep(notif.AgentID, builtin_tools.CurrentStepUpdate{
		Status:            status,
		Summary:           summary,
		Error:             errMsg,
		TranscriptBlobRef: transcriptRef,
	})

	if a.emitter != nil {
		cardStatus := "completed"
		if status == builtin_tools.PlanStepFailed {
			cardStatus = "failed"
		}
		a.emitter.EmitJSON(EventTypeInlineStepEnd, notif.AgentID, map[string]any{
			"agent_id": notif.AgentID,
			"status":   cardStatus,
			"summary":  summary,
		})
	}

	a.asyncRegistry.MarkDelivered(notif.AgentID)
}

// awaitAllBackgroundSubAgents blocks until every running background task
// has completed (含 sub_agent 与 X2 remote_step——HasRunning 不分 kind），
// draining each completion as it arrives. drain 内部按 kind 分流：
//   - sub_agent：注入 stepHistory（模型可见）
//   - remote_step：state.UpdateInlineStep 回写，不污染 stepHistory
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
// 显式过滤 Kind=remote_step（inline_step）：那些卡片用 EventTypeInlineStepEnd，
// 由 cancelRunningInlineSteps 配对处理。这里发 EventTypeSubAgentBgEnd 会让 TUI 把
// inline step 卡当 sub_agent 卡渲染、串台。
// Must only be called from the scheduler goroutine (single-writer invariant).
func (a *Agent) cancelRunningSubAgents() {
	if a == nil || a.asyncRegistry == nil || a.emitter == nil {
		return
	}
	for _, entry := range a.asyncRegistry.RunningAgents() {
		if entry == nil {
			continue
		}
		if entry.Kind == AsyncAgentKindInlineStep {
			continue
		}
		a.emitter.EmitJSON(EventTypeSubAgentBgEnd, entry.AgentID, map[string]any{
			"agent_id": entry.AgentID,
			"status":   "cancelled",
		})
	}
}

// cancelRunningInlineSteps emits a cancelled EventTypeInlineStepEnd for every
// inline_step registry entry still marked running. Pairs with cancelRunningSubAgents
// in the same scheduler cancel/teardown call sites.
//
// 与 cancelRunningSubAgents 对称：只处理 Kind=inline_step (AsyncAgentKindInlineStep
// 仍是旧名，commit 14 改名)。sub_agent 卡片通过同位置的 cancelRunningSubAgents 清理。
// Must only be called from the scheduler goroutine.
func (a *Agent) cancelRunningInlineSteps() {
	if a == nil || a.asyncRegistry == nil || a.emitter == nil {
		return
	}
	for _, entry := range a.asyncRegistry.RunningAgents() {
		if entry == nil {
			continue
		}
		if entry.Kind != AsyncAgentKindInlineStep {
			continue
		}
		a.emitter.EmitJSON(EventTypeInlineStepEnd, entry.AgentID, map[string]any{
			"agent_id": entry.AgentID,
			"status":   "cancelled",
		})
	}
}
