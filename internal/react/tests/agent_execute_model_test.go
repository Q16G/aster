package react_test

import (
	openai "aster/internal/ai/openai"
	. "aster/internal/react"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/runtimelog"
)

type executeModelReply struct {
	content   string
	toolCalls []*ai.FunctionTool
}

type executeModelTestClient struct {
	replies      []executeModelReply
	calls        int
	modelContext ai.ModelContextInfo
}

type executeModelStaticPlanner struct {
	result *builtin_tools.TaskPlannerResult
	err    error
}

type executeModelSequencePlanner struct {
	results []*builtin_tools.TaskPlannerResult
	err     error
	calls   int
	inputs  []string
}

type noopHistoryCompressor struct{}

func (p *executeModelStaticPlanner) Plan(ctx context.Context, input string) (*builtin_tools.TaskPlannerResult, error) {
	_ = ctx
	_ = input
	return p.result, p.err
}

func (p *executeModelSequencePlanner) Plan(ctx context.Context, input string) (*builtin_tools.TaskPlannerResult, error) {
	_ = ctx
	p.inputs = append(p.inputs, input)
	if p.err != nil {
		return nil, p.err
	}
	if len(p.results) == 0 {
		return &builtin_tools.TaskPlannerResult{}, nil
	}
	idx := p.calls
	if idx >= len(p.results) {
		idx = len(p.results) - 1
	}
	p.calls++
	return p.results[idx], nil
}

func (c *noopHistoryCompressor) Compress(ctx context.Context, aiClient ai.ChatClient, instruction string, history []*ai.MsgInfo) (*HistoryCompactionResult, error) {
	_ = ctx
	_ = aiClient
	_ = instruction
	return &HistoryCompactionResult{
		History:     history,
		State:       CompactionStateNormal,
		CanContinue: true,
	}, nil
}

func (c *executeModelTestClient) nextReply() executeModelReply {
	if len(c.replies) == 0 {
		return executeModelReply{}
	}
	idx := c.calls
	if idx >= len(c.replies) {
		idx = len(c.replies) - 1
	}
	c.calls++
	return c.replies[idx]
}

func (c *executeModelTestClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	return c.nextReply().content, nil
}

func (c *executeModelTestClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	reply := c.nextReply()
	return []*ai.ChatChoices{
		{
			Message: &ai.MsgInfo{
				Role:      "assistant",
				Content:   reply.content,
				ToolCalls: reply.toolCalls,
			},
		},
	}, nil
}

func (c *executeModelTestClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	return c.nextReply().content, nil
}

func (c *executeModelTestClient) ModelContextInfo() ai.ModelContextInfo {
	return c.modelContext
}

type executeModelTestFactory struct {
	clients map[string]ai.ChatClient
	calls   []string
}

func (f *executeModelTestFactory) CreateClient(modelID string) ai.ChatClient {
	f.calls = append(f.calls, "create:"+modelID)
	if f.clients == nil {
		return nil
	}
	return f.clients[modelID]
}

func (f *executeModelTestFactory) DefaultClient() ai.ChatClient {
	if f.clients == nil {
		return nil
	}
	return f.clients["default"]
}

func (f *executeModelTestFactory) CreateClientContext(ctx context.Context, modelID string) (ai.ChatClient, error) {
	f.calls = append(f.calls, "context:"+modelID)
	if f.clients == nil {
		return nil, nil
	}
	return f.clients[modelID], nil
}

func TestExecute_UsesConfiguredModel(t *testing.T) {
	primaryClient := &executeModelTestClient{
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
				// final_answer phase (step_replan fast path skips LLM)
				content: `{"is_complete":true,"status":"completed","reason":"所有步骤已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"primary-final-answer","references":[]}`,
			},
		},
	}
	secondaryClient := &executeModelTestClient{
		replies: []executeModelReply{
			{
				content: "secondary-reply",
			},
		},
	}
	factory := &executeModelTestFactory{
		clients: map[string]ai.ChatClient{
			"primary":   primaryClient,
			"secondary": secondaryClient,
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		primaryClient,
		WithEmitter(NewDummyEmitter()),
		WithAIClientFactory(factory),
		WithModelID("primary"),
		WithMaxIterations(5),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "primary-final-answer" {
		t.Fatalf("expected reply from configured model client, got %q", runResult.Result)
	}
	if len(factory.calls) == 0 {
		t.Fatalf("expected model factory to be called")
	}
	if factory.calls[0] != "context:primary" {
		t.Fatalf("expected configured model_id=primary, got %q", factory.calls[0])
	}
}

func TestExecute_ProducesFinalAnswerFromFinalAnswerPhase(t *testing.T) {
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
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"plain-final-answer","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "plain-final-answer" {
		t.Fatalf("expected final answer result, got %q", runResult.Result)
	}
}

func TestExecute_AllowsUnknownTopLevelFieldsInFinalAnswer(t *testing.T) {
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
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"plain-final-answer","references":[],"extra_field":"keep-compatible"}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "plain-final-answer" {
		t.Fatalf("expected final answer result, got %q", runResult.Result)
	}
}

func TestExecute_ReturnsLatestStepResultWhenConfigured(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok",
						"display_result": "step ok",
						"result":         "canonical-step-result",
					}),
				},
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"human-final-answer","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude(), WithResultSource(ResultSourceLatestStepResult))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "canonical-step-result" {
		t.Fatalf("expected latest step result, got %q", runResult.Result)
	}
}

func TestExecute_LatestStepResultModeFallsToFinalAnswerWhenStepResultMissing(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok",
						"display_result": "step ok",
						"result":         "",
					}),
				},
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"human-final-answer","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude(), WithResultSource(ResultSourceLatestStepResult))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success with final_answer fallback when step result missing, got %#v", runResult)
	}
	if runResult.Result != "human-final-answer" {
		t.Fatalf("expected final_answer content as fallback result, got %q", runResult.Result)
	}
}

