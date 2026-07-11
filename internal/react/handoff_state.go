package react

import (
	"context"
	"strings"

	"aster/internal/workspacefs"
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
		l := workspacefs.New(rootDir, a.workspaceNamespace)
		// 顶层（归一后 Namespace==""）沿用历史 artifacts/root/… 写点指针
		// （Layout 的 Legacy 只读回退路径），与既有输出逐字节一致。
		planCurrentPath := l.PlanCurrent()
		if l.Namespace == "" {
			planCurrentPath = l.LegacyPlanCurrent()
		}
		var wsPointers strings.Builder
		wsPointers.WriteString("\n\n工作区路径指针：\n")
		wsPointers.WriteString("parent_workspace_root: " + rootDir + "\n")
		wsPointers.WriteString("parent_step_contexts_path: " + l.StepContexts() + "\n")
		wsPointers.WriteString("parent_plan_current_path: " + planCurrentPath + "\n")
		wsPointers.WriteString("parent_task_context_path: " + l.TaskContext() + "\n")
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
