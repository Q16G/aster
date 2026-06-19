package react

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
	"aster/internal/runtimelog"
)

// spawnRemoteStep 派发一个远程 step 到后台 goroutine 独立执行。
// 复用 sub_agent_tool.executeAsync 的 spawn 模型，但走 RegisterRemoteStep 路径——
// 完成后通过 asyncRegistry.Complete → drain → state.UpdateInlineStep 自动回写 PlanItem。
//
// 与 sub_agent 的关键差异：
//   - Kind = remote_step（drain 分流到 handleRemoteStepNotification）
//   - 不发 EventTypeSubAgentBgStart（远程 step 卡片事件归面 5）
//   - 不登记父 workspace 的 ChildAgents 指针（远程 step 通过 plan item 已可见）
//
// ctx 寿命：直接继承调用方传入的 ctx（约定是 scheduler/runStepPhase 的 ctx，最终源自 TUI
// 顶层 ctx），**不 derive、不设超时**——step 内时间无法保证，用户通过 esc 取消（取消信号
// 传到顶层 ctx）。
//
// fire-and-forget；调用方不等待。错误来自前置校验（factory 缺失、stepID 无效、workspace 建失败
// 等），实际执行错误通过 RunResult 进 drain 路径。
//
// TODO（面 3.3）：当前 childDef 仅继承父 cfg.BashTool 一项，缺其他 Policies / 模型选择 /
// 上下文继承；面 3.3 落 transcript blob 时一并补齐。
func (a *Agent) spawnRemoteStep(ctx context.Context, stepID string, plan []*builtin_tools.PlanItem) error {
	if a == nil {
		return fmt.Errorf("nil agent")
	}
	// ctx 已被取消：早退避免起空跑 goroutine（child agent.Execute 拿到 cancelled ctx
	// 也会立即退出，但跳过 RegisterRemoteStep + Build 仍能省一次开销）。
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if a.agentFactory == nil {
		return fmt.Errorf("agent factory not injected (remote step requires AgentFactory.Build path)")
	}
	if a.asyncRegistry == nil {
		return fmt.Errorf("async registry not initialized")
	}

	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return fmt.Errorf("empty stepID")
	}

	var planItem *builtin_tools.PlanItem
	for _, item := range plan {
		if item != nil && strings.TrimSpace(item.ID) == stepID {
			planItem = item
			break
		}
	}
	if planItem == nil {
		return fmt.Errorf("plan item %q not found", stepID)
	}

	instruction := strings.TrimSpace(planItem.Step)
	if instruction == "" {
		return fmt.Errorf("plan item %q has empty step text", stepID)
	}

	childName := fmt.Sprintf("remote-step-%s", stepID)
	childDef := AgentDefinition{
		Name:        childName,
		Instruction: instruction,
		IsSubAgent:  true, // 远程 step 走 sub-agent 模式：不注册编排工具，不嵌套 spawn
		Policies: AgentPolicies{
			MaxIterations: 0, // 默认不限
		},
	}

	if a.cfg != nil && a.cfg.BashTool != nil {
		childDef.Policies.AllowBash = true
		childDef.Policies.BashPermissionContext = a.cfg.BashTool
	}

	childRootDir := filepath.Join(a.workspaceRootDir, "remote_steps", stepID)
	childRuntime, err := newLocalWorkspaceRuntime(
		a.workspaceSessionID,
		childRootDir,
		"root",
	)
	if err != nil {
		return fmt.Errorf("create remote step workspace: %w", err)
	}

	childAgent, err := a.agentFactory.Build(childDef)
	if err != nil {
		// Build 失败时清理 newLocalWorkspaceRuntime 留下的空目录，避免反复重试堆积。
		_ = os.RemoveAll(childRootDir)
		return fmt.Errorf("build remote step agent: %w", err)
	}

	execOpts := []ExecuteOption{
		WithWorkspaceRuntime(childRuntime),
		WithParentWorkspace(a.workspaceRootDir),
		WithSourceWorkingDir(a.sourceWorkingDir),
		WithSkipIntentPrelude(),
	}

	// Register 与 goroutine 启动之间不应有并发风险——本函数应仅由 scheduler 单 goroutine 调用。
	a.asyncRegistry.RegisterRemoteStep(stepID, childRootDir)

	// 把 PlanItem.Status 从 Pending 翻 InProgress，让右侧 Todo 列表立刻显示 ▸。
	// 与 MarkCurrentStepInProgress 的差异：按 stepID 显式定位，不动 CurrentStepID。
	if a.state != nil {
		a.state.MarkInlineStepInProgress(stepID)
	}

	// 发 BgStart 事件，让 TUI 立即出现 running 卡片（goroutine 完成才发 BgEnd 翻终态）。
	if a.emitter != nil {
		a.emitter.EmitJSON(EventTypeRemoteStepBgStart, stepID, map[string]any{
			"agent_id":  stepID,
			"step_id":   stepID,
			"step_text": instruction,
			"workspace": childRootDir,
		})
	}

	go func() {
		var result *builtin_tools.RunResult
		defer func() {
			if r := recover(); r != nil {
				runtimelog.LogJSON("error", map[string]any{
					"event":   "remote_step_panicked",
					"step_id": stepID,
					"panic":   fmt.Sprintf("%v", r),
				})
				result = &builtin_tools.RunResult{
					Success: false,
					Error:   fmt.Sprintf("remote step panicked: %v", r),
				}
			}
			// 落 transcript blob 到父 v2Store——必须在 Complete 之前（drain 路径
			// 读 result.TranscriptBlobRef 写到 PlanItem outcome）。失败/成功都写
			// （失败 step 的 transcript 同样有诊断价值），与主路径 persistStepTranscriptBlob 一致。
			if result != nil {
				history := childAgent.History()
				if ref := persistRemoteStepTranscript(a.v2Store, history); ref != "" {
					result.TranscriptBlobRef = ref
				}
			}
			a.asyncRegistry.Complete(stepID, result)
		}()

		var execErr error
		result, execErr = childAgent.Execute(ctx, instruction, execOpts...)
		if execErr != nil {
			// 强制兜底：execErr != nil 时无论 result 是否非 nil 都标失败，避免
			// "result.Success=true + err 同时返回" 时 drain 误判为 Completed。
			if result == nil {
				result = &builtin_tools.RunResult{Success: false, Error: execErr.Error()}
			} else {
				result.Success = false
				if strings.TrimSpace(result.Error) == "" {
					result.Error = execErr.Error()
				}
			}
		}
	}()

	return nil
}

