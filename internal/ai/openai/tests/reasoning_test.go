package openai_test

import (
	. "aster/internal/ai/openai"
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"aster/internal/ai"
)

type reasoningRoundTripper struct {
	statusCode int
	body       string
	reqBody    string
	header     http.Header
}

func (rt *reasoningRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req != nil && req.Body != nil {
		data, err := io.ReadAll(req.Body)
		if err == nil {
			rt.reqBody = string(data)
		}
	}
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

func captureOpenAILogOutput(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()

	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	}()

	fn()
	return buf.String()
}

func TestParseJSONResponse_PreservesReasoningContent(t *testing.T) {
	transport := &reasoningRoundTripper{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"答案","reasoning_content":"推理过程"}}],"usage":{"prompt_tokens":120,"completion_tokens":45,"completion_tokens_details":{"reasoning_tokens":12},"prompt_tokens_details":{"cached_tokens":18}}}`}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("你好")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.ReasoningOutput; got != "推理过程" {
		t.Fatalf("expected reasoning_content to be preserved, got %q", got)
	}
	if choices[0].Usage == nil {
		t.Fatalf("expected usage present")
	}
	if choices[0].Usage.InputTokens != 102 || choices[0].Usage.OutputTokens != 45 {
		t.Fatalf("unexpected usage: %#v", choices[0].Usage)
	}
	if choices[0].Usage.ReasoningTokens != 12 || choices[0].Usage.CacheReadTokens != 18 {
		t.Fatalf("unexpected reasoning/cache usage: %#v", choices[0].Usage)
	}
	if last := client.LastTokenUsage(); last == nil || last.ContextCountTokens() != 165 {
		t.Fatalf("expected last usage context count 165, got %#v", last)
	}
}

func TestParseJSONResponse_PreservesReasoningAlias(t *testing.T) {
	transport := &reasoningRoundTripper{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"答案","reasoning":"推理别名"}}]} `}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("你好")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.ReasoningOutput; got != "推理别名" {
		t.Fatalf("expected reasoning alias to be preserved, got %q", got)
	}
}

func TestParseJSONResponse_PrefersReasoningContentOverAlias(t *testing.T) {
	transport := &reasoningRoundTripper{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"答案","reasoning_content":"主字段","reasoning":"别名字段"}}]} `}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("你好")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.ReasoningOutput; got != "主字段" {
		t.Fatalf("expected reasoning_content to win over alias, got %q", got)
	}
}

func TestParseJSONResponse_ExtractorAndThinkTags(t *testing.T) {
	transport := &reasoningRoundTripper{
		body: `noise {"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"<think>推理过程</think>最终答案"}}],"usage":{"prompt_tokens":30,"completion_tokens":9}} tail`,
	}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("你好")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.ReasoningOutput; got != "推理过程" {
		t.Fatalf("expected think reasoning, got %q", got)
	}
	if got := choices[0].Message.Content; got != "最终答案" {
		t.Fatalf("expected cleaned content, got %#v", got)
	}
}

func TestChatEx_ParsesJSONCompletionWhenHeaderClaimsEventStream(t *testing.T) {
	transport := &reasoningRoundTripper{
		body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"pong"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
		header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
	}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("ping")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.Content; got != "pong" {
		t.Fatalf("expected streamed content pong, got %#v", got)
	}
	if choices[0].Usage == nil || choices[0].Usage.InputTokens != 5 || choices[0].Usage.OutputTokens != 2 {
		t.Fatalf("unexpected streamed usage: %#v", choices[0].Usage)
	}
}

