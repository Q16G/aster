package react

import (
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
)

type stepHistoryCompactionTestClient struct {
	summaries   []string
	chatExCalls int
	inputLimit  int
	outputLimit int
	modelName   string
}

func (c *stepHistoryCompactionTestClient) ModelContextInfo() ai.ModelContextInfo {
	info := ai.ModelContextInfo{
		ModelName:        strings.TrimSpace(c.modelName),
		InputTokenLimit:  c.inputLimit,
		OutputTokenLimit: c.outputLimit,
	}
	if info.ModelName == "" {
		info.ModelName = "test-model"
	}
	return info.Normalize()
}

func (c *stepHistoryCompactionTestClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *stepHistoryCompactionTestClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *stepHistoryCompactionTestClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.chatExCalls++
	summary := "summary"
	if len(c.summaries) > 0 {
		idx := c.chatExCalls - 1
		if idx >= 0 && idx < len(c.summaries) {
			summary = c.summaries[idx]
		}
	}
	return []*ai.ChatChoices{{
		Message:      ai.NewAIMsgInfo(summary),
		FinishReason: "stop",
	}}, nil
}

func makeToolRound(callID string, toolResult string) []*ai.MsgInfo {
	tc := &ai.FunctionTool{
		Id:   strings.TrimSpace(callID),
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:      "demo_tool",
			Arguments: "{}",
		},
	}
	assistant := ai.NewAIMsgInfo("")
	assistant.ToolCalls = []*ai.FunctionTool{tc}
	return []*ai.MsgInfo{
		assistant,
		ai.NewToolCallResultMsgInfo(toolResult, callID),
	}
}

func TestAIStepHistoryCompactor_PreservesToolCallSequenceAfterLayer2(t *testing.T) {
	client := &stepHistoryCompactionTestClient{
		summaries:   []string{"S1", "S2"},
		inputLimit:  260,
		outputLimit: 50,
	}

	history := make([]*ai.MsgInfo, 0, 8)
	history = append(history, makeToolRound("call-1", strings.Repeat("a", 1200))...)
	history = append(history, makeToolRound("call-2", strings.Repeat("b", 1200))...)
	history = append(history, makeToolRound("call-3", "ok")...)

	compactor := NewAIStepHistoryCompactor(0.90, 1, 1_000_000, nil)
	res, err := compactor.Compact(context.Background(), client, "ins", "system", history)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if res == nil || !res.DidCompact || !res.CanContinue {
		t.Fatalf("expected successful compaction, got %#v", res)
	}
	if res.AttemptCount == 0 || client.chatExCalls == 0 {
		t.Fatalf("expected layer2 summarize calls, got attempts=%d calls=%d", res.AttemptCount, client.chatExCalls)
	}
	if err := validateToolCallSequence(res.StepHistory); err != nil {
		t.Fatalf("expected valid tool call sequence after compaction, got: %v", err)
	}
	// Keep last round intact.
	foundCall3 := false
	for _, msg := range res.StepHistory {
		if msg == nil {
			continue
		}
		if strings.TrimSpace(msg.Role) == "tool" && strings.TrimSpace(msg.ToolCallID) == "call-3" {
			foundCall3 = true
			break
		}
	}
	if !foundCall3 {
		t.Fatalf("expected last tool round (call-3) to remain present")
	}
}

func TestAIStepHistoryCompactor_Layer1ShortensOldToolResultsOnly(t *testing.T) {
	client := &stepHistoryCompactionTestClient{
		inputLimit:  220,
		outputLimit: 50,
	}

	history := make([]*ai.MsgInfo, 0, 8)
	history = append(history, makeToolRound("call-1", strings.Repeat("a", 4000))...)
	history = append(history, makeToolRound("call-2", strings.Repeat("b", 4000))...)
	history = append(history, makeToolRound("call-3", strings.Repeat("c", 10))...)

	compactor := NewAIStepHistoryCompactor(0.90, 1, 64, nil)
	res, err := compactor.Compact(context.Background(), client, "ins", "system", history)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if res == nil || !res.DidCompact || !res.CanContinue {
		t.Fatalf("expected successful compaction, got %#v", res)
	}
	// Layer1 should be sufficient to exit; no summarize calls needed.
	if client.chatExCalls != 0 {
		t.Fatalf("expected no layer2 summarize calls, got %d", client.chatExCalls)
	}
	if err := validateToolCallSequence(res.StepHistory); err != nil {
		t.Fatalf("expected valid tool call sequence after layer1, got: %v", err)
	}
	// call-3 (kept window) should not be shortened.
	for _, msg := range res.StepHistory {
		if msg == nil || strings.TrimSpace(msg.Role) != "tool" {
			continue
		}
		if strings.TrimSpace(msg.ToolCallID) != "call-3" {
			continue
		}
		body, _ := msg.Content.(string)
		if strings.Contains(body, stepHistoryToolResultShortenedHint) {
			t.Fatalf("expected kept tool_result not to be shortened: %#v", msg)
		}
	}
}

