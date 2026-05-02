package react_test

import (
	. "aster/internal/react"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"aster/internal/ai/openai"
	"aster/internal/utils/argx"
)

type liveAIConfig struct {
	baseURL      string
	apiKey       string
	model        string
	proxy        string
	timeout      time.Duration
	insecureTLS  bool
	plannerInput string
	agentInput   string
}

func TestDefaultTaskPlanner_LiveFromENV(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode enabled")
	}

	client, cfg := loadAIFromENV(t)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	planner := NewDefaultTaskPlanner(client)
	t.Logf("[planner-live] base_url=%s model=%s timeout=%s", cfg.baseURL, cfg.model, cfg.timeout)
	t.Logf("[planner-live] input=%s", cfg.plannerInput)

	result, err := planner.Plan(ctx, cfg.plannerInput)
	if err != nil {
		t.Fatalf("planner.Plan failed: %v", err)
	}
	if result == nil {
		t.Fatalf("planner result is nil")
	}
	if !result.NeedsPlanning {
		t.Fatalf("expected needs_planning=true, got %#v", result)
	}
	if len(result.Plan) == 0 {
		t.Fatalf("expected non-empty plan, got %#v", result)
	}
	for i, item := range result.Plan {
		if item == nil {
			t.Fatalf("plan item %d is nil", i)
		}
		if strings.TrimSpace(item.Step) == "" {
			t.Fatalf("plan item %d step is empty: %#v", i, item)
		}
		if strings.TrimSpace(string(item.Status)) == "" {
			t.Fatalf("plan item %d status is empty: %#v", i, item)
		}
	}

	pretty, _ := json.MarshalIndent(result, "", "  ")
	t.Logf("[planner-live] result=\n%s", string(pretty))
}

func TestReActAgent_Execute_LiveFromENV(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode enabled")
	}

	client, cfg := loadAIFromENV(t)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	emitter := NewEmitter("react-live-test", "react-live-test", func(event *AgentOutputEvent) error {
		if event == nil {
			return nil
		}
		summary := summarizeLiveEvent(event)
		if summary == "" {
			t.Logf("[react-live][event=%s][iter=%d]", event.Type, event.Iteration)
			return nil
		}
		t.Logf("[react-live][event=%s][iter=%d] %s", event.Type, event.Iteration, summary)
		return nil
	})

	agent, err := NewReActAgent(
		"react-live-test",
		client,
		WithEmitter(emitter),
		WithMaxIterations(6),
		WithInstruction("你是一个直接执行型助手。对于简单问题，优先直接回答，不要无意义规划。"),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	t.Logf("[react-live] base_url=%s model=%s timeout=%s", cfg.baseURL, cfg.model, cfg.timeout)
	t.Logf("[react-live] input=%s", cfg.agentInput)

	runResult, err := agent.Execute(ctx, cfg.agentInput)
	if err != nil {
		t.Fatalf("agent.Execute failed: %v", err)
	}
	if runResult == nil {
		t.Fatalf("run result is nil")
	}
	if !runResult.Success {
		t.Fatalf("agent execute failed: %s", runResult.Error)
	}
	if strings.TrimSpace(runResult.Result) == "" {
		t.Fatalf("agent execute returned empty result")
	}

	t.Logf("[react-live] final=%s", runResult.Result)
}

