package react

import (
	"testing"
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