func TestChatEx_StrictlyRejectsSSEBodyWhenStreamDisabled(t *testing.T) {
	transport := &reasoningRoundTripper{
		body: strings.Join([]string{
			`data: {"choices":[{"index":0,"delta":{"role":"assistant"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`,
			`data: {"choices":[{"index":0,"delta":{"content":"pong"},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
			"",
		}, "\n"),
		header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
	}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("ping")})
	if err == nil {
		t.Fatalf("expected non-stream request to reject sse body")
	}
	if !strings.Contains(err.Error(), "unexpected sse body") {
		t.Fatalf("expected unexpected sse body error, got %v", err)
	}
}

func TestBuildRequestBody_EmbedsReasoningContent(t *testing.T) {
	transport := &reasoningRoundTripper{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("Q1"),
		{
			Role:            "assistant",
			Content:         "A1",
			ReasoningOutput: "R1",
		},
	}

	if _, err := client.ChatEx(context.Background(), history); err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if !strings.Contains(transport.reqBody, `"reasoning_content":"R1"`) {
		t.Fatalf("expected request body contains reasoning_content, got %s", transport.reqBody)
	}
}

func TestParseStreamResponse_PreservesReasoningContent(t *testing.T) {
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"思考1"}}],"usage":{"prompt_tokens":80,"completion_tokens":20,"completion_tokens_details":{"reasoning_tokens":8},"prompt_tokens_details":{"cached_tokens":10}}}`,
		`data: {"choices":[{"index":0,"delta":{"content":"最终答案"},"finish_reason":"stop"}]}`,
		"data: [DONE]",
		"",
	}, "\n")

	choices, _, err := client.ParseStreamResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseStreamResponse failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.ReasoningOutput; got != "思考1" {
		t.Fatalf("expected reasoning_content from stream, got %q", got)
	}
	if got := choices[0].Message.Content; got != "最终答案" {
		t.Fatalf("expected merged content from stream, got %#v", got)
	}
	if choices[0].Usage == nil {
		t.Fatalf("expected usage from stream")
	}
	if choices[0].Usage.ContextCountTokens() != 100 {
		t.Fatalf("expected context count 100, got %#v", choices[0].Usage)
	}
	if last := client.LastTokenUsage(); last == nil || last.ContextCountTokens() != 100 {
		t.Fatalf("expected last token usage from stream, got %#v", last)
	}
}

func TestParseStreamResponse_RejectsNonSSEJSONBody(t *testing.T) {
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
	)

	body := `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`
	_, _, err := client.ParseStreamResponse(strings.NewReader(body))
	if err == nil {
		t.Fatalf("expected parseStreamResponse to reject non-sse json body")
	}
	if !strings.Contains(err.Error(), "missing data prefix") {
		t.Fatalf("expected missing data prefix error, got %v", err)
	}
}

func TestParseStreamResponse_RawSSEDebugDisabledByDefault(t *testing.T) {
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"思考1"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"最终答案"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	output := captureOpenAILogOutput(t, func() {
		if _, _, err := client.ParseStreamResponse(strings.NewReader(body)); err != nil {
			t.Fatalf("parseStreamResponse failed: %v", err)
		}
	})

	if strings.Contains(output, "[openai.raw_sse]") {
		t.Fatalf("expected raw sse debug logs disabled by default, got %s", output)
	}
}

func TestParseStreamResponse_RawSSEDebugEnabledLogsRawLinesAndCandidates(t *testing.T) {
	t.Setenv("SAST_DEBUG_RAW_SSE", "1")

	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"思考1"}}]} {"choices":[{"index":1,"delta":{"content":"答复2"},"finish_reason":"stop"}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"最终答案"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	output := captureOpenAILogOutput(t, func() {
		if _, _, err := client.ParseStreamResponse(strings.NewReader(body)); err != nil {
			t.Fatalf("parseStreamResponse failed: %v", err)
		}
	})

	if !strings.Contains(output, `[openai.raw_sse] line=1 raw=data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"思考1"}}]} {"choices":[{"index":1,"delta":{"content":"答复2"},"finish_reason":"stop"}]}`) {
		t.Fatalf("expected raw line 1 in debug output, got %s", output)
	}
	if !strings.Contains(output, `[openai.raw_sse] line=1 candidate=1 json={"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"思考1"}}]}`) {
		t.Fatalf("expected candidate 1 in debug output, got %s", output)
	}
	if !strings.Contains(output, `[openai.raw_sse] line=1 candidate=2 json={"choices":[{"index":1,"delta":{"content":"答复2"},"finish_reason":"stop"}]}`) {
		t.Fatalf("expected candidate 2 in debug output, got %s", output)
	}
	if !strings.Contains(output, `[openai.raw_sse] line=3 raw=data: [DONE]`) {
		t.Fatalf("expected DONE raw line in debug output, got %s", output)
	}
}

func TestParseStreamResponse_PreservesReasoningAlias(t *testing.T) {
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"思考别名"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"最终答案"},"finish_reason":"stop"}]}`,
		"data: [DONE]",
		"",
	}, "\n")

	choices, _, err := client.ParseStreamResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseStreamResponse failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.ReasoningOutput; got != "思考别名" {
		t.Fatalf("expected reasoning alias from stream, got %q", got)
	}
	if got := choices[0].Message.Content; got != "最终答案" {
		t.Fatalf("expected merged content from stream, got %#v", got)
	}
}

func TestParseStreamResponse_PrefersReasoningContentOverAlias(t *testing.T) {
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"主字段","reasoning":"别名字段"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"最终答案"},"finish_reason":"stop"}]}`,
		"data: [DONE]",
		"",
	}, "\n")

	choices, _, err := client.ParseStreamResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseStreamResponse failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.ReasoningOutput; got != "主字段" {
		t.Fatalf("expected reasoning_content to win over alias in stream, got %q", got)
	}
}