func TestExecute_LatestStepResultModeSucceedsEvenWhenFinalAnswerSaysFailed(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "found 3 vulnerabilities",
						"display_result": "security scan complete",
						"result":         `{"total_findings":3,"findings":[{"id":"vuln-1"}]}`,
					}),
				},
			},
			{
				content: `{"is_complete":true,"status":"failed","reason":"发现安全漏洞","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"发现漏洞","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行安全扫描", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "scan code", WithSkipIntentPrelude(), WithResultSource(ResultSourceLatestStepResult))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success when step result exists (even if final_answer model says failed), got %#v", runResult)
	}
	if !strings.Contains(runResult.Result, "total_findings") {
		t.Fatalf("expected step result content, got %q", runResult.Result)
	}
}

func TestExecute_LatestStepResultModeKeepsMarkdownFinalAnswerForDisplay(t *testing.T) {
	stepResult := `{"findings_summary":{"confirmed_critical":3},"report_location":"shared/security_audit_report.md"}`
	markdownReport := "## YP-34944 项目安全审计报告\n\n### 执行摘要\n- 已确认 7 个漏洞\n- 标准报告已写入 `shared/security_audit_report.md`"
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "安全审计完成",
						"display_result": "审计完成",
						"result":         stepResult,
					}),
				},
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"审计完成","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":` + strconv.Quote(markdownReport) + `,"references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行安全审计", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "scan code", WithSkipIntentPrelude(), WithResultSource(ResultSourceLatestStepResult))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != stepResult {
		t.Fatalf("expected step result for machine consumption, got %q", runResult.Result)
	}

	snapshot := agent.State()
	if snapshot.FinalAnswer == nil {
		t.Fatal("expected final answer snapshot")
	}
	if strings.TrimSpace(snapshot.FinalAnswer.Content) != markdownReport {
		t.Fatalf("expected markdown final answer for display, got %q", snapshot.FinalAnswer.Content)
	}
	if snapshot.FinalAnswer.Source != "final_assessment" {
		t.Fatalf("expected final_assessment source, got %q", snapshot.FinalAnswer.Source)
	}
}

// contextCancelClient wraps an executeModelTestClient and cancels the context
// after a specified number of model calls (across ChatEx and ChatText).
type contextCancelClient struct {
	inner       *executeModelTestClient
	cancelFunc  context.CancelFunc
	cancelAfter int
	calls       int
}

func (c *contextCancelClient) afterCall() {
	c.calls++
	if c.calls >= c.cancelAfter && c.cancelFunc != nil {
		c.cancelFunc()
	}
}

func (c *contextCancelClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	result, err := c.inner.Chat(ctx, info, tools...)
	c.afterCall()
	return result, err
}

func (c *contextCancelClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	result, err := c.inner.ChatEx(ctx, infos, tools...)
	c.afterCall()
	return result, err
}

func (c *contextCancelClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	result, err := c.inner.ChatText(ctx, text, tools...)
	c.afterCall()
	return result, err
}

func (c *contextCancelClient) ModelContextInfo() ai.ModelContextInfo {
	return c.inner.ModelContextInfo()
}

// errorOnCallClient wraps an executeModelTestClient and returns an error on
// a specific model call number (across ChatEx and ChatText).
type errorOnCallClient struct {
	inner   *executeModelTestClient
	errorAt int
	calls   int
	err     error
}

func (c *errorOnCallClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	c.calls++
	if c.calls >= c.errorAt {
		if c.err != nil {
			return "", c.err
		}
		return "", context.DeadlineExceeded
	}
	return c.inner.Chat(ctx, info, tools...)
}

func (c *errorOnCallClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.calls++
	if c.calls >= c.errorAt {
		if c.err != nil {
			return nil, c.err
		}
		return nil, context.DeadlineExceeded
	}
	return c.inner.ChatEx(ctx, infos, tools...)
}

func (c *errorOnCallClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	c.calls++
	if c.calls >= c.errorAt {
		if c.err != nil {
			return "", c.err
		}
		return "", context.DeadlineExceeded
	}
	return c.inner.ChatText(ctx, text, tools...)
}

func (c *errorOnCallClient) ModelContextInfo() ai.ModelContextInfo {
	return c.inner.ModelContextInfo()
}

func TestExecute_LatestStepResultModeCanceledReturnsError(t *testing.T) {
	inner := &executeModelTestClient{
		replies: []executeModelReply{
			// call 1 — step phase: tool call with result
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "found 3 vulnerabilities",
						"display_result": "security scan complete",
						"result":         `{"total_findings":3,"findings":[{"id":"vuln-1"}]}`,
					}),
				},
			},
			// call 2 — final_answer phase (step_replan fast path skips LLM; may not be reached if canceled)
			{
				content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"done","references":[]}`,
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	wrapper := &contextCancelClient{
		inner:       inner,
		cancelFunc:  cancel,
		cancelAfter: 1, // cancel after the first ChatEx call (step phase)
	}

	agent, err := NewReActAgent(
		"model-agent",
		wrapper,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(10),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行安全扫描", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(ctx, "scan code", WithSkipIntentPrelude(), WithResultSource(ResultSourceLatestStepResult))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil {
		t.Fatal("expected non-nil result")
	}
	if runResult.Success {
		t.Fatalf("expected failure when context is canceled, but got Success: true with result=%q", runResult.Result)
	}
	if runResult.Error == "" {
		t.Fatal("expected non-empty error message for canceled task")
	}
}

