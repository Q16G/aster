package react

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/runtimelog"
	"aster/internal/utils/argx"
)

const (
	historyCompressMaxLoops                = 20
	HistoryCompactionRequestType           = "summary_request"
	HistoryCompactionSummaryType           = "summary"
	HistoryCompactionRequestText           = "What did we do so far?"
	HistoryCompactionClearedToolResultText = "[Old tool result content cleared]"
)

type CompactionState string

const (
	CompactionStateNormal          CompactionState = "normal"
	CompactionStateNeedsCompaction CompactionState = "needs_compaction"
	CompactionStateDone            CompactionState = "compaction_done"
)

const (
	CompactionTerminalOverflow     = "overflow"
	CompactionTerminalTimeout      = "timeout"
	CompactionTerminalInterrupted  = "interrupted"
	CompactionTerminalMaxAttempts  = "max_attempts"
	CompactionTerminalNoProgress   = "no_progress"
	CompactionTerminalEmptySummary = "empty_summary"
)

type HistoryCompactionResult struct {
	History        []*ai.MsgInfo
	State          CompactionState
	DidCompact     bool
	StillOverflow  bool
	CanContinue    bool
	AttemptCount   int
	Interrupted    bool
	TimedOut       bool
	Overflow       bool
	TerminalReason string
}

type historyCompactionNeed struct {
	shouldCompress bool
	overflow       bool
}

// HistoryCompressor 历史压缩器接口
type HistoryCompressor interface {
	Compress(ctx context.Context, aiClient ai.ChatClient, instruction string, history []*ai.MsgInfo) (*HistoryCompactionResult, error)
}

// AIHistoryCompressor AI 历史压缩器
type AIHistoryCompressor struct {
	keepLastRounds int
	triggerTokens  int
	promptManager  PromptManager
}

// NewAIHistoryCompressor 创建 AI 历史压缩器。
func NewAIHistoryCompressor(triggerTokens int, keepLastRounds int) *AIHistoryCompressor {
	if triggerTokens <= 0 {
		triggerTokens = 1
	}
	return &AIHistoryCompressor{
		keepLastRounds: keepLastRounds,
		triggerTokens:  triggerTokens,
	}
}

func NewAIHistoryCompressorWithTokenBudget(triggerTokens int, keepLastRounds int) *AIHistoryCompressor {
	if triggerTokens <= 0 {
		triggerTokens = 1
	}
	return &AIHistoryCompressor{
		keepLastRounds: keepLastRounds,
		triggerTokens:  triggerTokens,
	}
}

