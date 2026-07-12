package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// scriptedSubAgentClient 是驱动 RunSubAgentLoop 的脚本化 ChatClient：每次 ChatEx 返回 turns
// 里的下一条 choice；用尽后默认回空 assistant（走 B3 空文本路径）。
type scriptedSubAgentClient struct {
	turns []*ai.ChatChoices
	calls int
}

func (c *scriptedSubAgentClient) ModelContextInfo() ai.ModelContextInfo {
	return ai.ModelContextInfo{ModelName: "test-model", InputTokenLimit: 128000, OutputTokenLimit: 8000}.Normalize()
}

func (c *scriptedSubAgentClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *scriptedSubAgentClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *scriptedSubAgentClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	i := c.calls
	c.calls++
	if i >= len(c.turns) {
		return []*ai.ChatChoices{{Message: ai.NewAIMsgInfo(""), FinishReason: "stop"}}, nil
	}
	return []*ai.ChatChoices{c.turns[i]}, nil
}

func submitResultTurn(status, result string, usage *ai.TokenUsage) *ai.ChatChoices {
	args, _ := json.Marshal(map[string]any{"status": status, "result": result})
	tc := &ai.FunctionTool{
		Id:       "call_sr",
		Type:     "function",
		Function: &ai.FunctionDetail{Name: builtin_tools.SubmitResultToolName, Arguments: string(args)},
	}
	msg := ai.NewAIMsgInfo("")
	msg.ToolCalls = []*ai.FunctionTool{tc}
	return &ai.ChatChoices{Message: msg, Usage: usage, FinishReason: "tool_calls"}
}

func toolCallTurn(callID, toolName string) *ai.ChatChoices {
	tc := &ai.FunctionTool{
		Id:       callID,
		Type:     "function",
		Function: &ai.FunctionDetail{Name: toolName, Arguments: "{}"},
	}
	msg := ai.NewAIMsgInfo("")
	msg.ToolCalls = []*ai.FunctionTool{tc}
	return &ai.ChatChoices{Message: msg, FinishReason: "tool_calls"}
}

func textTurn(text string) *ai.ChatChoices {
	return &ai.ChatChoices{Message: ai.NewAIMsgInfo(text), FinishReason: "stop"}
}

// countingTool 是 dispatch 路径用的最小 Tool，记录被执行次数。
type countingTool struct{ calls int32 }

func (t *countingTool) Name() string        { return "counting_tool" }
func (t *countingTool) Description() string  { return "test tool" }
func (t *countingTool) Parameters() any      { return map[string]any{"type": "object"} }
func (t *countingTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	atomic.AddInt32(&t.calls, 1)
	return "ok", nil
}

func newSubAgentLoopTestAgent(t *testing.T, client ai.ChatClient, tools ...Tool) *Agent {
	t.Helper()
	opts := []Option{WithEmitter(NewDummyEmitter())}
	if len(tools) > 0 {
		opts = append(opts, WithTools(tools...))
	}
	agent, err := NewReActAgent("subloop-test", client, opts...)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	return agent
}