// selectFanOutPeers 是 fan-out 选择逻辑的纯函数版（无副作用，可独立单测）。
// 输入 ready 列表 + 当前 current ID + 配置上限 + 现有 running remote 数 + 已注册判定，
// 返回应该派发的 peer step ID 序列（按 ready 原序）。
//
//   - 跳过 current（主路径已处理）
//   - 跳过已在 registry 里的（正在跑或已完成未 purge）
//   - 派发数量上限：maxParallel - 1 - runningRemote
//
// maxParallel < 2 或 slots <= 0 时返回 nil（no-op）。
func selectFanOutPeers(maxParallel int, runningRemote int, currentID string, ready []string, alreadyRegistered func(string) bool) []string {
	if maxParallel < 2 {
		return nil
	}
	slotsAvailable := maxParallel - 1 - runningRemote
	if slotsAvailable <= 0 {
		return nil
	}
	currentID = strings.TrimSpace(currentID)
	out := make([]string, 0, slotsAvailable)
	for _, id := range ready {
		if slotsAvailable <= 0 {
			break
		}
		// 防御性：ready 来自 ReadyRunnablePlanStepIDs，理论只筛 PlanStepPending，
		// 主路径 current 在 MarkCurrentStepInProgress 后已是 InProgress，不会在此命中。
		// 保留这条作为不变量被破坏时的兜底。
		if id == currentID {
			continue
		}
		if alreadyRegistered != nil && alreadyRegistered(id) {
			continue
		}
		out = append(out, id)
		slotsAvailable--
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// fanOutReadyPeers 在 runStepPhase 入口扫 ready step，把当前 CurrentStepID 以外的 ready
// 派发为远程 step。spawn 数量受 MaxParallelSteps 上限与现有 RunningRemoteSteps 占用 slot 限制。
//
// MaxParallelSteps < 2 时为 no-op（保持串行）；asyncRegistry 缺失时为 no-op。
//
// 每个 spawn 失败仅记录日志，不阻断其他 peer 派发（与 LangGraph 重写 LLM Compiler 风格一致）。
func (a *Agent) fanOutReadyPeers(ctx context.Context, snapshot builtin_tools.StateSnapshot) {
	if a == nil || a.asyncRegistry == nil {
		return
	}
	maxParallel := a.maxParallelSteps()
	if maxParallel < 2 {
		return
	}
	// sub_agent 自身的 Agent 实例不持有 factory（避免嵌套 spawn）。
	// 早退避免每个 peer 都触发一次 spawnRemoteStep("factory not injected") warning。
	if a.agentFactory == nil {
		return
	}
	// 收尾路径防御：teardown 时（FinalAnswer / max-iter / ctx 取消）
	// drain 仍可能调到 fanOutReadyPeers（runtime_scheduler.go:171,1153 路径），
	// 此时若 plan 仍有 pending 会误派发新远程 step——直接早退。
	// ctx 取消由 spawnRemoteStep 内 ctx.Err() 再兜底；FinalAnswer 只能从这里挡。
	if snapshot.Phase == builtin_tools.AgentPhaseFinalAnswer {
		return
	}

	peers := selectFanOutPeers(
		maxParallel,
		a.asyncRegistry.RunningRemoteSteps(),
		snapshot.CurrentStepID,
		builtin_tools.ReadyRunnablePlanStepIDs(snapshot.Plan),
		func(id string) bool { return a.asyncRegistry.Get(id) != nil },
	)

	for _, id := range peers {
		if err := a.spawnRemoteStep(ctx, id, snapshot.Plan); err != nil {
			// TODO(面 3.3+)：spawn 失败的 step 保持 Pending，下个 iter / drain 会再次尝试。
			// 若失败是确定性的（配置错误 / agentFactory.Build 一致失败），会刷屏日志。
			// 后续应实现"失败 N 次后通过 UpdateInlineStep 标 Failed 收敛"机制。
			a.emitRuntimeLog("warn", "spawn remote step failed", snapshot, map[string]any{
				"event":   "remote_step_spawn_failed",
				"step_id": id,
				"error":   err.Error(),
			})
			continue
		}
	}
}

// persistRemoteStepTranscript 把远程 step 的 history 序列化后写入父 agent 的 v2Store blob。
//
// 关键设计：必须用**父 agent 的 store**——child agent 的 store 在 ${parent}/remote_steps/${stepID}/
// 子目录里，step_replan 注入 BlobPath(ref) 时走父 store 解引用；用父 store 写才能让 ref 解引用到。
//
// 失败时返回空字符串 + 写日志，不阻断 goroutine 继续 Complete。Content-addressed store
// 天然并发安全（path = sha256(content)），多 goroutine 并发 WriteBlob 无写冲突。
//
// TODO（已知风险，登记后续）：
//  1. child agent 的 history 可能已被 in-step compaction 改写（agent_execute.go:1496），
//     落到 blob 的是压缩态而非完整 transcript。主路径有 persistStepTranscriptBlob 的
//     「先存全本再压缩」保护（agent_execute.go:1295），远程路径目前没有等价保护。
//     X2 远程跑单 step 触发概率低（child IsSubAgent=true 不会再嵌套 sub_agent），
//     但高负载下仍可能丢全本。后续可考虑在 child 内复用相同 persist 路径并跨 store 复制 blob。
//  2. child agent 自己也在 ${parent}/remote_steps/${stepID}/ 下 Open 了一份 v2Store
//     （agent_execute.go:324 由 Execute 触发），跑完留下 events.jsonl/snapshot.json/blobs/
//     成为孤儿；父 store 不读它也不清理。X2 高并发会膨胀磁盘，后续应在 spawnRemoteStep
//     的 goroutine defer 里加一次性 cleanup 或运维定期归档。
func persistRemoteStepTranscript(store *persistv2.Store, history []*ai.MsgInfo) string {
	if store == nil {
		// 防御性 warn：现状 v2Store 在 agent_execute.go:324 一定 Open，到这里说明被绕过
		// （例如直接构造 Agent 跑 spawn 的测试或未来新入口）。drain 会读到空 ref 写到
		// PlanItem.TranscriptBlobRef，导致 step_replan 拿不到指针——是设计缺失信号。
		runtimelog.LogJSON("warn", map[string]any{
			"event": "remote_step_transcript_skipped_nil_store",
			"hint":  "parent agent v2Store should be initialized via agent_execute.go path",
		})
		return ""
	}
	if len(history) == 0 {
		return ""
	}
	raw, err := json.Marshal(ai.NormalizeMsgInfoSlice(history))
	if err != nil {
		runtimelog.LogJSON("warn", map[string]any{
			"event": "marshal_remote_step_transcript_failed",
			"error": err.Error(),
		})
		return ""
	}
	ref, err := store.WriteBlob(raw)
	if err != nil {
		runtimelog.LogJSON("warn", map[string]any{
			"event": "write_blob_remote_step_transcript_failed",
			"error": err.Error(),
		})
		return ""
	}
	return strings.TrimSpace(ref)
}
