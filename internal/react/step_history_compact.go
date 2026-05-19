package react

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"aster/internal/ai"
	"aster/internal/runtimelog"
)

const (
	stepHistoryCompactionSummaryType   = "step_compaction_summary"
	stepHistoryToolResultShortenedHint = "...(tool_result shortened for context budget)..."
)

type StepHistoryCompactor interface {
	Compact(ctx context.Context, aiClient ai.ChatClient, instruction string, systemPrompt string, stepHistory []*ai.MsgInfo) (*StepHistoryCompactionResult, error)
}

type StepHistoryCompactionResult struct {
	StepHistory     []*ai.MsgInfo
	DidCompact      bool
	AttemptCount    int
	StillNearBudget bool
	CanContinue     bool
	TerminalReason  string

	BeforeTokens int
	AfterTokens  int
	BeforeRounds int
	AfterRounds  int
}

type AIStepHistoryCompactor struct {
	keepLastRounds     int
	triggerRatio       float64
	toolResultMaxRunes int
	promptManager      PromptManager
}

func NewAIStepHistoryCompactor(triggerRatio float64, keepLastRounds int, toolResultMaxRunes int, promptManager PromptManager) *AIStepHistoryCompactor {
	if keepLastRounds < 0 {
		keepLastRounds = 0
	}
	if triggerRatio <= 0 || triggerRatio > 1 {
		triggerRatio = 0.90
	}
	if toolResultMaxRunes <= 0 {
		toolResultMaxRunes = 1024
	}
	return &AIStepHistoryCompactor{
		keepLastRounds:     keepLastRounds,
		triggerRatio:       triggerRatio,
		toolResultMaxRunes: toolResultMaxRunes,
		promptManager:      promptManager,
	}
}

