package react

import (
	"strings"
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
	if got := strings.Count(input.RecentOutcomes, "**"); got != 4 { // 每条产出块首行 **id** 一对
		t.Errorf("RecentOutcomes 应含 2 条产出块，got:\n%s", input.RecentOutcomes)
	}
	if got := strings.Count(input.InputTimeline, "\n") + 1; got != 2 {
		t.Errorf("InputTimeline 应为 2 行，got:\n%s", input.InputTimeline)
	}
	if !strings.Contains(input.InputTimeline, "帮我分析main.go") {
		t.Errorf("InputTimeline 应含首条输入，got:\n%s", input.InputTimeline)
	}
	if input.PendingSteps != "- s3: 输出报告" {
		t.Errorf("PendingSteps = %q, want %q", input.PendingSteps, "- s3: 输出报告")
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
	blocks := strings.Split(input.RecentOutcomes, "\n\n")
	if len(blocks) != 5 {
		t.Errorf("RecentOutcomes 块数 = %d, want 5 (all outcomes after reducer)", len(blocks))
	}
	if !strings.HasPrefix(blocks[0], "**s1**") {
		t.Errorf("首块应为 s1，got %q", blocks[0])
	}
	if !strings.Contains(blocks[0], "详情：detail-a") {
		t.Errorf("首块应含 LongSummary 详情行，got %q", blocks[0])
	}
	if !strings.Contains(blocks[0], "关键发现：\n  · fact1") {
		t.Errorf("首块应含 KeyFacts，got %q", blocks[0])
	}
	if !strings.Contains(blocks[1], "遗留问题：\n  · q1") {
		t.Errorf("第二块应含 OpenQuestions，got %q", blocks[1])
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
	if state.ReplanContext != nil {
		t.Error("carry should not set internal ReplanContext (intent recovery uses IntentContext)")
	}
	if state.IntentContext == nil {
		t.Fatal("carry should set IntentContext")
	}
	if state.IntentContext.Action != "carry" {
		t.Errorf("IntentContext.Action = %q, want 'carry'", state.IntentContext.Action)
	}
	if state.IntentContext.Reason != "user continuing previous analysis" {
		t.Errorf("IntentContext.Reason = %q, want 'user continuing previous analysis'", state.IntentContext.Reason)
	}
	if state.IntentContext.LatestInput != "input1" {
		t.Errorf("IntentContext.LatestInput = %q, want 'input1'", state.IntentContext.LatestInput)
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
		t.Error("carry should never set internal ReplanContext")
	}
	// carry 恒设 IntentContext（承接用户输入），即便 reason 为空。
	if state.IntentContext == nil || state.IntentContext.Action != "carry" {
		t.Fatalf("carry should set IntentContext{Action:carry}, got %+v", state.IntentContext)
	}
	if state.IntentContext.LatestInput != "go on" {
		t.Errorf("IntentContext.LatestInput = %q, want 'go on'", state.IntentContext.LatestInput)
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
	if state.ReplanContext != nil {
		t.Error("replan intent should not set internal ReplanContext (uses IntentContext)")
	}
	if state.IntentContext == nil {
		t.Fatal("IntentContext should be set for replan")
	}
	if state.IntentContext.Action != "replan" {
		t.Errorf("IntentContext.Action = %q, want 'replan'", state.IntentContext.Action)
	}
	if state.IntentContext.Reason != "user wants different direction" {
		t.Errorf("IntentContext.Reason = %q", state.IntentContext.Reason)
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