// Compress 压缩历史
func (c *AIHistoryCompressor) Compress(ctx context.Context, aiClient ai.ChatClient, instruction string, history []*ai.MsgInfo) (*HistoryCompactionResult, error) {
	if c == nil || aiClient == nil || len(history) == 0 {
		return &HistoryCompactionResult{
			History:     NormalizeHistoryMsgInfos(history),
			State:       CompactionStateNormal,
			CanContinue: true,
		}, nil
	}
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}
	windowed := ApplyHistoryCompactionWindow(history)
	working, summary := stripHistoryCompaction(windowed)
	if len(working) == 0 && strings.TrimSpace(summary) == "" {
		return &HistoryCompactionResult{
			History:     NormalizeHistoryMsgInfos(working),
			State:       CompactionStateNormal,
			CanContinue: true,
		}, nil
	}

	canonical := insertHistoryCompaction(working, summary)
	baseCanonical := NormalizeHistoryMsgInfos(canonical)
	result := &HistoryCompactionResult{
		History:     NormalizeHistoryMsgInfos(canonical),
		State:       CompactionStateNormal,
		CanContinue: true,
	}
	promptManager := c.promptManager
	if promptManager == nil {
		defaultPromptManager, err := newDefaultPromptManager()
		if err != nil {
			return nil, fmt.Errorf("create history compaction prompt manager failed: %w", err)
		}
		promptManager = defaultPromptManager
	}

	keepLastRounds := c.keepLastRounds
	if keepLastRounds < 0 {
		keepLastRounds = 0
	}

	overflowBudget := resolveContextBudget(aiClient)
	need := detectHistoryCompactionNeed(working, summary, c.triggerTokens, overflowBudget)
	result.Overflow = need.overflow
	if !need.shouldCompress {
		return result, nil
	}
	initialHistoryTokens := estimateHistoryTokens(working) + estimateSummaryTokens(summary)
	compactionReason := "estimated_token_threshold"
	if need.overflow {
		compactionReason = "usage_overflow_threshold"
	}
	runtimelog.LogJSON("info", map[string]any{
		"event":         "history_compaction_triggered",
		"before_tokens": initialHistoryTokens,
		"after_tokens":  initialHistoryTokens,
		"reason":        compactionReason,
	})

	result.State = CompactionStateNeedsCompaction

	for loops := 0; loops < historyCompressMaxLoops; loops++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			markHistoryCompactionInterrupted(result, baseCanonical, ctxErr)
			return result, nil
		}

		rounds := splitAssistantRounds(working)
		if len(rounds) <= keepLastRounds {
			result.History = NormalizeHistoryMsgInfos(canonical)
			result.StillOverflow = true
			result.CanContinue = false
			result.TerminalReason = CompactionTerminalOverflow
			return result, nil
		}

		compressableRounds := len(rounds) - keepLastRounds
		batch := compressableRounds / 2
		if batch < 1 {
			batch = 1
		}

		compressStart := rounds[0].start
		compressEnd := rounds[batch-1].end
		if compressStart < 0 || compressEnd <= compressStart || compressEnd > len(working) {
			result.History = NormalizeHistoryMsgInfos(canonical)
			result.StillOverflow = true
			result.CanContinue = false
			result.TerminalReason = CompactionTerminalNoProgress
			return result, nil
		}

		result.AttemptCount = loops + 1
		beforeWorking := NormalizeHistoryMsgInfos(working)
		beforeSummary := summary
		nextSummary, err := summarizeHistoryCompaction(
			ctx,
			aiClient,
			promptManager,
			strings.TrimSpace(instruction),
			strings.TrimSpace(summary),
			stripImagesFromExcerpt(working[compressStart:compressEnd]),
		)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				markHistoryCompactionInterrupted(result, baseCanonical, ctxErr)
				return result, nil
			}
			return nil, err
		}
		nextSummary = strings.TrimSpace(nextSummary)
		if nextSummary == "" {
			result.History = NormalizeHistoryMsgInfos(canonical)
			result.StillOverflow = true
			result.CanContinue = false
			result.TerminalReason = CompactionTerminalEmptySummary
			return result, nil
		}
		summary = nextSummary

		reduced := make([]*ai.MsgInfo, 0, len(working)-(compressEnd-compressStart))
		reduced = append(reduced, working[:compressStart]...)
		reduced = append(reduced, working[compressEnd:]...)
		working = reduced
		if !hasEffectiveHistoryCompaction(beforeWorking, beforeSummary, working, summary) {
			result.History = NormalizeHistoryMsgInfos(canonical)
			result.StillOverflow = true
			result.CanContinue = false
			result.TerminalReason = CompactionTerminalNoProgress
			return result, nil
		}

		canonical = insertHistoryCompaction(working, summary)
		result.History = NormalizeHistoryMsgInfos(canonical)
		result.DidCompact = true

		need = detectHistoryCompactionNeed(working, summary, c.triggerTokens, overflowBudget)
		result.Overflow = result.Overflow || need.overflow
		if !need.shouldCompress {
			result.State = CompactionStateDone
			result.StillOverflow = false
			result.CanContinue = true
			afterTokens := estimateHistoryTokens(working) + estimateSummaryTokens(summary)
			runtimelog.LogJSON("info", map[string]any{
				"event":         "history_compaction_completed",
				"before_tokens": initialHistoryTokens,
				"after_tokens":  afterTokens,
				"reason":        compactionReason,
				"attempt_count": result.AttemptCount,
				"overflow":      result.Overflow,
			})
			return result, nil
		}

		result.State = CompactionStateNeedsCompaction
		if result.AttemptCount >= historyCompressMaxLoops {
			result.StillOverflow = true
			result.CanContinue = false
			result.TerminalReason = CompactionTerminalMaxAttempts
			return result, nil
		}
	}

	result.History = NormalizeHistoryMsgInfos(canonical)
	result.State = CompactionStateNeedsCompaction
	result.StillOverflow = true
	result.CanContinue = false
	result.TerminalReason = CompactionTerminalMaxAttempts
	return result, nil
}