func (c *AIStepHistoryCompactor) Compact(ctx context.Context, aiClient ai.ChatClient, instruction string, systemPrompt string, stepHistory []*ai.MsgInfo) (*StepHistoryCompactionResult, error) {
	if c == nil || aiClient == nil || len(stepHistory) == 0 {
		return &StepHistoryCompactionResult{
			StepHistory: NormalizeHistoryMsgInfos(stepHistory),
			CanContinue: true,
		}, nil
	}
	if ctx == nil {
		return nil, fmt.Errorf("ctx must not be nil")
	}
	manager := c.promptManager
	if manager == nil {
		defaultPromptManager, err := newDefaultPromptManager()
		if err != nil {
			return nil, fmt.Errorf("create prompt manager failed: %w", err)
		}
		manager = defaultPromptManager
	}

	working := NormalizeHistoryMsgInfos(stepHistory)
	beforeTokens := estimateHistoryTokens(working) + estimateStringTokens(systemPrompt)
	beforeRounds := len(splitToolRounds(working))

	result := &StepHistoryCompactionResult{
		StepHistory:     working,
		CanContinue:     true,
		BeforeTokens:    beforeTokens,
		AfterTokens:     beforeTokens,
		BeforeRounds:    beforeRounds,
		AfterRounds:     beforeRounds,
		AttemptCount:    0,
		DidCompact:      false,
		StillNearBudget: false,
	}

	budget := resolveContextBudget(aiClient)
	triggerTokens := int(float64(budget.UsableInputTokens) * c.triggerRatio)
	if triggerTokens < 1 {
		triggerTokens = budget.UsableInputTokens
	}
	if triggerTokens < 1 {
		triggerTokens = 1
	}
	if beforeTokens < triggerTokens {
		return result, nil
	}

	if err := validateToolCallSequence(working); err != nil {
		// Best-effort: avoid making the transcript worse. Return the original.
		result.TerminalReason = "invalid_tool_call_sequence"
		return result, nil
	}

	runtimelog.LogJSON("info", map[string]any{
		"event":          "step_history_compaction_triggered",
		"before_tokens":  beforeTokens,
		"trigger_tokens": triggerTokens,
		"before_rounds":  beforeRounds,
	})

	// Layer 1: shorten old tool_result payloads without changing the message structure.
	working, shortened := shortenOldToolResults(working, c.keepLastRounds, c.toolResultMaxRunes)
	if shortened {
		result.DidCompact = true
		result.StepHistory = working
	}

	afterLayer1Tokens := estimateHistoryTokens(working) + estimateStringTokens(systemPrompt)
	result.AfterTokens = afterLayer1Tokens
	result.AfterRounds = len(splitToolRounds(working))
	if afterLayer1Tokens < triggerTokens {
		result.StillNearBudget = false
		return result, nil
	}

	// Layer 2: AI compaction (fold complete tool rounds into one assistant summary).
	prevSummary, working := stripStepCompactionSummary(working)
	for loops := 0; loops < historyCompressMaxLoops; loops++ {
		if ctx.Err() != nil {
			result.CanContinue = false
			result.TerminalReason = CompactionTerminalInterrupted
			return result, nil
		}

		rounds := splitToolRounds(working)
		if len(rounds) <= c.keepLastRounds {
			result.CanContinue = false
			result.TerminalReason = CompactionTerminalOverflow
			result.StepHistory = working
			result.AfterTokens = estimateHistoryTokens(working) + estimateStringTokens(systemPrompt)
			result.AfterRounds = len(rounds)
			return result, nil
		}

		compressable := len(rounds) - c.keepLastRounds
		batch := compressable / 2
		if batch < 1 {
			batch = 1
		}

		// Dynamic positioning: try from oldest rounds; if summarization fails, shrink batch
		// or shift the window forward to locate a compressible segment.
		windowStart := 0
		windowBatch := batch
		success := false
		var nextSummary string
		var compressStart, compressEnd int
		for attempts := 0; attempts < 12 && !success; attempts++ {
			if windowStart < 0 {
				windowStart = 0
			}
			if windowStart+windowBatch > compressable {
				windowStart = compressable - windowBatch
				if windowStart < 0 {
					windowStart = 0
				}
			}
			if windowBatch < 1 {
				windowBatch = 1
			}
			if windowStart >= compressable {
				break
			}
			if windowStart+windowBatch > len(rounds) {
				windowBatch = len(rounds) - windowStart
			}
			if windowStart+windowBatch > compressable {
				windowBatch = compressable - windowStart
			}
			if windowBatch < 1 {
				break
			}

			compressStart = rounds[windowStart].start
			compressEnd = rounds[windowStart+windowBatch-1].end
			if compressStart < 0 || compressEnd <= compressStart || compressEnd > len(working) {
				break
			}

			excerpt := working[compressStart:compressEnd]
			next, err := summarizeHistoryCompaction(ctx, aiClient, manager, strings.TrimSpace(instruction), strings.TrimSpace(prevSummary), excerpt)
			if err != nil || strings.TrimSpace(next) == "" {
				// Shrink first, then shift.
				if windowBatch > 1 {
					windowBatch = maxInt(1, windowBatch/2)
				} else {
					windowStart++
				}
				continue
			}
			next = strings.TrimSpace(next)
			beforeExcerptTokens := estimateHistoryTokens(excerpt) + estimateSummaryTokens(prevSummary)
			afterExcerptTokens := estimateStringTokens(next)
			if afterExcerptTokens >= beforeExcerptTokens {
				// No progress; shrink or shift.
				if windowBatch > 1 {
					windowBatch = maxInt(1, windowBatch/2)
				} else {
					windowStart++
				}
				continue
			}
			nextSummary = next
			success = true
		}

		result.AttemptCount = loops + 1
		if !success || strings.TrimSpace(nextSummary) == "" {
			result.CanContinue = false
			result.TerminalReason = CompactionTerminalNoProgress
			result.StepHistory = insertStepCompactionSummary(working, prevSummary)
			result.AfterTokens = estimateHistoryTokens(result.StepHistory) + estimateStringTokens(systemPrompt)
			result.AfterRounds = len(splitToolRounds(result.StepHistory))
			return result, nil
		}

		prevSummary = nextSummary
		reduced := make([]*ai.MsgInfo, 0, len(working)-(compressEnd-compressStart)+1)
		reduced = append(reduced, working[:compressStart]...)
		// Note: do not create tool_calls in summary; it is plain assistant text.
		summaryMsg := ai.NewAIMsgInfo(prevSummary)
		summaryMsg.Type = stepHistoryCompactionSummaryType
		reduced = append(reduced, summaryMsg)
		reduced = append(reduced, working[compressEnd:]...)
		working = NormalizeHistoryMsgInfos(reduced)
		result.DidCompact = true

		if err := validateToolCallSequence(working); err != nil {
			// Defensive: revert to a safe windowed transcript with summary injected.
			result.CanContinue = false
			result.TerminalReason = "invalid_tool_call_sequence_after_compaction"
			result.StepHistory = insertStepCompactionSummary(stepHistory, prevSummary)
			result.AfterTokens = estimateHistoryTokens(result.StepHistory) + estimateStringTokens(systemPrompt)
			result.AfterRounds = len(splitToolRounds(result.StepHistory))
			return result, nil
		}

		afterTokens := estimateHistoryTokens(working) + estimateStringTokens(systemPrompt)
		result.StepHistory = working
		result.AfterTokens = afterTokens
		result.AfterRounds = len(splitToolRounds(working))
		if afterTokens < triggerTokens {
			result.StillNearBudget = false
			runtimelog.LogJSON("info", map[string]any{
				"event":         "step_history_compaction_completed",
				"before_tokens": beforeTokens,
				"after_tokens":  afterTokens,
				"attempt_count": result.AttemptCount,
			})
			return result, nil
		}
	}

	result.StepHistory = insertStepCompactionSummary(working, prevSummary)
	result.AfterTokens = estimateHistoryTokens(result.StepHistory) + estimateStringTokens(systemPrompt)
	result.AfterRounds = len(splitToolRounds(result.StepHistory))
	result.CanContinue = false
	result.TerminalReason = CompactionTerminalMaxAttempts
	return result, nil
}

