package react

import (
	"fmt"

	"aster/internal/ai"
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

		default:
			return
		}
	}
}
