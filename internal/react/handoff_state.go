package react

import (
	"context"
	"strings"

	"aster/internal/memory"
)

type handoffState struct {
	summary string
	differ  *memory.TimelineMemoryDiffer
}

// DefaultOnHandoffFunc 默认的 Agent 交接回调
func DefaultOnHandoffFunc(ctx context.Context, agent *Agent, handoffTo string) string {
	if agent == nil {
		return ""
	}
	return agent.defaultOnHandoff(ctx, handoffTo)
}

func (a *Agent) defaultOnHandoff(ctx context.Context, handoffTo string) string {
	if a == nil || a.handoff == nil {
		return strings.TrimSpace("")
	}

	current := strings.TrimSpace(a.handoff.summary)
	if a.handoff.differ == nil || a.cfg == nil || a.cfg.AIClient == nil {
		return current
	}

	diff, err := a.handoff.differ.PeekDiff()
	if err != nil {
		return current
	}
	diff = strings.TrimSpace(diff)
	if diff == "" {
		return current
	}

	next, err := summarizeAgentHandoff(ctx, a.cfg.AIClient, a.promptManager, strings.TrimSpace(handoffTo), strings.TrimSpace(a.cfg.Instruction), current, diff)
	if err != nil {
		return current
	}
	next = strings.TrimSpace(next)
	if next == "" {
		return current
	}

	a.handoff.summary = next
	a.handoff.differ.SetBaseline()
	return next
}

func (a *Agent) buildAgentHandoffExtra(ctx context.Context, handoffTo string) string {
	if a == nil {
		return ""
	}
	handoffTo = strings.TrimSpace(handoffTo)

	handoffFunc := DefaultOnHandoffFunc
	if a.cfg != nil && a.cfg.OnHandoffFunc != nil {
		handoffFunc = a.cfg.OnHandoffFunc
	}
	return strings.TrimSpace(handoffFunc(ctx, a, handoffTo))
}
