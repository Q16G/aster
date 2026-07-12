package react

import (
	"context"
	"encoding/json"
	"errors"
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
	errs  []error // 可选：errs[i] 非空则第 i 次 ChatEx 返回该 error（测错误收尾分支）
	calls int
	// lastMsgs 捕获最近一次 ChatEx 收到的出站消息，供断言「任务/上下文进了 user 消息」。
	lastMsgs []*ai.MsgInfo
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

func (c *scriptedSubAgentClient) ChatEx(_ context.Context, msgs []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	i := c.calls
	c.calls++
	c.lastMsgs = msgs
	if i < len(c.errs) && c.errs[i] != nil {
		return nil, c.errs[i]
	}
	if i >= len(c.turns) {
		return []*ai.ChatChoices{{Message: ai.NewAIMsgInfo(""), FinishReason: "stop"}}, nil
	}
	return []*ai.ChatChoices{c.turns[i]}, nil
}

// outboundContains 报告捕获的出站消息里是否有任一条 content 含 substr。
func (c *scriptedSubAgentClient) outboundContains(substr string) bool {
	for _, m := range c.lastMsgs {
		if m != nil && strings.Contains(FormatMsgContent(m.Content), substr) {
			return true
		}
	}
	return false
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

// TestRunSubAgentLoop_FatalErrorCollapses 锁 F3：AICallProxy 返回 fatal/overflow error → 体面
// 收尾为 failed，Error 带 "recoverable:" 前缀（fatal 与 overflow 共用同一 if 分支）。
func TestRunSubAgentLoop_FatalErrorCollapses(t *testing.T) {
	client := &scriptedSubAgentClient{errs: []error{context.Canceled}}
	agent := newSubAgentLoopTestAgent(t, client)

	result, _, err := agent.RunSubAgentLoop(context.Background(), "任务", "", resolveSubAgentType("explore"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed, got %#v", result)
	}
	if result.TurnStatus != "failed" {
		t.Fatalf("turn_status=%q, want failed", result.TurnStatus)
	}
	if !strings.Contains(result.Error, "recoverable:") {
		t.Fatalf("fatal/overflow 分支 Error 应带 recoverable: 前缀, got %q", result.Error)
	}
}

// TestRunSubAgentLoop_GenericErrorCollapses 锁 F3：普通（非 fatal/overflow）error → failed 但
// 不带 recoverable: 前缀，Error 保留原因。
func TestRunSubAgentLoop_GenericErrorCollapses(t *testing.T) {
	client := &scriptedSubAgentClient{errs: []error{errors.New("provider boom")}}
	agent := newSubAgentLoopTestAgent(t, client)

	result, _, err := agent.RunSubAgentLoop(context.Background(), "任务", "", resolveSubAgentType("explore"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed, got %#v", result)
	}
	if strings.Contains(result.Error, "recoverable:") {
		t.Fatalf("普通错误不应带 recoverable: 前缀, got %q", result.Error)
	}
	if !strings.Contains(result.Error, "provider boom") {
		t.Fatalf("Error 应保留原因, got %q", result.Error)
	}
}

// TestParseSubmitResult_Forms 锁 F4：submit_result 参数各形态解析——含修复的 map 形态（不再静默失败）。
func TestParseSubmitResult_Forms(t *testing.T) {
	mk := func(args any) *ai.FunctionTool {
		return &ai.FunctionTool{Function: &ai.FunctionDetail{Name: builtin_tools.SubmitResultToolName, Arguments: args}}
	}
	cases := []struct {
		name       string
		args       any
		wantStatus string
		wantResult string
	}{
		{"string", `{"status":"completed","result":"ok"}`, "completed", "ok"},
		{"bytes", []byte(`{"status":"failed","result":"bad"}`), "failed", "bad"},
		{"map", map[string]any{"status": "COMPLETED", "result": "mres"}, "completed", "mres"}, // F4 修复点
		{"missing-result", `{"status":"completed"}`, "completed", ""},
		{"invalid-json", `not json`, "", ""},
		{"default-status", `{"result":"x"}`, "", "x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r := parseSubmitResult(mk(tc.args))
			if s != tc.wantStatus || r != tc.wantResult {
				t.Fatalf("parseSubmitResult(%v) = (%q,%q), want (%q,%q)", tc.args, s, r, tc.wantStatus, tc.wantResult)
			}
		})
	}
}

// TestFindToolCall_FirstMatch 锁 F4：返回首个匹配、无匹配返回 nil。
func TestFindToolCall_FirstMatch(t *testing.T) {
	mk := func(name string) *ai.FunctionTool { return &ai.FunctionTool{Function: &ai.FunctionDetail{Name: name}} }
	tcs := []*ai.FunctionTool{mk("a"), mk("submit_result"), mk("submit_result")}
	if got := findToolCall(tcs, "submit_result"); got != tcs[1] {
		t.Fatalf("expected first matching submit_result")
	}
	if findToolCall(tcs, "nope") != nil {
		t.Fatalf("no match should be nil")
	}
	if findToolCall(nil, "x") != nil {
		t.Fatalf("nil slice should be nil")
	}
}

// TestRunSubAgentLoop_SubmitWithWorkTool_OnlySubmits 锁 F4 语义：submit_result 与干活工具同轮并现时
// 只取 submit 收尾、不 dispatch 其余（安全保证：不误跑收尾轮里夹带的干活工具）。
func TestRunSubAgentLoop_SubmitWithWorkTool_OnlySubmits(t *testing.T) {
	tool := &countingTool{}
	turn := submitResultTurn("completed", "done", nil)
	// 干活工具排在 submit_result 前面，验证仍以 submit 为终止、不 dispatch 干活工具。
	turn.Message.ToolCalls = append(
		[]*ai.FunctionTool{{Id: "c1", Type: "function", Function: &ai.FunctionDetail{Name: tool.Name(), Arguments: "{}"}}},
		turn.Message.ToolCalls...,
	)
	client := &scriptedSubAgentClient{turns: []*ai.ChatChoices{turn}}
	agent := newSubAgentLoopTestAgent(t, client, tool)

	result, _, err := agent.RunSubAgentLoop(context.Background(), "干活", "", resolveSubAgentType("general-purpose"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected success, got %#v", result)
	}
	if atomic.LoadInt32(&tool.calls) != 0 {
		t.Fatalf("同轮 submit 时干活工具不应被 dispatch, got %d", tool.calls)
	}
}

// TestRunSubAgentLoop_ContextReachesUserMessage 锁 F7：委派任务 + 随附上下文真的进了子 loop 首条
// user 消息（替代测已废弃 TaskContextEntry 路径的旧测）。
func TestRunSubAgentLoop_ContextReachesUserMessage(t *testing.T) {
	client := &scriptedSubAgentClient{turns: []*ai.ChatChoices{submitResultTurn("completed", "done", nil)}}
	agent := newSubAgentLoopTestAgent(t, client)

	_, _, err := agent.RunSubAgentLoop(context.Background(), "查找入口函数", "委派上下文：仓库在 /myrepo", resolveSubAgentType("explore"))
	if err != nil {
		t.Fatalf("loop err: %v", err)
	}
	if !client.outboundContains("查找入口函数") {
		t.Fatalf("委派任务应进出站 user 消息")
	}
	if !client.outboundContains("/myrepo") {
		t.Fatalf("随附上下文应进出站 user 消息")
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
