package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
	"aster/internal/runtimelog"
)

func (a *Agent) restoreRuntimeFromV2Snapshot(store *persistv2.Store, snap *persistv2.Snapshot) error {
	if a == nil || store == nil || snap == nil {
		return nil
	}

	if a.state != nil && strings.TrimSpace(snap.RuntimeStateBlobRef) != "" {
		raw, err := store.ReadBlob(snap.RuntimeStateBlobRef)
		if err != nil {
			return fmt.Errorf("read runtime_state blob: %w", err)
		}
		if len(raw) > 0 {
			var st builtin_tools.StateSnapshot
			if err := json.Unmarshal(raw, &st); err != nil {
				return fmt.Errorf("unmarshal runtime_state: %w", err)
			}
			a.state.Replace(st)
		}
	}

	// Restore the in-flight step transcript so we can continue tool-call sequences.
	if strings.TrimSpace(snap.StepHistoryBlobRef) != "" {
		raw, err := store.ReadBlob(snap.StepHistoryBlobRef)
		if err != nil {
			return fmt.Errorf("read step_history blob: %w", err)
		}
		if len(raw) > 0 {
			var msgs []*ai.MsgInfo
			if err := json.Unmarshal(raw, &msgs); err != nil {
				return fmt.Errorf("unmarshal step_history: %w", err)
			}
			a.stepHistory = ai.NormalizeMsgInfoSlice(msgs)
		}
	}

	// Restore the long-term conversation history so the model retains prior-turn context.
	if strings.TrimSpace(snap.ConversationHistoryBlobRef) != "" {
		raw, err := store.ReadBlob(snap.ConversationHistoryBlobRef)
		if err != nil {
			return fmt.Errorf("read conversation_history blob: %w", err)
		}
		if len(raw) > 0 {
			var msgs []*ai.MsgInfo
			if err := json.Unmarshal(raw, &msgs); err != nil {
				return fmt.Errorf("unmarshal conversation_history: %w", err)
			}
			a.history = ai.NormalizeMsgInfoSlice(msgs)
			a.notifyHistoryReplace()
		}
	}

	// Re-align step-history bookkeeping with restored state.
	if a.state != nil {
		st := a.state.Snapshot()
		a.stepHistoryStepID = strings.TrimSpace(st.CurrentStepID)
		a.stepHistoryPhase = st.Phase
		a.stepHistoryPlanVer = st.PlanVersion
	}
	return nil
}

// softResetWithContext 从 V2 snapshot 读取前序上下文（StepOutcomes + InputTimeline +
// ConversationHistory），通过 reducer 条件压缩 outcomes，然后 SoftReset 保留上下文。
// blob 读取失败时降级为 SoftReset(nil, nil)，等价于 Reset。
func (a *Agent) softResetWithContext(ctx context.Context, client ai.ChatClient, store *persistv2.Store, snap *persistv2.Snapshot) {
	if a == nil || a.state == nil || store == nil || snap == nil {
		if a != nil && a.state != nil {
			a.state.SoftReset(nil, nil)
		}
		return
	}

	var outcomes []*builtin_tools.StepOutcome
	var timeline []*builtin_tools.TimelineInput

	if ref := strings.TrimSpace(snap.RuntimeStateBlobRef); ref != "" {
		raw, err := store.ReadBlob(ref)
		if err != nil {
			runtimelog.LogJSON("warn", map[string]any{"msg": "softResetWithContext: read runtime_state blob", "error": err.Error()})
			a.state.SoftReset(nil, nil)
			return
		}
		if len(raw) > 0 {
			var st builtin_tools.StateSnapshot
			if err := json.Unmarshal(raw, &st); err != nil {
				runtimelog.LogJSON("warn", map[string]any{"msg": "softResetWithContext: unmarshal runtime_state", "error": err.Error()})
				a.state.SoftReset(nil, nil)
				return
			}
			outcomes = st.StepOutcomes
			timeline = st.InputTimeline
		}
	}

	if len(outcomes) > 0 && client != nil {
		reduced, err := a.reduceStepOutcomesIfNeeded(ctx, client, outcomes)
		if err != nil {
			runtimelog.LogJSON("warn", map[string]any{"msg": "softResetWithContext: reduce outcomes", "error": err.Error()})
		} else {
			outcomes = reduced
		}
	}

	a.state.SoftReset(outcomes, timeline)

	if ref := strings.TrimSpace(snap.ConversationHistoryBlobRef); ref != "" {
		raw, err := store.ReadBlob(ref)
		if err != nil {
			runtimelog.LogJSON("warn", map[string]any{"msg": "softResetWithContext: read conversation_history blob", "error": err.Error()})
			return
		}
		if len(raw) > 0 {
			var msgs []*ai.MsgInfo
			if err := json.Unmarshal(raw, &msgs); err != nil {
				runtimelog.LogJSON("warn", map[string]any{"msg": "softResetWithContext: unmarshal conversation_history", "error": err.Error()})
				return
			}
			a.history = ai.NormalizeMsgInfoSlice(msgs)
			a.notifyHistoryReplace()
		}
	}
}

