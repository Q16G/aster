package react

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// --- helpers ---

type safeTool struct {
	name    string
	delay   time.Duration
	result  string
	counter *atomic.Int32
}

func (t *safeTool) Name() string          { return t.name }
func (t *safeTool) Description() string   { return t.name + " desc" }
func (t *safeTool) Parameters() any       { return map[string]any{"type": "object"} }
func (t *safeTool) ConcurrencySafe() bool { return true }
func (t *safeTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	if t.counter != nil {
		t.counter.Add(1)
	}
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	return t.result, nil
}

type unsafeTool struct {
	name   string
	result string
}

func (t *unsafeTool) Name() string        { return t.name }
func (t *unsafeTool) Description() string { return t.name + " desc" }
func (t *unsafeTool) Parameters() any     { return map[string]any{"type": "object"} }
func (t *unsafeTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	return t.result, nil
}

// fakeAgentTool 既是 ConcurrencySafeTool 也是 AgentTool（IsAgent=true），用于验证
// executeToolCallsConcurrently 按 slot.isAgent 走 agentSem 信号量。
type fakeAgentTool struct {
	name    string
	delay   time.Duration
	result  string
	current *atomic.Int32 // 当前进入数
	peak    *atomic.Int32 // 历史峰值（CAS 维护）
}

func (t *fakeAgentTool) Name() string          { return t.name }
func (t *fakeAgentTool) Description() string   { return t.name + " desc" }
func (t *fakeAgentTool) Parameters() any       { return map[string]any{"type": "object"} }
func (t *fakeAgentTool) IsAgent() bool         { return true }
func (t *fakeAgentTool) ConcurrencySafe() bool { return true }
func (t *fakeAgentTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	if t.current != nil {
		cur := t.current.Add(1)
		defer t.current.Add(-1)
		if t.peak != nil {
			for {
				p := t.peak.Load()
				if cur <= p || t.peak.CompareAndSwap(p, cur) {
					break
				}
			}
		}
	}
	if t.delay > 0 {
		time.Sleep(t.delay)
	}
	return t.result, nil
}

func makeTC(id, name string) *ai.FunctionTool {
	return &ai.FunctionTool{
		Id: id,
		Function: &ai.FunctionDetail{
			Name:      name,
			Arguments: "{}",
		},
	}
}

func newTestAgent(tools ...Tool) *Agent {
	agent, _ := NewReActAgent("test", &stubClient{}, WithEmitter(NewDummyEmitter()))
	for _, t := range tools {
		_ = agent.registerTool(t)
	}
	return agent
}

// --- partitionToolCalls ---

func TestPartitionToolCalls_SplitsSafeAndUnsafe(t *testing.T) {
	agent := newTestAgent(
		&safeTool{name: "grep_tool"},
		&unsafeTool{name: "write_tool"},
		&safeTool{name: "read_tool"},
	)

	tcs := []*ai.FunctionTool{
		makeTC("1", "grep_tool"),
		makeTC("2", "write_tool"),
		makeTC("3", "read_tool"),
	}

	safe, unsafe := partitionToolCalls(agent, tcs)

	if len(safe) != 2 {
		t.Fatalf("expected 2 safe tools, got %d", len(safe))
	}
	if len(unsafe) != 1 {
		t.Fatalf("expected 1 unsafe tool, got %d", len(unsafe))
	}
	if safe[0].Id != "1" || safe[1].Id != "3" {
		t.Fatalf("safe tools order wrong: %s, %s", safe[0].Id, safe[1].Id)
	}
	if unsafe[0].Id != "2" {
		t.Fatalf("unsafe tool wrong: %s", unsafe[0].Id)
	}
}

func TestPartitionToolCalls_UnknownToolGoesUnsafe(t *testing.T) {
	agent := newTestAgent(&safeTool{name: "known"})
	tcs := []*ai.FunctionTool{
		makeTC("1", "known"),
		makeTC("2", "unknown"),
	}

	safe, unsafe := partitionToolCalls(agent, tcs)
	if len(safe) != 1 || len(unsafe) != 1 {
		t.Fatalf("expected 1 safe + 1 unsafe, got %d + %d", len(safe), len(unsafe))
	}
}