func TestAIStepHistoryCompactor_DynamicWindowShrinkOnEmptySummary(t *testing.T) {
	client := &stepHistoryCompactionTestClient{
		// First summarize attempt returns empty -> should shrink the window and retry.
		summaries:   []string{"", "OK"},
		inputLimit:  240,
		outputLimit: 50,
	}

	history := make([]*ai.MsgInfo, 0, 12)
	history = append(history, makeToolRound("call-1", strings.Repeat("a", 1200))...)
	history = append(history, makeToolRound("call-2", strings.Repeat("b", 1200))...)
	history = append(history, makeToolRound("call-3", "ok")...)

	compactor := NewAIStepHistoryCompactor(0.90, 1, 1_000_000, nil)
	res, err := compactor.Compact(context.Background(), client, "ins", "system", history)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if res == nil || !res.DidCompact || !res.CanContinue {
		t.Fatalf("expected successful compaction, got %#v", res)
	}
	if client.chatExCalls < 2 {
		t.Fatalf("expected retry after empty summary, got %d calls", client.chatExCalls)
	}
	if err := validateToolCallSequence(res.StepHistory); err != nil {
		t.Fatalf("expected valid tool call sequence after compaction, got: %v", err)
	}
}

func TestShortenOldToolResults_ChatContextWithImages(t *testing.T) {
	tc := &ai.FunctionTool{
		Id:   "call-img",
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:      "screenshot",
			Arguments: "{}",
		},
	}
	assistant := ai.NewAIMsgInfo("")
	assistant.ToolCalls = []*ai.FunctionTool{tc}

	toolMsg := ai.NewToolCallResultMsgInfo([]*ai.ChatContext{
		{Type: "text", Text: strings.Repeat("x", 200)},
		{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAAA"}},
	}, "call-img")

	keepRound := makeToolRound("call-keep", "kept")
	history := []*ai.MsgInfo{assistant, toolMsg}
	history = append(history, keepRound...)

	result, did := shortenOldToolResults(history, 1, 50)
	if !did {
		t.Fatalf("expected shortenOldToolResults to modify history")
	}

	for _, msg := range result {
		if msg == nil || strings.TrimSpace(msg.ToolCallID) != "call-img" {
			continue
		}
		switch content := msg.Content.(type) {
		case []*ai.ChatContext:
			for _, ctx := range content {
				if ctx.Type == "image_url" {
					t.Fatalf("expected images to be stripped from old tool result")
				}
			}
			for _, ctx := range content {
				if ctx.Type == "text" && !strings.Contains(ctx.Text, stepHistoryToolResultShortenedHint) {
					t.Fatalf("expected text to be shortened with hint, got %q", ctx.Text)
				}
			}
		case string:
			if !strings.Contains(content, stepHistoryToolResultShortenedHint) {
				t.Fatalf("expected shortened hint in string content, got %q", content)
			}
		default:
			t.Fatalf("unexpected content type %T", msg.Content)
		}
	}
}