func TestExecute_LatestStepResultModeRuntimeFailedReturnsError(t *testing.T) {
	// Use MaxIterations=1 to trigger runtime-forced failure (max iterations reached).
	// The step phase completes with a non-empty result, but the scheduler hits the
	// iteration limit before completing all phases, triggering EnterFinalAnswer(Failed).
	client := &executeModelTestClient{
		replies: []executeModelReply{
			// call 0 — step phase: tool call with result
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "found 3 vulnerabilities",
						"display_result": "security scan complete",
						"result":         `{"total_findings":3,"findings":[{"id":"vuln-1"}]}`,
					}),
				},
			},
			// call 1+ — final_answer phase (after max iterations hit)
			{
				content: `{"is_complete":true,"status":"failed","reason":"max iterations","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"达到最大迭代次数","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(1),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行安全扫描", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "scan code", WithSkipIntentPrelude(), WithResultSource(ResultSourceLatestStepResult))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil {
		t.Fatal("expected non-nil result")
	}
	if runResult.Success {
		t.Fatalf("expected failure when max iterations reached, but got Success: true with result=%q", runResult.Result)
	}
	if runResult.Error == "" {
		t.Fatal("expected non-empty error message for runtime-forced failure")
	}
}

func TestExecute_LatestStepResultModeExternalInterruptKeepsReadableFinalAnswer(t *testing.T) {
	stepResult := `{"total_findings":3,"findings":[{"id":"vuln-1"}]}`
	inner := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step1", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "found 3 vulnerabilities",
						"display_result": "security scan complete",
						"result":         stepResult,
					}),
				},
			},
		},
	}
	client := &errorOnCallClient{
		inner:   inner,
		errorAt: 2,
		err: &openai.HTTPError{
			StatusCode: 429,
			Body:       `{"error":{"message":"Error from provider: insufficient quota","code":"insufficient_quota","type":"insufficient_quota"}}`,
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(8),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行安全扫描", Status: builtin_tools.PlanStepPending},
					{ID: "step-2", Step: "执行 SyntaxFlow 数据流验证", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "scan code", WithSkipIntentPrelude(), WithResultSource(ResultSourceLatestStepResult))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected partial delivery success, got %#v", runResult)
	}
	if strings.Contains(runResult.Result, `"total_findings"`) {
		t.Fatalf("expected readable final answer instead of raw step result, got %q", runResult.Result)
	}
	if !strings.Contains(runResult.Result, "当前可交付结果") || !strings.Contains(runResult.Result, "中断原因") {
		t.Fatalf("expected interrupted final report, got %q", runResult.Result)
	}

	snapshot := agent.State()
	if snapshot.ExternalInterrupt == nil {
		t.Fatal("expected external interrupt snapshot")
	}
	if snapshot.ExternalInterrupt.ReasonCode != openai.RetryReasonProviderQuota {
		t.Fatalf("expected provider quota reason, got %#v", snapshot.ExternalInterrupt)
	}
	if snapshot.FinalAnswer == nil || !strings.Contains(snapshot.FinalAnswer.Content, "未完成的步骤") {
		t.Fatalf("expected readable final answer content, got %#v", snapshot.FinalAnswer)
	}
}

func TestExecute_WritesPhaseLogsToTerminal(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

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
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"plain-final-answer","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	if _, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "\"event\":\"phase_selected\"") {
		t.Fatalf("expected phase_selected terminal log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"step_replan_completed\"") {
		t.Fatalf("expected step_replan_completed terminal log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"final_assessment_written\"") {
		t.Fatalf("expected final_assessment_written terminal log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"final_answer_written\"") {
		t.Fatalf("expected final_answer_written terminal log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"final_answer_history_persisted\"") {
		t.Fatalf("expected final_answer_history_persisted terminal log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"final_answer_completed\"") {
		t.Fatalf("expected final_answer_completed terminal log, got %s", out)
	}
}

func TestExecute_SelectsPlanPhaseBeforeStepByDefault(t *testing.T) {
	var selectedPhases []string
	emitter := NewEmitter("phase-order-session", "phase-order-agent", func(event *AgentOutputEvent) error {
		if event == nil || event.Type != EventTypeLog || event.Payload == nil {
			return nil
		}
		if strings.TrimSpace(phaseStringFromAny(event.Payload["event"])) != "phase_selected" {
			return nil
		}
		selectedPhases = append(selectedPhases, strings.TrimSpace(phaseStringFromAny(event.Payload["selected_phase"])))
		return nil
	})

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
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"done","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"phase-order-agent",
		client,
		WithEmitter(emitter),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	if _, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(selectedPhases) < 2 {
		t.Fatalf("expected at least plan and step phase selections, got %v", selectedPhases)
	}
	if selectedPhases[0] != string(builtin_tools.AgentPhasePlan) {
		t.Fatalf("expected first selected phase %q, got %v", builtin_tools.AgentPhasePlan, selectedPhases)
	}
	if selectedPhases[1] != string(builtin_tools.AgentPhaseStep) {
		t.Fatalf("expected second selected phase %q, got %v", builtin_tools.AgentPhaseStep, selectedPhases)
	}
}

func TestExecute_PlanPhaseUsesPlannerDirectResponseWhenPlannerReturnsEmptyPlan(t *testing.T) {
	var selectedPhases []string
	emitter := NewEmitter("implicit-plan-session", "implicit-plan-agent", func(event *AgentOutputEvent) error {
		if event == nil || event.Type != EventTypeLog || event.Payload == nil {
			return nil
		}
		if strings.TrimSpace(phaseStringFromAny(event.Payload["event"])) != "phase_selected" {
			return nil
		}
		selectedPhases = append(selectedPhases, strings.TrimSpace(phaseStringFromAny(event.Payload["selected_phase"])))
		return nil
	})

	client := &executeModelTestClient{
		replies: []executeModelReply{},
	}

	agent, err := NewReActAgent(
		"implicit-plan-agent",
		client,
		WithEmitter(emitter),
		WithMaxIterations(2),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning:  false,
				Plan:           []*builtin_tools.PlanItem{},
				DirectResponse: "done",
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "done" {
		t.Fatalf("expected planner direct response result, got %q", runResult.Result)
	}
	if client.calls != 0 {
		t.Fatalf("expected 0 model calls (planner is static, no step/final_answer), got %d", client.calls)
	}

	snapshot := agent.State()
	if len(snapshot.Plan) != 0 {
		t.Fatalf("expected no plan items when planner returns empty plan, got %+v", snapshot.Plan)
	}
	if snapshot.Status != builtin_tools.TaskStatusCompleted {
		t.Fatalf("expected completed status, got %q", snapshot.Status)
	}
	if snapshot.FinalAnswer == nil {
		t.Fatalf("expected final answer")
	}
	if strings.TrimSpace(snapshot.FinalAnswer.Content) != "done" {
		t.Fatalf("expected final answer content to match direct response, got %q", snapshot.FinalAnswer.Content)
	}
	if strings.TrimSpace(snapshot.FinalAnswer.Source) != "planner_direct" {
		t.Fatalf("expected final answer source %q, got %q", "planner_direct", snapshot.FinalAnswer.Source)
	}
	if len(selectedPhases) != 1 || selectedPhases[0] != string(builtin_tools.AgentPhasePlan) {
		t.Fatalf("expected only plan phase selection, got %v", selectedPhases)
	}
}

func TestExecute_DefaultPlannerDirectResponseSkipsStepAndFinalAnswer(t *testing.T) {
	var selectedPhases []string
	emitter := NewEmitter("planner-direct-session", "planner-direct-agent", func(event *AgentOutputEvent) error {
		if event == nil || event.Type != EventTypeLog || event.Payload == nil {
			return nil
		}
		if strings.TrimSpace(phaseStringFromAny(event.Payload["event"])) != "phase_selected" {
			return nil
		}
		selectedPhases = append(selectedPhases, strings.TrimSpace(phaseStringFromAny(event.Payload["selected_phase"])))
		return nil
	})

	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-submit-plan", "submit_plan", map[string]any{
						"needs_planning":  false,
						"plan":            []any{},
						"explanation":     "无需规划",
						"direct_response": "你好！",
					}),
				},
			},
		},
	}

	agent, err := NewReActAgent(
		"planner-direct-agent",
		client,
		WithEmitter(emitter),
		WithMaxIterations(2),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelAgenticPlanner{prompt: "plan this task"}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "你好！" {
		t.Fatalf("expected planner direct response result, got %q", runResult.Result)
	}
	if client.calls != 1 {
		t.Fatalf("expected 1 model call (planner only), got %d", client.calls)
	}

	snapshot := agent.State()
	if len(snapshot.Plan) != 0 {
		t.Fatalf("expected no plan items when planner returns empty plan, got %+v", snapshot.Plan)
	}
	if snapshot.Status != builtin_tools.TaskStatusCompleted {
		t.Fatalf("expected completed status, got %q", snapshot.Status)
	}
	if snapshot.FinalAnswer == nil {
		t.Fatalf("expected final answer")
	}
	if strings.TrimSpace(snapshot.FinalAnswer.Content) != "你好！" {
		t.Fatalf("expected final answer content to match direct response, got %q", snapshot.FinalAnswer.Content)
	}
	if strings.TrimSpace(snapshot.FinalAnswer.Source) != "planner_direct" {
		t.Fatalf("expected final answer source %q, got %q", "planner_direct", snapshot.FinalAnswer.Source)
	}
	if len(selectedPhases) != 1 || selectedPhases[0] != string(builtin_tools.AgentPhasePlan) {
		t.Fatalf("expected only plan phase selection, got %v", selectedPhases)
	}
}

func TestExecute_PlanPhaseWithToolsFallsBackToAssistantText(t *testing.T) {
	var selectedPhases []string
	emitter := NewEmitter("planner-fallback-session", "planner-fallback-agent", func(event *AgentOutputEvent) error {
		if event == nil || event.Type != EventTypeLog || event.Payload == nil {
			return nil
		}
		if strings.TrimSpace(phaseStringFromAny(event.Payload["event"])) != "phase_selected" {
			return nil
		}
		selectedPhases = append(selectedPhases, strings.TrimSpace(phaseStringFromAny(event.Payload["selected_phase"])))
		return nil
	})

	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				content: "你好！有什么可以帮助你的？",
			},
		},
	}

	agent, err := NewReActAgent(
		"planner-fallback-agent",
		client,
		WithEmitter(emitter),
		WithMaxIterations(2),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelAgenticPlanner{prompt: "plan this task"}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "你好", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "你好！有什么可以帮助你的？" {
		t.Fatalf("expected assistant text as result, got %q", runResult.Result)
	}
	if client.calls != 1 {
		t.Fatalf("expected 1 model call (planner only), got %d", client.calls)
	}

	snapshot := agent.State()
	if len(snapshot.Plan) != 0 {
		t.Fatalf("expected no plan items, got %+v", snapshot.Plan)
	}
	if snapshot.Status != builtin_tools.TaskStatusCompleted {
		t.Fatalf("expected completed status, got %q", snapshot.Status)
	}
	if snapshot.FinalAnswer == nil {
		t.Fatalf("expected final answer")
	}
	if strings.TrimSpace(snapshot.FinalAnswer.Content) != "你好！有什么可以帮助你的？" {
		t.Fatalf("expected final answer content to match assistant text, got %q", snapshot.FinalAnswer.Content)
	}
	if strings.TrimSpace(snapshot.FinalAnswer.Source) != "planner_direct" {
		t.Fatalf("expected final answer source %q, got %q", "planner_direct", snapshot.FinalAnswer.Source)
	}
	if len(selectedPhases) != 1 || selectedPhases[0] != string(builtin_tools.AgentPhasePlan) {
		t.Fatalf("expected only plan phase selection, got %v", selectedPhases)
	}
}

func TestExecute_WritesStepReplanLogsWhenStepCompletes(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

	largeResult := strings.Repeat("large-step-result-", 200)
	client := &executeModelTestClient{
		modelContext: ai.ModelContextInfo{ModelName: "test", ContextWindowTokens: 100, InputTokenLimit: 40, OutputTokenLimit: 10},
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        largeResult,
						"display_result": largeResult,
						"result":         largeResult,
					}),
				},
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"done","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	if _, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "\"event\":\"step_replan_completed\"") {
		t.Fatalf("expected step_replan_completed log, got %s", out)
	}
}

func phaseStringFromAny(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	case builtin_tools.AgentPhase:
		return string(typed)
	default:
		return ""
	}
}

func TestExecute_WritesFinalAnswerFallbackLogWhenModelReturnsPlainText(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

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
				content: `plain text final answer`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	if _, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude()); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "\"event\":\"final_answer_model_request\"") {
		t.Fatalf("expected final_answer_model_request log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"final_answer_model_fallback_text\"") {
		t.Fatalf("expected final_answer_model_fallback_text log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"final_answer_model_raw_response\"") {
		t.Fatalf("expected final_answer_model_raw_response log, got %s", out)
	}
	if !strings.Contains(out, "\"raw_response_length\":23") {
		t.Fatalf("expected raw_response_length logged for final answer fallback, got %s", out)
	}
}

func TestExecute_WritesFinalAnswerRequestAndEmptyResponseLogs(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

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
				content: ``,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	if _, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude()); err == nil {
		t.Fatalf("expected Execute to fail when final_answer model returns empty content")
	}

	out := buf.String()
	if !strings.Contains(out, "\"event\":\"final_answer_model_request\"") {
		t.Fatalf("expected final_answer_model_request log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"final_answer_model_raw_response\"") {
		t.Fatalf("expected final_answer_model_raw_response log, got %s", out)
	}
	if !strings.Contains(out, "\"mode\":\"parse_failed\"") {
		t.Fatalf("expected parse_failed mode in raw response log, got %s", out)
	}
	if !strings.Contains(out, "\"raw_response_length\":0") {
		t.Fatalf("expected raw_response_length=0 in raw response log, got %s", out)
	}
}

func TestExecute_WritesStepReplanEmptyResponseLog(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok",
						"display_result": "step ok",
						"result":         "step ok",
						"open_questions": []string{"need clarification"},
					}),
				},
			},
			// step_replan phase: empty response → defaults to no replan (agentic mode)
			{content: ``},
			// final_answer phase
			{
				content: `{"is_complete":true,"status":"completed","reason":"done","user_message":"done","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}

	out := buf.String()
	// With agentic step_replan, empty response defaults to no-replan instead of erroring.
	// The step_replan_completed event should be logged.
	if !strings.Contains(out, "\"event\":\"step_replan_completed\"") {
		t.Fatalf("expected step_replan_completed log, got %s", out)
	}
}

func TestExecute_StepReplanDefaultInnerRetryRemainsThree(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

	baseClient := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok",
						"display_result": "step ok",
						"result":         "step ok",
						"open_questions": []string{"need clarification"},
					}),
				},
			},
			{content: ``},
			{content: ``},
			{content: `{"should_replan":false,"replan_reason":"","next_goal":"","missing_items":[],"warnings":[]}`},
			{content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"done","references":[]}`},
		},
	}
	agent, err := NewReActAgent(
		"model-agent",
		baseClient,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected successful run result, got %#v", runResult)
	}
}

func TestExecute_FailedCurrentStepTerminatesTask(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-failed", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status": "failed",
						"error":  "step failed",
					}),
				},
			},
			{
				content: `{"should_replan":false,"replan_reason":"","next_goal":"","missing_items":[],"warnings":[]}`,
			},
			{
				content: `{"is_complete":true,"status":"failed","reason":"关键步骤失败且无需重规划。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"final answer for failure","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"model-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "inspect", Step: "梳理链路", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
		WithMaxIterations(6),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || runResult.Success {
		t.Fatalf("expected failed run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Error) != "final answer for failure" {
		t.Fatalf("expected final answer error propagated, got %q", runResult.Error)
	}
}

func TestExecute_StepReplanContinuesToNextStepWithoutFinalAnswer(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				// step-1
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-1-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok1",
						"display_result": "step1 ok",
						"result":         "step1 ok",
					}),
				},
			},
			{
				// step-1 replan (LLM always runs now)
				content: `{"should_replan":false,"replan_reason":"","next_goal":"","missing_items":[],"warnings":[]}`,
			},
			{
				// step-2
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-2-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok2",
						"display_result": "step2 ok",
						"result":         "step2 ok",
					}),
				},
			},
			{
				// step-2 replan
				content: `{"should_replan":false,"replan_reason":"","next_goal":"","missing_items":[],"warnings":[]}`,
			},
			{
				// final_answer
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"two-steps-done","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"two-steps-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(10),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "第一步", Status: builtin_tools.PlanStepPending},
					{ID: "step-2", Step: "第二步", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "two-steps-done" {
		t.Fatalf("expected final answer result, got %q", runResult.Result)
	}
	if client.calls != 5 {
		t.Fatalf("expected 5 model calls (step1+replan1+step2+replan2+final), got %d", client.calls)
	}
}

