package react_test

import (
	. "aster/internal/react"
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
)

type historyCompactionTestClient struct {
	response         string
	summary          string
	lastChatText     string
	lastChatMessages []*ai.MsgInfo
	chatExCalls      int
}

func (c *historyCompactionTestClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	return c.summary, nil
}

func (c *historyCompactionTestClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.chatExCalls++
	c.lastChatMessages = NormalizeHistoryMsgInfos(infos)
	content := c.summary
	if c.response != "" && c.chatExCalls == 1 {
		content = c.response
	}
	return []*ai.ChatChoices{{Message: ai.NewAIMsgInfo(content)}}, nil
}

func (c *historyCompactionTestClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	c.lastChatText = text
	return c.summary, nil
}

func TestApplyHistoryCompactionWindow_UseLatestBoundary(t *testing.T) {
	req1 := ai.NewUserMsgInfo(HistoryCompactionRequestText)
	req1.Type = HistoryCompactionRequestType
	sum1 := ai.NewAIMsgInfo("summary-1")
	sum1.Type = HistoryCompactionSummaryType
	req2 := ai.NewUserMsgInfo(HistoryCompactionRequestText)
	req2.Type = HistoryCompactionRequestType
	sum2 := ai.NewAIMsgInfo("summary-2")
	sum2.Type = HistoryCompactionSummaryType

	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("old-q"),
		ai.NewAIMsgInfo("old-a"),
		req1,
		sum1,
		ai.NewUserMsgInfo("mid-q"),
		ai.NewAIMsgInfo("mid-a"),
		req2,
		sum2,
		ai.NewUserMsgInfo("new-q"),
	}

	windowed := ApplyHistoryCompactionWindow(history)
	if len(windowed) != 3 {
		t.Fatalf("expected latest boundary window len=3, got %d", len(windowed))
	}
	if windowed[0] != req2 {
		t.Fatalf("expected boundary to start from latest request marker")
	}
	if windowed[1] != sum2 {
		t.Fatalf("expected second message to be latest summary marker")
	}
}

func TestAIHistoryCompressor_CompressWithCompactionMarkers(t *testing.T) {
	compressor := NewAIHistoryCompressorWithTokenBudget(1_000_000, 0)
	client := &historyCompactionTestClient{summary: "compacted-summary"}
	a1 := ai.NewAIMsgInfo("a1")
	a2 := ai.NewAIMsgInfo("a2")
	// 压缩触发条件：使用最新 assistant usage 判定上下文溢出。
	a2.Usage = &ai.TokenUsage{
		InputTokens:  100000,
		OutputTokens: 1,
	}
	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("q1"),
		a1,
		ai.NewUserMsgInfo("q2"),
		a2,
	}

	result, err := compressor.Compress(context.Background(), client, "ins", history)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	out := result.History
	if len(out) < 2 {
		t.Fatalf("expected compaction markers in output, got len=%d", len(out))
	}
	if !result.DidCompact || result.State != CompactionStateDone {
		t.Fatalf("expected compaction done result, got %#v", result)
	}
	if out[0].Role != "user" || out[0].Type != HistoryCompactionRequestType || out[0].Content != HistoryCompactionRequestText {
		t.Fatalf("unexpected compaction request marker: %#v", out[0])
	}
	if out[1].Role != "assistant" || out[1].Type != HistoryCompactionSummaryType || out[1].Content != "compacted-summary" {
		t.Fatalf("unexpected compaction summary marker: %#v", out[1])
	}
}

func TestAIHistoryCompressor_CompressByTotalUsageAcrossHistory(t *testing.T) {
	compressor := NewAIHistoryCompressorWithTokenBudget(1_000_000, 0)
	client := &historyCompactionTestClient{summary: "usage-total-summary"}
	a1 := ai.NewAIMsgInfo("a1")
	a1.Usage = &ai.TokenUsage{
		InputTokens:  1000,
		OutputTokens: 1000,
	}
	a2 := ai.NewAIMsgInfo("a2")
	a2.Usage = &ai.TokenUsage{
		InputTokens:  100000,
		OutputTokens: 1000,
	}
	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("q1"),
		a1,
		ai.NewUserMsgInfo("q2"),
		a2,
	}

	result, err := compressor.Compress(context.Background(), client, "ins", history)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	out := result.History
	if len(out) < 2 {
		t.Fatalf("expected compaction markers in output, got len=%d", len(out))
	}
	if out[0].Type != HistoryCompactionRequestType || out[1].Type != HistoryCompactionSummaryType {
		t.Fatalf("expected compaction markers at output head")
	}
}