type toolRound struct {
	start int
	end   int
	ids   map[string]struct{}
}

func splitToolRounds(stepHistory []*ai.MsgInfo) []toolRound {
	if len(stepHistory) == 0 {
		return nil
	}
	out := make([]toolRound, 0, 16)
	for idx := 0; idx < len(stepHistory); idx++ {
		msg := stepHistory[idx]
		if msg == nil || strings.TrimSpace(msg.Role) != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		ids := make(map[string]struct{}, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			if tc == nil {
				continue
			}
			id := strings.TrimSpace(tc.Id)
			if id != "" {
				ids[id] = struct{}{}
			}
		}
		if len(ids) == 0 {
			continue
		}
		end := idx + 1
		for end < len(stepHistory) {
			next := stepHistory[end]
			if next == nil {
				end++
				continue
			}
			if strings.TrimSpace(next.Role) != "tool" {
				break
			}
			callID := strings.TrimSpace(next.ToolCallID)
			if callID == "" {
				break
			}
			if _, ok := ids[callID]; !ok {
				break
			}
			end++
		}
		out = append(out, toolRound{start: idx, end: end, ids: ids})
		idx = end - 1
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validateToolCallSequence(stepHistory []*ai.MsgInfo) error {
	if len(stepHistory) == 0 {
		return nil
	}
	pending := make(map[string]struct{})
	for idx := 0; idx < len(stepHistory); idx++ {
		msg := stepHistory[idx]
		if msg == nil {
			continue
		}
		role := strings.TrimSpace(msg.Role)
		switch role {
		case "assistant":
			if len(msg.ToolCalls) == 0 {
				continue
			}
			// A new tool_calls block cannot start before finishing the previous one.
			if len(pending) > 0 {
				return fmt.Errorf("unanswered tool_call_ids before assistant tool_calls at idx=%d", idx)
			}
			for _, tc := range msg.ToolCalls {
				if tc == nil {
					continue
				}
				id := strings.TrimSpace(tc.Id)
				if id != "" {
					pending[id] = struct{}{}
				}
			}
		case "tool":
			callID := strings.TrimSpace(msg.ToolCallID)
			if callID == "" {
				return fmt.Errorf("tool message missing tool_call_id at idx=%d", idx)
			}
			if _, ok := pending[callID]; !ok {
				return fmt.Errorf("tool message tool_call_id=%s has no pending tool_calls at idx=%d", callID, idx)
			}
			delete(pending, callID)
		default:
			// user/system/etc: ignore
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("unanswered tool_call_ids remain: %d", len(pending))
	}
	return nil
}

func shortenOldToolResults(stepHistory []*ai.MsgInfo, keepLastRounds int, maxRunes int) ([]*ai.MsgInfo, bool) {
	if len(stepHistory) == 0 || maxRunes <= 0 {
		return stepHistory, false
	}
	rounds := splitToolRounds(stepHistory)
	if len(rounds) == 0 {
		return stepHistory, false
	}
	if keepLastRounds < 0 {
		keepLastRounds = 0
	}
	cutoff := len(rounds) - keepLastRounds
	if cutoff <= 0 {
		return stepHistory, false
	}
	did := false
	for r := 0; r < cutoff; r++ {
		round := rounds[r]
		for idx := round.start + 1; idx < round.end && idx < len(stepHistory); idx++ {
			msg := stepHistory[idx]
			if msg == nil || strings.TrimSpace(msg.Role) != "tool" {
				continue
			}
			s, ok := msg.Content.(string)
			if !ok {
				continue
			}
			next, changed := shortenToolResultText(s, maxRunes)
			if !changed {
				continue
			}
			msg.Content = next
			did = true
		}
	}
	return NormalizeHistoryMsgInfos(stepHistory), did
}

func shortenToolResultText(text string, maxRunes int) (string, bool) {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || text == "" {
		return text, false
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return text, false
	}
	const marker = "Full output saved to:"
	if idx := strings.Index(text, marker); idx >= 0 {
		suffix := strings.TrimSpace(text[idx:])
		if suffix != "" && utf8.RuneCountInString(suffix) > maxInt(64, maxRunes/2) {
			suffix = truncateByRunes(suffix, maxInt(64, maxRunes/2))
		}
		prefixBudget := maxRunes - utf8.RuneCountInString(suffix) - utf8.RuneCountInString(stepHistoryToolResultShortenedHint) - 8
		if prefixBudget < 0 {
			prefixBudget = 0
		}
		prefix := truncateByRunes(strings.TrimSpace(text[:idx]), prefixBudget)
		out := strings.TrimSpace(prefix)
		if out != "" {
			out += "\n\n"
		}
		out += stepHistoryToolResultShortenedHint
		if suffix != "" {
			out += "\n\n" + suffix
		}
		return strings.TrimSpace(out), true
	}
	out := truncateByRunes(text, maxRunes)
	if out == text {
		return text, false
	}
	return strings.TrimSpace(out) + "\n\n" + stepHistoryToolResultShortenedHint, true
}

func stripStepCompactionSummary(history []*ai.MsgInfo) (summary string, clean []*ai.MsgInfo) {
	if len(history) == 0 {
		return "", nil
	}
	clean = make([]*ai.MsgInfo, 0, len(history))
	for _, msg := range history {
		if msg == nil {
			continue
		}
		if strings.TrimSpace(msg.Role) == "assistant" && strings.EqualFold(strings.TrimSpace(msg.Type), stepHistoryCompactionSummaryType) {
			if body, ok := msg.Content.(string); ok {
				if strings.TrimSpace(body) != "" {
					summary = strings.TrimSpace(body)
				}
			}
			continue
		}
		clean = append(clean, msg)
	}
	return strings.TrimSpace(summary), NormalizeHistoryMsgInfos(clean)
}

func insertStepCompactionSummary(history []*ai.MsgInfo, summary string) []*ai.MsgInfo {
	summary = strings.TrimSpace(summary)
	history = NormalizeHistoryMsgInfos(history)
	if summary == "" {
		return history
	}
	out := make([]*ai.MsgInfo, 0, len(history)+1)
	msg := ai.NewAIMsgInfo(summary)
	msg.Type = stepHistoryCompactionSummaryType
	out = append(out, msg)
	out = append(out, history...)
	return NormalizeHistoryMsgInfos(out)
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