func TestExecute_StepSummaryReplansBeforeRunningOldPendingStep(t *testing.T) {
	var emittedEvents []*AgentOutputEvent
	emitter := NewEmitter("", "", func(e *AgentOutputEvent) error {
		if e != nil {
			emittedEvents = append(emittedEvents, e)
		}
		return nil
	})
	planner := &executeModelSequencePlanner{
		results: []*builtin_tools.TaskPlannerResult{
			{
				NeedsPlanning: false,
				Explanation:   "初始计划",
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "完成已有步骤", Status: builtin_tools.PlanStepPending},
					{ID: "legacy-step", Step: "过时旧待办", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
				},
			},
			{
				NeedsPlanning: true,
				Explanation:   "补齐新缺口",
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-2", Step: "围绕新缺口补齐验证", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
				},
			},
		},
	}
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-1-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok1",
						"display_result": "step1 ok",
						"result":         "step1 ok",
						"open_questions": []string{"新增验证缺口需要确认"},
					}),
				},
			},
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-submit-replan-1", "submit_plan", map[string]any{
						"should_replan": true,
						"replan_reason": "旧计划未覆盖新增验证缺口",
						"next_goal":     "围绕新缺口补齐验证",
						"missing_items": []any{"missing-1"},
						"warnings":      []any{"warn-1"},
					}),
				},
			},
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-2-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok2",
						"display_result": "step2 ok",
						"result":         "step2 ok",
					}),
				},
			},
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-submit-replan-2", "submit_plan", map[string]any{
						"should_replan": false,
						"replan_reason": "",
						"next_goal":     "",
						"missing_items": []any{},
						"warnings":      []any{},
					}),
				},
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"replanned-done","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"replan-agent",
		client,
		WithEmitter(emitter),
		WithMaxIterations(10),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "replanned-done" {
		t.Fatalf("expected replanned final answer result, got %q", runResult.Result)
	}
	if client.calls != 5 {
		t.Fatalf("expected 5 model calls (step+replan+step+replan+final), got %d", client.calls)
	}
	if planner.calls != 2 {
		t.Fatalf("expected planner called twice, got %d", planner.calls)
	}
	if len(planner.inputs) < 2 || !strings.Contains(planner.inputs[1], "<REPLAN_CONTEXT>") {
		t.Fatalf("expected second planner input to include replan context, got %q", strings.Join(planner.inputs, "\n---\n"))
	}
	if !strings.Contains(planner.inputs[1], "\"reason\":\"旧计划未覆盖新增验证缺口\"") {
		t.Fatalf("expected second planner input to include replan reason, got %q", planner.inputs[1])
	}

	snapshot := agent.State()
	if snapshot.PlanVersion != 2 {
		t.Fatalf("expected plan version 2 after replan, got %d", snapshot.PlanVersion)
	}
	if snapshot.ReplanContext != nil {
		t.Fatalf("expected replan context cleared after replanned plan finishes, got %+v", snapshot.ReplanContext)
	}

	statusByID := make(map[string]builtin_tools.PlanStepStatus, len(snapshot.Plan))
	for _, item := range snapshot.Plan {
		if item == nil {
			continue
		}
		statusByID[item.ID] = item.Status
	}
	if len(statusByID) != 2 {
		t.Fatalf("expected only completed old step and new replanned step, got %+v", statusByID)
	}
	if _, ok := statusByID["legacy-step"]; ok {
		t.Fatalf("expected legacy pending step removed after replan, got %+v", statusByID)
	}
	if statusByID["step-1"] != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected step-1 preserved as completed, got %+v", statusByID)
	}
	if statusByID["step-2"] != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected replanned step-2 completed, got %+v", statusByID)
	}

	taskPlanExplanations := make([]string, 0, 2)
	for _, event := range emittedEvents {
		if event == nil || event.Type != EventTypeTaskPlan {
			continue
		}
		taskPlanExplanations = append(taskPlanExplanations, strings.TrimSpace(builtin_tools.ToolRuntimeValue(event.Payload["explanation"])))
	}
	if len(taskPlanExplanations) < 2 {
		t.Fatalf("expected initial and replanned task_plan events, got %+v", taskPlanExplanations)
	}
	if got := taskPlanExplanations[len(taskPlanExplanations)-1]; got != "旧计划未覆盖新增验证缺口" {
		t.Fatalf("expected replanned task_plan explanation to use replan reason, got %q", got)
	}

	var stepReplanEvent *AgentOutputEvent
	for _, event := range emittedEvents {
		if event == nil || event.Type != EventTypeStepReplanResult {
			continue
		}
		stepReplanEvent = event
		break
	}
	if stepReplanEvent == nil {
		t.Fatal("expected step_replan_result event")
	}
	if got, _ := stepReplanEvent.Payload["should_replan"].(bool); !got {
		t.Fatalf("expected should_replan=true, got %#v", stepReplanEvent.Payload)
	}
	if got := strings.TrimSpace(builtin_tools.ToolRuntimeValue(stepReplanEvent.Payload["replan_reason"])); got != "旧计划未覆盖新增验证缺口" {
		t.Fatalf("expected replan reason in event, got %#v", stepReplanEvent.Payload)
	}
	if got := strings.TrimSpace(builtin_tools.ToolRuntimeValue(stepReplanEvent.Payload["next_goal"])); got != "围绕新缺口补齐验证" {
		t.Fatalf("expected next goal in event, got %#v", stepReplanEvent.Payload)
	}
}