func TestParseStreamResponse_ExtractorAndThinkTagsAcrossChunks(t *testing.T) {
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
	)

	body := strings.Join([]string{
		`data: noise {"choices":[{"index":0,"delta":{"role":"assistant","content":"<thi"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"nk>推理</th"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"ink>答案"},"finish_reason":"stop"}]}`,
		"data: [DONE]",
		"",
	}, "\n")

	choices, _, err := client.ParseStreamResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseStreamResponse failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.ReasoningOutput; got != "推理" {
		t.Fatalf("expected normalized think reasoning, got %q", got)
	}
	if got := choices[0].Message.Content; got != "答案" {
		t.Fatalf("expected normalized content, got %#v", got)
	}
}

func TestParseStreamResponse_StreamFuncReceivesNormalizedThinkDeltas(t *testing.T) {
	var streamedReasoning strings.Builder
	var streamedContent strings.Builder
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
		WithStreamFunc(func(event *StreamEvent) {
			if event == nil || event.Done {
				return
			}
			if strings.Contains(event.Content, "<think>") || strings.Contains(event.Content, "</think>") {
				t.Fatalf("stream content should not contain think tags: %q", event.Content)
			}
			if strings.Contains(event.ReasonContent, "<think>") || strings.Contains(event.ReasonContent, "</think>") {
				t.Fatalf("stream reasoning should not contain think tags: %q", event.ReasonContent)
			}
			streamedReasoning.WriteString(event.ReasonContent)
			streamedContent.WriteString(event.Content)
		}),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","content":"<think>思考</think>答"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"案"},"finish_reason":"stop"}]}`,
		"data: [DONE]",
		"",
	}, "\n")

	if _, _, err := client.ParseStreamResponse(strings.NewReader(body)); err != nil {
		t.Fatalf("parseStreamResponse failed: %v", err)
	}
	if got := streamedReasoning.String(); got != "思考" {
		t.Fatalf("expected streamed reasoning without tags, got %q", got)
	}
	if got := streamedContent.String(); got != "答案" {
		t.Fatalf("expected streamed content without tags, got %q", got)
	}
}

