package react_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"aster/internal/ai/openai"
	"aster/internal/builtin_tools"
	. "aster/internal/react"

	"gopkg.in/yaml.v3"
)

// slowTool simulates an MCP tool (like ssa_compile) that takes longer than the configured timeout.
type slowTool struct {
	name     string
	delay    time.Duration
	mu       sync.Mutex
	called   int
	canceled int
}

func (t *slowTool) Name() string { return t.name }
func (t *slowTool) Description() string {
	return "A slow tool that simulates a long-running operation like SSA compilation. Call this tool to analyze a target path."
}
func (t *slowTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{"type": "string", "description": "target path to analyze"},
		},
		"required":             []string{"target"},
		"additionalProperties": false,
	}
}

func (t *slowTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	t.mu.Lock()
	t.called++
	t.mu.Unlock()

	select {
	case <-time.After(t.delay):
		return "analysis complete", nil
	case <-ctx.Done():
		t.mu.Lock()
		t.canceled++
		t.mu.Unlock()
		return "", fmt.Errorf("transport error: %w", ctx.Err())
	}
}

func (t *slowTool) CallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.called
}

func (t *slowTool) CancelCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.canceled
}

// echoTool always succeeds — used to verify the agent can still call tools after a timeout.
type echoTool struct {
	mu     sync.Mutex
	called int
}

func (t *echoTool) Name() string { return "echo" }
func (t *echoTool) Description() string {
	return "Returns the input message unchanged. Use this to report findings or confirm results."
}
func (t *echoTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{"type": "string", "description": "message to echo back"},
		},
		"required":             []string{"message"},
		"additionalProperties": false,
	}
}

func (t *echoTool) Execute(_ context.Context, args map[string]any) (string, error) {
	t.mu.Lock()
	t.called++
	t.mu.Unlock()
	msg := builtin_tools.ToolRuntimeValue(args["message"])
	if msg == "" {
		msg = "(empty)"
	}
	return msg, nil
}

func (t *echoTool) CallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.called
}

// fixedPlanner injects a predetermined plan so the test doesn't depend on LLM planning quality.
type fixedPlanner struct {
	plan []*builtin_tools.PlanItem
}

func (p *fixedPlanner) Plan(_ context.Context, _ string) (*builtin_tools.TaskPlannerResult, error) {
	return &builtin_tools.TaskPlannerResult{
		NeedsPlanning: true,
		Plan:          p.plan,
		Explanation:   "fixed plan for timeout test",
	}, nil
}

