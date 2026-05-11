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
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "c1", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":            "completed",
						"summary":           "这是总结内容",
						"display_result":    "这是总结内容",
						"result":            "这是总结内容",
						"status_summary":    "已完成总结",
						"short_summary":     "这是总结内容",
						"long_summary":      "这是总结内容",
						"key_facts":         []string{"完成总结"},
						"open_questions":    []string{},
						"tool_calls_digest": []string{},
					}),
				},
			},
		},
	}
	planner := &executeModelStaticPlanner{
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

	result, err := agent.Execute(context.Background(), "请做个简单总结")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.Result != "这是总结内容" {
		t.Fatalf("unexpected result: %q", result.Result)
	}
	if client.calls != 1 {
		t.Fatalf("expected 1 model call (step only, no intent), got %d", client.calls)
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
						"status":            "completed",
						"summary":           "sub result",
						"display_result":    "sub result",
						"result":            "sub result",
						"status_summary":    "ok",
						"short_summary":     "sub result",
						"long_summary":      "sub result",
						"key_facts":         []string{},
						"open_questions":    []string{},
						"tool_calls_digest": []string{},
					}),
				},
			},
			{content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"sub-final","references":[]}`},
		},
	}
	planner := &executeModelStaticPlanner{
		result: &builtin_tools.TaskPlannerResult{
			NeedsPlanning: true,
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

	result, err := agent.Execute(context.Background(), "执行子任务")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.Result != "sub-final" {
		t.Fatalf("expected sub-final, got %q", result.Result)
	}
	if client.calls != 2 {
		t.Fatalf("expected 2 model calls (step + final_answer), got %d", client.calls)
	}
}

func TestExecute_FastCloseLatestStepResultKeepsDisplayResultForFinalAnswer(t *testing.T) {
	stepDisplay := "## 标准报告\n\n- 已生成 Markdown 审计报告"
	stepResult := `{"report_location":"shared/security_audit_report.md"}`
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "c1", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":            "completed",
						"summary":           "已生成审计报告",
						"display_result":    stepDisplay,
						"result":            stepResult,
						"status_summary":    "已生成审计报告",
						"short_summary":     "已生成审计报告",
						"long_summary":      "已生成 Markdown 审计报告",
						"key_facts":         []string{"审计报告已生成"},
						"open_questions":    []string{},
						"tool_calls_digest": []string{},
					}),
				},
			},
		},
	}
	planner := &executeModelStaticPlanner{
		result: &builtin_tools.TaskPlannerResult{
			NeedsPlanning: false,
			Plan:          []*builtin_tools.PlanItem{{ID: "s1", Step: "输出结果", Status: builtin_tools.PlanStepPending}},
		},
	}

	agent, err := NewReActAgent("fast-close-step-result", client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	result, err := agent.Execute(context.Background(), "生成简单结果", WithResultSource(ResultSourceLatestStepResult))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.Result != stepResult {
		t.Fatalf("expected machine result to stay on step result, got %q", result.Result)
	}

	snap := agent.State()
	if snap.FinalAnswer == nil {
		t.Fatal("expected final answer")
	}
	if snap.FinalAnswer.Content != stepDisplay {
		t.Fatalf("expected display result to remain final answer content, got %q", snap.FinalAnswer.Content)
	}
	if snap.FinalAnswer.Source != "fast_close" {
		t.Fatalf("expected fast_close source, got %q", snap.FinalAnswer.Source)
	}
}
