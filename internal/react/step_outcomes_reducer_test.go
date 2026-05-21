package react

import (
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

func TestParseReducedOutcomes_PreservesAllFields(t *testing.T) {
	raw := `[
		{
			"step_id": "s1",
			"status": "completed",
			"summary": "读取完成",
			"key_facts": ["fact1", "fact2"],
			"long_summary": "详细描述",
			"open_questions": ["q1", "q2"],
			"tool_calls_digest": ["read_file main.go"],
			"references": ["ref1"],
			"status_summary": "成功",
			"error": ""
		}
	]`

	outcomes, err := parseReducedOutcomes(raw)
	if err != nil {
		t.Fatalf("parseReducedOutcomes: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes len = %d, want 1", len(outcomes))
	}

	o := outcomes[0]
	if o.StepID != "s1" {
		t.Errorf("StepID = %q", o.StepID)
	}
	if o.LongSummary != "详细描述" {
		t.Errorf("LongSummary = %q", o.LongSummary)
	}
	if len(o.OpenQuestions) != 2 || o.OpenQuestions[0] != "q1" {
		t.Errorf("OpenQuestions = %v, want [q1 q2]", o.OpenQuestions)
	}
	if len(o.ToolCallsDigest) != 1 || o.ToolCallsDigest[0] != "read_file main.go" {
		t.Errorf("ToolCallsDigest = %v, want [read_file main.go]", o.ToolCallsDigest)
	}
	if len(o.KeyFacts) != 2 {
		t.Errorf("KeyFacts len = %d, want 2", len(o.KeyFacts))
	}
	if len(o.References) != 1 {
		t.Errorf("References len = %d, want 1", len(o.References))
	}
}

func TestParseReducedOutcomes_EmptyOptionalFields(t *testing.T) {
	raw := `[{"step_id":"s1","status":"completed","summary":"done"}]`

	outcomes, err := parseReducedOutcomes(raw)
	if err != nil {
		t.Fatalf("parseReducedOutcomes: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes len = %d, want 1", len(outcomes))
	}

	o := outcomes[0]
	if o.OpenQuestions != nil {
		t.Errorf("OpenQuestions should be nil, got %v", o.OpenQuestions)
	}
	if o.ToolCallsDigest != nil {
		t.Errorf("ToolCallsDigest should be nil, got %v", o.ToolCallsDigest)
	}
}

func TestStepOutcomesExceedBudget_BelowThreshold(t *testing.T) {
	client := &intentTestClient{}
	outcomes := []*builtin_tools.StepOutcome{
		{StepID: "s1", Summary: "short"},
		{StepID: "s2", Summary: "short"},
		{StepID: "s3", Summary: "short"},
		{StepID: "s4", Summary: "short"},
	}
	_, _, exceeded := stepOutcomesExceedBudget(client, outcomes)
	if exceeded {
		t.Error("small outcomes should not exceed budget")
	}
}

func TestStepOutcomesExceedBudget_TooFewOutcomes(t *testing.T) {
	client := &intentTestClient{}
	outcomes := []*builtin_tools.StepOutcome{
		{StepID: "s1", Summary: "a"},
		{StepID: "s2", Summary: "b"},
	}
	_, _, exceeded := stepOutcomesExceedBudget(client, outcomes)
	if exceeded {
		t.Error("outcomes <= keepLast should never exceed")
	}
}

func TestStepOutcomesExceedBudget_LargeOutcomes(t *testing.T) {
	client := &intentTestClient{}
	// 128k × 0.8 = 102,400 tokens; use enough data to reliably exceed
	big := strings.Repeat("analysis result data point ", 10000)
	outcomes := make([]*builtin_tools.StepOutcome, 5)
	for i := range outcomes {
		outcomes[i] = &builtin_tools.StepOutcome{
			StepID:  "s",
			Summary: big,
		}
	}
	_, _, exceeded := stepOutcomesExceedBudget(client, outcomes)
	if !exceeded {
		t.Error("500k+ chars should exceed 80% of 128k token budget")
	}
}

func TestReplaceStepOutcomes_WritesBack(t *testing.T) {
	agent := newMinimalAgent(t)
	agent.state.SoftReset(
		[]*builtin_tools.StepOutcome{
			{StepID: "s1", Summary: "old1"},
			{StepID: "s2", Summary: "old2"},
		},
		nil,
	)

	agent.state.ReplaceStepOutcomes([]*builtin_tools.StepOutcome{
		{StepID: "s1-reduced", Summary: "compressed"},
	})

	snap := agent.state.Snapshot()
	if len(snap.StepOutcomes) != 1 {
		t.Fatalf("StepOutcomes len = %d, want 1", len(snap.StepOutcomes))
	}
	if snap.StepOutcomes[0].StepID != "s1-reduced" {
		t.Errorf("StepID = %q, want s1-reduced", snap.StepOutcomes[0].StepID)
	}
}

func TestReplaceStepOutcomes_PreservesOtherState(t *testing.T) {
	agent := newMinimalAgent(t)
	agent.state.SoftReset(
		[]*builtin_tools.StepOutcome{{StepID: "s1"}},
		[]*builtin_tools.TimelineInput{{Content: "hello"}},
	)

	agent.state.ReplaceStepOutcomes([]*builtin_tools.StepOutcome{{StepID: "s1-new"}})

	snap := agent.state.Snapshot()
	if len(snap.InputTimeline) != 1 || snap.InputTimeline[0].Content != "hello" {
		t.Errorf("InputTimeline should be preserved, got %v", snap.InputTimeline)
	}
	if snap.Phase != builtin_tools.AgentPhasePlan {
		t.Errorf("Phase should be preserved, got %q", snap.Phase)
	}
}
