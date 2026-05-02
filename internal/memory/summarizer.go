package memory

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"aster/internal/ai"
)

// SummarizeTimelineItems 使用 timeline_memory_prompt.prompt 对指定 items 进行增量摘要合并
// 该方法与 TimelineMemory 的 summarize 行为一致，但不依赖 TimelineMemory 实例
func SummarizeTimelineItems(ctx context.Context, client ai.ChatClient, extraContext string, prevSummary string, items []*TimelineItem) (string, error) {
	if client == nil {
		return "", fmt.Errorf("timeline items summarizer is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	tmpl := NewTimeLine(ctx, client, func() string { return strings.TrimSpace(extraContext) })
	tmpl.summary = strings.TrimSpace(prevSummary)

	bufMemory := bytes.NewBuffer(nil)
	err := tmpl.memoryTemplate.Execute(bufMemory, map[string]any{
		"EXTRA_CONTEXT":  tmpl.extraInfo(),
		"PREV_SUMMARY":   strings.TrimSpace(tmpl.summary),
		"COMPRESS_ITEMS": items,
		"NONCE":          generateRandom(8),
	})
	if err != nil {
		return "", fmt.Errorf("execute memory template failed: %w", err)
	}

	prompt := bufMemory.String()
	var lastErr error
	for attempt := 0; attempt <= defaultAIOutputMaxRetries; attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if attempt > 0 {
			if err := sleepWithContext(ctx, retryDelay(attempt)); err != nil {
				return "", err
			}
		}

		resp, err := client.ChatText(ctx, prompt)
		if err != nil {
			return "", fmt.Errorf("chat text failed: %w", err)
		}
		summary := strings.TrimSpace(resp)
		if summary != "" {
			return summary, nil
		}
		lastErr = fmt.Errorf("summary is empty")
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("summary is empty")
	}
	return "", lastErr
}