func loadAIFromENV(t *testing.T) (*openai.Client, *liveAIConfig) {
	t.Helper()

	if !isTruthy(strings.TrimSpace(os.Getenv("SASTPRO_REACT_LIVE_TEST"))) {
		t.Skip("live test disabled; set SASTPRO_REACT_LIVE_TEST=1 to enable")
	}

	baseURL := firstNonEmptyEnv("SASTPRO_TEST_CHAT_URL", "SAST_AGENT_TEST_BASE_URL")
	if baseURL == "" {
		t.Skip("missing live chat url; set SASTPRO_TEST_CHAT_URL or SAST_AGENT_TEST_BASE_URL")
	}

	apiKey := firstNonEmptyEnv("SASTPRO_TEST_CHAT_API_KEY", "SAST_AGENT_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("missing live api key; set SASTPRO_TEST_CHAT_API_KEY or SAST_AGENT_TEST_API_KEY")
	}

	model := firstNonEmptyEnv("SASTPRO_TEST_CHAT_MODEL", "SAST_AGENT_TEST_MODEL")
	if model == "" {
		t.Skip("missing live model; set SASTPRO_TEST_CHAT_MODEL or SAST_AGENT_TEST_MODEL")
	}

	timeout := 120 * time.Second
	if raw := firstNonEmptyEnv("SASTPRO_REACT_TEST_TIMEOUT_SECS", "SAST_AGENT_TEST_TIMEOUT_SECS"); raw != "" {
		secs, err := strconv.Atoi(raw)
		if err != nil || secs <= 0 {
			t.Fatalf("invalid live timeout seconds: %q", raw)
		}
		timeout = time.Duration(secs) * time.Second
	}

	insecureTLS := false
	if raw := firstNonEmptyEnv("SASTPRO_REACT_TEST_INSECURE_TLS", "SAST_AGENT_TEST_INSECURE_TLS"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			t.Fatalf("invalid insecure tls flag: %q", raw)
		}
		insecureTLS = parsed
	}

	cfg := &liveAIConfig{
		baseURL:      strings.TrimSpace(baseURL),
		apiKey:       strings.TrimSpace(apiKey),
		model:        strings.TrimSpace(model),
		proxy:        firstNonEmptyEnv("SASTPRO_REACT_TEST_PROXY", "SAST_AGENT_TEST_PROXY"),
		timeout:      timeout,
		insecureTLS:  insecureTLS,
		plannerInput: strings.TrimSpace(os.Getenv("SASTPRO_REACT_TEST_PLANNER_INPUT")),
		agentInput:   strings.TrimSpace(os.Getenv("SASTPRO_REACT_TEST_AGENT_INPUT")),
	}
	if cfg.plannerInput == "" {
		cfg.plannerInput = "请为“重构一个包含 runtime、状态机、日志与测试的 Go ReAct Agent 内核，并补充验证”生成一份带依赖关系的执行计划。"
	}
	if cfg.agentInput == "" {
		cfg.agentInput = "请直接用一句中文回答：2+2 等于几？"
	}

	opts := []openai.Option{
		openai.WithURL(cfg.baseURL),
		openai.WithURLAutoComplete(false),
		openai.WithAPIKey(cfg.apiKey),
		openai.WithModel(cfg.model),
		openai.WithTimeout(cfg.timeout),
		openai.WithInsecureSkipVerify(cfg.insecureTLS),
		openai.WithMaxRetries(2),
		openai.WithStream(false),
	}
	if strings.TrimSpace(cfg.proxy) != "" {
		opts = append(opts, openai.WithProxy(strings.TrimSpace(cfg.proxy)))
	}

	return openai.NewClient(opts...), cfg
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(strings.TrimSpace(key))); value != "" {
			return value
		}
	}
	return ""
}

func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func summarizeLiveEvent(event *AgentOutputEvent) string {
	if event == nil || event.Payload == nil {
		return ""
	}

	switch event.Type {
	case EventTypeIteration:
		return fmt.Sprintf("description=%s", strings.TrimSpace(anyString(event.Payload["description"])))
	case EventTypeStateChange:
		return fmt.Sprintf(
			"phase=%s status=%s current_step_id=%s progress=%v",
			anyString(event.Payload["phase"]),
			anyString(event.Payload["status"]),
			anyString(event.Payload["current_step_id"]),
			event.Payload["progress"],
		)
	case EventTypeLog:
		return fmt.Sprintf("log=%s", oneLine(anyString(event.Payload["message"])))
	case EventTypeToolStart:
		return fmt.Sprintf("tool_start=%s", anyString(event.Payload["tool_name"]))
	case EventTypeToolEnd:
		return fmt.Sprintf("tool_end=%s error=%s", anyString(event.Payload["tool_name"]), oneLine(anyString(event.Payload["error"])))
	case EventTypeThink:
		return oneLine(anyString(event.Payload["content"]))
	case EventTypeResult:
		raw, _ := json.Marshal(event.Payload)
		return string(raw)
	default:
		return ""
	}
}

func anyString(value any) string {
	return argx.Text(value)
}

func oneLine(text string) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " | ")
	text = strings.ReplaceAll(text, "\t", " ")
	return text
}
