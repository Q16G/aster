package react

import (
	"strings"

	"aster/internal/react/persistv2"
)

type ResumeIntent string

const (
	ResumeIntentColdStart    ResumeIntent = "cold_start"
	ResumeIntentContextCarry ResumeIntent = "context_carry"
	ResumeIntentContextReplan ResumeIntent = "context_replan"
	ResumeIntentFullResume   ResumeIntent = "full_resume"
)

// resolveResumeIntent 阶段 1：确定性规则。
// 返回 (intent, needsClassification)。
// needsClassification=true 时 intent 为默认值 context_carry，
// 调用方应设置 Phase = AgentPhaseIntentClassification 交由调度器处理阶段 2。
func resolveResumeIntent(v2Snap *persistv2.Snapshot, cfg *ExecuteConfig, hasInput bool) (ResumeIntent, bool) {
	if cfg != nil && !cfg.resumeExecutionIntent &&
		cfg.interruptResolution == nil && cfg.interruptCancel == nil {
		return ResumeIntentColdStart, false
	}

	if cfg != nil && cfg.forceColdStart {
		return ResumeIntentColdStart, false
	}

	if v2Snap == nil || v2Snap.LastSeq == 0 {
		return ResumeIntentColdStart, false
	}

	switch v2Snap.SessionState {
	case persistv2.SessionStateBusy,
		persistv2.SessionStateRecovering,
		persistv2.SessionStateWaitingForHuman:
		return ResumeIntentFullResume, false
	}

	if v2Snap.SessionState == persistv2.SessionStateIdle {
		if strings.TrimSpace(v2Snap.RuntimeStateBlobRef) != "" && hasInput {
			return ResumeIntentContextCarry, true
		}
		if strings.TrimSpace(v2Snap.RuntimeStateBlobRef) == "" {
			return ResumeIntentColdStart, false
		}
	}

	return ResumeIntentColdStart, false
}