func detectHistoryCompactionNeed(history []*ai.MsgInfo, summary string, triggerTokens int, budget ContextBudget) historyCompactionNeed {
	usage, _ := LatestAssistantUsageFromHistory(history)
	if usage != nil {
		overflow := IsOverflowWithUsage(usage, budget)
		return historyCompactionNeed{
			shouldCompress: overflow,
			overflow:       overflow,
		}
	}
	effectiveTokens := estimateHistoryTokens(history) + estimateSummaryTokens(summary)
	return historyCompactionNeed{
		shouldCompress: effectiveTokens > triggerTokens,
	}
}

func hasEffectiveHistoryCompaction(beforeHistory []*ai.MsgInfo, beforeSummary string, afterHistory []*ai.MsgInfo, afterSummary string) bool {
	if strings.TrimSpace(afterSummary) == "" {
		return false
	}
	if len(afterHistory) < len(beforeHistory) {
		return true
	}
	beforeTokens := estimateHistoryTokens(beforeHistory) + estimateSummaryTokens(beforeSummary)
	afterTokens := estimateHistoryTokens(afterHistory) + estimateSummaryTokens(afterSummary)
	return afterTokens < beforeTokens
}

func markHistoryCompactionInterrupted(result *HistoryCompactionResult, fallback []*ai.MsgInfo, ctxErr error) {
	if result == nil {
		return
	}
	result.History = NormalizeHistoryMsgInfos(fallback)
	result.CanContinue = false
	result.State = CompactionStateNeedsCompaction
	if ctxErr == nil {
		result.Interrupted = true
		result.TerminalReason = CompactionTerminalInterrupted
		return
	}
	switch ctxErr {
	case context.DeadlineExceeded:
		result.TimedOut = true
		result.TerminalReason = CompactionTerminalTimeout
	default:
		result.Interrupted = true
		result.TerminalReason = CompactionTerminalInterrupted
	}
}

type assistantRound struct {
	start int
	end   int
}

func splitAssistantRounds(history []*ai.MsgInfo) []assistantRound {
	if len(history) == 0 {
		return nil
	}
	userStarts := make([]int, 0, 8)
	for idx, msg := range history {
		if msg == nil {
			continue
		}
		if strings.TrimSpace(msg.Role) == "user" {
			userStarts = append(userStarts, idx)
		}
	}
	if len(userStarts) == 0 {
		return nil
	}
	out := make([]assistantRound, 0, len(userStarts))
	for i, start := range userStarts {
		end := len(history)
		if i+1 < len(userStarts) {
			end = userStarts[i+1]
		}
		if end <= start {
			continue
		}
		hasAssistant := false
		for idx := start; idx < end; idx++ {
			msg := history[idx]
			if msg == nil {
				continue
			}
			if strings.TrimSpace(msg.Role) == "assistant" {
				hasAssistant = true
				break
			}
		}
		if !hasAssistant {
			continue
		}
		out = append(out, assistantRound{start: start, end: end})
	}
	return out
}

// ApplyHistoryCompactionWindow 仅保留最近 compaction 边界之后的历史窗口。
// 边界定义：user(summary_request) -> assistant(summary)。
func ApplyHistoryCompactionWindow(history []*ai.MsgInfo) []*ai.MsgInfo {
	if len(history) == 0 {
		return nil
	}
	for summaryIndex := len(history) - 1; summaryIndex >= 0; summaryIndex-- {
		if !isCompactionSummaryMsg(history[summaryIndex]) {
			continue
		}
		for requestIndex := summaryIndex - 1; requestIndex >= 0; requestIndex-- {
			if isCompactionRequestMsg(history[requestIndex]) {
				return history[requestIndex:]
			}
		}
		return history[summaryIndex:]
	}
	return history
}

func stripHistoryCompaction(history []*ai.MsgInfo) (clean []*ai.MsgInfo, summary string) {
	if len(history) == 0 {
		return nil, ""
	}
	clean = make([]*ai.MsgInfo, 0, len(history))
	for _, msg := range history {
		if isCompactionRequestMsg(msg) {
			continue
		}
		ok, body := parseCompactionSummaryMsg(msg)
		if ok {
			if strings.TrimSpace(body) != "" {
				summary = strings.TrimSpace(body)
			}
			continue
		}
		clean = append(clean, msg)
	}
	return clean, summary
}