func TestExecute_AppendsInputTimelineToState(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{content: `{"is_complete":true,"status":"completed","reason":"仅用于测试回路。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"ok","references":[]}`},
		},
	}

	agent, err := NewReActAgent(
		"timeline-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(1),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan:          []*builtin_tools.PlanItem{},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	_, err = agent.Execute(context.Background(), "first-input", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	snapshot := agent.State()
	if len(snapshot.InputTimeline) != 1 {
		t.Fatalf("expected 1 input timeline item, got %d", len(snapshot.InputTimeline))
	}
	if snapshot.InputTimeline[0] == nil || strings.TrimSpace(snapshot.InputTimeline[0].Content) != "first-input" {
		t.Fatalf("expected latest input stored, got %#v", snapshot.InputTimeline)
	}
}

func TestExecute_RejectsEmptyInput(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{content: "ok"},
		},
	}

	agent, err := NewReActAgent(
		"timeline-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(1),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	if _, err := agent.Execute(context.Background(), "   "); err == nil {
		t.Fatalf("expected empty input error, got nil")
	}
}

func TestFormatRuntimeStateJSONIncludesInputTimeline(t *testing.T) {
	raw := FormatRuntimeStateJSON(builtin_tools.StateSnapshot{
		CurrentGoal: "latest-goal",
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "first-input", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
			{Content: "second-input", CreatedAt: time.Date(2026, 4, 3, 10, 1, 0, 0, time.UTC)},
		},
	}, "ses-test")
	if !strings.Contains(raw, "\"input_timeline\"") {
		t.Fatalf("expected input_timeline in runtime state json, got %s", raw)
	}
	if !strings.Contains(raw, "first-input") || !strings.Contains(raw, "second-input") {
		t.Fatalf("expected all inputs in runtime state json, got %s", raw)
	}
}

