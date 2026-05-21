package react

import (
	"testing"
	"time"

	"aster/internal/ai"
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
	if len(input.PendingSteps) != 1 {
		t.Fatalf("PendingSteps len = %d, want 1", len(input.PendingSteps))
	}
	if input.PendingSteps[0].ID != "s3" || input.PendingSteps[0].Step != "输出报告" {
		t.Errorf("PendingSteps[0] = %+v, want {s3, 输出报告}", input.PendingSteps[0])
	}
}

func TestBuildIntentClassificationInput_AllOutcomesIncluded(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "s1", ShortSummary: "a", LongSummary: "detail-a", KeyFacts: []string{"fact1"}},
			{StepID: "s2", ShortSummary: "b", OpenQuestions: []string{"q1"}},
			{StepID: "s3", ShortSummary: "c"},
			{StepID: "s4", ShortSummary: "d"},
			{StepID: "s5", ShortSummary: "e", LongSummary: "detail-e", KeyFacts: []string{"fact2", "fact3"}},
		},
	}
	input := buildIntentClassificationInput(snapshot)
	if len(input.RecentOutcomes) != 5 {
		t.Errorf("RecentOutcomes len = %d, want 5 (all outcomes after reducer)", len(input.RecentOutcomes))
	}
	if input.RecentOutcomes[0].StepID != "s1" {
		t.Errorf("first outcome should be s1, got %q", input.RecentOutcomes[0].StepID)
	}
	if input.RecentOutcomes[0].LongSummary != "detail-a" {
		t.Errorf("LongSummary = %q, want 'detail-a'", input.RecentOutcomes[0].LongSummary)
	}
	if len(input.RecentOutcomes[0].KeyFacts) != 1 || input.RecentOutcomes[0].KeyFacts[0] != "fact1" {
		t.Errorf("KeyFacts = %v, want [fact1]", input.RecentOutcomes[0].KeyFacts)
	}
	if len(input.RecentOutcomes[1].OpenQuestions) != 1 || input.RecentOutcomes[1].OpenQuestions[0] != "q1" {
		t.Errorf("OpenQuestions = %v, want [q1]", input.RecentOutcomes[1].OpenQuestions)
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

func newMinimalAgent(t *testing.T) *Agent {
	t.Helper()
	client := &intentTestClient{}
	agent, err := NewReActAgent("test-apply", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	return agent
}

func TestApplyIntentClassification_Carry(t *testing.T) {
	agent := newMinimalAgent(t)
	agent.state.SoftReset(
		[]*builtin_tools.StepOutcome{{StepID: "s1", ShortSummary: "done"}},
		[]*builtin_tools.TimelineInput{{Content: "input1"}},
	)

	snapshot := agent.state.Snapshot()
	err := agent.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "carry", Reason: "user continuing previous analysis"})
	if err != nil {
		t.Fatalf("applyIntentClassification: %v", err)
	}

	state := agent.State()
	if state.Phase != builtin_tools.AgentPhasePlan {
		t.Errorf("Phase = %q, want plan", state.Phase)
	}
	if len(state.StepOutcomes) != 1 {
		t.Errorf("StepOutcomes should be preserved, got %d", len(state.StepOutcomes))
	}
	if len(state.InputTimeline) != 1 {
		t.Errorf("InputTimeline should be preserved, got %d", len(state.InputTimeline))
	}
	if state.ReplanContext == nil {
		t.Fatal("carry should set ReplanContext with reason")
	}
	if state.ReplanContext.Reason != "user continuing previous analysis" {
		t.Errorf("ReplanContext.Reason = %q, want 'user continuing previous analysis'", state.ReplanContext.Reason)
	}
	if state.ReplanContext.ReplacePending {
		t.Error("carry ReplanContext.ReplacePending should be false")
	}
	if state.ReplanContext.NextGoal != "input1" {
		t.Errorf("ReplanContext.NextGoal = %q, want 'input1'", state.ReplanContext.NextGoal)
	}
}

func TestApplyIntentClassification_Carry_EmptyReason(t *testing.T) {
	agent := newMinimalAgent(t)
	agent.state.SoftReset(
		[]*builtin_tools.StepOutcome{{StepID: "s1", ShortSummary: "done"}},
		[]*builtin_tools.TimelineInput{{Content: "go on"}},
	)

	snapshot := agent.state.Snapshot()
	err := agent.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "carry", Reason: ""})
	if err != nil {
		t.Fatalf("applyIntentClassification: %v", err)
	}

	state := agent.State()
	if state.Phase != builtin_tools.AgentPhasePlan {
		t.Errorf("Phase = %q, want plan", state.Phase)
	}
	if state.ReplanContext != nil {
		t.Error("carry with empty reason should NOT set ReplanContext")
	}
}

func TestApplyIntentClassification_Replan(t *testing.T) {
	agent := newMinimalAgent(t)
	agent.state.SoftReset(
		[]*builtin_tools.StepOutcome{{StepID: "s1", ShortSummary: "done"}},
		[]*builtin_tools.TimelineInput{{Content: "change approach", CreatedAt: time.Now()}},
	)

	snapshot := agent.state.Snapshot()
	err := agent.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "replan", Reason: "user wants different direction"})
	if err != nil {
		t.Fatalf("applyIntentClassification: %v", err)
	}

	state := agent.State()
	if state.Phase != builtin_tools.AgentPhasePlan {
		t.Errorf("Phase = %q, want plan", state.Phase)
	}
	if state.ReplanContext == nil {
		t.Fatal("ReplanContext should be set for replan")
	}
	if state.ReplanContext.Reason != "user wants different direction" {
		t.Errorf("ReplanContext.Reason = %q", state.ReplanContext.Reason)
	}
	if !state.ReplanContext.ReplacePending {
		t.Error("ReplanContext.ReplacePending should be true")
	}
	if len(state.StepOutcomes) != 1 {
		t.Errorf("StepOutcomes should be preserved, got %d", len(state.StepOutcomes))
	}
}

func TestApplyIntentClassification_ColdStart(t *testing.T) {
	agent := newMinimalAgent(t)
	agent.state.SoftReset(
		[]*builtin_tools.StepOutcome{{StepID: "s1", ShortSummary: "old"}},
		[]*builtin_tools.TimelineInput{{Content: "new unrelated task", CreatedAt: time.Now()}},
	)
	agent.history = []*ai.MsgInfo{{Role: "user", Content: "old"}}

	snapshot := agent.state.Snapshot()
	err := agent.applyIntentClassification(snapshot, intentClassificationModelOutput{Action: "cold_start", Reason: "unrelated"})
	if err != nil {
		t.Fatalf("applyIntentClassification: %v", err)
	}

	state := agent.State()
	if state.Phase != builtin_tools.AgentPhasePlan {
		t.Errorf("Phase = %q, want plan", state.Phase)
	}
	if len(state.StepOutcomes) != 0 {
		t.Errorf("StepOutcomes should be cleared, got %d", len(state.StepOutcomes))
	}
	if len(state.InputTimeline) != 1 {
		t.Errorf("InputTimeline should have latest input only, got %d", len(state.InputTimeline))
	}
	if state.InputTimeline[0].Content != "new unrelated task" {
		t.Errorf("InputTimeline[0].Content = %q, want 'new unrelated task'", state.InputTimeline[0].Content)
	}
	if len(agent.history) != 1 || agent.history[0].Content != "new unrelated task" {
		t.Errorf("history should be reset to latest input only")
	}
}
