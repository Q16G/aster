package react_test

import (
	. "aster/internal/react"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/ai/openai"
	aiusage "aster/internal/ai/usage"
)

type agentStreamRoundTripper struct {
	statusCode int
	body       string
	header     http.Header
}

func (rt *agentStreamRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.statusCode == 0 {
		rt.statusCode = http.StatusOK
	}
	resp := &http.Response{
		StatusCode: rt.statusCode,
		Body:       io.NopCloser(strings.NewReader(rt.body)),
		Header:     make(http.Header),
	}
	for key, values := range rt.header {
		for _, value := range values {
			resp.Header.Add(key, value)
		}
	}
	return resp, nil
}

type stepFinishTestClient struct {
	responseContent string
	summaryContent  string
	responseUsage   *ai.TokenUsage
	usagePricing    aiusage.PricingModel
	seenMessages    []*ai.MsgInfo
	chatExCalls     int
}

type streamingStepFinishTestClient struct {
	deltas          []*ai.StreamDelta
	usage           *ai.TokenUsage
	chatExCalls     int
	chatStreamCalls int
}

func (c *stepFinishTestClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	return c.summaryContent, nil
}

func (c *stepFinishTestClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.chatExCalls++
	if c.chatExCalls == 1 && len(infos) > 1 {
		c.seenMessages = NormalizeHistoryMsgInfos(infos[1:])
	}
	content := c.summaryContent
	usage := (*ai.TokenUsage)(nil)
	if c.chatExCalls == 1 {
		content = c.responseContent
		usage = ai.NormalizeTokenUsagePtr(c.responseUsage)
	}
	msg := ai.NewAIMsgInfo(content)
	msg.Usage = usage
	return []*ai.ChatChoices{{
		Message:      msg,
		Usage:        usage,
		FinishReason: "stop",
	}}, nil
}

func (c *stepFinishTestClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	return c.summaryContent, nil
}

func (c *stepFinishTestClient) UsagePricingModel() aiusage.PricingModel {
	return c.usagePricing
}

func (c *streamingStepFinishTestClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *streamingStepFinishTestClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.chatExCalls++
	return nil, nil
}

func (c *streamingStepFinishTestClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *streamingStepFinishTestClient) ChatStream(ctx context.Context, infos []*ai.MsgInfo, handler ai.StreamHandler, tools ...*ai.FunctionTool) error {
	c.chatStreamCalls++
	for _, item := range c.deltas {
		if item == nil {
			continue
		}
		cp := *item
		if err := handler(&cp, false); err != nil {
			return err
		}
	}
	if handler != nil {
		return handler(nil, true)
	}
	return nil
}

func (c *streamingStepFinishTestClient) LastTokenUsage() *ai.TokenUsage {
	return ai.NormalizeTokenUsagePtr(c.usage)
}