func TestPartitionToolCalls_NilsFiltered(t *testing.T) {
	agent := newTestAgent(&safeTool{name: "a"})
	tcs := []*ai.FunctionTool{
		nil,
		makeTC("1", "a"),
		{Id: "2", Function: nil},
	}

	safe, unsafe := partitionToolCalls(agent, tcs)
	if len(safe) != 1 {
		t.Fatalf("expected 1 safe, got %d", len(safe))
	}
	if len(unsafe) != 0 {
		t.Fatalf("expected 0 unsafe, got %d", len(unsafe))
	}
}

func TestPartitionToolCalls_NeverConcurrentOverridesSafeDeclaration(t *testing.T) {
	for name := range neverConcurrentTools {
		agent := newTestAgent(&safeTool{name: name})
		tcs := []*ai.FunctionTool{makeTC("1", name)}

		safe, unsafe := partitionToolCalls(agent, tcs)
		if len(safe) != 0 {
			t.Fatalf("tool %q is in neverConcurrentTools but was partitioned as safe", name)
		}
		if len(unsafe) != 1 {
			t.Fatalf("tool %q should be in unsafe partition, got %d", name, len(unsafe))
		}
	}
}

// TestPartitionToolCalls_SubAgentGoesToSafeBucket 回归 A.1：sub_agent 已从
// neverConcurrentTools 摘出（由 SubAgentTool.ConcurrencySafe() 显式声明并发安全），
// partition 必须把它分入 safe 桶，让 executeToolCallsConcurrently 按 a.maxParallelSteps()
// 信号量并发跑同回合多路 sub_agent。
func TestPartitionToolCalls_SubAgentGoesToSafeBucket(t *testing.T) {
	// sub_agent 名字下挂一个声明 ConcurrencySafe=true 的 fake 工具——partition
	// 仅看 isConcurrencySafe + neverConcurrentTools，不依赖 SubAgentTool 实现。
	agent := newTestAgent(&safeTool{name: builtin_tools.SubAgentToolName})

	tcs := []*ai.FunctionTool{
		makeTC("1", builtin_tools.SubAgentToolName),
		makeTC("2", builtin_tools.SubAgentToolName),
		makeTC("3", builtin_tools.SubAgentToolName),
	}

	safe, unsafe := partitionToolCalls(agent, tcs)
	if len(safe) != 3 {
		t.Fatalf("expected 3 sub_agent calls in safe bucket, got safe=%d unsafe=%d", len(safe), len(unsafe))
	}
	if len(unsafe) != 0 {
		t.Fatalf("expected 0 unsafe, got %d", len(unsafe))
	}
}

// TestSubAgentToolImplementsConcurrencySafe：A.1 验证 *SubAgentTool 实现
// ConcurrencySafeTool 接口且 ConcurrencySafe() 返回 true。
func TestSubAgentToolImplementsConcurrencySafe(t *testing.T) {
	var tool any = (*SubAgentTool)(nil)
	if _, ok := tool.(ConcurrencySafeTool); !ok {
		t.Fatal("*SubAgentTool must satisfy ConcurrencySafeTool interface")
	}
	// 用一个空实例确认值（nil 调用 ConcurrencySafe 安全，方法不依赖 receiver）。
	st := &SubAgentTool{}
	if !st.ConcurrencySafe() {
		t.Fatal("SubAgentTool.ConcurrencySafe() must return true")
	}
}

// --- isConcurrencySafe ---

func TestIsConcurrencySafe_InterfacePriority(t *testing.T) {
	st := &safeTool{name: "custom_safe"}
	if !isConcurrencySafe(st) {
		t.Fatal("expected ConcurrencySafeTool interface to mark safe")
	}
}