func TestPlannerInputFromSnapshotUsesInputTimeline(t *testing.T) {
	text := PlannerInputFromSnapshot(builtin_tools.StateSnapshot{
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "first-input", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
			{Content: "second-input", CreatedAt: time.Date(2026, 4, 3, 10, 1, 0, 0, time.UTC)},
		},
	}, PlannerInputOptions{})
	if !strings.Contains(text, "用户输入时间线") {
		t.Fatalf("expected planner input to include timeline header, got %q", text)
	}
	if !strings.Contains(text, "first-input") || !strings.Contains(text, "second-input") {
		t.Fatalf("expected planner input to include all inputs, got %q", text)
	}
}

func TestPlannerInputFromSnapshotRejectsEmptyTimeline(t *testing.T) {
	text := PlannerInputFromSnapshot(builtin_tools.StateSnapshot{
		CurrentGoal: "only-latest-goal",
	}, PlannerInputOptions{})
	if text != "" {
		t.Fatalf("expected empty planner input when timeline is empty, got %q", text)
	}
}

// executeModelAgenticPlanner implements both TaskPlanner and PlannerPromptBuilder,
// triggering the runPlanPhaseWithTools path in runPlanPhase.
type executeModelAgenticPlanner struct {
	prompt string
}

func (p *executeModelAgenticPlanner) Plan(ctx context.Context, input string) (*builtin_tools.TaskPlannerResult, error) {
	return nil, fmt.Errorf("should not be called when PlannerPromptBuilder is implemented")
}

func (p *executeModelAgenticPlanner) BuildPrompt(input TaskPlannerPromptInput) (string, error) {
	return p.prompt, nil
}

func TestExecute_PlanPhaseWithToolsParsesPlanFromAIProxy(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			// plan phase via AICallProxy: model calls submit_plan
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-submit-plan", "submit_plan", map[string]any{
						"needs_planning": true,
						"plan": []any{
							map[string]any{"id": "step-1", "step": "执行用户请求", "status": "pending", "depends_on": []any{}},
						},
						"explanation": "需要规划",
					}),
				},
			},
			// step phase: step completes
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
			// final_answer phase (step_replan fast path skips LLM)
			{
				content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"agentic-plan-final","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"agentic-plan-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelAgenticPlanner{prompt: "plan this task"}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "agentic-plan-final" {
		t.Fatalf("expected final answer from agentic plan path, got %q", runResult.Result)
	}

	snapshot := agent.State()
	if len(snapshot.Plan) != 1 {
		t.Fatalf("expected 1 plan item, got %d", len(snapshot.Plan))
	}
	if snapshot.Plan[0].ID != "step-1" {
		t.Fatalf("expected plan item id step-1, got %q", snapshot.Plan[0].ID)
	}
}

func TestExecute_PlanPhaseWithToolsDirectResponse(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			// plan phase via AICallProxy: model calls submit_plan with direct response
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-submit-plan", "submit_plan", map[string]any{
						"needs_planning":  false,
						"plan":            []any{},
						"explanation":     "简单问题",
						"direct_response": "这是直接回复",
					}),
				},
			},
		},
	}

	agent, err := NewReActAgent(
		"agentic-direct-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(2),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelAgenticPlanner{prompt: "plan this task"}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "这是直接回复" {
		t.Fatalf("expected direct response, got %q", runResult.Result)
	}
	if client.calls != 1 {
		t.Fatalf("expected 1 model call (plan only), got %d", client.calls)
	}
}

// realFileWorkspaceRuntime uses actual builtin_tools file I/O for step_contexts.jsonl,
// exercising the full validation + persistence path (not a recording mock).
type realFileWorkspaceRuntime struct {
	rootDir string
}

func (w *realFileWorkspaceRuntime) SessionID() string  { return "test-session" }
func (w *realFileWorkspaceRuntime) RootDir() string     { return w.rootDir }
func (w *realFileWorkspaceRuntime) Namespace() string   { return "" }
func (w *realFileWorkspaceRuntime) SharedDir() string   { return w.rootDir + "/shared" }
func (w *realFileWorkspaceRuntime) ReadFileRel(_ string) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}
func (w *realFileWorkspaceRuntime) WriteFileRel(_ string, _ []byte) error { return nil }
func (w *realFileWorkspaceRuntime) LoadWorkspaceState() (*builtin_tools.WorkspaceState, error) {
	return &builtin_tools.WorkspaceState{}, nil
}
func (w *realFileWorkspaceRuntime) SaveWorkspaceState(_ *builtin_tools.WorkspaceState) error {
	return nil
}
func (w *realFileWorkspaceRuntime) LoadWorkspaceReferences() ([]*builtin_tools.WorkspaceReferenceRecord, error) {
	return nil, nil
}
func (w *realFileWorkspaceRuntime) AppendWorkspaceReferences(_ []*builtin_tools.WorkspaceReferenceRecord) error {
	return nil
}
func (w *realFileWorkspaceRuntime) LoadStepContextRecords(limit int) ([]*builtin_tools.StepContextRecord, error) {
	return builtin_tools.LoadWorkspaceStepContextRecords(w.rootDir, limit)
}
func (w *realFileWorkspaceRuntime) AppendStepContextRecords(records []*builtin_tools.StepContextRecord) error {
	return builtin_tools.AppendWorkspaceStepContextRecords(w.rootDir, records)
}
func (w *realFileWorkspaceRuntime) ArtifactWritePath(relPath string) (string, string, error) {
	return relPath, w.rootDir + "/" + relPath, nil
}

