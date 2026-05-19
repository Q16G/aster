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
