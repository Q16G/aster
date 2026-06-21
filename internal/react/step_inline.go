package react

import (
	"context"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// inline_step 调度纯函数 + 集合辅助。
//
// 本文件提供 commit 8 真接桶并发时调度层会用的两个原语：
//   - selectInlineStepPeers: 在给定 ready 列表 + 现有占用 + 上限下，纯计算可派发 peer
//   - collectInlineStepIDs:  把 current + 可派发 peer 串成统一的 stepID 列表（commit 8
//     的 runStepsConcurrently 会按此列表起 goroutine）
//
// commit 7 阶段这两个函数尚无调度调用方——step_fanout.go 旧 X2 路径仍走 selectFanOutPeers，
// commit 8 把 runStepPhase 换成"current + peers 统一并发"模型时切到本文件函数并删除旧 fanout。

// selectInlineStepPeers 是 step_fanout.go:selectFanOutPeers 的迁移版本，语义不变：
// 在已有 running / current / 上限三层过滤下，从 ready 列表里挑出本轮要派发的 peer step ID。
//
//   - 跳过 current（主路径已处理）
//   - 跳过已在 registry 里的（正在跑或已完成未 purge）
//   - 派发数量上限：maxParallel - 1 - runningInline
//
// maxParallel < 2 或 slots <= 0 时返回 nil（no-op，退化为单步串行）。
//
// **degenerate case 设计**：maxParallel=1 时返回 nil → collectInlineStepIDs 只返回
// [current]，runStepsConcurrently 单 goroutine 跑——与多桶代码路径完全一致，不保留
// 任何 "if maxParallel < 2 { ... 走旧串行路径 }" 的分叉特殊化（参见
// [[feedback_no_atomic_ledger_tools]] 和 plan 文件 §fallback 不变质原则）。
func selectInlineStepPeers(maxParallel int, runningInline int, currentID string, ready []string, alreadyRegistered func(string) bool) []string {
	if maxParallel < 2 {
		return nil
	}
	slotsAvailable := maxParallel - 1 - runningInline
	if slotsAvailable <= 0 {
		return nil
	}
	currentID = strings.TrimSpace(currentID)
	out := make([]string, 0, slotsAvailable)
	for _, id := range ready {
		if slotsAvailable <= 0 {
			break
		}
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

// peerScopedSnapshot 返回 snapshot 副本，runCtx 非空时把 CurrentStepID 投影到 runCtx.StepID。
//
// peer goroutine 内的 emitRuntimeLog 等以 snapshot.CurrentStepID 派生 step_id 字段时，
// 直接用全局 a.state.Snapshot() 会看到主路径 step（peer 跑 s2 但日志显示 s1）——这是
// Bug B 难以发现的次因（日志骗了 debug）。peer 路径所有 emitRuntimeLog 调用都应当
// 先经此 helper 投影。runCtx==nil（主路径）时是 no-op。
func peerScopedSnapshot(snapshot builtin_tools.StateSnapshot, runCtx *InlineStepCtx) builtin_tools.StateSnapshot {
	if runCtx == nil {
		return snapshot
	}
	if strings.TrimSpace(runCtx.StepID) != "" {
		snapshot.CurrentStepID = runCtx.StepID
	}
	if runCtx.LocalActiveSkillNames != nil {
		// 方案 B：peer 日志 / runtimelog payload 看到自己的 skill 集合而非全局
		snapshot.ActiveSkillNames = builtin_tools.CloneStringSlice(*runCtx.LocalActiveSkillNames)
	}
	return snapshot
}

// runInlineStep 跑单 stepID 的 think_act：parts build → AICallProxy → auto-complete or dispatch。
// 共享主 agent 的 state / workspaceRuntime / emitter；按 runCtx 路由 history 与 PlanItem 写入：
//   - runCtx == nil（主路径 current step）：a.stepHistory 写入；auto-complete 走 UpdateCurrentStep
//   - runCtx != nil（inline peer，commit 8c 启用）：runCtx.Bucket.msgs 写入；
//     auto-complete 走 UpdateInlineStep(runCtx.StepID)
//
// 调用方：commit 8b 只由 runStepPhase 用 nil 调用（行为不变）；commit 8c 增加 goroutine
// 包装的 runStepsConcurrently 调用，给每个 peer 传带桶的 runCtx。
//
// snapshot 参数是调用方在合适时机抓的状态快照；runInlineStep 内部仍会按需 re-snapshot
// （compaction / auto-complete 决策点）。
func (a *Agent) runInlineStep(
	ctx context.Context,
	runCtx *InlineStepCtx,
	runClient ai.ChatClient,
	iter int,
	snapshot builtin_tools.StateSnapshot,
	extraText string,
) error {
	// Peer 路径：thinkActPartsForStep 内部基于 snapshot.CurrentStepID 取 step 信息构建
	// prompt。peer goroutine 跑时 snapshot.CurrentStepID 仍是主路径的 ID，需要复制一份
	// snapshot 并把 CurrentStepID 改成 runCtx.StepID，让 prompt 锁定到 peer 的 step。
	//
	// 这是 pragmatic 做法——未来如果 thinkActPartsForStep 内部还隐式读了其他 stepID-绑定
	// 的 agent state（如 frozenLineageByStep / lastStepTranscriptBlobRef），那些路径需要
	// commit 13 测试覆盖时再补显式参数化（见 [[feedback_no_atomic_ledger_tools]] 的「pragmatic
	// 优先 + 测试承担正确性」哲学）。
	promptSnapshot := snapshot
	if runCtx != nil {
		if strings.TrimSpace(runCtx.StepID) != "" && runCtx.StepID != snapshot.CurrentStepID {
			promptSnapshot.CurrentStepID = runCtx.StepID
		}
		if runCtx.LocalActiveSkillNames != nil {
			// 方案 B：peer 视角的 skill set——BuildThinkActPrompt 读 snapshot.ActiveSkillNames
			// 时自然看到 peer overlay 而非主路径全局。skill prompt 注入按 peer 的 baseline + 自治
			// overlay 派生，不被其他 peer load/eject 干扰。
			promptSnapshot.ActiveSkillNames = builtin_tools.CloneStringSlice(*runCtx.LocalActiveSkillNames)
		}
	}
	currentStep := promptSnapshot.CurrentStep()

	parts := a.thinkActPartsForStep(ctx, extraText, promptSnapshot)
	fnTools, allowedTools := a.BuildFunctionTools(runCtx, builtin_tools.AgentPhaseStep)

	thinkCtx, thinkCancel := context.WithCancel(ctx)
	defer thinkCancel()
	callResult, err := a.AICallProxy(thinkCtx, runCtx, iter, runClient, parts, promptFamilyThinkAct, fnTools...)
	if err != nil {
		return err
	}

	snapshot = a.state.Snapshot()
	if shouldStopAfterCompaction(callResult.Compaction, snapshot) {
		reason := ""
		if callResult.Compaction != nil {
			reason = callResult.Compaction.TerminalReason
		}
		return &CompactionTerminatedError{
			Reason:  reason,
			Message: buildHistoryCompactionStopMessage(callResult.Compaction),
		}
	}

	// 自动完成：模型未调任何工具但出了正文。
	// 主路径用 UpdateCurrentStep；inline peer 用 UpdateInlineStep(peerStepID) 显式定位。
	if callResult != nil && len(callResult.ToolCalls) == 0 {
		// A4 守卫：仍有真正的后台 **sub_agent**（user 显式 spawn 的子任务）在跑时
		// 不能自动完成本 step——避免父 turn 终止取消子 ctx 丢结果。
		//
		// 必须按 Kind 过滤：HasRunning() 不分 Kind 会把 inline peer 也算成「后台任务」，
		// 让主路径无限 defer（peer 跑完才退出 await，但 peer 也在等主路径前进，最坏死锁）。
		// inline peer 由 runStepsConcurrently 的 wg 自然兜底，不需要 A4 守卫。
		if runCtx == nil && a.asyncRegistry != nil && a.asyncRegistry.HasRunningSubAgent() {
			a.awaitBackgroundRequested = true
			a.emitRuntimeLog("info", "deferring step completion: background sub-agents running", peerScopedSnapshot(snapshot, runCtx), map[string]any{
				"event":   "step_defer_for_background",
				"step_id": stepIDOf(currentStep),
				"running": len(a.asyncRegistry.RunningAgents()),
			})
			return nil
		}
		assistantText := strings.TrimSpace(callResult.AssistantText)
		if assistantText == "" {
			a.emitRuntimeLog("error", "step phase produced empty output", peerScopedSnapshot(snapshot, runCtx), map[string]any{
				"event":   "step_phase_empty_output_error",
				"step_id": stepIDOf(currentStep),
			})
			return fmt.Errorf("step produced no tool calls and empty content")
		}
		if runCtx != nil {
			// inline peer 自动完成：UpdateInlineStep 显式按 stepID 定位，不动 CurrentStepID/Phase。
			snapshot = a.state.UpdateInlineStep(runCtx.StepID, builtin_tools.CurrentStepUpdate{
				Status:        builtin_tools.PlanStepCompleted,
				Summary:       assistantText,
				DisplayResult: assistantText,
			})
		} else {
			snapshot = a.state.Snapshot()
			current := snapshot.CurrentStep()
			if current == nil || strings.TrimSpace(current.ID) == "" {
				return fmt.Errorf("step missing current plan item")
			}
			snapshot = a.state.UpdateCurrentStep(builtin_tools.CurrentStepUpdate{
				Status:        builtin_tools.PlanStepCompleted,
				Summary:       assistantText,
				DisplayResult: assistantText,
			})
		}
		a.emitter.EmitStateChange(snapshot)
		a.emitRuntimeLog("warning", "auto completed step from assistant content", peerScopedSnapshot(snapshot, runCtx), map[string]any{
			"event":        "auto_step_complete",
			"step_id":      stepIDOf(currentStep),
			"content_size": len(assistantText),
		})
		return nil
	}

	executedToolCalls, dispatchErr := a.dispatchToolCalls(ctx, runCtx, iter, callResult.ToolCalls, allowedTools)
	if dispatchErr != nil {
		return dispatchErr
	}

	snapshot = a.state.Snapshot()
	a.emitRuntimeLog("info", "step phase executed tool calls", peerScopedSnapshot(snapshot, runCtx), map[string]any{
		"event":                "step_phase_tool_calls_executed",
		"tool_calls_requested": len(callResult.ToolCalls),
		"tool_calls_executed":  executedToolCalls,
		"will_continue":        !snapshot.Terminal(),
	})
	return nil
}

// spawnInlinePeer 为指定的 peer stepID 起后台 goroutine 跑 runInlineStep 循环。
//
// 与老 step_fanout.spawnRemoteStep 的关键差异：
//   - **不**起 child agent / 不建独立 workspace
//   - **不**走 agentFactory.Build 路径（共享主 agent 实例）
//   - bucket seed 从主 a.stepHistory 当前末尾 deep copy（commit 2 settle）
//   - 完成态：通过 a.state.UpdateInlineStep(stepID, ...) 显式回写，不动 CurrentStepID/Phase
//
// 调用方契约：仅由 scheduler 单写者 goroutine 调用（runStepsConcurrently）。
// peer goroutine 内可能在 think_act 中 emit、改 state（state 自带 mutex）、
// 写共享文件（runtime mutex 兜底，prompt 纪律承担 RMW 正确性）。
//
// fire-and-forget；调用方不等待。失败仅记录日志（旧实现的 spawn 失败重试已删）。
func (a *Agent) spawnInlinePeer(parentCtx context.Context, runClient ai.ChatClient, iter int, peerStepID string) error {
	if a == nil {
		return fmt.Errorf("nil agent")
	}
	if parentCtx != nil && parentCtx.Err() != nil {
		return parentCtx.Err()
	}
	if a.asyncRegistry == nil {
		return fmt.Errorf("async registry not initialized")
	}
	peerStepID = strings.TrimSpace(peerStepID)
	if peerStepID == "" {
		return fmt.Errorf("empty peerStepID")
	}

	// 在 scheduler goroutine 内 deep-copy 主 history 末尾作为桶 seed——必须在 spawn
	// goroutine 之前完成，避免桶 seed 引用主 slice 后续 append 触发 race。
	seed := append([]*ai.MsgInfo(nil), a.stepHistory...)
	planVer := 0
	var skillBaseline []string
	if a.state != nil {
		snap := a.state.Snapshot()
		planVer = snap.PlanVersion
		// 方案 B 关键：spawn 时 deep-copy 当时全局 ActiveSkillNames 作为 peer 的
		// skill overlay baseline。peer 内 load/eject 只动 overlay，不污染全局，
		// 也不被其他 peer 干扰（详见 InlineStepCtx.LocalActiveSkillNames doc）。
		skillBaseline = builtin_tools.CloneStringSlice(snap.ActiveSkillNames)
	}
	bucket := a.ensureBucket(peerStepID, builtin_tools.AgentPhaseStep, planVer, seed)
	runCtx := &InlineStepCtx{
		StepID:                peerStepID,
		Bucket:                bucket,
		FinalAnswerAllowed:    false, // peer 桶禁 finalize（防 state 双写——P0-2 防线）
		LocalActiveSkillNames: &skillBaseline,
	}

	// 注册到 asyncRegistry（仍走老 RegisterInlineStep API 直到 commit 14 改名）。
	// drain 路径会通过该注册看到 peer 的生命周期。
	a.asyncRegistry.RegisterInlineStep(peerStepID, a.workspaceRootDir)
	// 翻 PlanItem Pending→InProgress：observer 自动 emit task_item + inline_step_start
	// + ensureStepFileScaffold。手 emit / 手 scaffold 全部下沉到 state_observer_*。
	if a.state != nil {
		a.state.MarkInlineStepInProgress(peerStepID)
	}

	// fix/10（P1-8）spawn 兜底——register 已落、MarkInlineStepInProgress 已翻 InProgress，
	// 但若本同步段 panic（极端如 OOM）或 go 关键字执行前异常，goroutine 永不存在 →
	// 其内部 defer 不会跑 → PlanItem 永远 InProgress，registry 永远 running。
	//
	// observer 改造后简化：spawn 失败时调 UpdateInlineStep(Failed)，observer 自动
	// emit task_item + inline_step_end，TUI 卡片正常翻终态。defer 仅剩
	// registry.Complete + dropBucket 这俩 observer 管不到的副作用。
	spawnSucceeded := false
	defer func() {
		if spawnSucceeded {
			return
		}
		if a.state != nil {
			a.state.UpdateInlineStep(peerStepID, builtin_tools.CurrentStepUpdate{
				Status: builtin_tools.PlanStepFailed,
				Error:  "inline peer spawn failed before goroutine start",
			})
		}
		a.asyncRegistry.Complete(peerStepID, &builtin_tools.RunResult{
			Success: false,
			Error:   "inline peer spawn failed before goroutine start",
		})
		a.dropBucket(peerStepID)
	}()

	go func() {
		var result *builtin_tools.RunResult
		defer func() {
			if r := recover(); r != nil {
				result = &builtin_tools.RunResult{
					Success: false,
					Error:   fmt.Sprintf("inline peer panicked: %v", r),
				}
			}
			// peer terminal：一次性持久化桶 transcript blob，ref 塞 RunResult.TranscriptBlobRef
			// 让 drain 路径写到 StepAttemptResult.TranscriptBlobRef，下游 step_replan
			// 可按 ref 解引用读 peer 跑过的完整 transcript（与主路径 lastStepTranscriptBlobRef
			// 对称）。单 writer：本 goroutine 已退出 think_act loop，桶 msgs 不再写；
			// 主路径不竞争该 blob。
			//
			// **result == nil 防御**：所有正常 loop 退出路径都已设 result（终态 / err / 取消 /
			// panic recover）；nil 只可能在未来 patch 漏写时出现——默认 Failed 让漏写显形，
			// 不静默成功（silent-success-on-empty 是危险默认）。
			if result == nil {
				result = &builtin_tools.RunResult{Success: false, Error: "inline peer exited with nil result"}
			}
			if ref := a.persistBucketTranscriptBlob(bucket); ref != "" {
				result.TranscriptBlobRef = ref
			}
			a.asyncRegistry.Complete(peerStepID, result)
			a.dropBucket(peerStepID)
		}()

		// peer 循环：跑到 PlanItem terminal 或 ctx 取消。
		// 与老 child agent Execute 的多 iter loop 对齐——peer 自己跑完所有 think_act 至终态。
		for {
			if parentCtx != nil && parentCtx.Err() != nil {
				result = &builtin_tools.RunResult{Success: false, Error: "parent context cancelled"}
				return
			}
			curSnap := a.state.Snapshot()
			if isInlineStepTerminal(curSnap, peerStepID) {
				result = &builtin_tools.RunResult{Success: true}
				return
			}
			if err := a.runInlineStep(parentCtx, runCtx, runClient, iter, curSnap, ""); err != nil {
				result = &builtin_tools.RunResult{Success: false, Error: err.Error()}
				return
			}
		}
	}()

	// goroutine 已起，spawn 同步段安全退出；spawn-level defer 不再兜底。
	spawnSucceeded = true
	return nil
}

// isInlineStepTerminal 判定指定 stepID 在 snapshot 里是否已是终态（Completed/Failed/Skipped）。
func isInlineStepTerminal(snap builtin_tools.StateSnapshot, stepID string) bool {
	stepID = strings.TrimSpace(stepID)
	for _, item := range snap.Plan {
		if item == nil {
			continue
		}
		if strings.TrimSpace(item.ID) != stepID {
			continue
		}
		switch item.Status {
		case builtin_tools.PlanStepCompleted,
			builtin_tools.PlanStepFailed,
			builtin_tools.PlanStepSkipped:
			return true
		}
		return false
	}
	return false
}

// runStepsConcurrently 是 step phase 的并发入口：把 [current, peer1, peer2, ...] 中的
// peer 派发到后台 goroutine，主路径在 caller goroutine 同步跑 current 的 runInlineStep。
//
// 与老 fanOutReadyPeers 的差异：peer 不再起 child agent，而是在主 agent 上下文里
// 以独立 bucket 跑 runInlineStep——共享 state / workspaceRuntime / emitter / v2Store。
//
// MaxParallelSteps=1 时 collectInlineStepIDs 只返回 [current]，没有 peer 派发，等价
// 于纯串行——degenerate case 走完全相同代码路径，无 if maxParallel<2 分叉。
func (a *Agent) runStepsConcurrently(
	ctx context.Context,
	runClient ai.ChatClient,
	iter int,
	snapshot builtin_tools.StateSnapshot,
	extraText string,
) error {
	stepIDs := a.collectInlineStepIDs(snapshot)
	if len(stepIDs) == 0 {
		return nil
	}
	currentID := strings.TrimSpace(stepIDs[0])
	if currentID != strings.TrimSpace(snapshot.CurrentStepID) {
		// 不变量保护：collectInlineStepIDs 第一项必须是 snapshot.CurrentStepID。
		return fmt.Errorf("collectInlineStepIDs[0]=%q != snapshot.CurrentStepID=%q", currentID, snapshot.CurrentStepID)
	}

	// 派发 peer（fire-and-forget）。失败仅记录日志，不阻塞主路径——peer 失败时下一轮
	// scheduler iter 重扫 ready 会再次尝试派发（asyncRegistry alreadyRegistered 兜底防重）。
	for _, peerID := range stepIDs[1:] {
		if err := a.spawnInlinePeer(ctx, runClient, iter, peerID); err != nil {
			a.emitRuntimeLog("warn", "spawn inline peer failed", snapshot, map[string]any{
				"event":   "inline_peer_spawn_failed",
				"step_id": peerID,
				"error":   err.Error(),
			})
		}
	}

	// 主路径同步跑 current（沿用老 runStepPhase 行为）。
	return a.runInlineStep(ctx, nil, runClient, iter, snapshot, extraText)
}

// collectInlineStepIDs 把本轮要并发跑的 stepID 集合化为 [current, peer1, peer2, ...]。
//
// 返回切片至少含 current（若存在），peer 顺序与 ReadyRunnablePlanStepIDs 输出一致。
// MaxParallelSteps < 2 或无可派发 peer 时仅返回 [current]；snapshot 无 current 时
// 返回 nil（退到 EnsureCurrentStep 兜底）。
//
// 调用方契约：scheduler 单 goroutine 调用——本函数读 a.asyncRegistry，写者在同一调度
// goroutine 内完成，不需要桶级锁。返回的 stepID 列表交 runStepsConcurrently 起 goroutine。
func (a *Agent) collectInlineStepIDs(snap builtin_tools.StateSnapshot) []string {
	if a == nil {
		return nil
	}
	currentID := strings.TrimSpace(snap.CurrentStepID)
	if currentID == "" {
		return nil
	}
	out := []string{currentID}

	if a.asyncRegistry == nil {
		return out
	}
	maxParallel := a.maxParallelSteps()
	if maxParallel < 2 {
		return out
	}
	peers := selectInlineStepPeers(
		maxParallel,
		a.asyncRegistry.RunningInlineSteps(),
		currentID,
		builtin_tools.ReadyRunnablePlanStepIDs(snap.Plan),
		func(id string) bool { return a.asyncRegistry.Get(id) != nil },
	)
	out = append(out, peers...)
	return out
}