func TestExecute_WritesStepContextsAfterStepReplan(t *testing.T) {
	wsRoot := t.TempDir()
	wsRuntime := &realFileWorkspaceRuntime{rootDir: wsRoot}

	client := &executeModelTestClient{
		replies: []executeModelReply{
			// step phase: step completes with tool_calls_digest
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":            "completed",
						"summary":           "ok",
						"display_result":    "step ok",
						"result":            "step ok",
						"short_summary":     "completed analysis",
						"tool_calls_digest": []string{"read_file(main.go)", "rg('TODO')"},
						"key_facts":         []string{"found 3 TODOs"},
					}),
				},
			},
			// final_answer phase (step_replan fast path skips LLM)
			{
				content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"contexts-test-done","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"contexts-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "分析代码", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello",
		WithSkipIntentPrelude(),
		WithWorkspaceRuntime(wsRuntime),
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}

	// Load records from the real step_contexts.jsonl file via builtin_tools
	records, loadErr := builtin_tools.LoadWorkspaceStepContextRecords(wsRoot, 0)
	if loadErr != nil {
		t.Fatalf("LoadWorkspaceStepContextRecords failed: %v", loadErr)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 step context records (plan + step), got %d", len(records))
	}
	// First record: plan context
	planRec := records[0]
	if planRec.StepID != "__plan__" {
		t.Fatalf("expected first record step_id=__plan__, got %q", planRec.StepID)
	}
	// Second record: step context
	rec := records[1]
	if rec.StepID != "step-1" {
		t.Fatalf("expected step_id=step-1, got %q", rec.StepID)
	}
	if rec.PlanVersion < 1 {
		t.Fatalf("expected plan_version >= 1, got %d", rec.PlanVersion)
	}
	if rec.ContextKey == "" {
		t.Fatalf("expected non-empty context_key")
	}
	if rec.ShortSummary != "completed analysis" {
		t.Fatalf("expected short_summary='completed analysis', got %q", rec.ShortSummary)
	}
	if len(rec.ToolCallsDigest) != 2 {
		t.Fatalf("expected 2 tool_calls_digest entries, got %d: %v", len(rec.ToolCallsDigest), rec.ToolCallsDigest)
	}
	if len(rec.KeyFacts) != 1 || rec.KeyFacts[0] != "found 3 TODOs" {
		t.Fatalf("expected key_facts=[found 3 TODOs], got %v", rec.KeyFacts)
	}
}

func TestExecute_WritesStepContextsForMultiStepPlan(t *testing.T) {
	wsRoot := t.TempDir()
	wsRuntime := &realFileWorkspaceRuntime{rootDir: wsRoot}

		client := &executeModelTestClient{
			replies: []executeModelReply{
				// step-1 completes
				{
					toolCalls: []*ai.FunctionTool{
						mustBuildToolCall(t, "call-step-1-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":            "completed",
						"summary":           "step1 done",
						"display_result":    "step1 ok",
						"result":            "step1 ok",
						"short_summary":     "first step done",
						"tool_calls_digest": []string{"bash(ls)"},
						}),
					},
				},
				// step-1 replan (LLM always runs now)
				{content: `{"should_replan":false,"replan_reason":"","next_goal":"","missing_items":[],"warnings":[]}`},
				// step-2 completes
				{
					toolCalls: []*ai.FunctionTool{
						mustBuildToolCall(t, "call-step-2-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":            "completed",
						"summary":           "step2 done",
						"display_result":    "step2 ok",
						"result":            "step2 ok",
						"short_summary":     "second step done",
						"tool_calls_digest": []string{"read_file(a.go)", "read_file(b.go)"},
						"key_facts":         []string{"fact-a", "fact-b"},
						}),
					},
				},
				// step-2 replan
				{content: `{"should_replan":false,"replan_reason":"","next_goal":"","missing_items":[],"warnings":[]}`},
				// final_answer
				{
					content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"multi-step-done","references":[]}`,
				},
			},
		}

	agent, err := NewReActAgent(
		"multi-step-contexts-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(10),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "第一步", Status: builtin_tools.PlanStepPending},
					{ID: "step-2", Step: "第二步", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello",
		WithSkipIntentPrelude(),
		WithWorkspaceRuntime(wsRuntime),
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}

	records, loadErr := builtin_tools.LoadWorkspaceStepContextRecords(wsRoot, 0)
	if loadErr != nil {
		t.Fatalf("LoadWorkspaceStepContextRecords failed: %v", loadErr)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 step context records, got %d", len(records))
	}

	// records[0] = __plan__ (plan phase context record)
	if records[0].StepID != "__plan__" {
		t.Fatalf("expected first record step_id=__plan__, got %q", records[0].StepID)
	}

	// Verify step records are in execution order
	if records[1].StepID != "step-1" {
		t.Fatalf("expected second record step_id=step-1, got %q", records[1].StepID)
	}
	if records[2].StepID != "step-2" {
		t.Fatalf("expected third record step_id=step-2, got %q", records[2].StepID)
	}

	// Verify step records have same plan version
	if records[1].PlanVersion != records[2].PlanVersion {
		t.Fatalf("expected same plan_version, got %d vs %d", records[1].PlanVersion, records[2].PlanVersion)
	}

	// Verify context keys are unique
	if records[1].ContextKey == records[2].ContextKey {
		t.Fatalf("expected unique context_keys, both are %q", records[1].ContextKey)
	}

	// Verify step-2 data
	if records[2].ShortSummary != "second step done" {
		t.Fatalf("expected step-2 short_summary='second step done', got %q", records[2].ShortSummary)
	}
	if len(records[2].ToolCallsDigest) != 2 {
		t.Fatalf("expected step-2 tool_calls_digest len=2, got %d", len(records[2].ToolCallsDigest))
	}
	if len(records[2].KeyFacts) != 2 {
		t.Fatalf("expected step-2 key_facts len=2, got %d", len(records[2].KeyFacts))
	}
}

// recordingChatClient wraps executeModelTestClient and captures the system message
// (msgs[0]) from each ChatEx call, enabling tests to verify that multi-round
// tool loops retain the full system prompt.
type recordingChatClient struct {
	executeModelTestClient
	systemMessages []string // Content of msgs[0] from each ChatEx call
}

func (c *recordingChatClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	if len(infos) > 0 && infos[0] != nil {
		if s, ok := infos[0].Content.(string); ok {
			c.systemMessages = append(c.systemMessages, s)
		}
	}
	return c.executeModelTestClient.ChatEx(ctx, infos, tools...)
}

type noopPhaseTool struct{ name string }

func (t *noopPhaseTool) Name() string                                            { return t.name }
func (t *noopPhaseTool) Description() string                                     { return "noop" }
func (t *noopPhaseTool) Parameters() any                                         { return map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}} }
func (t *noopPhaseTool) Execute(_ context.Context, _ map[string]any) (string, error) { return "ok", nil }

func TestStepReplan_MultiRoundRetainsSystemPrompt(t *testing.T) {
	client := &recordingChatClient{
		executeModelTestClient: executeModelTestClient{
			replies: []executeModelReply{
				// step phase: step completes with open_questions to trigger LLM replan
				{
					toolCalls: []*ai.FunctionTool{
						mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
							"status":         "completed",
							"summary":        "analysis done",
							"display_result": "found issues",
							"result":         "result data",
							"short_summary":  "analysis complete",
							"open_questions": []string{"need to check middleware"},
						}),
					},
				},
				// step_replan round 1: model calls read_file tool
				{
					toolCalls: []*ai.FunctionTool{
						mustBuildToolCall(t, "call-read", builtin_tools.ReadFileToolName, map[string]any{
							"path": "/tmp/nonexistent.go",
						}),
					},
				},
				// step_replan round 2: model calls submit_plan
				{
					toolCalls: []*ai.FunctionTool{
						mustBuildToolCall(t, "call-submit-replan", "submit_plan", map[string]any{
							"should_replan": false,
							"replan_reason": "",
							"next_goal":     "",
							"missing_items": []any{},
							"warnings":      []any{},
						}),
					},
				},
				// final_answer
				{
					content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"replan-prompt-test","references":[]}`,
				},
			},
		},
	}

	agent, err := NewReActAgent(
		"replan-prompt-retain",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(10),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTools(&noopPhaseTool{name: builtin_tools.ReadFileToolName}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "分析代码", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}

	// Find the step_replan system messages (they contain STEP_OUTCOME).
	// The sequence is: step phase call(s), step_replan round 1, step_replan round 2, final_answer.
	var replanSystemMsgs []string
	for _, sys := range client.systemMessages {
		if strings.Contains(sys, "STEP_OUTCOME") {
			replanSystemMsgs = append(replanSystemMsgs, sys)
		}
	}

	if len(replanSystemMsgs) < 2 {
		t.Fatalf("expected at least 2 step_replan rounds with system prompt, got %d (total calls: %d)",
			len(replanSystemMsgs), len(client.systemMessages))
	}

	// Round 2 system message must be identical to round 1
	if replanSystemMsgs[0] != replanSystemMsgs[1] {
		t.Fatalf("round-2 system message differs from round-1:\nround1 len=%d\nround2 len=%d",
			len(replanSystemMsgs[0]), len(replanSystemMsgs[1]))
	}

	// Verify critical markers are present in round 2
	round2 := replanSystemMsgs[1]
	for _, marker := range []string{"CURRENT_GOAL", "STEP_OUTCOME", "JSON-SCHEMA", "should_replan"} {
		if !strings.Contains(round2, marker) {
			t.Errorf("round-2 system prompt missing marker %q", marker)
		}
	}

	// Verify it's not empty
	if len(round2) < 100 {
		t.Fatalf("round-2 system prompt suspiciously short (%d bytes), likely empty or truncated", len(round2))
	}
}