func TestIsConcurrencySafe_DefaultMap(t *testing.T) {
	for _, name := range []string{"list_files", "read_file", "rg", "task_status"} {
		ut := &unsafeTool{name: name}
		if !isConcurrencySafe(ut) {
			t.Fatalf("expected %q in default safe tools", name)
		}
	}
}

func TestIsConcurrencySafe_UnknownToolNotSafe(t *testing.T) {
	ut := &unsafeTool{name: "custom_write"}
	if isConcurrencySafe(ut) {
		t.Fatal("unknown tools should not be safe by default")
	}
}

// --- dispatchToolCalls ---

func TestDispatchToolCalls_SingleSafeFallsBackToSequential(t *testing.T) {
	counter := &atomic.Int32{}
	agent := newTestAgent(&safeTool{name: "grep", counter: counter, result: "ok"})
	agent.state = NewStateTracker()

	tcs := []*ai.FunctionTool{makeTC("1", "grep")}

	executed, err := agent.dispatchToolCalls(context.Background(), nil, 1, tcs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed != 1 {
		t.Fatalf("expected 1 executed, got %d", executed)
	}
	if counter.Load() != 1 {
		t.Fatalf("expected tool executed once, got %d", counter.Load())
	}
}

func TestDispatchToolCalls_MultipleSafeRunConcurrently(t *testing.T) {
	counter := &atomic.Int32{}
	agent := newTestAgent(
		&safeTool{name: "tool_a", delay: 50 * time.Millisecond, result: "a", counter: counter},
		&safeTool{name: "tool_b", delay: 50 * time.Millisecond, result: "b", counter: counter},
		&safeTool{name: "tool_c", delay: 50 * time.Millisecond, result: "c", counter: counter},
	)
	agent.state = NewStateTracker()

	tcs := []*ai.FunctionTool{
		makeTC("1", "tool_a"),
		makeTC("2", "tool_b"),
		makeTC("3", "tool_c"),
	}

	start := time.Now()
	executed, err := agent.dispatchToolCalls(context.Background(), nil, 1, tcs, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed != 3 {
		t.Fatalf("expected 3 executed, got %d", executed)
	}
	if counter.Load() != 3 {
		t.Fatalf("expected 3 tool calls, got %d", counter.Load())
	}
	// Concurrent: ~50ms. Sequential: ~150ms.
	if elapsed > 120*time.Millisecond {
		t.Fatalf("tools should run concurrently; elapsed=%v (expected <120ms)", elapsed)
	}
}

func TestDispatchToolCalls_ResultsInOriginalOrder(t *testing.T) {
	agent := newTestAgent(
		&safeTool{name: "fast", delay: 10 * time.Millisecond, result: "fast_result"},
		&safeTool{name: "slow", delay: 80 * time.Millisecond, result: "slow_result"},
	)
	agent.state = NewStateTracker()

	tcs := []*ai.FunctionTool{
		makeTC("slow-id", "slow"),
		makeTC("fast-id", "fast"),
	}

	_, err := agent.dispatchToolCalls(context.Background(), nil, 1, tcs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// stepHistory should contain tool results in original order: slow first, then fast
	if len(agent.stepHistory) < 2 {
		t.Fatalf("expected at least 2 stepHistory entries, got %d", len(agent.stepHistory))
	}

	// Find tool result messages by their tool_call_ids
	var callIDs []string
	for _, msg := range agent.stepHistory {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			callIDs = append(callIDs, msg.ToolCallID)
		}
	}

	if len(callIDs) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(callIDs))
	}
	if callIDs[0] != "slow-id" || callIDs[1] != "fast-id" {
		t.Fatalf("expected order [slow-id, fast-id], got %v", callIDs)
	}
}

