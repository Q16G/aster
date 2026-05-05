package react

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/structuredoutput"
	"aster/internal/utils/argx"
)

type IntentMode string

const (
	IntentModeSimpleReply IntentMode = "simple_reply"
	IntentModeReactRun    IntentMode = "react_run"
)

type IntentDecision struct {
	Mode                IntentMode `json:"mode"`
	IntentSummary       string     `json:"intent_summary,omitempty"`
	Complexity          string     `json:"complexity,omitempty"`
	MatchedCapabilities []string   `json:"matched_capabilities,omitempty"`
	ReplyHint           string     `json:"reply_hint,omitempty"`
	Confidence          float64    `json:"confidence,omitempty"`
}

type promptHistoryItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

//go:embed prompts/intent_recognition.prompt
var intentRecognitionPrompt string

//go:embed prompts/simple_reply.prompt
var simpleReplyPrompt string

var (
	simpleIntentPattern = regexp.MustCompile(`(?i)^\s*(你好|您好|嗨|哈喽|hello|hi|hey|thanks|thank\s+you|谢谢|感谢|在吗|在不在|ping|test|你是谁|你是什么|你能做什么|你会什么|你的功能|你的能力|who\s+are\s+you|what\s+can\s+you\s+do)\s*[!?？！。,.]*\s*$`)
	looseSimpleIntentPattern = regexp.MustCompile(
		`(?i)^\s*(你好|您好|嗨|哈喽|hello|hi|hey|thanks|thank\s+you|谢谢|感谢|在吗|在不在|ping|test|你是谁|你是什么|你能做什么|你会什么|你的功能|你的能力|who\s+are\s+you|what\s+can\s+you\s+do)` +
			`[\s]*(agent|助手|吗|呢|啊|呀|\?|？|。|！|!)*\s*$`,
	)
	complexIntentPattern = regexp.MustCompile("(?i)(```|/[\\w./-]+|\\.go\\b|\\.ts\\b|\\.tsx\\b|\\.vue\\b|\\.java\\b|审计|漏洞|代码|文件|函数|类|项目|分析|修复|编译|测试|报错|sql注入|xss|调用链|规则|报告|复现|\\brg\\b|read\\s+file|list\\s+files|tool|workspace|repo|repository)")
)

func (a *Agent) runIntentPrelude(ctx context.Context, runClient ai.ChatClient, input string) *IntentDecision {
	snapshot := a.state.Snapshot()
	if decision, ok := fastIntentDecision(input); ok {
		a.emitRuntimeLog("info", "intent prelude fast gate matched", snapshot, map[string]any{
			"event":             "intent_fast_gate_matched",
			"intent_mode":       decision.Mode,
			"intent_complexity": decision.Complexity,
			"intent_confidence": decision.Confidence,
		})
		return decision
	}

	prompt, err := a.buildIntentRecognitionPrompt(input)
	if err != nil {
		a.emitRuntimeLog("warning", "intent prelude prompt build failed", snapshot, map[string]any{
			"event": "intent_prompt_build_failed",
			"error": strings.TrimSpace(err.Error()),
		})
		return fallbackIntentDecision("intent_prompt_build_failed")
	}

	retryResult, err := runStructuredOutputWithRetry(a, ctx, snapshot, runClient, "intent_recognition", prompt, ParseIntentDecisionOutput)
	if err != nil {
		a.emitRuntimeLog("warning", "intent prelude retry exhausted, fallback to react run", snapshot, map[string]any{
			"event":         "intent_recognition_retry_exhausted",
			"error":         strings.TrimSpace(err.Error()),
			"last_response": strings.TrimSpace(structuredoutput.LastResponse(err)),
		})
		return fallbackIntentDecision("intent_retry_exhausted")
	}

	decision := normalizeIntentDecision(retryResult.Value)
	a.emitRuntimeLog("info", "intent prelude decided route", snapshot, map[string]any{
		"event":                    "intent_decision_ready",
		"intent_mode":              decision.Mode,
		"intent_complexity":        decision.Complexity,
		"intent_confidence":        decision.Confidence,
		"matched_capability_count": len(decision.MatchedCapabilities),
	})
	return &decision
}

