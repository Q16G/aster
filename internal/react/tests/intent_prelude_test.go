package react_test

import (
	. "aster/internal/react"
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type intentPreludeCapturePlanner struct {
	result *builtin_tools.TaskPlannerResult
	err    error
	inputs []string
}

func (p *intentPreludeCapturePlanner) Plan(ctx context.Context, input string) (*builtin_tools.TaskPlannerResult, error) {
	_ = ctx
	p.inputs = append(p.inputs, input)
	return p.result, p.err
}

func TestParseIntentDecisionOutput_UsesJSONExtractorThenUnmarshal(t *testing.T) {
	raw := "前言\n```json\n{\"mode\":\"simple_reply\",\"intent_summary\":\"简单问候\",\"complexity\":\"simple\",\"matched_capabilities\":[],\"reply_hint\":\"直接回复\",\"confidence\":0.9}\n```\n后记"
	out, err := ParseIntentDecisionOutput(raw)
	if err != nil {
		t.Fatalf("parseIntentDecisionOutput failed: %v", err)
	}
	if out.Mode != IntentModeSimpleReply {
		t.Fatalf("expected simple_reply, got %q", out.Mode)
	}
	if out.IntentSummary != "简单问候" {
		t.Fatalf("unexpected intent summary: %q", out.IntentSummary)
	}
}

func TestExecute_SimpleReplyBypassesPlannerAndScheduler(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{content: "你好，我可以帮你进行代码安全审计。"},
		},
	}
	planner := &intentPreludeCapturePlanner{
		result: &builtin_tools.TaskPlannerResult{
			NeedsPlanning: false,
			Plan: []*builtin_tools.PlanItem{
				{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
			},
		},
	}

	agent, err := NewReActAgent(
		"intent-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "你好")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success result, got %#v", runResult)
	}
	if runResult.Result != "你好，我可以帮你进行代码安全审计。" {
		t.Fatalf("unexpected simple reply result: %q", runResult.Result)
	}
	if len(planner.inputs) != 0 {
		t.Fatalf("expected planner to be bypassed, got inputs=%v", planner.inputs)
	}

	snapshot := agent.State()
	if snapshot.FinalAnswer == nil || snapshot.FinalAnswer.Source != "simple_reply" {
		t.Fatalf("expected simple_reply final answer source, got %+v", snapshot.FinalAnswer)
	}
}

func TestExecute_HiddenIntentCanRouteToSimpleReply(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				content: "```json\n{\"mode\":\"simple_reply\",\"intent_summary\":\"用户要一个轻量说明\",\"complexity\":\"simple\",\"matched_capabilities\":[],\"reply_hint\":\"直接简短说明\",\"confidence\":0.88}\n```",
			},
			{
				content: "我是 SAST 安全分析助手，可以帮你分析代码和排查漏洞。",
			},
		},
	}
	planner := &intentPreludeCapturePlanner{}

	agent, err := NewReActAgent(
		"intent-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "请简单介绍一下自己")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success result, got %#v", runResult)
	}
	if !strings.Contains(runResult.Result, "SAST 安全分析助手") {
		t.Fatalf("unexpected simple reply result: %q", runResult.Result)
	}
	if len(planner.inputs) != 0 {
		t.Fatalf("expected planner to be bypassed, got inputs=%v", planner.inputs)
	}
	if client.calls != 2 {
		t.Fatalf("expected one intent call and one simple reply call, got %d", client.calls)
	}
}

func TestExecute_IntentRetryExhaustedFallsBackToReactRun(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{content: "not-json-1"},
			{content: "not-json-2"},
			{content: "still-not-json"},
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok",
						"display_result": "step ok",
						"result":         "step ok",
					}),
				},
			},
			{
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"react-run-final","references":[]}`,
			},
		},
	}
	planner := &intentPreludeCapturePlanner{
		result: &builtin_tools.TaskPlannerResult{
			NeedsPlanning: false,
			Plan: []*builtin_tools.PlanItem{
				{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
			},
		},
	}

	agent, err := NewReActAgent(
		"intent-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "继续")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success result, got %#v", runResult)
	}
	if runResult.Result != "react-run-final" {
		t.Fatalf("unexpected fallback final result: %q", runResult.Result)
	}
	if len(planner.inputs) != 1 {
		t.Fatalf("expected fallback to reach planner once, got inputs=%v", planner.inputs)
	}
	if client.calls != 6 {
		t.Fatalf("expected 3 intent retries + scheduler calls, got %d", client.calls)
	}
}

func TestExecute_WithSkipIntentPreludeSkipsIntentRecognition(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok",
						"display_result": "step ok",
						"result":         "step ok",
					}),
				},
			},
			{
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"skip-intent-final","references":[]}`,
			},
		},
	}
	planner := &intentPreludeCapturePlanner{
		result: &builtin_tools.TaskPlannerResult{
			NeedsPlanning: false,
			Plan: []*builtin_tools.PlanItem{
				{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
			},
		},
	}

	agent, err := NewReActAgent(
		"intent-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "继续", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success result, got %#v", runResult)
	}
	if runResult.Result != "skip-intent-final" {
		t.Fatalf("unexpected final result: %q", runResult.Result)
	}
	if len(planner.inputs) != 1 {
		t.Fatalf("expected planner to run once, got inputs=%v", planner.inputs)
	}
	if client.calls != 3 {
		t.Fatalf("expected scheduler calls only, got %d", client.calls)
	}
}