func TestDispatchToolCalls_MixedSafeUnsafe(t *testing.T) {
	agent := newTestAgent(
		&safeTool{name: "safe_a", result: "a"},
		&safeTool{name: "safe_b", result: "b"},
		&unsafeTool{name: "unsafe_c", result: "c"},
	)
	agent.state = NewStateTracker()

	tcs := []*ai.FunctionTool{
		makeTC("1", "safe_a"),
		makeTC("2", "unsafe_c"),
		makeTC("3", "safe_b"),
	}

	executed, err := agent.dispatchToolCalls(context.Background(), nil, 1, tcs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed != 3 {
		t.Fatalf("expected 3 executed, got %d", executed)
	}
}

// --- AsyncAgentRegistry ---

func TestAsyncAgentRegistry_RegisterAndComplete(t *testing.T) {
	reg := NewAsyncAgentRegistry()
	reg.Register("agent-1", "test instruction", "/tmp/ws")

	running := reg.RunningAgents()
	if len(running) != 1 {
		t.Fatalf("expected 1 running, got %d", len(running))
	}
	if running[0].AgentID != "agent-1" {
		t.Fatalf("wrong agent ID: %s", running[0].AgentID)
	}

	reg.Complete("agent-1", &builtin_tools.RunResult{Success: true, Result: "done"})

	running = reg.RunningAgents()
	if len(running) != 0 {
		t.Fatalf("expected 0 running after complete, got %d", len(running))
	}

	entry := reg.Get("agent-1")
	if entry.Status != "completed" {
		t.Fatalf("expected completed, got %s", entry.Status)
	}

	select {
	case notif := <-reg.notifications:
		if notif.AgentID != "agent-1" || notif.Status != "completed" {
			t.Fatalf("unexpected notification: %+v", notif)
		}
	default:
		t.Fatal("expected notification in channel")
	}
}

func TestAsyncAgentRegistry_CompleteFailed(t *testing.T) {
	reg := NewAsyncAgentRegistry()
	reg.Register("agent-2", "fail test", "/tmp/ws2")
	reg.Complete("agent-2", &builtin_tools.RunResult{Success: false, Error: "boom"})

	entry := reg.Get("agent-2")
	if entry.Status != "failed" {
		t.Fatalf("expected failed, got %s", entry.Status)
	}
}

func TestAsyncAgentRegistry_HasRunning(t *testing.T) {
	reg := NewAsyncAgentRegistry()
	if reg.HasRunning() {
		t.Fatal("empty registry should not have running")
	}

	reg.Register("a", "test", "/tmp")
	if !reg.HasRunning() {
		t.Fatal("expected HasRunning=true")
	}

	reg.Complete("a", &builtin_tools.RunResult{Success: true})
	<-reg.notifications // drain
	if reg.HasRunning() {
		t.Fatal("expected HasRunning=false after complete")
	}
}

// --- SubAgentStatusTool ---

func TestSubAgentStatusTool_ReturnsOnlyRunning(t *testing.T) {
	agent := newTestAgent()
	agent.asyncRegistry = NewAsyncAgentRegistry()
	agent.asyncRegistry.Register("running-1", "scan sql", "/tmp/ws1")
	agent.asyncRegistry.Register("running-2", "scan xss", "/tmp/ws2")
	agent.asyncRegistry.Complete("running-2", &builtin_tools.RunResult{Success: true, Result: "done"})
	<-agent.asyncRegistry.notifications // drain

	tool := NewSubAgentStatusTool(agent)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "running-1") {
		t.Fatal("expected running-1 in result")
	}
	if strings.Contains(result, "running-2") {
		t.Fatal("completed agent should not appear in status result")
	}
}

