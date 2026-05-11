package react

import (
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
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

	// Re-align step-history bookkeeping with restored state.
	if a.state != nil {
		st := a.state.Snapshot()
		a.stepHistoryStepID = strings.TrimSpace(st.CurrentStepID)
		a.stepHistoryPhase = st.Phase
		a.stepHistoryPlanVer = st.PlanVersion
	}
	return nil
}