func TestAIHistoryCompressor_CompressByEstimateWhenUsageMissing(t *testing.T) {
	compressor := NewAIHistoryCompressorWithTokenBudget(1, 0)
	client := &historyCompactionTestClient{summary: "estimated-summary"}
	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("q1"),
		ai.NewAIMsgInfo("a1"),
		ai.NewUserMsgInfo("q2"),
		ai.NewAIMsgInfo("a2"),
	}

	result, err := compressor.Compress(context.Background(), client, "ins", history)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	out := result.History
	if len(out) < 2 {
		t.Fatalf("expected compaction markers in output, got len=%d", len(out))
	}
	if out[0].Type != HistoryCompactionRequestType || out[1].Type != HistoryCompactionSummaryType {
		t.Fatalf("expected compaction markers at output head")
	}
}

func TestAIHistoryCompressor_StopsWhenOverflowCannotBeCompacted(t *testing.T) {
	compressor := NewAIHistoryCompressorWithTokenBudget(1, 1)
	client := &historyCompactionTestClient{summary: "unused"}
	a1 := ai.NewAIMsgInfo("a1")
	a1.Usage = &ai.TokenUsage{
		InputTokens:  100000,
		OutputTokens: 1,
	}
	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("q1"),
		a1,
	}

	result, err := compressor.Compress(context.Background(), client, "ins", history)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if result.DidCompact {
		t.Fatalf("expected no compaction when no round can be removed, got %#v", result)
	}
	if result.State != CompactionStateNeedsCompaction || !result.StillOverflow || result.CanContinue {
		t.Fatalf("expected terminal overflow result, got %#v", result)
	}
	if result.TerminalReason != CompactionTerminalOverflow {
		t.Fatalf("expected overflow terminal reason, got %#v", result)
	}
}

func TestAIHistoryCompressor_InterruptedCompactionDoesNotPersistPartialState(t *testing.T) {
	compressor := NewAIHistoryCompressorWithTokenBudget(1, 0)
	client := &historyCompactionTestClient{summary: "unused"}
	a2 := ai.NewAIMsgInfo("a2")
	a2.Usage = &ai.TokenUsage{
		InputTokens:  100000,
		OutputTokens: 1,
	}
	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("q1"),
		ai.NewAIMsgInfo("a1"),
		ai.NewUserMsgInfo("q2"),
		a2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := compressor.Compress(ctx, client, "ins", history)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if !result.Interrupted || result.CanContinue {
		t.Fatalf("expected interrupted terminal result, got %#v", result)
	}
	if result.DidCompact {
		t.Fatalf("expected interrupted compaction to discard partial compacted state, got %#v", result)
	}
	if result.TerminalReason != CompactionTerminalInterrupted {
		t.Fatalf("expected interrupted terminal reason, got %#v", result)
	}
	if HistoryCompacted(history, result.History) {
		t.Fatalf("expected interrupted compaction to keep original stable history, got %#v", result.History)
	}
}

func TestAIHistoryCompressor_HistoricalSummaryTextNotTreatedAsCompactionMarker(t *testing.T) {
	compressor := NewAIHistoryCompressorWithTokenBudget(128000, 5)
	client := &historyCompactionTestClient{summary: "unused"}
	historicalSummaryText := ai.NewUserMsgInfo("【历史摘要】\nhistorical-summary")
	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("old-q"),
		historicalSummaryText,
		ai.NewAIMsgInfo("new-a"),
	}

	result, err := compressor.Compress(context.Background(), client, "ins", history)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	out := result.History
	if len(out) != 3 {
		t.Fatalf("expected historical summary text kept as-is when not overflow, got len=%d", len(out))
	}
	if result.DidCompact || result.State != CompactionStateNormal {
		t.Fatalf("expected no compaction result, got %#v", result)
	}
	if out[1].Role != "user" || out[1].Type != "" || out[1].Content != "【历史摘要】\nhistorical-summary" {
		t.Fatalf("unexpected historical summary text handling: %#v", out[1])
	}
	for _, msg := range out {
		if msg == nil {
			continue
		}
		if msg.Type == HistoryCompactionRequestType || msg.Type == HistoryCompactionSummaryType {
			t.Fatalf("unexpected compaction marker generated without overflow: %#v", msg)
		}
	}
}