func TestParseStreamResponse_StreamFuncReceivesNormalizedReasoningContentDeltas(t *testing.T) {
	var streamedReasoning []string
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
		WithStreamFunc(func(event *StreamEvent) {
			if event == nil || event.Done {
				return
			}
			if event.ReasonContent != "" {
				streamedReasoning = append(streamedReasoning, event.ReasonContent)
			}
		}),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"用户"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"发送"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"reasoning_content":"用户发送了“你好”"}}]}`,
		`data: {"choices":[{"index":0,"delta":{"content":"你好！"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	choices, _, err := client.ParseStreamResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseStreamResponse failed: %v", err)
	}
	if len(streamedReasoning) != 3 {
		t.Fatalf("expected 3 streamed reasoning deltas, got %#v", streamedReasoning)
	}
	if streamedReasoning[0] != "用户" {
		t.Fatalf("unexpected first reasoning delta: %q", streamedReasoning[0])
	}
	if streamedReasoning[1] != "发送" {
		t.Fatalf("unexpected second reasoning delta: %q", streamedReasoning[1])
	}
	if streamedReasoning[2] != "了“你好”" {
		t.Fatalf("unexpected normalized snapshot reasoning delta: %q", streamedReasoning[2])
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.ReasoningOutput; got != "用户发送了“你好”" {
		t.Fatalf("unexpected final aggregated reasoning: %q", got)
	}
	if got := choices[0].Message.Content; got != "你好！" {
		t.Fatalf("unexpected final content: %#v", got)
	}
}

func TestNormalizeToolArgumentDelta(t *testing.T) {
	testCases := []struct {
		name     string
		existing string
		incoming string
		want     string
	}{
		{name: "empty existing keeps incoming", existing: "", incoming: `{"a":"1"}`, want: `{"a":"1"}`},
		{name: "empty incoming ignored", existing: `{"a":"1"}`, incoming: "", want: ""},
		{name: "prefix snapshot keeps suffix only", existing: `{"target":"or`, incoming: `{"target":"order-service"}`, want: `der-service"}`},
		{name: "identical snapshot emits empty delta", existing: `{"target":"order-service"}`, incoming: `{"target":"order-service"}`, want: ""},
		{name: "non prefix payload kept whole", existing: `{"target":"order-service"}`, incoming: `{"target":"billing-service"}`, want: `{"target":"billing-service"}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeToolArgumentDelta(tc.existing, tc.incoming); got != tc.want {
				t.Fatalf("NormalizeToolArgumentDelta(%q, %q) = %q, want %q", tc.existing, tc.incoming, got, tc.want)
			}
		})
	}
}

func TestParseStreamResponse_StreamFuncReceivesNormalizedToolCallArgumentDeltas(t *testing.T) {
	var streamedArgs []string
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
		WithStreamFunc(func(event *StreamEvent) {
			if event == nil || event.Done {
				return
			}
			for _, tool := range event.ToolCalls {
				if tool == nil || tool.Function == nil {
					continue
				}
				if args, ok := tool.Function.Arguments.(string); ok {
					streamedArgs = append(streamedArgs, args)
				}
			}
		}),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_lookup_1","type":"function","function":{"name":"mock_lookup_tool","arguments":"{\"target\":\"or"}}]}}]}`,
		`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"target\":\"order-service\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
		"",
	}, "\n")

	choices, _, err := client.ParseStreamResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseStreamResponse failed: %v", err)
	}
	if len(streamedArgs) != 2 {
		t.Fatalf("expected 2 streamed tool argument deltas, got %#v", streamedArgs)
	}
	if streamedArgs[0] != `{"target":"or` {
		t.Fatalf("unexpected first tool arguments delta: %q", streamedArgs[0])
	}
	if streamedArgs[1] != `der-service"}` {
		t.Fatalf("unexpected normalized second tool arguments delta: %q", streamedArgs[1])
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if len(choices[0].Message.ToolCalls) != 1 || choices[0].Message.ToolCalls[0] == nil || choices[0].Message.ToolCalls[0].Function == nil {
		t.Fatalf("unexpected tool calls: %#v", choices[0].Message.ToolCalls)
	}
	gotArgs, _ := choices[0].Message.ToolCalls[0].Function.Arguments.(string)
	if gotArgs != `{"target":"order-service"}` {
		t.Fatalf("unexpected final aggregated tool arguments: %q", gotArgs)
	}
}

func TestParseStreamResponse_UsageCompatibility_BedrockMetadata(t *testing.T) {
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"思考中"}}],"usage":{"inputTokens":"180","outputTokens":"20","metadata":{"bedrock":{"usage":{"cacheReadInputTokens":"60","cacheWriteInputTokens":"10"}}}}}`,
		`data: {"choices":[{"index":0,"delta":{"content":"stream-answer"},"finish_reason":"stop"}]}`,
		"data: [DONE]",
		"",
	}, "\n")

	choices, _, err := client.ParseStreamResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseStreamResponse failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if choices[0].Usage == nil {
		t.Fatalf("expected usage from stream")
	}
	if choices[0].Usage.InputTokens != 180 || choices[0].Usage.OutputTokens != 20 {
		t.Fatalf("unexpected stream input/output tokens: %#v", choices[0].Usage)
	}
	if choices[0].Usage.CacheReadTokens != 60 || choices[0].Usage.CacheWriteTokens != 10 {
		t.Fatalf("unexpected stream cache tokens: %#v", choices[0].Usage)
	}
	if got := choices[0].Usage.ContextCountTokens(); got != 270 {
		t.Fatalf("expected stream context count 270, got %d", got)
	}
	if last := client.LastTokenUsage(); last == nil || last.ContextCountTokens() != 270 {
		t.Fatalf("expected last stream token usage context count 270, got %#v", last)
	}
}

func TestParseStreamResponse_UsageCompatibility_LiteLLMStyle(t *testing.T) {
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(true),
	)

	body := strings.Join([]string{
		`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"thinking"}}],"usage":{"input_tokens":300,"output_tokens":50,"reasoning_tokens":9,"cache_read_input_tokens":80,"cache_creation_input_tokens":20}}`,
		`data: {"choices":[{"index":0,"delta":{"content":"stream-lite-result"},"finish_reason":"stop"}]}`,
		"data: [DONE]",
		"",
	}, "\n")

	choices, _, err := client.ParseStreamResponse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parseStreamResponse failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if choices[0].Usage == nil {
		t.Fatalf("expected usage from stream")
	}
	if choices[0].Usage.InputTokens != 200 || choices[0].Usage.OutputTokens != 50 {
		t.Fatalf("unexpected stream lite input/output tokens: %#v", choices[0].Usage)
	}
	if choices[0].Usage.ReasoningTokens != 9 {
		t.Fatalf("expected stream lite reasoning=9, got %#v", choices[0].Usage)
	}
	if choices[0].Usage.CacheReadTokens != 80 || choices[0].Usage.CacheWriteTokens != 20 {
		t.Fatalf("unexpected stream lite cache tokens: %#v", choices[0].Usage)
	}
	if got := choices[0].Usage.ContextCountTokens(); got != 350 {
		t.Fatalf("expected stream lite context count 350, got %d", got)
	}
	if last := client.LastTokenUsage(); last == nil || last.ContextCountTokens() != 350 {
		t.Fatalf("expected last stream lite token usage context count 350, got %#v", last)
	}
}

func TestChatStream_EmitsReasoningAndContentDeltas(t *testing.T) {
	transport := &reasoningRoundTripper{
		body: strings.Join([]string{
			`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":"思考1"}}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`,
			`data: {"choices":[{"index":0,"delta":{"reasoning_content":"思考2","content":"答"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"案"},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
			"",
		}, "\n"),
		header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
	}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	var (
		reasoning strings.Builder
		content   strings.Builder
		finishes  []string
		doneCalls int
	)
	err := client.ChatStream(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("你好")}, func(delta *ai.StreamDelta, done bool) error {
		if done {
			doneCalls++
			return nil
		}
		if delta == nil {
			t.Fatalf("expected non-nil delta before done")
		}
		reasoning.WriteString(delta.ReasoningContent)
		content.WriteString(delta.Content)
		if delta.FinishReason != "" {
			finishes = append(finishes, delta.FinishReason)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	if got := reasoning.String(); got != "思考1思考2" {
		t.Fatalf("unexpected reasoning deltas: %q", got)
	}
	if got := content.String(); got != "答案" {
		t.Fatalf("unexpected content deltas: %q", got)
	}
	if len(finishes) != 1 || finishes[0] != "stop" {
		t.Fatalf("unexpected finish reasons: %#v", finishes)
	}
	if doneCalls != 1 {
		t.Fatalf("expected done callback once, got %d", doneCalls)
	}
	if last := client.LastTokenUsage(); last == nil || last.ContextCountTokens() != 14 {
		t.Fatalf("expected last usage context count 14, got %#v", last)
	}
}

func TestChatStream_EmitsReasoningAliasAndContentDeltas(t *testing.T) {
	transport := &reasoningRoundTripper{
		body: strings.Join([]string{
			`data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"思考1"}}],"usage":{"prompt_tokens":11,"completion_tokens":3}}`,
			`data: {"choices":[{"index":0,"delta":{"reasoning":"思考2","content":"答"}}]}`,
			`data: {"choices":[{"index":0,"delta":{"content":"案"},"finish_reason":"stop"}]}`,
			`data: [DONE]`,
			"",
		}, "\n"),
		header: http.Header{
			"Content-Type": []string{"text/event-stream; charset=utf-8"},
		},
	}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	var (
		reasoning strings.Builder
		content   strings.Builder
	)
	err := client.ChatStream(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("你好")}, func(delta *ai.StreamDelta, done bool) error {
		if done {
			return nil
		}
		if delta == nil {
			t.Fatalf("expected non-nil delta before done")
		}
		reasoning.WriteString(delta.ReasoningContent)
		content.WriteString(delta.Content)
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}
	if got := reasoning.String(); got != "思考1思考2" {
		t.Fatalf("unexpected reasoning alias deltas: %q", got)
	}
	if got := content.String(); got != "答案" {
		t.Fatalf("unexpected content deltas: %q", got)
	}
}

func TestParseJSONResponse_UsageCompatibility_LiteLLMStyle(t *testing.T) {
	transport := &reasoningRoundTripper{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"input_tokens":300,"output_tokens":50,"reasoning_tokens":9,"cache_read_input_tokens":80,"cache_creation_input_tokens":20}}`}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if choices[0].Usage == nil {
		t.Fatalf("expected usage present")
	}
	if choices[0].Usage.InputTokens != 200 {
		t.Fatalf("expected adjusted input tokens=200, got %#v", choices[0].Usage)
	}
	if choices[0].Usage.OutputTokens != 50 || choices[0].Usage.ReasoningTokens != 9 {
		t.Fatalf("unexpected output/reasoning tokens: %#v", choices[0].Usage)
	}
	if choices[0].Usage.CacheReadTokens != 80 || choices[0].Usage.CacheWriteTokens != 20 {
		t.Fatalf("unexpected cache tokens: %#v", choices[0].Usage)
	}
	if got := choices[0].Usage.ContextCountTokens(); got != 350 {
		t.Fatalf("expected context count 350, got %d", got)
	}
}

