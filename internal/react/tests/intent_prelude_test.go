package react_test

import (
	. "aster/internal/react"
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

func TestExecute_LooseSimplePatternRoutesToSimpleReply(t *testing.T) {
	inputs := []string{
		"你是什么 agent",
		"你是谁啊",
		"你能做什么呢",
		"hello agent",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			client := &executeModelTestClient{
				replies: []executeModelReply{
					{content: "我是你的助手。"},
				},
			}
			planner := &intentPreludeCapturePlanner{
				result: &builtin_tools.TaskPlannerResult{
					NeedsPlanning: false,
					Plan:          []*builtin_tools.PlanItem{{ID: "s1", Step: "x", Status: builtin_tools.PlanStepPending}},
				},
			}
			agent, err := NewReActAgent("test-loose", client, WithEmitter(NewDummyEmitter()), WithTaskPlanner(planner))
			if err != nil {
				t.Fatalf("NewReActAgent: %v", err)
			}
			result, err := agent.Execute(context.Background(), input)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !result.Success {
				t.Fatalf("expected success, got %+v", result)
			}
			if len(planner.inputs) != 0 {
				t.Fatalf("expected planner bypassed, got %d calls", len(planner.inputs))
			}
			snap := agent.State()
			if snap.FinalAnswer == nil || snap.FinalAnswer.Source != "simple_reply" {
				t.Fatalf("expected simple_reply source, got %+v", snap.FinalAnswer)
			}
		})
	}
}

func TestExecute_LoosePatternRejectsLongInput(t *testing.T) {
	longSuffix := strings.Repeat("请帮我做安全审计", 20)
	input := "你是什么 " + longSuffix

	client := &executeModelTestClient{
		replies: []executeModelReply{
			{content: `{"mode":"react_run","intent_summary":"complex","complexity":"complex","matched_capabilities":[],"reply_hint":"","confidence":0.9}`},
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "c1", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status": "completed", "summary": "done", "display_result": "done", "result": "done",
					}),
				},
			},
			{content: `{"status_summary":"ok","step_short_summary":"ok","step_long_summary":"ok","key_facts":[],"open_questions":[]}`},
			{content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"final","references":[]}`},
		},
	}
	planner := &intentPreludeCapturePlanner{
		result: &builtin_tools.TaskPlannerResult{
			NeedsPlanning: false,
			Plan:          []*builtin_tools.PlanItem{{ID: "s1", Step: "x", Status: builtin_tools.PlanStepPending}},
		},
	}
	agent, err := NewReActAgent("test-reject", client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	result, err := agent.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success")
	}
	if len(planner.inputs) == 0 {
		t.Fatalf("expected planner to be called for long input")
	}
}

func TestExecute_AgentKeywordNoLongerTriggersComplexPattern(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{content: `{"mode":"simple_reply","intent_summary":"能力询问","complexity":"simple","matched_capabilities":[],"reply_hint":"简洁回答","confidence":0.85}`},
			{content: "我是一个 agent 助手。"},
		},
	}
	planner := &intentPreludeCapturePlanner{}
	agent, err := NewReActAgent("test-agent-kw", client,
		WithEmitter(NewDummyEmitter()),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	result, err := agent.Execute(context.Background(), "what is an agent")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success")
	}
	if len(planner.inputs) != 0 {
		t.Fatalf("expected planner bypassed since LLM returned simple_reply, got %d", len(planner.inputs))
	}
}

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
