package react

import (
	"testing"
	"time"

	"aster/internal/builtin_tools"
)

func TestParseIntentClassificationOutput_ValidJSON(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		expect string
	}{
		{"carry", `{"action":"carry","reason":"continue"}`, "carry"},
		{"replan", `{"action":"replan","reason":"switch approach"}`, "replan"},
		{"cold_start", `{"action":"cold_start","reason":"unrelated"}`, "cold_start"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := parseIntentClassificationOutput(tt.raw)
			if out.Action != tt.expect {
				t.Errorf("Action = %q, want %q", out.Action, tt.expect)
			}
		})
	}
}

func TestParseIntentClassificationOutput_InvalidJSON_FallbackCarry(t *testing.T) {
	out := parseIntentClassificationOutput("this is not json at all")
	if out.Action != "carry" {
		t.Errorf("expected fallback carry, got %q", out.Action)
	}
}

func TestParseIntentClassificationOutput_Empty_FallbackCarry(t *testing.T) {
	out := parseIntentClassificationOutput("")
	if out.Action != "carry" {
		t.Errorf("expected fallback carry for empty, got %q", out.Action)
	}
}

func TestParseIntentClassificationOutput_WrappedJSON(t *testing.T) {
	raw := `Here is my analysis:\n{"action":"replan","reason":"user wants new direction"}\nDone.`
	out := parseIntentClassificationOutput(raw)
	if out.Action != "replan" {
		t.Errorf("expected replan from wrapped JSON, got %q", out.Action)
	}
}

func TestParseIntentClassificationOutput_InvalidAction_FallbackCarry(t *testing.T) {
	out := parseIntentClassificationOutput(`{"action":"unknown","reason":"test"}`)
	if out.Action != "carry" {
		t.Errorf("expected fallback carry for invalid action, got %q", out.Action)
	}
}

func TestBuildIntentClassificationInput(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{
		CurrentGoal: "分析 main.go",
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "读取文件", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "分析漏洞", Status: builtin_tools.PlanStepCompleted},
			{ID: "s3", Step: "输出报告", Status: builtin_tools.PlanStepPending},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "s1", ShortSummary: "读取完成", Status: builtin_tools.StepOutcomeCompleted},
			{StepID: "s2", ShortSummary: "发现3个漏洞", Status: builtin_tools.StepOutcomeCompleted},
		},
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "帮我分析main.go", CreatedAt: time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)},
			{Content: "再看看utils.go", CreatedAt: time.Date(2025, 1, 1, 10, 5, 0, 0, time.UTC)},
		},
	}

	input := buildIntentClassificationInput(snapshot)

	if input.PreviousGoal != "分析 main.go" {
		t.Errorf("PreviousGoal = %q", input.PreviousGoal)
	}
	if input.CompletedCount != 2 {
		t.Errorf("CompletedCount = %d, want 2", input.CompletedCount)
	}
	if input.TotalCount != 3 {
		t.Errorf("TotalCount = %d, want 3", input.TotalCount)
	}
	if len(input.RecentOutcomes) != 2 {
		t.Errorf("RecentOutcomes len = %d, want 2", len(input.RecentOutcomes))
	}
	if len(input.InputTimeline) != 2 {
		t.Errorf("InputTimeline len = %d, want 2", len(input.InputTimeline))
	}
	if input.InputTimeline[0].Content != "帮我分析main.go" {
		t.Errorf("InputTimeline[0].Content = %q", input.InputTimeline[0].Content)
	}
}

func TestBuildIntentClassificationInput_OutcomesTruncatedToThree(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "s1", ShortSummary: "a"},
			{StepID: "s2", ShortSummary: "b"},
			{StepID: "s3", ShortSummary: "c"},
			{StepID: "s4", ShortSummary: "d"},
			{StepID: "s5", ShortSummary: "e"},
		},
	}
	input := buildIntentClassificationInput(snapshot)
	if len(input.RecentOutcomes) != 3 {
		t.Errorf("RecentOutcomes len = %d, want 3 (last 3)", len(input.RecentOutcomes))
	}
	if input.RecentOutcomes[0].StepID != "s3" {
		t.Errorf("first outcome should be s3, got %q", input.RecentOutcomes[0].StepID)
	}
}

func TestIsValidIntentAction(t *testing.T) {
	valid := []string{"carry", "replan", "cold_start", " CARRY ", "Cold_Start"}
	for _, v := range valid {
		if !isValidIntentAction(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}
	invalid := []string{"", "unknown", "resume", "start"}
	for _, v := range invalid {
		if isValidIntentAction(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}
