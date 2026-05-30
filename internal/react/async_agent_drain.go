package react

import (
	"context"
	"fmt"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// drainAsyncAgentNotifications reads all pending completed/failed async agent notifications
// and injects them into stepHistory as user messages.
// Must only be called from the scheduler goroutine (single-writer invariant).
func (a *Agent) drainAsyncAgentNotifications() {
	if a == nil || a.asyncRegistry == nil {
		return
	}
	for {
		select {
		case notif := <-a.asyncRegistry.notifications:
			if a.asyncRegistry.Get(notif.AgentID) == nil {
				continue
			}
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

		default:
			a.asyncRegistry.PurgeDelivered()
			return
		}
	}
}

// awaitAllBackgroundSubAgents blocks until every running background sub-agent
// has completed, draining each completion as it arrives (injecting it into
// stepHistory and emitting EventTypeSubAgentBgEnd). If ctx is cancelled it stops
// early; callers on cancel paths should follow up with cancelRunningSubAgents to
// settle any still-running panel cards.
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
		a.drainAsyncAgentNotifications()
	}
	// Final drain: pick up any completion that landed but was not yet processed.
	a.drainAsyncAgentNotifications()
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
// registry entry still marked running, so the TUI panel cards do not stay stuck
// on "running" when the parent turn aborts (ctx-cancel / forced failure) before
// the children settle.
// Must only be called from the scheduler goroutine (single-writer invariant).
func (a *Agent) cancelRunningSubAgents() {
	if a == nil || a.asyncRegistry == nil || a.emitter == nil {
		return
	}
	for _, entry := range a.asyncRegistry.RunningAgents() {
		if entry == nil {
			continue
		}
		a.emitter.EmitJSON(EventTypeSubAgentBgEnd, entry.AgentID, map[string]any{
			"agent_id": entry.AgentID,
			"status":   "cancelled",
		})
	}
}
