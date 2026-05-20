package react

import (
	"context"
	"path/filepath"
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

	if rootDir := strings.TrimSpace(a.workspaceRootDir); rootDir != "" {
		var wsPointers strings.Builder
		wsPointers.WriteString("\n\n工作区路径指针：\n")
		wsPointers.WriteString("parent_workspace_root: " + rootDir + "\n")
		wsPointers.WriteString("parent_step_contexts_path: " + filepath.Join(rootDir, "workspace", "step_contexts.jsonl") + "\n")
		ns := strings.TrimSpace(a.workspaceNamespace)
		if ns == "" {
			ns = "root"
		}
		wsPointers.WriteString("parent_plan_current_path: " + filepath.Join(rootDir, "artifacts", ns, "plan", "current.json") + "\n")
		next = next + wsPointers.String()
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