func TestPlanPhaseWithTools_MultiRoundRetainsSystemPrompt(t *testing.T) {
	client := &recordingChatClient{
		executeModelTestClient: executeModelTestClient{
			replies: []executeModelReply{
				// plan round 1: model calls list_files tool
				{
					toolCalls: []*ai.FunctionTool{
						mustBuildToolCall(t, "call-list", builtin_tools.ListFilesToolName, map[string]any{
							"path": "/tmp",
						}),
					},
				},
				// plan round 2: model calls submit_plan
				{
					toolCalls: []*ai.FunctionTool{
						mustBuildToolCall(t, "call-submit-plan", "submit_plan", map[string]any{
							"needs_planning": true,
							"plan": []any{
								map[string]any{"id": "step-1", "step": "执行", "status": "pending"},
							},
							"explanation": "planned",
						}),
					},
				},
				// step phase: step completes
				{
					toolCalls: []*ai.FunctionTool{
						mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
							"status":         "completed",
							"summary":        "done",
							"display_result": "ok",
							"result":         "ok",
							"short_summary":  "step done",
						}),
					},
				},
				// final_answer (step_replan fast path skips LLM)
				{
					content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"plan-prompt-test","references":[]}`,
				},
			},
		},
	}

	agent, err := NewReActAgent(
		"plan-prompt-retain",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(10),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTools(&noopPhaseTool{name: builtin_tools.ListFilesToolName}),
		WithTaskPlanner(&executeModelAgenticPlanner{prompt: "你是任务规划器。\n<JSON-SCHEMA>\nplan schema here\n</JSON-SCHEMA>"}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}

	// Find plan phase system messages (they contain "任务规划器")
	var planSystemMsgs []string
	for _, sys := range client.systemMessages {
		if strings.Contains(sys, "任务规划器") {
			planSystemMsgs = append(planSystemMsgs, sys)
		}
	}

	if len(planSystemMsgs) < 2 {
		t.Fatalf("expected at least 2 plan rounds with system prompt, got %d (total calls: %d)",
			len(planSystemMsgs), len(client.systemMessages))
	}

	// Round 2 must be identical to round 1
	if planSystemMsgs[0] != planSystemMsgs[1] {
		t.Fatalf("plan round-2 system message differs from round-1:\nround1 len=%d\nround2 len=%d",
			len(planSystemMsgs[0]), len(planSystemMsgs[1]))
	}

	// Verify critical markers in round 2
	round2 := planSystemMsgs[1]
	for _, marker := range []string{"任务规划器", "JSON-SCHEMA"} {
		if !strings.Contains(round2, marker) {
			t.Errorf("plan round-2 system prompt missing marker %q", marker)
		}
	}

	if len(round2) < 20 {
		t.Fatalf("plan round-2 system prompt suspiciously short (%d bytes)", len(round2))
	}
}

func TestPlanPhaseWithTools_SubmitPlanValidationRetry(t *testing.T) {
	client := &executeModelTestClient{
		replies: []executeModelReply{
			// round 1: model calls submit_plan with invalid depends_on
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-submit-bad", "submit_plan", map[string]any{
						"needs_planning": true,
						"plan": []any{
							map[string]any{"id": "step-0", "step": "分析代码", "status": "pending"},
							map[string]any{"id": "step-1", "step": "修复漏洞", "status": "pending", "depends_on": []string{"S-01"}},
						},
						"explanation": "planned",
					}),
				},
			},
			// round 2: model retries with corrected depends_on
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-submit-ok", "submit_plan", map[string]any{
						"needs_planning": true,
						"plan": []any{
							map[string]any{"id": "step-0", "step": "分析代码", "status": "pending"},
							map[string]any{"id": "step-1", "step": "修复漏洞", "status": "pending", "depends_on": []string{"step-0"}},
						},
						"explanation": "planned",
					}),
				},
			},
			// step 1: completes
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-1-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status": "completed", "summary": "done", "display_result": "ok",
						"result": "ok", "short_summary": "step done",
					}),
				},
			},
			// step_replan: no replan
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-replan-1", "submit_plan", map[string]any{
						"should_replan": false, "replan_reason": "", "next_goal": "",
						"missing_items": []string{}, "warnings": []string{},
					}),
				},
			},
			// step 2: completes
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-2-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status": "completed", "summary": "done", "display_result": "ok",
						"result": "ok", "short_summary": "step done",
					}),
				},
			},
			// step_replan: no replan
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-replan-2", "submit_plan", map[string]any{
						"should_replan": false, "replan_reason": "", "next_goal": "",
						"missing_items": []string{}, "warnings": []string{},
					}),
				},
			},
			// final_answer
			{
				content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"validation retry ok","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"submit-plan-retry",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(20),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelAgenticPlanner{prompt: "你是任务规划器。\n<JSON-SCHEMA>\nplan schema\n</JSON-SCHEMA>"}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "test validation retry", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success but got %#v", runResult)
	}
	if !strings.Contains(runResult.Result, "validation retry ok") {
		t.Fatalf("expected result to contain 'validation retry ok', got %q", runResult.Result)
	}
}

func mustBuildToolCall(t *testing.T, callID string, name string, args map[string]any) *ai.FunctionTool {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args failed: %v", err)
	}
	return &ai.FunctionTool{
		Id:   callID,
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:      name,
			Arguments: string(raw),
		},
	}
}
