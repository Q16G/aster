package react

import (
	"context"
	"strings"
)

type handoffState struct {
	summary string
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
	snapshot := a.state.Snapshot()
	next := renderCompletedStepHandoffContext(snapshot.Plan, snapshot.StepOutcomes)
	if strings.TrimSpace(next) == "" {
		return current
	}

	a.handoff.summary = next
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