func TestSubAgentStatusTool_NilRegistry(t *testing.T) {
	agent := newTestAgent()
	tool := NewSubAgentStatusTool(agent)
	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"agents":[]`) {
		t.Fatalf("expected empty agents array, got %s", result)
	}
}

func TestSubAgentStatusTool_QueryByID(t *testing.T) {
	agent := newTestAgent()
	agent.asyncRegistry = NewAsyncAgentRegistry()
	agent.asyncRegistry.Register("agent-x", "scan", "/tmp/wsx")
	agent.asyncRegistry.Register("agent-y", "test", "/tmp/wsy")

	tool := NewSubAgentStatusTool(agent)
	result, err := tool.Execute(context.Background(), map[string]any{"agent_id": "agent-x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "agent-x") {
		t.Fatal("expected agent-x in result")
	}
	if strings.Contains(result, "agent-y") {
		t.Fatal("should not contain agent-y when querying specific ID")
	}
}

func TestSubAgentStatusTool_IsConcurrencySafe(t *testing.T) {
	tool := NewSubAgentStatusTool(newTestAgent())
	if !tool.ConcurrencySafe() {
		t.Fatal("SubAgentStatusTool should be concurrency safe")
	}
	if !isConcurrencySafe(tool) {
		t.Fatal("isConcurrencySafe should recognize SubAgentStatusTool")
	}
}

// --- drainAsyncAgentNotifications ---

func TestDrainAsyncAgentNotifications_InjectsUserMessage(t *testing.T) {
	agent := newTestAgent()
	agent.state = NewStateTracker()
	agent.asyncRegistry = NewAsyncAgentRegistry()
	agent.asyncRegistry.Register("bg-1", "test task", t.TempDir())
	agent.asyncRegistry.Complete("bg-1", &builtin_tools.RunResult{
		Success: true,
		Result:  "found 3 vulnerabilities",
	})

	agent.drainAsyncAgentNotifications(context.Background())

	if len(agent.stepHistory) != 1 {
		t.Fatalf("expected 1 stepHistory entry, got %d", len(agent.stepHistory))
	}

	msg := agent.stepHistory[0]
	if msg.Role != "user" {
		t.Fatalf("expected user role, got %s", msg.Role)
	}

	content := fmt.Sprintf("%v", msg.Content)
	if !strings.Contains(content, "bg-1") {
		t.Fatal("notification should contain agent_id")
	}
	if !strings.Contains(content, "found 3 vulnerabilities") {
		t.Fatal("notification should contain result summary")
	}
	if !strings.Contains(content, "async_result.json") {
		t.Fatal("notification should contain result_file path")
	}
}

func TestDrainAsyncAgentNotifications_NilRegistry(t *testing.T) {
	agent := newTestAgent()
	agent.state = NewStateTracker()
	// No panic when asyncRegistry is nil
	agent.drainAsyncAgentNotifications(context.Background())
	if len(agent.stepHistory) != 0 {
		t.Fatal("expected empty stepHistory")
	}
}

func TestAsyncAgentRegistry_DoubleCompleteNoPanic(t *testing.T) {
	reg := NewAsyncAgentRegistry()
	reg.Register("agent-dup", "test", "/tmp/ws")

	reg.Complete("agent-dup", &builtin_tools.RunResult{Success: true, Result: "first"})
	// Second call should not panic (double close guard)
	reg.Complete("agent-dup", &builtin_tools.RunResult{Success: false, Error: "second"})

	entry := reg.Get("agent-dup")
	if entry.Status != "completed" {
		t.Fatalf("expected first completion to win, got %s", entry.Status)
	}
}

func TestAsyncAgentRegistry_CompleteNonExistent(t *testing.T) {
	reg := NewAsyncAgentRegistry()
	// Should not panic
	reg.Complete("does-not-exist", &builtin_tools.RunResult{Success: true})
}

func TestDrainAsyncAgentNotifications_TruncatesLongResult(t *testing.T) {
	agent := newTestAgent()
	agent.state = NewStateTracker()
	agent.asyncRegistry = NewAsyncAgentRegistry()

	longResult := strings.Repeat("x", 5000)
	agent.asyncRegistry.Register("bg-2", "task", t.TempDir())
	agent.asyncRegistry.Complete("bg-2", &builtin_tools.RunResult{
		Success: true,
		Result:  longResult,
	})

	agent.drainAsyncAgentNotifications(context.Background())

	content := fmt.Sprintf("%v", agent.stepHistory[0].Content)
	if len(content) > 2000 {
		t.Fatalf("notification content too long: %d chars, expected truncation", len(content))
	}
	if !strings.Contains(content, "truncated") {
		t.Fatal("truncated result should contain truncation marker")
	}
}

// TestConcurrent_AgentToolUsesGenericSem 回归撤回 agentSem 后行为：sub_agent /
// agent-tool 现在与普通 safe 工具共用 cap=defaultMaxToolConcurrency=5 的同一信号量；
// 真正的 outbound AI 请求并发由 AgentRequestPool 在 ai 包统一入口限流。
//
// 本测试单纯断言 tool dispatch 层的 fakeAgentTool 在 cap=5 下并发跑通，**不**断言
// 严格上限（peak 可能就是 5 或调度上来时还更低，关键是不被任何"agent-tool 专用 sem"
// 卡到 a.maxParallelSteps=2 这种与 dispatch 无关的预算）。
func TestConcurrent_AgentToolUsesGenericSem(t *testing.T) {
	current := &atomic.Int32{}
	peak := &atomic.Int32{}

	tool := &fakeAgentTool{
		name:    "fake_agent",
		delay:   30 * time.Millisecond,
		result:  "ok",
		current: current,
		peak:    peak,
	}

	agent := newTestAgent(tool)
	agent.state = NewStateTracker()
	// 故意把 cfg.MaxParallelSteps 设成 2——撤回 agentSem 后该字段不再约束 dispatch；
	// 真正的 AI 请求限流靠 requestPool（本测试不构造 pool，故无限制）。
	agent.cfg.MaxParallelSteps = 2

	const n = 5
	tcs := make([]*ai.FunctionTool, n)
	for i := 0; i < n; i++ {
		tcs[i] = makeTC(fmt.Sprintf("call-%d", i), "fake_agent")
	}

	executed, err := agent.dispatchToolCalls(context.Background(), nil, 1, tcs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed != n {
		t.Fatalf("expected %d executed, got %d", n, executed)
	}
	// 峰值上限 = defaultMaxToolConcurrency(5)；agent-tool 不再被 MaxParallelSteps=2 卡。
	if got := peak.Load(); got > int32(defaultMaxToolConcurrency) {
		t.Fatalf("peak=%d exceeds defaultMaxToolConcurrency=%d", got, defaultMaxToolConcurrency)
	}
	if peak.Load() < 2 {
		t.Fatalf("expected agent-tool calls to actually run concurrently (peak>=2), got %d", peak.Load())
	}
}

// TestConcurrent_GenericSafeToolUsesDefaultMaxConcurrency 回归 A.2：非 agent-tool
// 的 safe 工具不吃 MaxParallelSteps，仍按 defaultMaxToolConcurrency=5 限流。
// 这条用例的关键是确认 MaxParallelSteps=1 不会反向砍读类工具的并发。
func TestConcurrent_GenericSafeToolUsesDefaultMaxConcurrency(t *testing.T) {
	counter := &atomic.Int32{}
	agent := newTestAgent(
		&safeTool{name: "rg_a", delay: 30 * time.Millisecond, result: "a", counter: counter},
		&safeTool{name: "rg_b", delay: 30 * time.Millisecond, result: "b", counter: counter},
		&safeTool{name: "rg_c", delay: 30 * time.Millisecond, result: "c", counter: counter},
	)
	agent.state = NewStateTracker()
	agent.cfg.MaxParallelSteps = 1 // 串行 step 调度，但读类工具仍应并发

	tcs := []*ai.FunctionTool{
		makeTC("1", "rg_a"),
		makeTC("2", "rg_b"),
		makeTC("3", "rg_c"),
	}

	start := time.Now()
	executed, err := agent.dispatchToolCalls(context.Background(), nil, 1, tcs, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if executed != 3 {
		t.Fatalf("expected 3 executed, got %d", executed)
	}
	// 3 路 30ms 并发 ≈ 30ms；若被 MaxParallelSteps=1 误伤会变成 ≈ 90ms。
	if elapsed > 70*time.Millisecond {
		t.Fatalf("generic safe tools should NOT be throttled by MaxParallelSteps=1; elapsed=%v", elapsed)
	}
}