func TestAIHistoryCompressor_UsesDedicatedCompactionPrompt(t *testing.T) {
	compressor := NewAIHistoryCompressorWithTokenBudget(1_000_000, 1)
	client := &historyCompactionTestClient{summary: "next-summary"}

	req := ai.NewUserMsgInfo(HistoryCompactionRequestText)
	req.Type = HistoryCompactionRequestType
	sum := ai.NewAIMsgInfo("previous-summary")
	sum.Type = HistoryCompactionSummaryType
	a2 := ai.NewAIMsgInfo("a2")
	a2.Usage = &ai.TokenUsage{
		InputTokens:  100000,
		OutputTokens: 1,
	}
	history := []*ai.MsgInfo{
		req,
		sum,
		ai.NewUserMsgInfo("q1"),
		ai.NewAIMsgInfo("a1"),
		ai.NewUserMsgInfo("q2"),
		a2,
	}

	result, err := compressor.Compress(context.Background(), client, "follow repo rules", history)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	out := result.History
	if len(out) < 2 {
		t.Fatalf("expected compacted history markers, got len=%d", len(out))
	}
	if len(client.lastChatMessages) != 3 {
		t.Fatalf("expected compacted slice plus prompt, got %d messages", len(client.lastChatMessages))
	}
	promptMsg := client.lastChatMessages[len(client.lastChatMessages)-1]
	if promptMsg == nil || promptMsg.Role != "user" {
		t.Fatalf("expected last compaction message to be user prompt, got %#v", promptMsg)
	}
	promptText, _ := promptMsg.Content.(string)
	if !strings.Contains(promptText, "Existing compaction summary") {
		t.Fatalf("expected dedicated compaction prompt, got %q", promptText)
	}
	if !strings.Contains(promptText, "previous-summary") {
		t.Fatalf("expected previous summary merged into prompt, got %q", promptText)
	}
	if strings.Contains(promptText, "Conversation excerpt that should be compacted") {
		t.Fatalf("expected prompt without transcript block, got %q", promptText)
	}
	if !strings.Contains(promptText, "follow repo rules") {
		t.Fatalf("expected instruction included in compaction prompt, got %q", promptText)
	}
	if first := client.lastChatMessages[0]; first == nil || first.Role != "user" || first.Content != "q1" {
		t.Fatalf("expected raw compacted history message preserved, got %#v", first)
	}
	if second := client.lastChatMessages[1]; second == nil || second.Role != "assistant" || second.Content != "a1" {
		t.Fatalf("expected raw compacted assistant message preserved, got %#v", second)
	}
}

func TestAgentAICallProxy_EmitsHistoryCompactedEvent(t *testing.T) {
	client := &historyCompactionTestClient{
		response: "final-answer",
		summary:  "compacted-summary",
	}
	events := make([]*AgentOutputEvent, 0, 4)
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
			func() *ai.MsgInfo {
				msg := ai.NewAIMsgInfo("a2")
				msg.Usage = &ai.TokenUsage{
					InputTokens:  100000,
					OutputTokens: 1,
				}
				return msg
			}(),
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

	var compacted *AgentOutputEvent
	for i := range events {
		if events[i] != nil && events[i].Type == EventTypeHistoryCompacted {
			compacted = events[i]
			break
		}
	}
	if compacted == nil {
		t.Fatalf("expected history_compacted event")
	}
	if compacted.Payload == nil {
		t.Fatalf("expected payload in history_compacted event")
	}
	beforeMessages, _ := compacted.Payload["before_messages"].(int)
	afterMessages, _ := compacted.Payload["after_messages"].(int)
	if beforeMessages <= 0 || afterMessages <= 0 {
		t.Fatalf("expected positive message counts, got before=%d after=%d", beforeMessages, afterMessages)
	}
	rawHistory, ok := compacted.Payload["history"]
	if !ok {
		t.Fatalf("expected compressed history in payload")
	}
	history, ok := rawHistory.([]*ai.MsgInfo)
	if !ok || len(history) == 0 {
		t.Fatalf("expected typed compressed history in payload")
	}
	hasCompactionSummary := false
	for i := range history {
		if history[i] == nil {
			continue
		}
		if history[i].Type == HistoryCompactionSummaryType {
			hasCompactionSummary = true
			break
		}
	}
	if !hasCompactionSummary {
		t.Fatalf("expected compaction summary marker in compressed history")
	}
}