func isCompactionRequestMsg(msg *ai.MsgInfo) bool {
	if msg == nil {
		return false
	}
	if strings.TrimSpace(msg.Role) != "user" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(msg.Type), HistoryCompactionRequestType) {
		return true
	}
	content, ok := msg.Content.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(content) == HistoryCompactionRequestText
}

func isCompactionSummaryMsg(msg *ai.MsgInfo) bool {
	if msg == nil {
		return false
	}
	return strings.TrimSpace(msg.Role) == "assistant" && strings.EqualFold(strings.TrimSpace(msg.Type), HistoryCompactionSummaryType)
}

func parseCompactionSummaryMsg(msg *ai.MsgInfo) (ok bool, summary string) {
	if msg == nil {
		return false, ""
	}
	if strings.TrimSpace(msg.Role) == "assistant" && strings.EqualFold(strings.TrimSpace(msg.Type), HistoryCompactionSummaryType) {
		return true, FormatMsgContent(msg.Content)
	}
	return false, ""
}

func insertHistoryCompaction(history []*ai.MsgInfo, summary string) []*ai.MsgInfo {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return history
	}
	reqMsg := ai.NewUserMsgInfo(HistoryCompactionRequestText)
	reqMsg.Type = HistoryCompactionRequestType
	sumMsg := ai.NewAIMsgInfo(summary)
	sumMsg.Type = HistoryCompactionSummaryType

	out := make([]*ai.MsgInfo, 0, len(history)+2)
	out = append(out, reqMsg, sumMsg)
	out = append(out, history...)
	return out
}

func FormatMsgContent(content any) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []byte:
		return strings.TrimSpace(string(v))
	case []*ai.ChatContext:
		var parts []string
		for _, ctx := range v {
			if ctx == nil {
				continue
			}
			if ctx.Type == "text" && strings.TrimSpace(ctx.Text) != "" {
				parts = append(parts, strings.TrimSpace(ctx.Text))
			} else if ctx.Type == "image_url" {
				parts = append(parts, "[image]")
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return argx.Text(v)
		}
		return strings.TrimSpace(string(raw))
	}
}

func LatestAssistantUsageFromHistory(history []*ai.MsgInfo) (*ai.TokenUsage, int) {
	if len(history) == 0 {
		return nil, -1
	}
	for idx := len(history) - 1; idx >= 0; idx-- {
		msg := history[idx]
		if msg == nil {
			continue
		}
		if strings.TrimSpace(msg.Role) != "assistant" {
			continue
		}
		if msg.Usage == nil {
			continue
		}
		usage := ai.NormalizeTokenUsagePtr(msg.Usage)
		if usage == nil {
			continue
		}
		return usage, idx
	}
	return nil, -1
}

func TotalAssistantUsageContextTokens(history []*ai.MsgInfo) (int, bool) {
	if len(history) == 0 {
		return 0, false
	}
	total := 0
	hasUsage := false
	for _, msg := range history {
		if msg == nil {
			continue
		}
		if strings.TrimSpace(msg.Role) != "assistant" {
			continue
		}
		if msg.Usage == nil {
			continue
		}
		usage := ai.NormalizeTokenUsagePtr(msg.Usage)
		if usage == nil {
			continue
		}
		hasUsage = true
		total += usage.ContextCountTokens()
		if total < 0 {
			total = 0
		}
	}
	return total, hasUsage
}

func IsOverflowWithUsage(usage *ai.TokenUsage, budget ContextBudget) bool {
	if usage == nil {
		return false
	}
	return IsOverflowWithUsageTotal(usage.ContextCountTokens(), budget)
}

func IsOverflowWithUsageTotal(total int, budget ContextBudget) bool {
	usable := budget.UsableInputTokens
	if usable <= 0 {
		return false
	}
	if total <= 0 {
		return false
	}
	return total >= usable
}

// CompactionTerminatedError 表示任务因 history compaction 不可恢复而终止。
// 调用方可通过 errors.As / IsCompactionTerminated 区分此类错误与普通任务执行失败。
type CompactionTerminatedError struct {
	Reason  string // CompactionTerminal* 常量之一
	Message string
}

func (e *CompactionTerminatedError) Error() string {
	return e.Message
}

// IsCompactionTerminated 判断 err 是否为 compaction 终止错误。
func IsCompactionTerminated(err error) bool {
	var target *CompactionTerminatedError
	return errors.As(err, &target)
}
