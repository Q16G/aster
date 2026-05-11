package react_test

import (
	. "aster/internal/react"
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type stepHistoryIsolationTool struct{}

func (t *stepHistoryIsolationTool) Name() string { return "echo_tool" }

func (t *stepHistoryIsolationTool) Description() string {
	return "test tool: returns a stable echo output"
}

func (t *stepHistoryIsolationTool) Parameters() any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"msg": map[string]any{"type": "string"}},
		"additionalProperties": false,
	}
}

func (t *stepHistoryIsolationTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	_ = ctx
	msg := builtin_tools.ToolRuntimeValue(args["msg"])
	if msg == "" {
		msg = "empty"
	}
	return "echo:" + msg, nil
}

type stepHistoryIsolationClient struct {
	t          *testing.T
	chatExCall int
}

func (c *stepHistoryIsolationClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	_ = ctx
	_ = info
	_ = tools
	return "", nil
}

func (c *stepHistoryIsolationClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	_ = ctx
	_ = tools
	c.chatExCall++

	switch c.chatExCall {
	case 1:
		// step-1 / turn-1: call a regular tool, but keep the step open.
		msg := ai.NewAIMsgInfo("")
		msg.ToolCalls = []*ai.FunctionTool{
			mustBuildToolCall(c.t, "call-echo", "echo_tool", map[string]any{"msg": "step-1"}),
		}
		return []*ai.ChatChoices{{Message: msg, FinishReason: "stop"}}, nil
	case 2:
		// step-1 / turn-2: the same step should still be able to see its own transcript layer.
		sawAssistantToolCall := false
		sawToolResult := false
		for _, m := range infos {
			if m == nil {
				continue
			}
			if strings.TrimSpace(m.Role) == "assistant" && len(m.ToolCalls) > 0 {
				sawAssistantToolCall = true
			}
			if strings.TrimSpace(m.Role) == "tool" && strings.Contains(strings.TrimSpace(FormatMsgContent(m.Content)), "echo:step-1") {
				sawToolResult = true
			}
		}
		if !sawAssistantToolCall || !sawToolResult {
			c.t.Fatalf("expected current step to see its own onion transcript, assistant=%v tool=%v infos=%#v", sawAssistantToolCall, sawToolResult, infos)
		}

		msg := ai.NewAIMsgInfo("")
		msg.ToolCalls = []*ai.FunctionTool{
			mustBuildToolCall(c.t, "call-step-1", builtin_tools.UpdateCurrentStepToolName, map[string]any{
				"status":         "completed",
				"summary":        "ok1",
				"display_result": "step1 ok",
				"result":         "step1 ok",
			}),
		}
		return []*ai.ChatChoices{{Message: msg, FinishReason: "stop"}}, nil
	case 3:
		// step-2: ensure no tool transcript from step-1 leaks into the model input.
		for _, m := range infos {
			if m == nil {
				continue
			}
			if strings.TrimSpace(m.Role) == "tool" {
				c.t.Fatalf("expected no tool messages in step-2 input, got %#v", m)
			}
			if strings.TrimSpace(m.Role) == "assistant" && len(m.ToolCalls) > 0 {
				c.t.Fatalf("expected no assistant tool-call messages in step-2 input, got %#v", m)
			}
		}

		msg := ai.NewAIMsgInfo("")
		msg.ToolCalls = []*ai.FunctionTool{
			mustBuildToolCall(c.t, "call-step-2", builtin_tools.UpdateCurrentStepToolName, map[string]any{
				"status":         "completed",
				"summary":        "ok2",
				"display_result": "step2 ok",
				"result":         "step2 ok",
			}),
		}
		return []*ai.ChatChoices{{Message: msg, FinishReason: "stop"}}, nil
	default:
		return []*ai.ChatChoices{{Message: ai.NewAIMsgInfo(""), FinishReason: "stop"}}, nil
	}
}

func (c *stepHistoryIsolationClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	_ = ctx
	_ = tools
	if strings.Contains(text, "step_replan") || strings.Contains(text, "`step_replan`") {
		return `{"should_replan":false,"replan_reason":"","next_goal":"","missing_items":[],"warnings":[]}`, nil
	}
	if strings.Contains(text, "`final_answer`") {
		return `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"done","references":[]}`, nil
	}
	return "", nil
}

func TestStepHistoryIsolation_DoesNotLeakToolTranscriptAcrossSteps(t *testing.T) {
	client := &stepHistoryIsolationClient{t: t}
	agent, err := NewReActAgent(
		"isolation-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(10),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTool(&stepHistoryIsolationTool{}),
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
	if runResult == nil || !runResult.Success || strings.TrimSpace(runResult.Result) != "done" {
		t.Fatalf("unexpected run result: %#v", runResult)
	}
	if client.chatExCall != 3 {
		t.Fatalf("expected 3 step-phase model calls, got %d", client.chatExCall)
	}

	// Long-term history should only contain the skeleton; no tool or tool-call messages.
	for _, msg := range agent.History() {
		if msg == nil {
			continue
		}
		if strings.TrimSpace(msg.Role) == "tool" {
			t.Fatalf("expected no tool messages in long-term history, got %#v", msg)
		}
		if strings.TrimSpace(msg.Role) == "assistant" && len(msg.ToolCalls) > 0 {
			t.Fatalf("expected no assistant tool-call messages in long-term history, got %#v", msg)
		}
	}
}