func fastIntentDecision(input string) (*IntentDecision, bool) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return &IntentDecision{
			Mode:       IntentModeSimpleReply,
			Complexity: "simple",
			ReplyHint:  "直接简洁回应当前输入即可。",
			Confidence: 1,
		}, true
	}

	if isContinuationInput(trimmed) {
		return &IntentDecision{
			Mode:          IntentModeReactRun,
			IntentSummary: "用户请求继续/恢复执行。",
			Complexity:    "complex",
			ReplyHint:     "优先从 durable checkpoint 恢复执行进度并继续推进，不要把“继续”当作新的业务目标。",
			Confidence:    0.98,
		}, true
	}

	if simpleIntentPattern.MatchString(trimmed) {
		return &IntentDecision{
			Mode:          IntentModeSimpleReply,
			IntentSummary: "用户在进行简单对话或能力询问。",
			Complexity:    "simple",
			ReplyHint:     "直接给出简洁、友好的单轮答复，不要规划，不要展示内部流程。",
			Confidence:    0.98,
		}, true
	}

	if looseSimpleIntentPattern.MatchString(trimmed) &&
		len([]rune(trimmed)) <= 60 &&
		strings.Count(trimmed, "\n") == 0 {
		return &IntentDecision{
			Mode:          IntentModeSimpleReply,
			IntentSummary: "用户在进行简单对话或能力询问（宽松匹配）。",
			Complexity:    "simple",
			ReplyHint:     "直接给出简洁、友好的单轮答复，不要规划，不要展示内部流程。",
			Confidence:    0.92,
		}, true
	}

	if complexIntentPattern.MatchString(trimmed) || strings.Count(trimmed, "\n") >= 2 || len([]rune(trimmed)) > 180 {
		return &IntentDecision{
			Mode:          IntentModeReactRun,
			IntentSummary: "用户请求明显涉及代码、项目、审计或多步骤任务，需进入完整执行路径。",
			Complexity:    "complex",
			ReplyHint:     "进入完整 ReAct 执行流程。",
			Confidence:    0.95,
		}, true
	}

	return nil, false
}

func fallbackIntentDecision(reason string) *IntentDecision {
	return &IntentDecision{
		Mode:          IntentModeReactRun,
		IntentSummary: strings.TrimSpace(reason),
		Complexity:    "unknown",
		Confidence:    0,
	}
}

func normalizeIntentDecision(in IntentDecision) IntentDecision {
	in.Mode = IntentMode(strings.TrimSpace(string(in.Mode)))
	if in.Mode != IntentModeSimpleReply {
		in.Mode = IntentModeReactRun
	}
	in.IntentSummary = strings.TrimSpace(in.IntentSummary)
	in.Complexity = strings.TrimSpace(in.Complexity)
	in.ReplyHint = strings.TrimSpace(in.ReplyHint)
	in.MatchedCapabilities = normalizeStringSlice(in.MatchedCapabilities)
	if in.Confidence < 0 {
		in.Confidence = 0
	}
	if in.Confidence > 1 {
		in.Confidence = 1
	}
	if in.Complexity == "" {
		if in.Mode == IntentModeSimpleReply {
			in.Complexity = "simple"
		} else {
			in.Complexity = "complex"
		}
	}
	return in
}

func ParseIntentDecisionOutput(raw string) (IntentDecision, error) {
	var zero IntentDecision
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return zero, structuredoutput.MissingJSONObjectError("intent_recognition output is empty")
	}

	candidates := buildJSONCandidates(raw)
	if len(candidates) == 0 {
		return zero, structuredoutput.MissingJSONObjectError("intent_recognition output missing json object")
	}

	var lastErr error
	for _, candidate := range candidates {
		var out IntentDecision
		if err := json.Unmarshal([]byte(candidate), &out); err != nil {
			lastErr = structuredoutput.UnmarshalFailedError(err)
			continue
		}
		out = normalizeIntentDecision(out)
		if err := validateIntentDecision(out); err != nil {
			lastErr = structuredoutput.ValidationFailedError(err)
			continue
		}
		return out, nil
	}

	if lastErr == nil {
		lastErr = structuredoutput.UnmarshalFailedError(fmt.Errorf("intent_recognition output invalid json"))
	}
	return zero, lastErr
}

func validateIntentDecision(decision IntentDecision) error {
	switch decision.Mode {
	case IntentModeSimpleReply, IntentModeReactRun:
	default:
		return fmt.Errorf("invalid intent mode: %s", strings.TrimSpace(string(decision.Mode)))
	}
	switch strings.TrimSpace(decision.Complexity) {
	case "simple", "complex", "unknown":
	default:
		return fmt.Errorf("invalid intent complexity: %s", strings.TrimSpace(decision.Complexity))
	}
	if decision.Confidence < 0 || decision.Confidence > 1 {
		return fmt.Errorf("intent confidence out of range: %v", decision.Confidence)
	}
	return nil
}

func (a *Agent) buildIntentRecognitionPrompt(input string) (string, error) {
	if a == nil || a.promptManager == nil {
		return "", fmt.Errorf("agent is nil")
	}
	return a.promptManager.BuildIntentRecognitionPrompt(IntentRecognitionPromptInput{
		UserInstruction: strings.TrimSpace(a.cfg.Instruction),
		Input:           strings.TrimSpace(input),
		RecentHistory:   recentHistoryItems(a.history, 6),
		Nonce:           generateRandomString(8),
	})
}