func TestAgentAICallProxy_CompressesHistoryAfterResponse(t *testing.T) {
	client := &stepFinishTestClient{
		responseContent: "final-answer",
		summaryContent:  "compacted-summary",
		responseUsage: &ai.TokenUsage{
			InputTokens:  100000,
			OutputTokens: 1,
		},
	}

	events := make([]*AgentOutputEvent, 0, 8)
	emitter := NewEmitter("sess-1", "agent-1", func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		cp := *e
		events = append(events, &cp)
		return nil
	})

	agent, err := NewReActAgent(
		"history-agent",
		client,
		WithEmitter(emitter),
		WithHistoryCompressor(NewAIHistoryCompressorWithTokenBudget(1, 1)),
		WithInitialHistory([]*ai.MsgInfo{
			ai.NewUserMsgInfo("q1"),
			ai.NewAIMsgInfo("a1"),
			ai.NewUserMsgInfo("q2"),
			ai.NewAIMsgInfo("a2"),
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	result, err := agent.AICallProxy(context.Background(), 1, client, "system prompt", "")
	if err != nil {
		t.Fatalf("aiCallProxy failed: %v", err)
	}
	if result == nil || result.Compaction == nil || !result.Compaction.DidCompact {
		t.Fatalf("expected compaction result, got %#v", result)
	}

	if len(client.seenMessages) != 0 {
		t.Fatalf("expected no raw history to be sent to the model, got %d messages", len(client.seenMessages))
	}

	var stepFinish *AgentOutputEvent
	var compacted *AgentOutputEvent
	for _, event := range events {
		if event == nil {
			continue
		}
		if event.Type == EventTypeStepFinish {
			stepFinish = event
		}
		if event.Type == EventTypeHistoryCompacted {
			compacted = event
		}
	}
	if stepFinish == nil {
		t.Fatalf("expected step_finish event")
	}
	if compacted == nil {
		t.Fatalf("expected history_compacted event")
	}
	usagePayload, _ := stepFinish.Payload["usage"].(map[string]int)
	if usagePayload["input_tokens"] != 100000 {
		t.Fatalf("unexpected step_finish usage payload: %#v", stepFinish.Payload)
	}
}

func TestAgentAICallProxy_StreamsReasoningDeltasBeforeStepFinish(t *testing.T) {
	client := &streamingStepFinishTestClient{
		deltas: []*ai.StreamDelta{
			{ReasoningContent: "思考1"},
			{ReasoningContent: "思考2"},
			{Content: "答案", FinishReason: "stop"},
		},
		usage: &ai.TokenUsage{
			InputTokens:  12,
			OutputTokens: 4,
		},
	}

	events := make([]*AgentOutputEvent, 0, 8)
	emitter := NewEmitter("sess-stream", "agent-stream", func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		cp := *e
		events = append(events, &cp)
		return nil
	})

	agent, err := NewReActAgent(
		"stream-agent",
		client,
		WithEmitter(emitter),
		WithMaxIterations(1),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	result, err := agent.AICallProxy(context.Background(), 1, client, "system prompt", "")
	if err != nil {
		t.Fatalf("aiCallProxy failed: %v", err)
	}
	if result == nil || result.AssistantText != "答案" {
		t.Fatalf("unexpected aiCallProxy result: %#v", result)
	}
	if client.chatStreamCalls != 1 {
		t.Fatalf("expected ChatStream to be used once, got %d", client.chatStreamCalls)
	}
	if client.chatExCalls != 0 {
		t.Fatalf("expected ChatEx not to be used, got %d", client.chatExCalls)
	}

	var (
		thinkEvents []*AgentOutputEvent
		streamEvent *AgentOutputEvent
		stepFinish  *AgentOutputEvent
	)
	for _, event := range events {
		if event == nil {
			continue
		}
		switch event.Type {
		case EventTypeThink:
			thinkEvents = append(thinkEvents, event)
		case EventTypeStream:
			streamEvent = event
		case EventTypeStepFinish:
			stepFinish = event
		}
	}

	if len(thinkEvents) != 2 {
		t.Fatalf("expected 2 streamed think events, got %d", len(thinkEvents))
	}
	if got := thinkEvents[0].Payload["think_content"]; got != "思考1" {
		t.Fatalf("unexpected first think delta: %#v", got)
	}
	if got := thinkEvents[1].Payload["reasoning_content"]; got != "思考1思考2" {
		t.Fatalf("unexpected cumulative reasoning snapshot: %#v", got)
	}
	if streamEvent == nil || streamEvent.Content != "答案" {
		t.Fatalf("expected answer stream event, got %#v", streamEvent)
	}
	if stepFinish == nil {
		t.Fatalf("expected step_finish event")
	}
	if got := stepFinish.Payload["reasoning_content"]; got != "思考1思考2" {
		t.Fatalf("unexpected step_finish reasoning payload: %#v", stepFinish.Payload)
	}
	if got := stepFinish.Payload["content"]; got != "答案" {
		t.Fatalf("unexpected step_finish content payload: %#v", stepFinish.Payload)
	}
}

func TestAgentAICallProxy_NormalizesSnapshotStyleReasoningDeltas(t *testing.T) {
	transport := &agentStreamRoundTripper{
		body: strings.Join([]string{
			`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"用户"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"reasoning_content":"发送"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"reasoning_content":"用户发送了“你好”"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"你好！"},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
			"",
		}, "\n"),
		header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
	}
	client := openai.NewClient(
		openai.WithURL("https://example.com/v1"),
		openai.WithModel("gpt-4o-mini"),
		openai.WithStream(false),
		openai.WithHTTPClient(&http.Client{Transport: transport}),
	)

	events := make([]*AgentOutputEvent, 0, 8)
	emitter := NewEmitter("sess-stream-reasoning", "agent-stream-reasoning", func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		cp := *e
		events = append(events, &cp)
		return nil
	})

	agent, err := NewReActAgent(
		"stream-reasoning-agent",
		client,
		WithEmitter(emitter),
		WithMaxIterations(1),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	result, err := agent.AICallProxy(context.Background(), 1, client, "system prompt", "")
	if err != nil {
		t.Fatalf("aiCallProxy failed: %v", err)
	}
	if result == nil || result.AssistantText != "你好！" {
		t.Fatalf("unexpected aiCallProxy result: %#v", result)
	}

	var thinkEvents []*AgentOutputEvent
	var stepFinish *AgentOutputEvent
	for _, event := range events {
		if event == nil {
			continue
		}
		switch event.Type {
		case EventTypeThink:
			thinkEvents = append(thinkEvents, event)
		case EventTypeStepFinish:
			stepFinish = event
		}
	}

	if len(thinkEvents) != 3 {
		t.Fatalf("expected 3 think events, got %#v", thinkEvents)
	}
	if got := thinkEvents[0].Payload["think_content"]; got != "用户" {
		t.Fatalf("unexpected first think delta: %#v", got)
	}
	if got := thinkEvents[1].Payload["think_content"]; got != "发送" {
		t.Fatalf("unexpected second think delta: %#v", got)
	}
	if got := thinkEvents[2].Payload["think_content"]; got != "了“你好”" {
		t.Fatalf("unexpected normalized third think delta: %#v", got)
	}
	if got := thinkEvents[2].Payload["reasoning_content"]; got != "用户发送了“你好”" {
		t.Fatalf("unexpected cumulative reasoning snapshot: %#v", got)
	}
	if stepFinish == nil {
		t.Fatalf("expected step_finish event")
	}
	if got := stepFinish.Payload["reasoning_content"]; got != "用户发送了“你好”" {
		t.Fatalf("unexpected step_finish reasoning payload: %#v", stepFinish.Payload)
	}
	if got := stepFinish.Payload["content"]; got != "你好！" {
		t.Fatalf("unexpected step_finish content payload: %#v", stepFinish.Payload)
	}
}

func TestAgentAICallProxy_StreamToolCallArgumentsRemainDeduplicated(t *testing.T) {
	transport := &agentStreamRoundTripper{
		body: strings.Join([]string{
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_lookup_1","type":"function","function":{"name":"mock_lookup_tool","arguments":"{\"target\":\"or"}}]}}]}`,
			`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"target\":\"order-service\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
			"",
		}, "\n"),
		header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
	}
	client := openai.NewClient(
		openai.WithURL("https://example.com/v1"),
		openai.WithModel("gpt-4o-mini"),
		openai.WithStream(false),
		openai.WithHTTPClient(&http.Client{Transport: transport}),
	)
	emitter := NewEmitter("sess-stream-tool", "agent-stream-tool", func(e *AgentOutputEvent) error {
		return nil
	})

	agent, err := NewReActAgent(
		"stream-tool-agent",
		client,
		WithEmitter(emitter),
		WithMaxIterations(1),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	result, err := agent.AICallProxy(context.Background(), 1, client, "system prompt", "")
	if err != nil {
		t.Fatalf("aiCallProxy failed: %v", err)
	}
	if result == nil {
		t.Fatalf("expected aiCallProxy result")
	}
	if result.FinishReason != "tool_calls" {
		t.Fatalf("unexpected finish reason: %#v", result)
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0] == nil || result.ToolCalls[0].Function == nil {
		t.Fatalf("unexpected tool call result: %#v", result.ToolCalls)
	}
	gotArgs, _ := result.ToolCalls[0].Function.Arguments.(string)
	if gotArgs != `{"target":"order-service"}` {
		t.Fatalf("unexpected deduplicated tool args: %q", gotArgs)
	}
}
