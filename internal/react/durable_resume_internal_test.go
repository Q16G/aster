package react

import (
	"encoding/json"
	"testing"

	"aster/internal/builtin_tools"
)

func TestSynthesizeResumeSnapshot_RestoresResultFromSharedStepViewJSON(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "step-1", Step: "生成结果", Status: builtin_tools.PlanStepCompleted},
	}
	stepOutcomes := []*builtin_tools.StepOutcome{
		{
			StepID:       "step-1",
			Status:       builtin_tools.StepOutcomeCompleted,
			ShortSummary: "摘要很短",
			Result:       "result-only-payload",
		},
	}
	views := collectAllStepContextViews(plan, stepOutcomes)

	raw, err := json.Marshal(map[string]any{
		"session_id":   "resume-session",
		"plan_version": 1,
		"assessed_state": map[string]any{
			"status":        builtin_tools.TaskStatusRunning,
			"plan":          plan,
			"plan_version":  1,
			"step_outcomes": views,
		},
		"assessment": map[string]any{
			"is_complete": false,
			"status":      string(builtin_tools.TaskStatusRunning),
		},
	})
	if err != nil {
		t.Fatalf("marshal final assessment payload failed: %v", err)
	}

	var artifact FinalAssessmentArtifact
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatalf("unmarshal final assessment payload failed: %v", err)
	}

	snapshot, planValid := synthesizeResumeSnapshot(nil, nil, nil, &artifact, 0)
	if !planValid {
		t.Fatal("expected synthesized plan to be valid")
	}
	if len(snapshot.StepOutcomes) != 1 {
		t.Fatalf("expected one resumed step outcome, got %d", len(snapshot.StepOutcomes))
	}
	if snapshot.StepOutcomes[0] == nil {
		t.Fatal("expected resumed step outcome to be non-nil")
	}
	if snapshot.StepOutcomes[0].Result != "result-only-payload" {
		t.Fatalf("expected resumed result to be preserved, got %q", snapshot.StepOutcomes[0].Result)
	}
}
