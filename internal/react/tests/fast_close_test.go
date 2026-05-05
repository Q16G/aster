package react_test

import (
	. "aster/internal/react"
	"context"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

func TestExecute_SingleStepFastClose(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{content: `{"mode":"react_run","intent_summary":"简单总结","complexity":"simple","matched_capabilities":[],"reply_hint":"","confidence":0.9}`},
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "c1", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "这是总结内容",
						"display_result": "这是总结内容",
						"result":         "这是总结内容",
					}),
				},
			},
		},
	}
	planner := &intentPreludeCapturePlanner{
		result: &builtin_tools.TaskPlannerResult{
			NeedsPlanning: false,
			Plan:          []*builtin_tools.PlanItem{{ID: "s1", Step: "总结", Status: builtin_tools.PlanStepPending}},
		},
	}

	agent, err := NewReActAgent("fast-close-test", client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	result, err := agent.Execute(context.Background(), "请做个��单总结")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.Result != "这是总结内容" {
		t.Fatalf("unexpected result: %q", result.Result)
	}
	if len(planner.inputs) != 0 {
		t.Fatalf("expected planner bypassed, got %d calls", len(planner.inputs))
	}
	if client.calls != 2 {
		t.Fatalf("expected 2 model calls (intent + step), got %d", client.calls)
	}
	snap := agent.State()
	if snap.FinalAnswer == nil {
		t.Fatalf("expected final answer")
	}
	if snap.FinalAnswer.Source != "fast_close" {
		t.Fatalf("expected fast_close source, got %q", snap.FinalAnswer.Source)
	}
	if snap.Status != builtin_tools.TaskStatusCompleted {
		t.Fatalf("expected completed, got %q", snap.Status)
	}
}

func TestExecute_SubAgentDoesNotFastClose(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "c1", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "sub result",
						"display_result": "sub result",
						"result":         "sub result",
					}),
				},
			},
			{content: `{"status_summary":"ok","step_short_summary":"ok","step_long_summary":"ok","key_facts":[],"open_questions":[]}`},
			{content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"sub-final","references":[]}`},
		},
	}
	planner := &intentPreludeCapturePlanner{
		result: &builtin_tools.TaskPlannerResult{
			NeedsPlanning: false,
			Plan:          []*builtin_tools.PlanItem{{ID: "s1", Step: "sub task", Status: builtin_tools.PlanStepPending}},
		},
	}

	agent, err := NewReActAgent("sub-agent-test", client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	result, err := agent.Execute(context.Background(), "执行子任务", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.Result != "sub-final" {
		t.Fatalf("expected sub-final, got %q", result.Result)
	}
	if client.calls != 3 {
		t.Fatalf("expected 3 model calls for sub-agent (no fast close), got %d", client.calls)
	}
}
