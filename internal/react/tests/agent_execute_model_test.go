package react_test

import (
	openai "aster/internal/ai/openai"
	. "aster/internal/react"
	"bytes"
	"context"
	"encoding/json"
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
				// step_summary phase
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
			},
			{
				// final_answer phase
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
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
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
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "plain-final-answer" {
		t.Fatalf("expected final answer result, got %q", runResult.Result)
	}
}

func TestExecute_ReturnsPublishedOutputWhenPublishConfigEnabled(t *testing.T) {
	published := `{"ok":true,"items":[1,2]}`
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
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"human-final-answer","references":[],"published_output":` + published + `}`,
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

	runResult, err := agent.Execute(
		context.Background(),
		"hello",
		WithSkipIntentPrelude(),
		WithFinalAnswerPublishConfig(&FinalAnswerPublishConfig{
			Contract: &PublishedOutputContract{Name: "test", Schema: `{"type":"object"}`},
			Strict:   true,
		}),
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != published {
		t.Fatalf("expected published_output result, got %q", runResult.Result)
	}
}

func TestExecute_PublishConfigFailsWhenPublishedOutputMissing(t *testing.T) {
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
				// published_output missing
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

	_, err = agent.Execute(
		context.Background(),
		"hello",
		WithSkipIntentPrelude(),
		WithFinalAnswerPublishConfig(&FinalAnswerPublishConfig{
			Contract: &PublishedOutputContract{Name: "test", Schema: `{"type":"object"}`},
			Strict:   true,
		}),
	)
	if err == nil {
		t.Fatalf("expected Execute to fail when published_output missing")
	}
	if !strings.Contains(err.Error(), "published_output") {
		t.Fatalf("expected published_output error, got %v", err)
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
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
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
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
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

func TestExecute_LatestStepResultModeFailsWhenStepResultMissing(t *testing.T) {
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
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
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
	if runResult == nil || runResult.Success {
		t.Fatalf("expected failure run result when step result missing, got %#v", runResult)
	}
	if !strings.Contains(runResult.Error, "update_current_step.result") {
		t.Fatalf("expected explicit latest_step_result error, got %q", runResult.Error)
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
				content: `{"status_summary":"完成","step_short_summary":"扫描完成","step_long_summary":"扫描完成，发现3个漏洞。","key_facts":["f1"],"open_questions":[]}`,
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
				content: `{"status_summary":"完成","step_short_summary":"审计完成","step_long_summary":"已生成结构化审计结果。","key_facts":["report ready"],"open_questions":[]}`,
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
				NeedsPlanning: false,
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
			// call 2 — step_summary phase (after cancel, scheduler detects ctx.Err before reaching here)
			{
				content: `{"status_summary":"完成","step_short_summary":"扫描完成","step_long_summary":"完成。","key_facts":["f1"],"open_questions":[]}`,
			},
			// call 3 — final_answer phase (may or may not be reached depending on cancel timing)
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
			{
				content: `{"status_summary":"完成","step_short_summary":"扫描完成","step_long_summary":"扫描完成，发现 3 个漏洞。","key_facts":["f1"],"open_questions":[]}`,
			},
			{
				content: `{"is_complete":false,"status":"running","reason":"还需要继续验证","should_replan":true,"next_goal":"继续验证","missing_items":["syntaxflow"],"warnings":[],"user_message":"继续执行","references":[]}`,
			},
		},
	}
	client := &errorOnCallClient{
		inner:   inner,
		errorAt: 3,
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
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
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
	if !strings.Contains(out, "\"event\":\"phase_selected\"") {
		t.Fatalf("expected phase_selected terminal log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"step_window_computed\"") {
		t.Fatalf("expected step_window_computed terminal log, got %s", out)
	}
	if strings.Contains(out, "\"event\":\"step_reducer_started\"") {
		t.Fatalf("did not expect step_reducer_started for small step window, got %s", out)
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
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
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

func TestExecute_PlanPhaseBuildsImplicitStepWhenPlannerReturnsEmptyPlan(t *testing.T) {
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
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"done","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"implicit-plan-agent",
		client,
		WithEmitter(emitter),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
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

	runResult, err := agent.Execute(context.Background(), "hello", WithSkipIntentPrelude())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}

	snapshot := agent.State()
	if len(snapshot.Plan) != 1 {
		t.Fatalf("expected implicit single-step plan, got %+v", snapshot.Plan)
	}
	if snapshot.Plan[0] == nil || strings.TrimSpace(snapshot.Plan[0].Step) != "hello" {
		t.Fatalf("expected implicit step from latest input, got %+v", snapshot.Plan)
	}
	if len(selectedPhases) < 2 {
		t.Fatalf("expected at least plan and step phase selections, got %v", selectedPhases)
	}
	if selectedPhases[0] != string(builtin_tools.AgentPhasePlan) || selectedPhases[1] != string(builtin_tools.AgentPhaseStep) {
		t.Fatalf("expected plan then step, got %v", selectedPhases)
	}
}

func TestExecute_WritesReducerLogsWhenStepWindowExceedsBudget(t *testing.T) {
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
				content: `{"status_summary":"压缩完成","window_summary":"窗口已压缩","new_facts":["f1","f2"],"important_changes":["c1"],"references":["artifacts/runtime/r/steps/s/result.json"],"artifact_changes":["summary.md"],"open_questions":[],"noise_removed":["n1"]}`,
			},
			{
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
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
	if !strings.Contains(out, "\"event\":\"step_reducer_started\"") {
		t.Fatalf("expected step_reducer_started log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"step_reducer_completed\"") {
		t.Fatalf("expected step_reducer_completed log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"step_artifacts_written\"") {
		t.Fatalf("expected step_artifacts_written log, got %s", out)
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
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
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
	if !strings.Contains(out, "\"event\":\"final_answer_model_request\"") {
		t.Fatalf("expected final_answer_model_request log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"final_answer_model_fallback_text\"") {
		t.Fatalf("expected final_answer_model_fallback_text log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"final_answer_model_raw_response\"") {
		t.Fatalf("expected final_answer_model_raw_response log, got %s", out)
	}
	if !strings.Contains(out, "\"raw_response\":\"plain text final answer\"") {
		t.Fatalf("expected raw response logged for final answer fallback, got %s", out)
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
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
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

func TestExecute_WritesStepSummaryEmptyResponseLog(t *testing.T) {
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
			{
				content: ``,
			},
			{
				content: ``,
			},
			{
				content: `{"is_complete":true,"status":"failed","reason":"step summary failed","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"step summary failed","references":[]}`,
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
	if runResult == nil || runResult.Success {
		t.Fatalf("expected failed run result, got %#v", runResult)
	}

	out := buf.String()
	if !strings.Contains(out, "\"event\":\"step_summary_model_raw_response\"") {
		t.Fatalf("expected step_summary_model_raw_response log, got %s", out)
	}
	if !strings.Contains(out, "\"mode\":\"parse_failed\"") {
		t.Fatalf("expected parse_failed mode in step summary raw response log, got %s", out)
	}
	if !strings.Contains(out, "\"raw_response_length\":0") {
		t.Fatalf("expected raw_response_length=0 in step summary raw response log, got %s", out)
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
				content: `{"status_summary":"失败","step_short_summary":"该 step 失败","step_long_summary":"该 step 失败。","key_facts":["step failed"],"open_questions":[]}`,
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

func TestExecute_StepSummaryContinuesToNextStepWithoutFinalAnswer(t *testing.T) {
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
				// step_summary for step-1
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
			},
			{
				// step-2 (should run before final_answer)
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
				// step_summary for step-2
				content: `{"status_summary":"完成","step_short_summary":"完成第二步","step_long_summary":"完成第二个 step。","key_facts":["f2"],"open_questions":[]}`,
			},
			{
				// final_answer (only after no runnable steps remain)
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
		t.Fatalf("expected 5 model calls (step+summary+step+summary+final), got %d", client.calls)
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
					}),
				},
			},
			{
				content: `{"status_summary":"发现计划缺口","step_short_summary":"需要补计划","step_long_summary":"当前 step 已暴露出新的验证缺口，需要改写后续步骤。","key_facts":["f1"],"open_questions":[],"should_replan":true,"replan_reason":"旧计划未覆盖新增验证缺口","next_goal":"围绕新缺口补齐验证","missing_items":["missing-1"],"warnings":["warn-1"]}`,
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
				content: `{"status_summary":"完成","step_short_summary":"补齐新步骤","step_long_summary":"完成重规划后的补齐步骤。","key_facts":["f2"],"open_questions":[]}`,
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
		t.Fatalf("expected 5 model calls (step+summary+step+summary+final), got %d", client.calls)
	}
	if planner.calls != 2 {
		t.Fatalf("expected planner called twice, got %d", planner.calls)
	}
	if len(planner.inputs) < 2 || !strings.Contains(planner.inputs[1], "<REPLAN_CONTEXT>") {
		t.Fatalf("expected second planner input to include replan context, got %q", strings.Join(planner.inputs, "\n---\n"))
	}
	if !strings.Contains(planner.inputs[1], "\"reason\": \"旧计划未覆盖新增验证缺口\"") {
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
	}, nil, PlannerInputOptions{})
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
	}, nil, PlannerInputOptions{})
	if text != "" {
		t.Fatalf("expected empty planner input when timeline is empty, got %q", text)
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