func TestParseJSONResponse_UsageCompatibility_AnthropicMetadata(t *testing.T) {
	transport := &reasoningRoundTripper{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"input_tokens":220,"output_tokens":30,"metadata":{"anthropic":{"cacheReadInputTokens":100,"cacheCreationInputTokens":40}}}}`}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Usage == nil {
		t.Fatalf("unexpected choices/usage: %#v", choices)
	}
	if choices[0].Usage.InputTokens != 220 {
		t.Fatalf("expected anthropic style input kept as 220, got %#v", choices[0].Usage)
	}
	if choices[0].Usage.CacheReadTokens != 100 || choices[0].Usage.CacheWriteTokens != 40 {
		t.Fatalf("unexpected anthropic cache tokens: %#v", choices[0].Usage)
	}
	if got := choices[0].Usage.ContextCountTokens(); got != 390 {
		t.Fatalf("expected context count 390, got %d", got)
	}
}

func TestParseJSONResponse_UsageCompatibility_BedrockMetadataStringValues(t *testing.T) {
	transport := &reasoningRoundTripper{body: `{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"inputTokens":"180","outputTokens":"20","output_tokens_details":{"reasoning_tokens":"5"},"metadata":{"bedrock":{"usage":{"cacheReadInputTokens":"60","cacheWriteInputTokens":"10"}}}}}`}
	client := NewClient(
		WithURL("https://example.com/v1"),
		WithModel("gpt-4o-mini"),
		WithStream(false),
		WithHTTPClient(&http.Client{Transport: transport}),
	)

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{ai.NewUserMsgInfo("hello")})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Usage == nil {
		t.Fatalf("unexpected choices/usage: %#v", choices)
	}
	if choices[0].Usage.InputTokens != 180 || choices[0].Usage.OutputTokens != 20 {
		t.Fatalf("unexpected bedrock input/output tokens: %#v", choices[0].Usage)
	}
	if choices[0].Usage.ReasoningTokens != 5 {
		t.Fatalf("expected reasoning=5, got %#v", choices[0].Usage)
	}
	if choices[0].Usage.CacheReadTokens != 60 || choices[0].Usage.CacheWriteTokens != 10 {
		t.Fatalf("unexpected bedrock cache tokens: %#v", choices[0].Usage)
	}
	if got := choices[0].Usage.ContextCountTokens(); got != 270 {
		t.Fatalf("expected context count 270, got %d", got)
	}
	if last := client.LastTokenUsage(); last == nil || last.ContextCountTokens() != 270 {
		t.Fatalf("expected last token usage context count 270, got %#v", last)
	}
}