func TestStripImagesFromExcerpt(t *testing.T) {
	msgs := []*ai.MsgInfo{
		{
			Role: "user",
			Content: []*ai.ChatContext{
				{Type: "text", Text: "analyze this"},
				{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAAA"}},
			},
		},
		{
			Role:    "assistant",
			Content: "ok I see the image",
		},
	}

	result := stripImagesFromExcerpt(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	contexts, ok := result[0].Content.([]*ai.ChatContext)
	if !ok {
		t.Fatalf("expected []*ai.ChatContext, got %T", result[0].Content)
	}
	for _, ctx := range contexts {
		if ctx.Type == "image_url" {
			t.Fatalf("expected image_url to be replaced with [image] placeholder")
		}
	}
	hasPlaceholder := false
	for _, ctx := range contexts {
		if ctx.Type == "text" && ctx.Text == "[image]" {
			hasPlaceholder = true
		}
	}
	if !hasPlaceholder {
		t.Fatalf("expected [image] placeholder in stripped content")
	}

	// String content should be unchanged.
	if result[1].Content != "ok I see the image" {
		t.Fatalf("expected string content to be preserved, got %v", result[1].Content)
	}
}

func TestStripImagesFromExcerpt_DoesNotMutateOriginal(t *testing.T) {
	original := &ai.MsgInfo{
		Role: "user",
		Content: []*ai.ChatContext{
			{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAAA"}},
		},
	}
	msgs := []*ai.MsgInfo{original}

	result := stripImagesFromExcerpt(msgs)

	origContexts := original.Content.([]*ai.ChatContext)
	if origContexts[0].Type != "image_url" {
		t.Fatalf("original message was mutated")
	}

	resultContexts, ok := result[0].Content.([]*ai.ChatContext)
	if !ok {
		t.Fatalf("expected []*ai.ChatContext result")
	}
	if resultContexts[0].Type != "text" || resultContexts[0].Text != "[image]" {
		t.Fatalf("expected [image] placeholder, got %+v", resultContexts[0])
	}
}

func TestShortenOldToolResults_ChatContextImageOnly(t *testing.T) {
	tc := &ai.FunctionTool{
		Id:   "call-imgonly",
		Type: "function",
		Function: &ai.FunctionDetail{Name: "screenshot", Arguments: "{}"},
	}
	assistant := ai.NewAIMsgInfo("")
	assistant.ToolCalls = []*ai.FunctionTool{tc}

	toolMsg := ai.NewToolCallResultMsgInfo([]*ai.ChatContext{
		{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAAA"}},
	}, "call-imgonly")

	keepRound := makeToolRound("call-keep2", "kept")
	history := []*ai.MsgInfo{assistant, toolMsg}
	history = append(history, keepRound...)

	result, did := shortenOldToolResults(history, 1, 50)
	if !did {
		t.Fatalf("expected modification")
	}

	for _, msg := range result {
		if msg == nil || strings.TrimSpace(msg.ToolCallID) != "call-imgonly" {
			continue
		}
		s, ok := msg.Content.(string)
		if !ok {
			t.Fatalf("expected string content when all images stripped, got %T", msg.Content)
		}
		if s != "[content truncated]" {
			t.Fatalf("unexpected content: %q", s)
		}
	}
}

func TestShortenOldToolResults_ChatContextPreservesDetail(t *testing.T) {
	tc := &ai.FunctionTool{
		Id:   "call-detail",
		Type: "function",
		Function: &ai.FunctionDetail{Name: "tool", Arguments: "{}"},
	}
	assistant := ai.NewAIMsgInfo("")
	assistant.ToolCalls = []*ai.FunctionTool{tc}

	toolMsg := ai.NewToolCallResultMsgInfo([]*ai.ChatContext{
		{Type: "text", Text: "short", Detail: "high"},
		{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAAA"}},
	}, "call-detail")

	keepRound := makeToolRound("call-k", "kept")
	history := []*ai.MsgInfo{assistant, toolMsg}
	history = append(history, keepRound...)

	result, did := shortenOldToolResults(history, 1, 5000)
	if !did {
		t.Fatalf("expected modification due to image removal")
	}

	for _, msg := range result {
		if msg == nil || strings.TrimSpace(msg.ToolCallID) != "call-detail" {
			continue
		}
		contexts, ok := msg.Content.([]*ai.ChatContext)
		if !ok {
			t.Fatalf("expected []*ai.ChatContext, got %T", msg.Content)
		}
		if len(contexts) != 1 {
			t.Fatalf("expected 1 context, got %d", len(contexts))
		}
		if contexts[0].Detail != "high" {
			t.Fatalf("expected Detail preserved, got %q", contexts[0].Detail)
		}
	}
}

func TestStripImagesFromExcerpt_AllImages(t *testing.T) {
	msgs := []*ai.MsgInfo{
		{
			Role: "user",
			Content: []*ai.ChatContext{
				{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,A"}},
				{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,B"}},
			},
		},
	}
	result := stripImagesFromExcerpt(msgs)
	contexts, ok := result[0].Content.([]*ai.ChatContext)
	if !ok {
		t.Fatalf("expected []*ai.ChatContext, got %T", result[0].Content)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 placeholders, got %d", len(contexts))
	}
	for _, ctx := range contexts {
		if ctx.Type != "text" || ctx.Text != "[image]" {
			t.Fatalf("expected [image] placeholder, got %+v", ctx)
		}
	}
}

func TestStripImagesFromExcerpt_NilMessages(t *testing.T) {
	msgs := []*ai.MsgInfo{nil, nil}
	result := stripImagesFromExcerpt(msgs)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestStripImagesFromExcerpt_EmptyChatContext(t *testing.T) {
	msgs := []*ai.MsgInfo{
		{
			Role:    "user",
			Content: []*ai.ChatContext{},
		},
	}
	result := stripImagesFromExcerpt(msgs)
	s, ok := result[0].Content.(string)
	if !ok {
		t.Fatalf("expected string for empty contexts, got %T", result[0].Content)
	}
	if s != "[image]" {
		t.Fatalf("unexpected: %q", s)
	}
}