func (a *Agent) buildSimpleReplyPrompt(decision *IntentDecision) (string, error) {
	if a == nil || a.promptManager == nil {
		return "", fmt.Errorf("agent is nil")
	}
	if decision == nil {
		decision = fallbackIntentDecision("simple_reply")
	}
	return a.promptManager.BuildSimpleReplyPrompt(SimpleReplyPromptInput{
		UserInstruction:     strings.TrimSpace(a.cfg.Instruction),
		IntentSummary:       strings.TrimSpace(decision.IntentSummary),
		IntentComplexity:    strings.TrimSpace(decision.Complexity),
		MatchedCapabilities: decision.MatchedCapabilities,
		ReplyHint:           strings.TrimSpace(decision.ReplyHint),
		Nonce:               generateRandomString(8),
	})
}

func (a *Agent) runSimpleReplyPath(ctx context.Context, runClient ai.ChatClient, decision *IntentDecision) (*builtin_tools.RunResult, error) {
	if a == nil {
		return nil, fmt.Errorf("agent is nil")
	}
	if runClient == nil {
		runClient = a.cfg.AIClient
	}
	if runClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}
	if decision == nil {
		decision = fallbackIntentDecision("simple_reply")
	}

	snapshot := a.state.Snapshot()
	a.emitRuntimeLog("info", "simple reply path started", snapshot, map[string]any{
		"event":          "simple_reply_started",
		"intent_summary": strings.TrimSpace(decision.IntentSummary),
	})

	prompt, err := a.buildSimpleReplyPrompt(decision)
	if err != nil {
		return nil, err
	}

	msgs := make([]*ai.MsgInfo, 0, 1+len(a.history))
	msgs = append(msgs, ai.NewSystemMsgInfo(prompt))
	msgs = append(msgs, a.history...)

	choices, err := runClient.ChatEx(ctx, msgs)
	if err != nil {
		a.emitRuntimeLog("warning", "simple reply failed, fallback to react run", snapshot, map[string]any{
			"event": "simple_reply_failed",
			"error": strings.TrimSpace(err.Error()),
		})
		a.currentIntent = fallbackIntentDecision("simple_reply_failed")
		return a.runSchedulerLoop(ctx, runClient, "", nil, a.cfg.MaxIterations)
	}
	if len(choices) == 0 || choices[0] == nil || choices[0].Message == nil {
		a.emitRuntimeLog("warning", "simple reply returned empty choice, fallback to react run", snapshot, map[string]any{
			"event": "simple_reply_empty_choice",
		})
		a.currentIntent = fallbackIntentDecision("simple_reply_empty_choice")
		return a.runSchedulerLoop(ctx, runClient, "", nil, a.cfg.MaxIterations)
	}

	msg := choices[0].Message
	msg.Role = "assistant"
	if msg.Usage == nil && choices[0].Usage != nil {
		msg.Usage = ai.NormalizeTokenUsagePtr(choices[0].Usage)
	}
	if len(msg.ToolCalls) > 0 {
		a.emitRuntimeLog("warning", "simple reply attempted tool call, fallback to react run", snapshot, map[string]any{
			"event": "simple_reply_tool_call_rejected",
		})
		a.currentIntent = fallbackIntentDecision("simple_reply_tool_call_rejected")
		return a.runSchedulerLoop(ctx, runClient, "", nil, a.cfg.MaxIterations)
	}

	content := ""
	if msg.Content != nil {
		if text, ok := msg.Content.(string); ok {
			content = strings.TrimSpace(text)
		}
	}
	if content == "" {
		a.emitRuntimeLog("warning", "simple reply returned empty content, fallback to react run", snapshot, map[string]any{
			"event": "simple_reply_empty_content",
		})
		a.currentIntent = fallbackIntentDecision("simple_reply_empty_content")
		return a.runSchedulerLoop(ctx, runClient, "", nil, a.cfg.MaxIterations)
	}

	a.history = append(a.history, msg)
	a.notifyHistoryReplace()
	a.AddMemoryAssistantOutput(content)

	snapshot = a.state.Finalize(builtin_tools.TaskStatusCompleted, content, "simple_reply", "")
	a.emitter.EmitStateChange(snapshot)
	a.emitRuntimeLog("info", "simple reply completed", snapshot, map[string]any{
		"event":          "simple_reply_completed",
		"content_length": len(content),
	})

	return a.finalizeResult(snapshot), nil
}

func recentHistoryItems(history []*ai.MsgInfo, limit int) []promptHistoryItem {
	if len(history) == 0 || limit <= 0 {
		return []promptHistoryItem{}
	}
	window := ApplyHistoryCompactionWindow(history)
	if len(window) == 0 {
		window = history
	}
	if len(window) > limit {
		window = window[len(window)-limit:]
	}
	items := make([]promptHistoryItem, 0, len(window))
	for _, msg := range window {
		if msg == nil {
			continue
		}
		content := strings.TrimSpace(promptMsgContent(msg))
		if content == "" {
			continue
		}
		items = append(items, promptHistoryItem{
			Role:    strings.TrimSpace(msg.Role),
			Content: content,
		})
	}
	if len(items) == 0 {
		return []promptHistoryItem{}
	}
	return items
}

func promptMsgContent(msg *ai.MsgInfo) string {
	if msg == nil {
		return ""
	}
	return argx.Text(msg.Content)
}