// TestToolTimeout_AgentDoesNotJumpToFinalAnswer verifies that when a tool call times out
// (context.DeadlineExceeded), the agent does NOT treat it as a user cancellation and
// jump straight to final_answer. Instead, it should continue working — proceeding to
// step_replan and then the next step.
//
// Setup:
//   - fixedPlanner injects a 2-step plan: step1 calls slow_analysis (will timeout), step2 calls echo
//   - slow_analysis has a 30s delay but tool timeout is 3s → guaranteed DeadlineExceeded
//   - Real LLM (opencode-go) drives the think-act loop within each step
//
// Expected: After slow_analysis times out in step1, the scheduler should enter step_replan,
// then continue to step2, call echo, and eventually reach final_answer normally.
func TestToolTimeout_AgentDoesNotJumpToFinalAnswer(t *testing.T) {
	if os.Getenv("SASTPRO_REACT_LIVE_TEST") != "1" {
		t.Skip("live test disabled; set SASTPRO_REACT_LIVE_TEST=1")
	}

	client := newOpenCodeGoClient(t)
	sessionID := fmt.Sprintf("test-tool-timeout-%d", time.Now().UnixNano())
	root := t.TempDir()

	slow := &slowTool{name: "slow_analysis", delay: 30 * time.Second}
	echo := &echoTool{}

	planner := &fixedPlanner{
		plan: []*builtin_tools.PlanItem{
			{ID: "step1", Step: "用 slow_analysis 工具分析 /tmp/test-project", Status: builtin_tools.PlanStepPending},
			{ID: "step2", Step: "用 echo 工具报告分析结果或超时情况", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step1"}},
		},
	}

	var mu sync.Mutex
	var phases []string
	var toolCalls []string
	var toolErrors []string

	emitter := NewEmitter("timeout-test", "timeout-test", func(event *AgentOutputEvent) error {
		if event == nil {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()

		switch event.Type {
		case EventTypeStateChange:
			if p, ok := event.Payload["phase"].(string); ok && p != "" {
				phases = append(phases, p)
			}
		case EventTypeToolStart:
			if name, ok := event.Payload["tool_name"].(string); ok {
				toolCalls = append(toolCalls, name)
			}
		case EventTypeToolEnd:
			if errStr, ok := event.Payload["error"].(string); ok && errStr != "" {
				toolErrors = append(toolErrors, errStr)
			}
		}

		summary := ""
		switch event.Type {
		case EventTypeStateChange:
			summary = fmt.Sprintf("phase=%s status=%s step=%s",
				anyStr(event.Payload["phase"]),
				anyStr(event.Payload["status"]),
				anyStr(event.Payload["current_step_id"]))
		case EventTypeToolStart:
			summary = fmt.Sprintf("tool=%s", anyStr(event.Payload["tool_name"]))
		case EventTypeToolEnd:
			summary = fmt.Sprintf("tool=%s err=%s",
				anyStr(event.Payload["tool_name"]),
				truncStr(anyStr(event.Payload["error"]), 80))
		}
		if summary != "" {
			t.Logf("[event=%s][iter=%d] %s", event.Type, event.Iteration, summary)
		}
		return nil
	})

	agent, err := NewReActAgent(
		"timeout-test",
		client,
		WithEmitter(emitter),
		WithMaxIterations(20),
		WithDefaultToolTimeout(3_000), // 3s — slow_analysis (30s) will always timeout
		WithTaskPlanner(planner),
		WithTools(slow, echo),
		WithInstruction(`你是一个安全分析助手。你有两个工具可用：
1. slow_analysis(target) — 深度分析，可能很慢或超时
2. echo(message) — 回显消息

执行规则：
- 按计划步骤执行
- 如果工具调用超时或返回错误，把错误信息用 echo 报告，然后继续下一步
- 完成所有步骤后用 update_current_step 标记完成`),
	)
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	result, err := agent.Execute(ctx,
		"请按计划执行：先用 slow_analysis 分析 /tmp/test-project，然后用 echo 报告结果。",
		WithWorkspaceSession(sessionID, root),
	)

	// Allow partial success: even if final_answer LLM call fails due to overall timeout,
	// the important thing is whether the agent continued past the tool timeout.
	mu.Lock()
	capturedPhases := append([]string{}, phases...)
	capturedToolCalls := append([]string{}, toolCalls...)
	capturedToolErrors := append([]string{}, toolErrors...)
	mu.Unlock()

	t.Logf("=== RESULTS ===")
	t.Logf("phases:      %v", capturedPhases)
	t.Logf("tool_calls:  %v", capturedToolCalls)
	t.Logf("tool_errors: %v", capturedToolErrors)
	t.Logf("slow_analysis: called=%d canceled=%d", slow.CallCount(), slow.CancelCount())
	t.Logf("echo:          called=%d", echo.CallCount())
	if err != nil {
		t.Logf("Execute err: %v", err)
	}
	if result != nil {
		t.Logf("result: success=%v error=%q", result.Success, truncStr(result.Error, 100))
		t.Logf("final:  %s", truncStr(result.Result, 200))
	}

	// --- Assertions ---

	// 1. slow_analysis must have been called and context-canceled (timed out)
	if slow.CallCount() == 0 {
		t.Fatal("slow_analysis was never called — plan injection may have failed")
	}
	if slow.CancelCount() == 0 {
		t.Fatal("slow_analysis was never canceled — tool timeout didn't fire")
	}

	// 2. There must be a timeout error in tool events
	hasTimeoutErr := false
	for _, e := range capturedToolErrors {
		if strings.Contains(e, "timed out") || strings.Contains(e, "deadline exceeded") {
			hasTimeoutErr = true
			break
		}
	}
	if !hasTimeoutErr {
		t.Error("no timeout error captured in tool end events")
	}

	// 3. THE CRITICAL CHECK: after slow_analysis timeout, did the agent continue?
	// Look for any tool call AFTER slow_analysis.
	toolCallsAfterTimeout := 0
	sawSlowTool := false
	for _, tc := range capturedToolCalls {
		if tc == "slow_analysis" {
			sawSlowTool = true
			continue
		}
		if sawSlowTool {
			toolCallsAfterTimeout++
		}
	}

	// Also check for phase transitions after the timeout
	phasesAfterStep1 := 0
	sawStepPhase := false
	for _, p := range capturedPhases {
		if p == "step" {
			sawStepPhase = true
			continue
		}
		if sawStepPhase && (p == "step_replan" || p == "plan" || p == "step") {
			phasesAfterStep1++
		}
	}

	t.Logf("tool_calls_after_timeout=%d phases_after_step1=%d", toolCallsAfterTimeout, phasesAfterStep1)

	if toolCallsAfterTimeout == 0 && phasesAfterStep1 == 0 {
		t.Error("CRITICAL: Agent stopped all work after tool timeout. " +
			"No further tool calls or phase transitions occurred. " +
			"This indicates context.DeadlineExceeded from the tool leaked to the scheduler context, " +
			"or step_replan incorrectly routed to final_answer.")
	}

	// 4. If the agent continued, echo should have been called at least once
	if echo.CallCount() > 0 {
		t.Logf("PASS: echo was called %d time(s) after slow_analysis timeout — agent recovered correctly", echo.CallCount())
	}
}

// TestToolTimeout_ParentContextSurvives directly validates Go context isolation:
// a child context's DeadlineExceeded must not propagate to the parent.
func TestToolTimeout_ParentContextSurvives(t *testing.T) {
	parentCtx, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	type ctxKey struct{}
	callCtx := context.WithValue(parentCtx, ctxKey{}, "tool-runtime")
	execCtx, execCancel := context.WithTimeout(callCtx, 50*time.Millisecond)
	defer execCancel()

	<-execCtx.Done()

	if execCtx.Err() != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded on execCtx, got %v", execCtx.Err())
	}
	if parentCtx.Err() != nil {
		t.Fatalf("parent context cancelled after child timeout: %v", parentCtx.Err())
	}
	if callCtx.Err() != nil {
		t.Fatalf("callCtx cancelled after child timeout: %v", callCtx.Err())
	}
}

// --- helpers ---

func newOpenCodeGoClient(t *testing.T) *openai.Client {
	t.Helper()
	baseURL, apiKey, proxy := resolveOpenCodeGoForTimeoutTest(t)
	if baseURL == "" || apiKey == "" {
		t.Skip("opencode-go config not found; set ASTER_BASE_URL+ASTER_API_KEY or configure ~/.aster/")
	}
	opts := []openai.Option{
		openai.WithURL(baseURL),
		openai.WithAPIKey(apiKey),
		openai.WithModel("deepseek-v4-flash"),
		openai.WithContextWindowTokens(128000),
		openai.WithURLAutoComplete(true),
		openai.WithStream(false),
		openai.WithTimeout(90 * time.Second),
		openai.WithMaxRetries(2),
	}
	if proxy != "" {
		opts = append(opts, openai.WithProxy(proxy))
	}
	return openai.NewClient(opts...)
}

func resolveOpenCodeGoForTimeoutTest(t *testing.T) (baseURL, apiKey, proxy string) {
	t.Helper()
	if u := os.Getenv("ASTER_BASE_URL"); u != "" {
		baseURL = u
	}
	if k := os.Getenv("ASTER_API_KEY"); k != "" {
		apiKey = k
	}
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return
	}
	cfgPath := filepath.Join(homeDir, ".aster", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err == nil {
		var cfg struct {
			DefaultProvider string `yaml:"default_provider"`
			Providers       map[string]struct {
				BaseURL string            `yaml:"base_url"`
				APIKey  string            `yaml:"api_key"`
				Env     map[string]string `yaml:"env"`
			} `yaml:"providers"`
		}
		if yaml.Unmarshal(data, &cfg) == nil {
			name := cfg.DefaultProvider
			if name == "" {
				name = "opencode-go"
			}
			if p, ok := cfg.Providers[name]; ok {
				if baseURL == "" {
					baseURL = p.BaseURL
				}
				if apiKey == "" && p.APIKey != "" {
					apiKey = p.APIKey
				}
				if p.Env != nil && proxy == "" {
					proxy = p.Env["HTTPS_PROXY"]
				}
			}
		}
	}
	if apiKey == "" {
		credPath := filepath.Join(homeDir, ".aster", "credentials.yaml")
		data, err := os.ReadFile(credPath)
		if err == nil {
			var creds map[string]string
			if yaml.Unmarshal(data, &creds) == nil {
				for _, n := range []string{"opencode-go", "opencode", "openai"} {
					if k, ok := creds[n]; ok && k != "" {
						apiKey = k
						break
					}
				}
			}
		}
	}
	return
}

func anyStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