func TestRunSubAgentLoop_SubmitResultCompleted(t *testing.T) {
	client := &scriptedSubAgentClient{turns: []*ai.ChatChoices{
		submitResultTurn("completed", "结论：端口=8080，见 /ws/out.md", &ai.TokenUsage{TotalTokens: 123}),
	}}
	agent := newSubAgentLoopTestAgent(t, client)

	result, usage, err := agent.RunSubAgentLoop(context.Background(), "查端口", "", resolveSubAgentType("explore"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if !strings.Contains(result.Result, "端口=8080") {
		t.Fatalf("result missing submitted text: %q", result.Result)
	}
	if result.TurnStatus != "succeeded" {
		t.Fatalf("expected turn_status succeeded, got %q", result.TurnStatus)
	}
	if usage == nil || usage.Tokens != 123 {
		t.Fatalf("expected usage.Tokens=123, got %#v", usage)
	}
	if client.calls != 1 {
		t.Fatalf("expected exactly 1 model call (submit terminates), got %d", client.calls)
	}
}

func TestRunSubAgentLoop_SubmitResultFailed(t *testing.T) {
	client := &scriptedSubAgentClient{turns: []*ai.ChatChoices{
		submitResultTurn("failed", "未能定位配置文件", nil),
	}}
	agent := newSubAgentLoopTestAgent(t, client)

	result, _, err := agent.RunSubAgentLoop(context.Background(), "定位配置", "", resolveSubAgentType("general-purpose"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failure, got %#v", result)
	}
	if result.TurnStatus != "failed" {
		t.Fatalf("expected turn_status failed, got %q", result.TurnStatus)
	}
}

func TestRunSubAgentLoop_NoToolEmptyText_Failed(t *testing.T) {
	client := &scriptedSubAgentClient{turns: []*ai.ChatChoices{textTurn("")}}
	agent := newSubAgentLoopTestAgent(t, client)

	result, _, err := agent.RunSubAgentLoop(context.Background(), "任务", "", resolveSubAgentType("explore"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failure on empty output (B3), got %#v", result)
	}
	if result.TurnStatus != "failed" {
		t.Fatalf("expected failed, got %q", result.TurnStatus)
	}
}

func TestRunSubAgentLoop_NoToolWithText_FallbackSuccess(t *testing.T) {
	client := &scriptedSubAgentClient{turns: []*ai.ChatChoices{textTurn("结论文本兜底")}}
	agent := newSubAgentLoopTestAgent(t, client)

	result, _, err := agent.RunSubAgentLoop(context.Background(), "任务", "", resolveSubAgentType("explore"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected fallback success, got %#v", result)
	}
	if !strings.Contains(result.Result, "结论文本兜底") {
		t.Fatalf("result missing fallback text: %q", result.Result)
	}
}

func TestRunSubAgentLoop_ToolThenSubmit_DispatchAndUsage(t *testing.T) {
	tool := &countingTool{}
	client := &scriptedSubAgentClient{turns: []*ai.ChatChoices{
		toolCallTurn("call_1", tool.Name()),
		submitResultTurn("completed", "干完了", nil),
	}}
	agent := newSubAgentLoopTestAgent(t, client, tool)

	result, usage, err := agent.RunSubAgentLoop(context.Background(), "干活", "", resolveSubAgentType("general-purpose"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if atomic.LoadInt32(&tool.calls) != 1 {
		t.Fatalf("expected counting_tool dispatched once, got %d", tool.calls)
	}
	if usage == nil || usage.ToolUses < 1 {
		t.Fatalf("expected usage.ToolUses>=1, got %#v", usage)
	}
	if client.calls != 2 {
		t.Fatalf("expected 2 model calls (dispatch turn + submit turn), got %d", client.calls)
	}
}

// TestDrainAsyncBackfillsDroppedNotifications 锁 B10：Complete() 在 channel（cap 64）满时
// 静默丢弃的完成通知，drain 补扫（closed && !delivered）零丢失，且不重复注入。
func TestDrainAsyncBackfillsDroppedNotifications(t *testing.T) {
	agent := newSubAgentLoopTestAgent(t, &scriptedSubAgentClient{})
	agent.asyncRegistry = NewAsyncAgentRegistry()

	const n = 80 // > channel cap 64：至少 16 条通知会被丢弃，全靠补扫兜回
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("bg-%d", i)
		agent.asyncRegistry.Register(id, "task", "")
		agent.asyncRegistry.Complete(id, &builtin_tools.RunResult{Success: true, Result: "r"})
	}

	agent.drainAsyncAgentNotifications(context.Background())

	if left := agent.asyncRegistry.UndeliveredNotifications(); len(left) != 0 {
		t.Fatalf("expected zero undelivered after drain backfill, got %d", len(left))
	}
	// 每条完成通知恰注入一次 stepHistory（补扫 + channel 幂等去重，无双注入）。
	if got := len(agent.stepHistory); got != n {
		t.Fatalf("expected %d notifications injected exactly once, got %d", n, got)
	}
}

func TestRunSubAgentLoop_ParentCancel(t *testing.T) {
	// 首轮就取消：循环开头 ctx.Err() 守卫应返回 cancelled，不发模型调用。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &scriptedSubAgentClient{turns: []*ai.ChatChoices{submitResultTurn("completed", "x", nil)}}
	agent := newSubAgentLoopTestAgent(t, client)

	result, _, err := agent.RunSubAgentLoop(ctx, "任务", "", resolveSubAgentType("explore"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected cancelled failure, got %#v", result)
	}
	if result.TurnStatus != "cancelled" {
		t.Fatalf("expected turn_status cancelled, got %q", result.TurnStatus)
	}
	if client.calls != 0 {
		t.Fatalf("expected no model call after parent cancel, got %d", client.calls)
	}
}
