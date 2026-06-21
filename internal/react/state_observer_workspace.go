package react

import (
	"strings"

	"aster/internal/builtin_tools"
)

// workspaceStateObserver 桥接 PlanItemChange → ensureStepFileScaffold。
//
// 显式表达不变量：**任何 PlanItem 翻进 InProgress 时其 step_<id>.md 骨架必须就位**。
// 旧实现把 ensureStepFileScaffold 挂在 runStepPhase（主路径）一个调用点，peer 路径
// 漏调导致 session 288b1031 看到 step_p2.md / step_p4.md 缺失。改走 observer 后，
// MarkCurrentStepInProgress / MarkInlineStepInProgress 任一翻 Pending→InProgress 都
// 自动触发，未来加显式 start_step 工具或 resume 路径也零成本对齐。
//
// agent 字段保存活引用：observer 不在 PlanItemChange 里塞 workspaceRuntime 句柄，
// 改在回调时从 agent 现取——workspaceRuntime 在 Agent.Execute 内被赋值，observer
// 注册时刻可能尚未就绪，回调时刻则一定就绪。
type workspaceStateObserver struct {
	agent *Agent
}

func newWorkspaceStateObserver(agent *Agent) *workspaceStateObserver {
	if agent == nil {
		return nil
	}
	return &workspaceStateObserver{agent: agent}
}

func (o *workspaceStateObserver) OnPlanItemChange(change PlanItemChange) {
	if o == nil || o.agent == nil {
		return
	}
	rt := o.agent.workspaceRuntime
	if rt == nil {
		return
	}
	// 仅 Pending→InProgress 触发 scaffold。其他翻转（Pending→Completed、终态间过渡、
	// 新建 plan 时多个 pending 条目）均不创建——避免给「未来才会跑」或「永远不跑」
	// 的 step 提前落空文件骨架。
	if change.PrevStatus != builtin_tools.PlanStepPending {
		return
	}
	if change.NextStatus != builtin_tools.PlanStepInProgress {
		return
	}
	stepID := strings.TrimSpace(change.StepID)
	if stepID == "" {
		return
	}
	stepTitle := ""
	if change.Item != nil {
		stepTitle = strings.TrimSpace(change.Item.Step)
	}
	// observer 在 state writer 锁内被调用——不可调用 state.Snapshot()（RWMutex 非可重入会
	// 死锁）。warn 路径走 emitter 而非 emitRuntimeLog，避免 snapshot 依赖。
	if err := ensureStepFileScaffold(rt, stepID, stepTitle); err != nil {
		if o.agent.emitter != nil {
			o.agent.emitter.EmitLogPayload(map[string]any{
				"level":   "warn",
				"message": "ensure step file scaffold failed",
				"event":   "step_file_scaffold_failed",
				"step_id": stepID,
				"error":   err.Error(),
			})
		}
	}
}
