package react

import (
	"testing"

	"aster/internal/react/persistv2"
)

func TestResolveResumeIntent_ColdStart_ForceCold(t *testing.T) {
	snap := &persistv2.Snapshot{
		SessionState:        persistv2.SessionStateIdle,
		RuntimeStateBlobRef: "sha256:abc",
		LastSeq:             5,
	}
	cfg := &ExecuteConfig{forceColdStart: true}
	intent, needs := resolveResumeIntent(snap, cfg, true)
	if intent != ResumeIntentColdStart {
		t.Errorf("got intent=%q, want cold_start", intent)
	}
	if needs {
		t.Error("needsClassification should be false for forceColdStart")
	}
}

func TestResolveResumeIntent_ColdStart_NoSnapshot(t *testing.T) {
	intent, needs := resolveResumeIntent(nil, &ExecuteConfig{}, true)
	if intent != ResumeIntentColdStart || needs {
		t.Errorf("got (%q, %v), want (cold_start, false)", intent, needs)
	}
}

func TestResolveResumeIntent_ColdStart_ZeroSeq(t *testing.T) {
	snap := &persistv2.Snapshot{SessionState: persistv2.SessionStateIdle, LastSeq: 0}
	intent, needs := resolveResumeIntent(snap, &ExecuteConfig{}, true)
	if intent != ResumeIntentColdStart || needs {
		t.Errorf("got (%q, %v), want (cold_start, false)", intent, needs)
	}
}

func TestResolveResumeIntent_FullResume_Busy(t *testing.T) {
	snap := &persistv2.Snapshot{SessionState: persistv2.SessionStateBusy, LastSeq: 3}
	intent, needs := resolveResumeIntent(snap, &ExecuteConfig{resumeExecutionIntent: true}, true)
	if intent != ResumeIntentFullResume || needs {
		t.Errorf("got (%q, %v), want (full_resume, false)", intent, needs)
	}
}

func TestResolveResumeIntent_FullResume_Recovering(t *testing.T) {
	snap := &persistv2.Snapshot{SessionState: persistv2.SessionStateRecovering, LastSeq: 2}
	intent, needs := resolveResumeIntent(snap, &ExecuteConfig{resumeExecutionIntent: true}, false)
	if intent != ResumeIntentFullResume || needs {
		t.Errorf("got (%q, %v), want (full_resume, false)", intent, needs)
	}
}

func TestResolveResumeIntent_FullResume_WaitingForHuman(t *testing.T) {
	snap := &persistv2.Snapshot{SessionState: persistv2.SessionStateWaitingForHuman, LastSeq: 1}
	intent, needs := resolveResumeIntent(snap, &ExecuteConfig{resumeExecutionIntent: true}, true)
	if intent != ResumeIntentFullResume || needs {
		t.Errorf("got (%q, %v), want (full_resume, false)", intent, needs)
	}
}

func TestResolveResumeIntent_Idle_NeedsClassification(t *testing.T) {
	snap := &persistv2.Snapshot{
		SessionState:        persistv2.SessionStateIdle,
		RuntimeStateBlobRef: "sha256:abc",
		LastSeq:             10,
	}
	intent, needs := resolveResumeIntent(snap, &ExecuteConfig{resumeExecutionIntent: true}, true)
	if intent != ResumeIntentContextCarry {
		t.Errorf("got intent=%q, want context_carry", intent)
	}
	if !needs {
		t.Error("needsClassification should be true for IDLE + blob + input")
	}
}

func TestResolveResumeIntent_Idle_NoBlobRef(t *testing.T) {
	snap := &persistv2.Snapshot{
		SessionState: persistv2.SessionStateIdle,
		LastSeq:      5,
	}
	intent, needs := resolveResumeIntent(snap, &ExecuteConfig{resumeExecutionIntent: true}, true)
	if intent != ResumeIntentColdStart || needs {
		t.Errorf("got (%q, %v), want (cold_start, false)", intent, needs)
	}
}

func TestResolveResumeIntent_Idle_NoInput(t *testing.T) {
	snap := &persistv2.Snapshot{
		SessionState:        persistv2.SessionStateIdle,
		RuntimeStateBlobRef: "sha256:abc",
		LastSeq:             5,
	}
	intent, needs := resolveResumeIntent(snap, &ExecuteConfig{resumeExecutionIntent: true}, false)
	if intent != ResumeIntentColdStart || needs {
		t.Errorf("got (%q, %v), want (cold_start, false) when no input", intent, needs)
	}
}

func TestResolveResumeIntent_ForceCold_OverridesAll(t *testing.T) {
	snap := &persistv2.Snapshot{
		SessionState:        persistv2.SessionStateBusy,
		RuntimeStateBlobRef: "sha256:abc",
		LastSeq:             10,
	}
	cfg := &ExecuteConfig{forceColdStart: true, resumeExecutionIntent: true}
	intent, needs := resolveResumeIntent(snap, cfg, true)
	if intent != ResumeIntentColdStart || needs {
		t.Errorf("forceColdStart should override BUSY: got (%q, %v)", intent, needs)
	}
}

func TestResolveResumeIntent_NoResumeIntent_ColdStart(t *testing.T) {
	snap := &persistv2.Snapshot{
		SessionState:        persistv2.SessionStateIdle,
		RuntimeStateBlobRef: "sha256:abc",
		LastSeq:             10,
	}
	intent, needs := resolveResumeIntent(snap, &ExecuteConfig{resumeExecutionIntent: false}, true)
	if intent != ResumeIntentColdStart || needs {
		t.Errorf("without resumeExecutionIntent should be cold_start: got (%q, %v)", intent, needs)
	}
}

func TestResolveResumeIntent_InterruptResolution_FullResume(t *testing.T) {
	snap := &persistv2.Snapshot{
		SessionState: persistv2.SessionStateBusy,
		LastSeq:      5,
	}
	cfg := &ExecuteConfig{
		resumeExecutionIntent: false,
		interruptResolution:   &interruptResolution{InterruptID: "int-1", Answer: "yes"},
	}
	intent, needs := resolveResumeIntent(snap, cfg, true)
	if intent != ResumeIntentFullResume || needs {
		t.Errorf("interrupt resolution should trigger full_resume: got (%q, %v)", intent, needs)
	}
}
