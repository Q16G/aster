package react

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
	"aster/internal/runtimelog"
	"aster/internal/structuredoutput"

	"github.com/google/uuid"
)

// iterationAllowed 判断当前迭代是否可继续。maxIterations <= 0 表示不限制迭代次数，
// 循环只受终态收尾与 ctx 取消控制；maxIterations > 0 时按 opt-in 上限收敛。
func iterationAllowed(iter, maxIterations int) bool {
	return maxIterations <= 0 || iter <= maxIterations
}

func (a *Agent) runSchedulerLoop(ctx context.Context, runClient ai.ChatClient, extraText string, taskContext *TaskContextData, maxIterations int) (*builtin_tools.RunResult, error) {
	for iter := 1; iterationAllowed(iter, maxIterations); iter++ {
		a.drainAsyncAgentNotifications(ctx)

		if ctx != nil && ctx.Err() != nil {
			snapshot := a.state.Snapshot()
			if a.v2Store != nil && errors.Is(context.Cause(ctx), ErrTurnAbortRequested) {
				if _, err := a.v2Store.AppendEvent(&persistv2.Event{
					Type:    "TURN_ABORT_REQUESTED",
					GroupID: strings.TrimSpace(a.currentGroupID),
					TurnID:  strings.TrimSpace(a.currentTurnID),
				}); err != nil {
					// Best-effort signal: the turn is already being aborted, but persistence failure
					// must still be visible to the user for diagnostics.
					a.emitRuntimeLog("error", "persistence failed: append_event", snapshot, map[string]any{
						"kind":   "persistence",
						"action": "append_event",
						"err":    err.Error(),
					})
				}
			}
			a.emitRuntimeLog("warning", "scheduler context canceled", snapshot, map[string]any{
				"event": "scheduler_context_canceled",
				"error": ctx.Err().Error(),
			})
			// 取消也统一进入 final_answer phase（final_answer 内部会避免再调模型）。
			_ = a.state.EnterFinalAnswer(builtin_tools.TaskStatusCanceled, ctx.Err().Error())
			a.syncStepHistoryLayer(a.state.Snapshot())
			snapshot, _ = a.runFinalAnswerPhase(ctx, iter, runClient)
			// ctx is already canceled, so awaiting would return immediately; settle
			// any still-running sub-agent cards as cancelled instead.
			a.cancelRunningSubAgents()
			a.cancelRunningInlineSteps()
			a.emitter.EmitIteration(iter, maxIterations, "terminal")
			return a.finalizeResult(snapshot), nil
		}

		_ = a.state.SetIteration(iter)

		// X2 滚动收尾扫描：把已终态但从未经 step_replan 的 step（peer、滚动中已过的
		// 主路径 current）烘焙并写入 planner.jsonl。必须在 reduceStepOutcomesInState 之前——
		// 后者可能压缩旧 step outcome，先固化才不丢产出。
		a.finalizeUnjournaledTerminalSteps(a.state.Snapshot())

		a.reduceStepOutcomesInState(ctx, runClient)
		snapshot := a.state.Snapshot()
		phase := currentPhase(snapshot, a.maxParallelSteps())
		if phase != snapshot.Phase {
			_ = a.state.SetPhase(phase)
			snapshot = a.state.Snapshot()
		}

		// X2 并发屏障：真正进 step_replan 前等所有 in_progress inline peer 落定。
		// step_replan 会读 plan 做复核 / 整盘替换（NewPlan），若此刻还有 peer 在跑，会基于
		// 不完整状态重规划、且与 peer 完成回写竞态（结果丢失 / plan_version 错配）。await 后
		// 重算 phase：peer 落定可能解锁新 ready → 回 Step 继续滚动；否则全 terminal 才进 step_replan。
		// 仅在 phase 解析为 StepReplan 时阻塞，不影响 X2 滚动派发（那条路 phase=Step）。
		if phase == builtin_tools.AgentPhaseStepReplan && a.asyncRegistry != nil && a.asyncRegistry.HasRunningInlineSteps() {
			a.awaitRunningInlineSteps(ctx)
			a.finalizeUnjournaledTerminalSteps(a.state.Snapshot())
			snapshot = a.state.Snapshot()
			phase = currentPhase(snapshot, a.maxParallelSteps())
			if phase != snapshot.Phase {
				_ = a.state.SetPhase(phase)
				snapshot = a.state.Snapshot()
			}
		}

		a.syncStepHistoryLayer(snapshot)
		a.emitRuntimeLog("info", "scheduler iteration started", snapshot, map[string]any{
			"event":                "scheduler_iteration_start",
			"selected_phase":       phase,
			"input_timeline_count": len(snapshot.InputTimeline),
			"terminal":             snapshot.Terminal(),
		})
		a.emitRuntimeLog("info", "scheduler selected phase", snapshot, map[string]any{
			"event":          "phase_selected",
			"selected_phase": phase,
		})
		a.emitter.EmitIteration(iter, maxIterations, "phase:"+string(phase))

		switch phase {
		case builtin_tools.AgentPhasePlan:
			if err := a.runPlanPhase(ctx, iter, runClient, extraText, taskContext); err != nil {
				return a.handlePhaseError(ctx, err, iter, maxIterations, runClient)
			}
		case builtin_tools.AgentPhaseStep:
			if err := a.runStepPhase(ctx, iter, runClient, extraText, taskContext); err != nil {
				return a.handlePhaseError(ctx, err, iter, maxIterations, runClient)
			}
		case builtin_tools.AgentPhaseStepReplan:
			if err := a.runStepReplanPhase(ctx, iter, runClient); err != nil {
				return a.handlePhaseError(ctx, err, iter, maxIterations, runClient)
			}
		case builtin_tools.AgentPhaseIntentClassification:
			if err := a.runIntentClassificationPhase(ctx, iter, runClient); err != nil {
				return a.handlePhaseError(ctx, err, iter, maxIterations, runClient)
			}
		case builtin_tools.AgentPhaseFinalAnswer:
			if _, err := a.runFinalAnswerPhase(ctx, iter, runClient); err != nil {
				return nil, err
			}
		}

		snapshot = a.state.Snapshot()
		a.syncStepHistoryLayer(snapshot)
		if snapshot.Phase == builtin_tools.AgentPhaseFinalAnswer && snapshot.Terminal() {
			// Settle background sub-agents before returning so their panel cards do
			// not stay stuck on "running": their completion notifications are
			// otherwise silently discarded by asyncRegistry.Reset() next turn.
			if a.asyncRegistry != nil && a.asyncRegistry.HasRunning() {
				a.awaitAllBackgroundSubAgents(ctx)
				a.cancelRunningSubAgents()
				a.cancelRunningInlineSteps()
			}
			a.emitRuntimeLog("info", "scheduler iteration ended", snapshot, map[string]any{
				"event":                "scheduler_iteration_end",
				"next_phase":           snapshot.Phase,
				"terminal":             true,
				"will_continue":        false,
				"input_timeline_count": len(snapshot.InputTimeline),
			})
			a.emitter.EmitIteration(iter, maxIterations, "terminal")
			return a.finalizeResult(snapshot), nil
		}

		// Out-of-band park: when await_subagents was requested (explicitly via the
		// tool, or implicitly by the A4 guard in runStepPhase), block here without
		// any model call until ALL background sub-agents complete or ctx is
		// canceled. No timeout is imposed. Each completion is drained into
		// stepHistory as it arrives. The flag is cleared unconditionally so a stale
		// request (e.g. await called when no sub-agent was running) never leaks
		// into a later iteration.
		if a.awaitBackgroundRequested {
			a.awaitBackgroundRequested = false
			if a.asyncRegistry != nil && a.asyncRegistry.HasRunning() {
				a.emitRuntimeLog("info", "waiting for background sub-agents", snapshot, map[string]any{
					"event":   "await_background_subagents",
					"running": len(a.asyncRegistry.RunningAgents()),
				})
				a.emitter.EmitIteration(iter, maxIterations, "awaiting_background")
				a.awaitAllBackgroundSubAgents(ctx)
			}
		}

		a.emitRuntimeLog("info", "scheduler iteration ended", snapshot, map[string]any{
			"event":                "scheduler_iteration_end",
			"next_phase":           snapshot.Phase,
			"terminal":             snapshot.Terminal(),
			"will_continue":        true,
			"input_timeline_count": len(snapshot.InputTimeline),
		})
		a.emitter.EmitIteration(iter, maxIterations, "iteration_end")
	}

	snapshot := a.state.Snapshot()
	a.emitRuntimeLog("warning", "scheduler max iterations reached", snapshot, map[string]any{
		"event":          "scheduler_max_iterations_reached",
		"max_iterations": maxIterations,
	})
	_ = a.state.EnterFinalAnswer(builtin_tools.TaskStatusFailed, fmt.Sprintf("reach max iterations: %d", maxIterations))
	a.syncStepHistoryLayer(a.state.Snapshot())
	snapshot, _ = a.runFinalAnswerPhase(ctx, maxIterations, runClient)
	if a.asyncRegistry != nil && a.asyncRegistry.HasRunning() {
		a.awaitAllBackgroundSubAgents(ctx)
		a.cancelRunningSubAgents()
		a.cancelRunningInlineSteps()
	}
	return a.finalizeResult(snapshot), nil
}

// currentPhase 根据 state snapshot 决定下一轮路由进入的 phase。
//
// maxParallel 用于 X2 路由：≥2 时启用「Phase=StepReplan 但还有 ready → 绕回 Step」
// 让主路径继续滚动跑下一个 ready；<2 时退化为原行为（保持串行兼容）。
//
// 关键边界：guard 比「step-terminal-defense」优先级低——后者守住"全部 terminal 进 FinalAnswer"
// 不变量；guard 只在 StepReplan 分支生效。
func currentPhase(snapshot builtin_tools.StateSnapshot, maxParallel int) builtin_tools.AgentPhase {
	// 防御：卡在 Step 但已无可跑 frontier step 且全部 terminal —— 双条件路由（D6）：
	//   - 所有 phase 已收束（completed/blocked）→ 直达 FinalAnswer；
	//   - 仍有 phase 未收束 → 回 StepReplan 让 phase_assessments 定夺（final_answer 的
	//     should_replan 回流仍兜底），避免 completed step 但 lane 未判定就提前收尾。
	if snapshot.Phase == builtin_tools.AgentPhaseStep &&
		len(snapshot.Plan) > 0 &&
		builtin_tools.NextFrontierPlanStepID(snapshot.Plan, snapshot.Phases) == "" &&
		builtin_tools.AllPlanStepsTerminal(snapshot.Plan) {
		if builtin_tools.AllPhasesSettled(snapshot.Phases) {
			return builtin_tools.AgentPhaseFinalAnswer
		}
		return builtin_tools.AgentPhaseStepReplan
	}

	// X2 滚动 guard：主路径 step 完成时 state.go 翻 Phase=StepReplan，但若 frontier 上
	// 仍有 ready（被刚完成的 step / 刚解锁的 phase 释放的下一波），先绕回 Step 让主路径
	// 滚动派发，直到 frontier=0 且 in_progress=0 才真正进 step_replan（frontier barrier）。
	if maxParallel >= 2 &&
		snapshot.Phase == builtin_tools.AgentPhaseStepReplan &&
		builtin_tools.NextFrontierPlanStepID(snapshot.Plan, snapshot.Phases) != "" {
		return builtin_tools.AgentPhaseStep
	}

	switch snapshot.Phase {
	case builtin_tools.AgentPhasePlan, builtin_tools.AgentPhaseStep, builtin_tools.AgentPhaseStepReplan, builtin_tools.AgentPhaseFinalAnswer, builtin_tools.AgentPhaseIntentClassification:
		return snapshot.Phase
	default:
		return builtin_tools.AgentPhasePlan
	}
}

func (a *Agent) runPlanPhase(ctx context.Context, iter int, runClient ai.ChatClient, extraText string, taskContext *TaskContextData) error {
	_ = a.state.SetPhase(builtin_tools.AgentPhasePlan)
	snapshot := a.state.Snapshot()
	a.emitter.EmitStateChange(snapshot)
	a.emitRuntimeLog("info", "enter plan phase", snapshot, map[string]any{
		"event":                "phase_enter",
		"input_timeline_count": len(snapshot.InputTimeline),
	})

	planner := a.GetTaskPlanner()
	if planner == nil {
		return fmt.Errorf("task planner not configured")
	}

	snapshot = a.state.Snapshot()
	// 顶层 planner 冷启时若共享区事实板尚未落盘，预创建仅含两节空标题的骨架——
	// 须在 PromptContext 组装前完成，让 pc.TaskContextBoard 读到现成结构；存在则不覆盖。
	if !a.cfg.IsSubAgent && a.workspaceRuntime != nil {
		a.ensureTaskContextSkeleton()
	}
	// 统一 PromptContext：内存字段与共享区文件字段全部经动态 preview 上限投影（M2 接线）。
	pc := a.buildPromptContext(snapshot, "")
	// regenGoal：用户改向（intent=replan）回流时强制重产意图半径——不注入旧 GU，让 planner
	// 基于当前输入重新生成；其余回流（step_replan 内部重规划、intent=carry）沿用旧 GU。
	regenGoal := snapshot.ReplanContext != nil && snapshot.ReplanContext.RegenerateGoal
	// TaskContextBoard 仅顶层且有 workspace 时注入（与下方 plannerInput.TaskContextBoard 守卫一致）。
	injectsTaskBoard := !a.cfg.IsSubAgent && a.workspaceRuntime != nil
	// Layer A 聚合封顶：只对本回合确实会注入的字段记账，避免高估总量过度降级必需字段。
	a.applyInjectionBudget(planInjectionBudgetFields(pc, regenGoal, injectsTaskBoard), promptInjectionBudget(a.usableInputTokens))
	// 恢复回合现场（含 maybeBuildRecoveryChildContextJSON「用后即清」副作用）与 replan 上下文
	// 均为 plan 阶段瞬态，剥离为显式构建，保 buildPromptContext 纯函数化。
	recoveryCtx := a.buildRecoveryContext(snapshot)
	replanCtx := a.buildReplanContext(snapshot)
	inputStr := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{
		HandoffContext:      strings.TrimSpace(extraText),
		WorkspaceRootDir:    strings.TrimSpace(a.workspaceRootDir),
		WorkspaceNamespace:  strings.TrimSpace(a.workspaceNamespace),
		RecoveryContextJSON: recoveryCtx.Text,
		InputTimeline:       pc.InputTimeline.Text,
		TaskItemsJSON:       pc.Plan.Text,
		PhasesJSON:          pc.Phases.Text,
		ReplanContextJSON:   replanCtx.Text,
	})
	if inputStr == "" {
		a.emitRuntimeLog("error", "plan phase rejected empty input timeline", snapshot, map[string]any{
			"event":                "plan_input_missing",
			"input_timeline_count": len(snapshot.InputTimeline),
		})
		return fmt.Errorf("input timeline is empty")
	}

	skillsCtx := a.buildSkillsPromptContext(ctx, snapshot)
	mcpCtx := a.buildMCPPromptContext()

	plannerInput := TaskPlannerPromptInput{
		AgentProfile:    AgentProfile{AgentRole: strings.TrimSpace(a.cfg.Role), AgentBackground: strings.TrimSpace(a.cfg.Background)},
		CapabilityIndex: CapabilityIndex{SkillsContext: skillsCtx, MCPContext: mcpCtx},
		Input:           inputStr,
	}
	if a.workspaceRuntime != nil {
		if l := a.wsLayout(); l.SharedDir() != "" {
			plannerInput.TaskContextPath = l.TaskContext()
			plannerInput.OpenItemsLedgerPath = l.OpenItems()
		}
	}
	// userInputTurn：本回合由顶层用户新输入触发（cold_start 首次规划，或意图分类置 UserInitiated 的
	// carry/replan），区别于 step_replan 内部重规划与子 Agent 等待这类「运行过程中」回合。仅用户回合
	// 才让 planner 校正 task_context.md 的 `## 输入事实`（见 task_planner 意图理解段的"事实板同步"）。
	// 子 Agent 首次规划的 ReplanContext 同样为 nil，但子 Agent 工作区不承担顶层事实板维护契约——
	// 经 IsSubAgent 守卫排除，避免子 Agent 被强制注入用户回合段。
	plannerInput.UserInputTurn = !a.cfg.IsSubAgent && (snapshot.ReplanContext == nil || snapshot.ReplanContext.UserInitiated)
	plannerInput.HasReplanContext = snapshot.ReplanContext != nil
	// IsSubAgent 单独承担"顶层事实板维护契约"段的守卫——即便兜底回流（UserInitiated=false）
	// 也保留契约，避免事实板因守卫过窄被静默跳过。CanSpawnSubAgent 仅顶层 planner 开放：
	// 子 Agent 内 sub_agent 工具本身被运行时关闭，prompt 同步关闭委派条款，避免无意义引导。
	plannerInput.IsSubAgent = a.cfg.IsSubAgent
	plannerInput.CanSpawnSubAgent = !a.cfg.IsSubAgent
	if injectsTaskBoard {
		// TaskContextBoard 走 PromptContext 统一 preview：task_context.md 是精简事实板，
		// 正常不会过大，但无约束时 ## 执行中补充 会无限膨胀；超限时尾部截断并提示文件路径。
		// 骨架预创建已提前到 buildPromptContext 之前（骨架本身视作零内容快照）。
		plannerInput.TaskContextBoard = pc.TaskContextBoard.Text
	}
	if !regenGoal {
		plannerInput.GoalUnderstanding = pc.GoalUnderstanding.Text
	}
	a.applyPlannerOverflowHints(&plannerInput)

	// 仅在「全新意图」首次规划、或用户改向（regenGoal）时强制要求 goal_understanding：
	// 续写/重规划（含 ReplanContext、已有 plan）以及从意图感知恢复的回合（snapshot 已带
	// goal_understanding）一律沿用既有理解，不强制重做意图分析；planner 若提交了非空理解仍会
	// 覆盖（见 SetGoalUnderstanding）。
	requireGoalUnderstanding := (strings.TrimSpace(snapshot.GoalUnderstanding) == "" &&
		snapshot.ReplanContext == nil && len(snapshot.Plan) == 0) || regenGoal

	var res *builtin_tools.TaskPlannerResult
	if promptBuilder, ok := planner.(PlannerPromptBuilder); ok {
		planRes, err := a.runPlanPhaseWithTools(ctx, iter, runClient, plannerInput, promptBuilder, requireGoalUnderstanding)
		if err != nil {
			return err
		}
		res = planRes
	} else {
		plannerCtx := structuredoutput.WithLogger(ctx, a.structuredOutputLogger(snapshot))
		cfg := a.resolveStructuredOutputConfig(nil)
		cfg.StreamHandler = a.buildStructuredOutputStreamHandler()
		plannerCtx = structuredoutput.WithConfig(plannerCtx, cfg)
		planRes, err := planner.Plan(plannerCtx, inputStr)
		if err != nil {
			return err
		}
		res = planRes
	}

	var items []*builtin_tools.PlanItem
	needsPlanning := false
	plannerExplanation := ""
	if res != nil {
		needsPlanning = res.NeedsPlanning
		plannerExplanation = strings.TrimSpace(res.Explanation)
	}
	explanation := plannerExplanation
	if snapshot.ReplanContext != nil {
		needsPlanning = true
		explanation = firstNonEmpty(snapshot.ReplanContext.Reason, plannerExplanation)
	}

	if res != nil && len(res.Plan) > 0 {
		planItems := res.Plan
		if snapshot.ReplanContext != nil && snapshot.ReplanContext.ReplacePending {
			planItems = mergeReplannedPlan(snapshot.Plan, planItems)
		}
		normalized, err := builtin_tools.NormalizePlanItems(planItems, true)
		if err != nil {
			return fmt.Errorf("planner returned invalid plan: %w", err)
		}
		items = normalized
	}

	if len(items) == 0 {
		directResponse := ""
		if res != nil {
			directResponse = strings.TrimSpace(res.DirectResponse)
		}
		if directResponse == "" {
			directResponse = strings.TrimSpace(explanation)
		}
		if directResponse == "" {
			directResponse = "已完成。"
		}

		snapshot = a.state.ApplyFinalAnswerPhaseUpdate(finalAnswerPhaseUpdate{
			NextPhase:          builtin_tools.AgentPhaseFinalAnswer,
			Status:             builtin_tools.TaskStatusCompleted,
			FinalAnswerContent: directResponse,
			FinalAnswerSource:  "planner_direct",
		})
		a.emitter.EmitStateChange(snapshot)
		if snapshot.FinalAnswer != nil {
			a.emitter.EmitFinalAnswerResult(snapshot.FinalAnswer)
		}

		historyText := truncateForHistory(directResponse, "planner_direct")
		a.history = append(a.history, ai.NewAIMsgInfo(historyText))
		a.notifyHistoryReplace()

		a.emitRuntimeLog("info", "planner direct response: no plan items", snapshot, map[string]any{
			"event":          "planner_direct_response",
			"needs_planning": needsPlanning,
			"explanation":    explanation,
			"content_length": len(directResponse),
		})
		return nil
	}

	if res != nil {
		a.SetGoalUnderstanding(res.GoalUnderstanding)
		a.state.SetSimpleTask(res.Simple && len(items) == 1)
		// 业务 lane 贯穿：parse 侧已完成与既有 phases 的按 id 合并（completed/blocked 保留），
		// 此处原子替换。simple/direct 任务未提交 phases 时保留既有 lane，由 UpdatePlan 的
		// SynthesizePhasesIfMissing 兜底挂靠新 item。
		if len(res.Phases) > 0 {
			a.state.SetPhases(res.Phases)
		}
	}
	snapshot = a.ApplyPlanAndEmit(ctx, items, explanation, needsPlanning)
	if res != nil && len(items) > 0 {
		a.appendPlanContextRecord(res, snapshot)
	}
	a.emitRuntimeLog("info", "planner applied plan", snapshot, map[string]any{
		"event":               "plan_applied",
		"plan_count":          len(items),
		"needs_planning":      needsPlanning,
		"explanation":         explanation,
		"planner_explanation": plannerExplanation,
	})

	// 计划非空但所有 step 已 terminal：UpdatePlan 会把 Phase 误设为 Step 且无 runnable step，
	// 导致相位机空转。改道 final_answer 阶段，让模型基于已完成的 step 产出收尾报告。
	if builtin_tools.AllPlanStepsTerminal(items) {
		snapshot = a.state.SetPhase(builtin_tools.AgentPhaseFinalAnswer)
		a.emitter.EmitStateChange(snapshot)
		a.emitRuntimeLog("info", "all plan steps terminal after plan phase, route to final answer", snapshot, map[string]any{
			"event":      "plan_all_terminal_to_final",
			"plan_count": len(items),
		})
	}
	return nil
}

const submitPlanToolName = "submit_plan"

func (a *Agent) runPlanPhaseWithTools(ctx context.Context, iter int, runClient ai.ChatClient, input TaskPlannerPromptInput, promptBuilder PlannerPromptBuilder, requireGoalUnderstanding bool) (*builtin_tools.TaskPlannerResult, error) {
	fnTools, allowedTools := a.BuildFunctionTools(nil, builtin_tools.AgentPhasePlan)
	fnTools = append(fnTools, buildSubmitPlanFunctionTool())

	input.AvailableTools = functionToolsToAvailableInfo(fnTools)

	prompt, err := promptBuilder.BuildPrompt(input)
	if err != nil {
		return nil, fmt.Errorf("build task planner prompt failed: %w", err)
	}
	prompt.SystemAgent = a.identityEnvBlock()

	const maxSubmitRetries = 3
	const maxNoUsefulPlanRounds = 2
	submitRetries := 0
	noUsefulRounds := 0

	for round := 0; ; round++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		planCtx, planCancel := context.WithCancel(ctx)
		callResult, callErr := a.AICallProxy(planCtx, nil, iter, runClient, prompt, promptFamilyTaskPlanner, fnTools...)
		planCancel()
		if callErr != nil {
			return nil, fmt.Errorf("plan phase AICallProxy failed: %w", callErr)
		}

		// Plan 阶段必须产出结果（plan 或 direct_response），空响应是硬错误。
		// 若模型返回了文本但未调用 submit_plan，将文本视为 direct_response 回退。
		if len(callResult.ToolCalls) == 0 {
			assistantText := strings.TrimSpace(callResult.AssistantText)
			if assistantText == "" {
				return nil, fmt.Errorf("planner produced no plan and no tool calls")
			}
			a.emitRuntimeLog("warning", "plan phase fell back to assistant text without submit_plan", a.state.Snapshot(), map[string]any{
				"event":        "plan_phase_text_fallback",
				"round":        round,
				"content_size": len(assistantText),
			})
			return &builtin_tools.TaskPlannerResult{
				NeedsPlanning:  false,
				Plan:           nil,
				Explanation:    "模型直接回复，未调用 submit_plan",
				DirectResponse: assistantText,
			}, nil
		}

		anyUsefulTool := false
		for _, tc := range callResult.ToolCalls {
			if ctx.Err() != nil {
				break
			}
			if tc == nil || tc.Function == nil {
				continue
			}
			if tc.Function.Name == submitPlanToolName {
				priorSnap := a.state.Snapshot()
				parsed, parseErr := parseSubmitPlanArgs(tc.Function.Arguments, requireGoalUnderstanding, priorSnap.Phases, priorSnap.Plan)
				if parseErr != nil {
					submitRetries++
					if submitRetries > maxSubmitRetries {
						return nil, fmt.Errorf("submit_plan failed after %d retries: %w", maxSubmitRetries, parseErr)
					}
					a.AICallProxyWriteToolResult(nil,
						strings.TrimSpace(tc.Id), submitPlanToolName,
						"", nil, "",
						fmt.Sprintf("submit_plan 参数校验失败（第 %d/%d 次重试）：%s", submitRetries, maxSubmitRetries, parseErr.Error()),
						false,
					)
					anyUsefulTool = true
					continue
				}
				if parsed.NeedsPlanning && len(parsed.Plan) > 0 {
					// 校验对象须与调度侧（本文件 runPlanPhase 内 mergeReplannedPlan + NormalizePlanItems）
					// 保持一致：replan 回流须先按 ReplacePending 合并旧 plan 再校验，否则
					// mergeReplannedPlan 丢弃旧 pending 项现造的悬空 depends_on 只会在调度侧终态崩、
					// 绕过本重试通道（参见 step-35 unknown dependency 事故）。非 replan 时
					// validateTarget == parsed.Plan，行为与合并前一致。
					validateTarget := parsed.Plan
					if priorSnap.ReplanContext != nil && priorSnap.ReplanContext.ReplacePending {
						validateTarget = mergeReplannedPlan(priorSnap.Plan, parsed.Plan)
					}
					if _, normErr := builtin_tools.NormalizePlanItems(validateTarget, true); normErr != nil {
						submitRetries++
						if submitRetries > maxSubmitRetries {
							return nil, fmt.Errorf("submit_plan plan validation failed after %d retries: %w", maxSubmitRetries, normErr)
						}
						a.AICallProxyWriteToolResult(nil,
							strings.TrimSpace(tc.Id), submitPlanToolName,
							"", nil, "",
							fmt.Sprintf("submit_plan plan 结构校验失败（第 %d/%d 次重试）：%s\n请按报错指示修正 plan 结构（含 step_id 唯一性、depends_on 引用闭包等）后重新调用 submit_plan。", submitRetries, maxSubmitRetries, normErr.Error()),
							false,
						)
						anyUsefulTool = true
						continue
					}
					// 粒度机械校验（P3 最小化兜底，与 prompt 侧 Atomic Step Contract 同口径）：
					// step 文案承载多句堆叠（中文分号 `；` 出现）或超长（runes > planItemStepMaxRunes）
					// 时几乎必然包含多个 object/action/acceptance——绕开 LLM 自检直接 reject 让其拆。
					// 失败时同样走 retry 通道。
					if granErr := validatePlanItemsGranularity(parsed.Plan); granErr != nil {
						submitRetries++
						if submitRetries > maxSubmitRetries {
							// 降级放行：粒度是质量门（不是正确性门），3 次仍不收敛时不应
							// 把整条 run 判死——下游 step 阶段本就承担动态拆条。emit warning
							// 并在 Explanation 末尾打降级标记，便于 step 阶段 / 复盘可见。
							violationIDs := collectGranularityViolationIDs(parsed.Plan)
							a.emitRuntimeLog("warning", "submit_plan granularity check did not converge after retries; degrading to accept", a.state.Snapshot(), map[string]any{
								"event":           "submit_plan_granularity_degraded",
								"round":           round,
								"violation_ids":   violationIDs,
								"violation_count": len(violationIDs),
								"last_error":      granErr.Error(),
							})
							degradeMark := fmt.Sprintf("[runtime] 粒度校验 %d 次未收敛（违例 step: %s），已按降级策略放行；建议执行阶段按账本机械拆条。", maxSubmitRetries, strings.Join(violationIDs, ", "))
							parsed.Explanation = strings.TrimRight(parsed.Explanation, " \t\n")
							if parsed.Explanation != "" {
								parsed.Explanation += "\n"
							}
							parsed.Explanation += degradeMark
							// fall through 让后续子 Agent 完成性 + 输入事实闸门继续生效
						} else {
							a.AICallProxyWriteToolResult(nil,
								strings.TrimSpace(tc.Id), submitPlanToolName,
								"", nil, "",
								fmt.Sprintf("submit_plan 粒度校验失败（第 %d/%d 次重试）：%s", submitRetries, maxSubmitRetries, granErr.Error()),
								false,
							)
							anyUsefulTool = true
							continue
						}
					}
				}
				// 子 Agent 完成性守卫：规划期委派的子 Agent 全部结束并归并产出后才允许
				// submit_plan（「规划期委派」契约的机械兜底；step 相位由 ChildAgentChecker
				// 承担同职责）。超限后由 runtime 代为等待（与 runStepPhase A4 安全网同型，
				// 不丢子 Agent 产出），归并缺失降级记 warning。
				if a.asyncRegistry != nil && a.asyncRegistry.HasRunning() {
					running := a.runningChildAgentNames()
					submitRetries++
					if submitRetries <= maxSubmitRetries {
						a.AICallProxyWriteToolResult(nil,
							strings.TrimSpace(tc.Id), submitPlanToolName,
							"", nil, "",
							fmt.Sprintf("submit_plan 阻塞（第 %d/%d 次重试）：仍有后台子 Agent 运行中：%s。请先调用 await_subagents 等待其全部结束、把有价值产出按入板闸门归并进 `## 执行中补充` 后再 submit_plan。", submitRetries, maxSubmitRetries, strings.Join(running, ", ")),
							false,
						)
						anyUsefulTool = true
						continue
					}
					a.emitRuntimeLog("warning", "plan submitted with running sub-agents; runtime awaits on model's behalf", a.state.Snapshot(), map[string]any{
						"event":   "plan_submit_awaits_subagents",
						"round":   round,
						"running": running,
					})
					a.awaitAllBackgroundSubAgents(ctx)
				}
				// 共享区终态闸门：用户输入回合的执行计划提交前，task_context.md 的
				// `## 输入事实` 须已落盘（planning_system「共享区终态」契约的机械兜底）。
				// 超限降级为接受 + warning——闸门用于逼模型补写，事实板缺失不应使整个任务失败。
				if parsed.NeedsPlanning && input.UserInputTurn && a.workspaceRuntime != nil {
					raw := readSharedFileOptional(a.workspaceRuntime, taskContextFileName)
					if !taskContextInputFactsPresent(raw) {
						submitRetries++
						if submitRetries <= maxSubmitRetries {
							a.AICallProxyWriteToolResult(nil,
								strings.TrimSpace(tc.Id), submitPlanToolName,
								"", nil, "",
								fmt.Sprintf("submit_plan 阻塞（第 %d/%d 次重试）：共享区终态未成立：task_context.md 的 `## 输入事实` 为空。请把用户输入中确定的具体操作事实逐条写入该节（每行 `- 名称: 值`）后重新调用 submit_plan。", submitRetries, maxSubmitRetries),
								false,
							)
							anyUsefulTool = true
							continue
						}
						a.emitRuntimeLog("warning", "task_context input facts still missing after submit retries", a.state.Snapshot(), map[string]any{
							"event": "plan_submit_input_facts_missing",
							"round": round,
						})
					}
				}
				return parsed, nil
			}
			if _, ok := allowedTools[strings.TrimSpace(tc.Function.Name)]; ok {
				anyUsefulTool = true
				if err := a.executeToolCall(ctx, nil, iter, tc, allowedTools); err != nil {
					return nil, err
				}
			} else {
				a.AICallProxyWriteToolResult(nil, strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name), "", nil, "",
					fmt.Sprintf("工具 %q 在当前 plan 阶段不可用。本阶段可用工具：%s。若已具备规划所需信息，请直接调用 submit_plan 提交计划。",
						strings.TrimSpace(tc.Function.Name), strings.Join(sortedToolNames(allowedTools, submitPlanToolName), ", ")),
					false)
			}
		}
		if !anyUsefulTool {
			noUsefulRounds++
			if noUsefulRounds > maxNoUsefulPlanRounds {
				return nil, fmt.Errorf("planner produced no plan and no usable tool calls")
			}
		}
	}
}

func functionToolsToAvailableInfo(fnTools []*ai.FunctionTool) []AvailableToolInfo {
	infos := make([]AvailableToolInfo, 0, len(fnTools))
	for _, ft := range fnTools {
		if ft == nil || ft.Function == nil {
			continue
		}
		name := strings.TrimSpace(ft.Function.Name)
		if name == "" {
			continue
		}
		infos = append(infos, AvailableToolInfo{
			Name:        name,
			Description: strings.TrimSpace(ft.Function.Description),
		})
	}
	return infos
}

// sortedToolNames returns a stable, de-duplicated list of tool names from the
// allowed set plus any extra names, for use in operator-facing feedback.
func sortedToolNames(allowed map[string]struct{}, extra ...string) []string {
	seen := make(map[string]struct{}, len(allowed)+len(extra))
	names := make([]string, 0, len(allowed)+len(extra))
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}
	for n := range allowed {
		add(n)
	}
	for _, n := range extra {
		add(n)
	}
	sort.Strings(names)
	return names
}

// buildSubmitPlanFunctionTool 的 description 只描述工具职责本身；用户输入回合下的事实板维护
// 前置约束由 task_planner system prompt 的 USER_INPUT_TURN 守卫段（# 共享区终态）承载，不在
// 工具 description 重复——避免约束跨 system / user / tool 三层耦合。
func buildSubmitPlanFunctionTool() *ai.FunctionTool {
	return &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        submitPlanToolName,
			Description: "当你完成调查、准备好输出执行计划时，调用此工具提交计划。参数即为计划的结构化内容。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"needs_planning", "plan", "explanation"},
				"properties": map[string]any{
					"needs_planning": map[string]any{
						"type":        "boolean",
						"description": "是否需要执行计划。true=需多步骤规划并输出 plan；false=简单问答，仅填 direct_response。",
					},
					"plan": map[string]any{
						"type":        "array",
						"minItems":    1,
						"description": "执行计划步骤列表。needs_planning=true 时必填且非空；须承接已有 <TASK_ITEMS>/<EXECUTION_LINE>，不得无视既有完成项从零改写。minItems=1 是 schema 级硬约束，传空数组 [] 会被 function-call 校验直接拒绝。needs_planning=false 时本字段可省略（function-call 协议层允许 required 字段缺省，由 runtime 走 direct_response 分支）。",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"id", "step", "status", "phase_id", "depends_on"},
							"properties": map[string]any{
								"id":   map[string]any{"type": "string", "description": "步骤唯一标识，不得为空或重复。"},
								"step": map[string]any{"type": "string", "description": "一条 step 必须是 atomic work item：object × action × acceptance。object 是一个具体执行对象（文件、接口、参数、页面、账户等对象标识）；action 是唯一动作维度（枚举、观测、验证某项属性、生成报告等）；acceptance 是一个可独立验收的产出或结论。规划时先列 objects，再列 actions，最后生成三元组；任一维度不同就拆成不同 step。清单是数据流：生成清单可作为一个 step，消费清单时必须展开清单内 objects，清单文件名本身不是批量执行对象。机械兜底门：单条 step 文案 ≤120 字符，且不得出现中文分号「；」（多句堆叠强信号）；超限或含分号会被 runtime 拒绝并回写要求拆条重试。不得为空，不得出现 <SKILLS_INDEX>/<MCP_SERVERS> 中的名称。"},
								"status": map[string]any{
									"type":        "string",
									"enum":        []string{"pending", "in_progress", "completed", "failed"},
									"description": "步骤状态。新规划步骤填 pending；承接已完成步骤时保留其原状态。",
								},
								"phase_id": map[string]any{
									"type":        "string",
									"description": "所属业务 lane 的 phase id，必须引用 phases 中的有效条目（或承接既有 completed/blocked phase 的 id）。",
								},
								"depends_on": map[string]any{
									"type":        "array",
									"items":       map[string]any{"type": "string"},
									"description": "前置依赖的步骤 id 列表；不得引用无效 id 或形成循环依赖。",
								},
							},
						},
					},
					"phases": map[string]any{
						"type":        "array",
						"description": "业务 lane 清单。needs_planning=true 且 simple=false 时必填且非空。一个 phase 是一个最小的、可从浅到深推进的可闭环切面（纵向深度层在 phase 内以 step 推进，不拆成多个 phase）；互不依赖的 phase 会被并发调度，仅当一个 phase 的启动确实需要另一个 phase 的产出时才写 depends_on——深度递进链应留在同一 phase 内，不要拆成串行 phase 链。重规划回合按 id 承接既有 phases：completed/blocked 项由 runtime 保留，取消一个 lane 只能显式提交其 status=blocked，不得静默省略。",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"id", "name", "depends_on"},
							"properties": map[string]any{
								"id":   map[string]any{"type": "string", "description": "phase 稳定标识，不得为空或重复，不得使用 runtime 保留 id「phase-synthetic」。"},
								"name": map[string]any{"type": "string", "description": "lane 语义描述，格式「<对象> 的 <深度推进目标>」，内联具体对象/工件名，禁泛化范畴词。"},
								"depends_on": map[string]any{
									"type":        "array",
									"items":       map[string]any{"type": "string"},
									"description": "前置依赖的 phase id 列表；被依赖 phase 全部收束（completed/blocked）前本 lane 的 step 不会释放。不得引用无效 id 或成环。",
								},
								"status": map[string]any{
									"type":        "string",
									"enum":        []string{"pending", "completed", "blocked"},
									"description": "lane 状态。新建 lane 填 pending 或省略；承接既有 lane 保留其状态；取消 lane 显式填 blocked。",
								},
							},
						},
					},
					"explanation": map[string]any{
						"type":        "string",
						"description": "用 1-2 句话说明规划判断依据，不复述全部步骤。",
					},
					"goal_understanding": map[string]any{
						"type":        "string",
						"description": "对用户输入的结构化复述；按七要素小标题覆盖：核心目标 / 范围边界 / 约束 / 交付物与验收标准 / 显式聚焦 / 隐含需求与假设 / 未决歧义。needs_planning=true 时必填。",
					},
					"simple": map[string]any{
						"type":        "boolean",
						"description": "可选：简单任务标记。needs_planning=true 且计划为单步、该步完成即可交付时置 true；该步完成后将跳过三轴判定直达验收（验收仍保留回流兜底）。多步计划或开放式诉求不要置 true。",
					},
					"direct_response": map[string]any{
						"type":        "string",
						"description": "当 needs_planning=false 时，直接输出对用户的完整回复；此字段仅在 needs_planning=false 时必填。",
					},
					"summary": map[string]any{
						"type":        "string",
						"description": "调查阶段的简要总结。如果你在提交 plan 前使用了工具进行调查，请在此总结调查发现，后续执行步骤将获得此上下文。",
					},
					"tool_calls_digest": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "工具调用摘要。格式：[工具名] 参数摘要 → 结果要点。如果你使用了工具，请填写此字段。",
					},
					"key_facts": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "调查过程中发现的关键事实（文件路径、架构模式、技术栈、关键函数等）。",
					},
				},
			},
		},
	}
}

// planItemStepMaxRunes 是单条 step 文案的字符上限——超过则机械判定为可能塞入多个 atomic work items。
// 经验值：120 字符足够承载"动词 + 单工件 + 单动作维度 + 单产出 + 验收"标准句式（30-80 字），>120 一般有多句。
const planItemStepMaxRunes = 120

// planItemStepExcerptRunes 是 retry 反馈里携带违例 step 文案摘要的 rune 截断上限。
// 60 字足够定位违例点（首句通常含核心 object/action），又控制反馈面长度。
const planItemStepExcerptRunes = 60

// stepTextExcerpt 取 step 文案前 max 个 rune，超出加 `…` 截断标记——用于 retry 反馈定位。
func stepTextExcerpt(step string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(step)
	if len(runes) <= max {
		return step
	}
	return string(runes[:max]) + "…"
}

// collectGranularityViolationIDs 列出违反 validatePlanItemsGranularity 任一规则的 step ID，
// 供降级放行分支记录到 runtime warning 与 Explanation 降级标记中。
func collectGranularityViolationIDs(items []*builtin_tools.PlanItem) []string {
	var ids []string
	for _, item := range items {
		if item == nil {
			continue
		}
		step := strings.TrimSpace(item.Step)
		if step == "" {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = "<unnamed>"
		}
		if strings.Contains(step, "；") {
			ids = append(ids, id)
			continue
		}
		if utf8.RuneCountInString(step) > planItemStepMaxRunes {
			ids = append(ids, id)
		}
	}
	return ids
}

// validatePlanItemsGranularity 对 plan items 做两条机械粒度兜底检查（与 Atomic Step Contract 同口径）：
//  1. step 文案 runes 数 ≤ planItemStepMaxRunes
//  2. step 文案不含中文分号 `；`（多句堆叠的强信号）
//
// 遍历完整 plan，把所有违例聚合到一条 error 返回，并携带每条违例 step 的文案摘要——
// 单次 retry 反馈即可让 LLM 一次整改完所有违例，避免「修对一条又留另一条」的死循环。
// 校验失败由调用方走 submit_plan 的 retry 通道；3 次 retry 用尽后由 runtime 走降级放行
// （粒度是质量门而非正确性门，不应让 8 小时 / 上亿 token 长跑被一条经验门判死）。
// 这是对 prompt 侧自检（A-M + N0-N5 + P1/P2 计 20+ 条规则）的机械兜底——LLM 自觉失败时的最后一刀。
func validatePlanItemsGranularity(items []*builtin_tools.PlanItem) error {
	var violations []string
	for _, item := range items {
		if item == nil {
			continue
		}
		step := strings.TrimSpace(item.Step)
		if step == "" {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = "<unnamed>"
		}
		excerpt := stepTextExcerpt(step, planItemStepExcerptRunes)
		if strings.Contains(step, "；") {
			violations = append(violations, fmt.Sprintf(
				"step %q 文案包含中文分号 `；`（多句堆叠信号）——摘要：「%s」",
				id, excerpt,
			))
			continue
		}
		if runeCount := utf8.RuneCountInString(step); runeCount > planItemStepMaxRunes {
			violations = append(violations, fmt.Sprintf(
				"step %q 文案长度 %d > %d 字符上限——摘要：「%s」",
				id, runeCount, planItemStepMaxRunes, excerpt,
			))
		}
	}
	if len(violations) == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "submit_plan 粒度校验共 %d 条违例（超长 / 多句堆叠几乎必然混合多个 object × action × acceptance）：", len(violations))
	for i, v := range violations {
		fmt.Fprintf(&b, "\n[%d] %s", i+1, v)
	}
	fmt.Fprintf(&b, "\n请把上述 step 按 object/action/acceptance 拆为多条 atomic step（单条 ≤%d 字符、不含 `；`）后重新调用 submit_plan。", planItemStepMaxRunes)
	return errors.New(b.String())
}

func parseSubmitPlanArgs(args any, requireGoalUnderstanding bool, priorPhases []*builtin_tools.PlanPhase, priorPlan []*builtin_tools.PlanItem) (*builtin_tools.TaskPlannerResult, error) {
	var data []byte
	switch v := args.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("submit_plan: marshal args failed: %w", err)
		}
	}
	var result builtin_tools.TaskPlannerResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("submit_plan: parse args failed: %w", err)
	}
	if result.NeedsPlanning && len(result.Plan) == 0 {
		return nil, fmt.Errorf(
			"submit_plan: needs_planning=true 但 plan 为空。\n%s\n"+
				"请根据任务实际状态二选一修正：补全 plan 字段走「需要规划」分支"+
				"（遵循 Atomic Step Contract：单 step 单 object 单 action 单 acceptance）；"+
				"或把 needs_planning 改为 false 并把对用户的完整答复写入 direct_response 走「不需要规划」分支\n%s",
			submitPlanShapeReminder, submitPlanItemSample)
	}
	if requireGoalUnderstanding && result.NeedsPlanning && strings.TrimSpace(result.GoalUnderstanding) == "" {
		return nil, fmt.Errorf("submit_plan: needs_planning=true 但 goal_understanding 为空。" +
			"请先按七要素结构化复述输入（核心目标 / 范围边界 / 约束 / 交付物与验收标准 / 显式聚焦 / 隐含需求与假设 / 未决歧义），" +
			"填入 goal_understanding 字段后重新调用")
	}
	if !result.NeedsPlanning && strings.TrimSpace(result.DirectResponse) == "" {
		return nil, fmt.Errorf(
			"submit_plan: needs_planning=false 但 direct_response 为空。\n%s\n"+
				"请根据任务实际状态二选一修正：补全 direct_response 字段（对用户的完整答复）"+
				"走「不需要规划」分支；或把 needs_planning 改为 true 并把计划步骤写入 plan "+
				"走「需要规划」分支\n%s",
			submitPlanShapeReminder, submitPlanItemSample)
	}

	// Phases（业务 lane）校验与合并：
	//  1. needs_planning 且非 simple 时 phases 必填；
	//  2. 归一化（id 唯一 / 引用闭包 / 无环 / 禁 synthetic 保留 id）；
	//  3. 与既有 phases 按 id 合并——completed/blocked 项被省略时保留（取消 lane 只能
	//     显式提交 blocked，见 D7b）；
	//  4. plan item 的 phase_id 必须在合并后的 phase 集内（防悬空引用被错挂 synthetic）。
	if result.NeedsPlanning {
		normalized, err := builtin_tools.NormalizePlanPhases(result.Phases, true)
		if err != nil {
			return nil, fmt.Errorf("submit_plan: phases 校验失败：%w。请修正 phases 结构（id 唯一、depends_on 引用有效且无环）后重新调用", err)
		}
		if !result.Simple && len(normalized) == 0 {
			return nil, fmt.Errorf("submit_plan: needs_planning=true 且非 simple 任务时 phases 必填。" +
				"请提交业务 lane 清单（每项 {id, name, depends_on}，name 格式「<对象> 的 <深度推进目标>」），" +
				"并给每个 plan item 填 phase_id 后重新调用")
		}
		merged := mergePlanPhases(priorPhases, normalized, priorPlan)
		if len(merged) > 0 {
			known := make(map[string]struct{}, len(merged))
			for _, phase := range merged {
				known[phase.ID] = struct{}{}
			}
			var dangling []string
			for _, item := range result.Plan {
				if item == nil {
					continue
				}
				phaseID := canonicalizePlanPhaseRef(item.PhaseID)
				if phaseID == "" {
					dangling = append(dangling, fmt.Sprintf("step %q 缺 phase_id", strings.TrimSpace(item.ID)))
					continue
				}
				if _, ok := known[phaseID]; !ok {
					dangling = append(dangling, fmt.Sprintf("step %q 引用未知 phase %q", strings.TrimSpace(item.ID), phaseID))
				}
			}
			if len(dangling) > 0 {
				return nil, fmt.Errorf("submit_plan: plan 与 phases 引用不闭合：%s。"+
					"每个 plan item 的 phase_id 必须引用 phases 中的有效条目（或承接既有 completed/blocked phase 的 id）；"+
					"取消 lane 请显式提交该 phase 为 blocked，不要静默省略", strings.Join(dangling, "；"))
			}
		}
		result.Phases = merged
	}
	return &result, nil
}

// canonicalizePlanPhaseRef 与 NormalizePlanPhases 的 id 规整同源，供 phase_id 引用对齐。
func canonicalizePlanPhaseRef(raw string) string {
	return builtin_tools.CanonicalizePlanIDToken(raw)
}

// mergePlanPhases 把 planner 本轮提交的 phases 与既有 phases 按 id 合并：
// 提交项以 planner 为准（承接/重开/显式 blocked 均由其表达）；被省略的既有 phase
// 在两种情况下保留（与 mergeReplannedPlan 保留 non-pending step 同型，避免保留 step 的
// phase_id 悬空被错挂 synthetic）：
//   - completed/blocked 的 lane（依赖锚点与展示痕迹）；
//   - 被 priorPlan 中任一 non-pending（terminal）step 引用的 lane（该 step 会被
//     mergeReplannedPlan 保留，其 phase 归属必须一并保留）。
//
// 被省略且无 terminal step 引用的既有 pending lane 不保留——其下 pending step 若仍存在
// 会因引用不闭合被 parse 拒绝，倒逼 planner 显式表达。
func mergePlanPhases(prev []*builtin_tools.PlanPhase, next []*builtin_tools.PlanPhase, priorPlan []*builtin_tools.PlanItem) []*builtin_tools.PlanPhase {
	if len(prev) == 0 {
		return next
	}
	if len(next) == 0 {
		return builtin_tools.ClonePlanPhases(prev)
	}
	submitted := make(map[string]struct{}, len(next))
	for _, phase := range next {
		if phase != nil {
			submitted[strings.TrimSpace(phase.ID)] = struct{}{}
		}
	}
	// 收集被 terminal step 引用的 phase id——这些 step 会被 mergeReplannedPlan 保留。
	referencedByTerminal := make(map[string]struct{})
	for _, item := range priorPlan {
		if item == nil || item.Status == builtin_tools.PlanStepPending {
			continue
		}
		if id := strings.TrimSpace(item.PhaseID); id != "" {
			referencedByTerminal[id] = struct{}{}
		}
	}
	merged := make([]*builtin_tools.PlanPhase, 0, len(prev)+len(next))
	for _, phase := range prev {
		if phase == nil {
			continue
		}
		id := strings.TrimSpace(phase.ID)
		if _, ok := submitted[id]; ok {
			continue
		}
		_, refd := referencedByTerminal[id]
		if !phase.Terminal() && !refd {
			continue
		}
		clone := *phase
		clone.DependsOn = builtin_tools.CloneStringSlice(phase.DependsOn)
		merged = append(merged, &clone)
	}
	merged = append(merged, next...)
	return merged
}

// submitPlanShapeReminder 列出 submit_plan 的两种合法形态。
// 校验失败时随 error 文本带回给 LLM,目的是让模型先看到「合法形态」全貌、
// 再根据本次错误自决回到哪条分支,避免在 needs_planning=true/false 之间反复翻转死锁。
const submitPlanShapeReminder = "" +
	"submit_plan 的两种合法形态：\n" +
	"- 需要规划：needs_planning=true，plan 写入计划步骤列表\n" +
	"- 不需要规划：needs_planning=false，direct_response 写入对用户的完整答复"

// submitPlanItemSample 给 LLM 一个 plan item 的最小可对照样板，避免「只告知错了
// 没告知正确长啥样」导致 AI 3 次重试都修不好同类错误。校验失败时随 error 一起回写
// 让模型能直接 copy 结构、按本次任务填具体字段。step 字段示例刻意保留具体性
// （读取 → 确认 → 字段），避免模型从抽象描述里反向编造结构。
const submitPlanItemSample = "" +
	"plan item 最小结构样板（按当前任务调整 id/step 内容，status 新规划填 \"pending\"）：\n" +
	"\"plan\": [\n" +
	"  {\"id\":\"s1\",\"step\":\"读取 config.yaml 的 server.port 字段并确认当前值\",\"status\":\"pending\",\"depends_on\":[]},\n" +
	"  {\"id\":\"s2\",\"step\":\"在 main.go 注册新 /healthz handler 并返回 200\",\"status\":\"pending\",\"depends_on\":[\"s1\"]}\n" +
	"]"

type PlannerInputOptions struct {
	HandoffContext     string
	WorkspaceRootDir   string
	WorkspaceNamespace string
	// RecoveryContextJSON 仅在恢复回合且命中 gate 时非空，渲染为 planner prompt 的独立 RECOVERY 段。
	RecoveryContextJSON string
	// 以下为 PromptContext preview 注入（M2 接线）：非空时直接采用（已经动态上限
	// 截断 + 指针），空时按 snapshot 原地构建（兼容未接线调用方与既有测试）。
	InputTimeline     string
	TaskItemsJSON     string
	PhasesJSON        string
	ReplanContextJSON string
}

type plannerStepOutcomeView struct {
	StepID        string   `json:"step_id,omitempty"`
	Status        string   `json:"status,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	ShortSummary  string   `json:"short_summary,omitempty"`
	LongSummary   string   `json:"long_summary,omitempty"`
	KeyFacts      []string `json:"key_facts,omitempty"`
	OpenQuestions []string `json:"open_questions,omitempty"`
	References    []string `json:"references,omitempty"`
	SummaryFile   string   `json:"summary_file,omitempty"`
	ResultFile    string   `json:"result_file,omitempty"`
	TimelineFile  string   `json:"timeline_file,omitempty"`
	ContextKey    string   `json:"context_key,omitempty"`
}

type plannerStepContextView struct {
	ContextKey           string   `json:"context_key,omitempty"`
	Namespace            string   `json:"namespace,omitempty"`
	StepID               string   `json:"step_id,omitempty"`
	PlanVersion          int      `json:"plan_version,omitempty"`
	AgentProfile         string   `json:"agent_profile,omitempty"`
	ShortSummary         string   `json:"short_summary,omitempty"`
	KeyFacts             []string `json:"key_facts,omitempty"`
	ResultKeys           []string `json:"result_keys,omitempty"`
	SummaryFile          string   `json:"summary_file,omitempty"`
	ResultFile           string   `json:"result_file,omitempty"`
	TimelineFile         string   `json:"timeline_file,omitempty"`
	References           []string `json:"references,omitempty"`
	InheritedContextKeys []string `json:"inherited_context_keys,omitempty"`
}

func PlannerInputFromSnapshot(snapshot builtin_tools.StateSnapshot, opts PlannerInputOptions) string {
	if len(snapshot.InputTimeline) == 0 {
		return ""
	}

	opts.HandoffContext = strings.TrimSpace(opts.HandoffContext)
	opts.WorkspaceRootDir = strings.TrimSpace(opts.WorkspaceRootDir)
	opts.WorkspaceNamespace = strings.TrimSpace(opts.WorkspaceNamespace)

	data := plannerInputData{
		HandoffContext:      opts.HandoffContext,
		RecoveryContextJSON: strings.TrimSpace(opts.RecoveryContextJSON),
	}

	// Build INPUT_TIMELINE：优先采用 PromptContext preview（已动态上限截断 + 指针），
	// 未接线调用方回退按 snapshot 原地拼接（行格式同 formatInputTimelineLines）。
	if opts.InputTimeline != "" {
		data.InputTimeline = opts.InputTimeline
	} else {
		data.InputTimeline = formatInputTimelineLines(snapshot.InputTimeline)
	}
	if data.InputTimeline == "" {
		return ""
	}

	// TASK_ITEMS：plan 真相源投影（烘焙产出小字段 + 指针，指针转绝对路径；slim 投影
	// 去 digest——planner 按需顺 timeline_file / journal 回读）。
	// 取代旧的 EXECUTION_LINE / WORKSPACE_STEP_CONTEXTS 全量注入（copy→pointer）。
	if opts.TaskItemsJSON != "" {
		data.TaskItemsJSON = opts.TaskItemsJSON
	} else if len(snapshot.Plan) > 0 {
		data.TaskItemsJSON = prettyJSON(ProjectPlanItemCardsSlim(snapshot.Plan, opts.WorkspaceRootDir))
	}

	// PHASES：既有业务 lane 清单（含状态），重规划回合供 planner 承接。
	if opts.PhasesJSON != "" {
		data.PhasesJSON = opts.PhasesJSON
	} else if len(snapshot.Phases) > 0 {
		data.PhasesJSON = prettyJSON(snapshot.Phases)
	}

	// planner.jsonl：plan 唯一真相源的按需回读指针（文件存在才注入；helper 内置 stat 与 size>0 判定）。
	data.PlannerJournalPath = resolvePlannerJournalPointer(opts.WorkspaceRootDir)

	// REPLAN_CONTEXT
	if opts.ReplanContextJSON != "" {
		data.ReplanContextJSON = opts.ReplanContextJSON
	} else if snapshot.ReplanContext != nil {
		data.ReplanContextJSON = prettyJSON(snapshot.ReplanContext)
	}

	var buf strings.Builder
	if err := plannerInputTmpl.Execute(&buf, data); err != nil {
		// Fallback: should never happen with a valid template
		return ""
	}
	return buf.String()
}

func mergeReplannedPlan(prev []*builtin_tools.PlanItem, next []*builtin_tools.PlanItem) []*builtin_tools.PlanItem {
	if len(prev) == 0 || len(next) == 0 {
		return next
	}
	merged := make([]*builtin_tools.PlanItem, 0, len(prev)+len(next))
	preserved := make(map[string]struct{}, len(prev))
	// preservedText: 保留项 normalizeStepText → 保留项 id。用于把「因文案撞车被去重
	// 丢弃的 next 项」的依赖重指到这个同文案保留项（见下方 dropped/remap），从源头
	// 消灭 merge 现造的悬空依赖。首个占位者胜出（与原「存在即去重」语义一致）。
	preservedText := make(map[string]string, len(prev))
	for _, item := range prev {
		// 所有 non-pending 项（completed / in_progress / failed / skipped）保留为依赖锚点
		// 与烘焙载体；pending 由 next 完整替换。失败 / 跳过项一并保留可避免：
		// ①烘焙字段（step_file / timeline_file / coverage_file / references）丢失；
		// ②planner.jsonl 全量重写后历史痕迹消失；③模型新 plan 用同 id 复活但 BakeOutcome 清零。
		if item == nil || item.Status == builtin_tools.PlanStepPending {
			continue
		}
		// 完整浅拷贝 + 切片字段克隆，保留 BakeOutcome 写回的烘焙字段（short_summary /
		// key_facts / tool_calls_digest / coverage_checklist / step_file / result_file /
		// timeline_file / coverage_file / references），避免直达 Step 重编排时把
		// completed 项重置为只有 id/step/status/depends_on 的裸 PlanItem。
		clone := *item
		clone.ID = strings.TrimSpace(item.ID)
		clone.Step = strings.TrimSpace(item.Step)
		clone.DependsOn = builtin_tools.CloneStringSlice(item.DependsOn)
		clone.KeyFacts = builtin_tools.CloneStringSlice(item.KeyFacts)
		clone.ToolCallsDigest = builtin_tools.CloneStringSlice(item.ToolCallsDigest)
		clone.References = builtin_tools.CloneStringSlice(item.References)
		clone.ResolvedDependsOn = nil
		if len(item.CoverageChecklist) > 0 {
			clone.CoverageChecklist = append([]builtin_tools.CoverageChecklistItem(nil), item.CoverageChecklist...)
		}
		merged = append(merged, &clone)
		if clone.ID != "" {
			preserved[clone.ID] = struct{}{}
		}
		if norm := normalizeStepText(clone.Step); norm != "" {
			if _, ok := preservedText[norm]; !ok {
				preservedText[norm] = clone.ID
			}
		}
	}
	// dropped: 被文案去重丢弃的 next 项 canonical id → 保留的同文案项 id。
	// 用于把 depends_on 里指向「被丢弃 next 项」的引用重指到语义等价的保留项，
	// 避免 mergeReplannedPlan 现造悬空依赖（NormalizePlanItems 随后仍兜底校验残余悬空）。
	var dropped map[string]string
	for _, item := range next {
		if item == nil {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id != "" {
			// id 撞保留项：同 id 保留项已在 merged，指向它的依赖仍能解析，无需重指。
			if _, exists := preserved[id]; exists {
				continue
			}
		}
		if norm := normalizeStepText(item.Step); norm != "" {
			if keepID, exists := preservedText[norm]; exists {
				// 文案撞保留项被丢弃：记录 next.id → 保留项 id 的依赖重指。
				if canon := builtin_tools.CanonicalizePlanIDToken(id); canon != "" && keepID != "" {
					if dropped == nil {
						dropped = make(map[string]string)
					}
					dropped[canon] = keepID
				}
				continue
			}
		}
		// next 项浅拷贝 + DependsOn 克隆后入板，保持 mergeReplannedPlan 纯函数语义
		// （下方 remap 会改写 DependsOn，不能 mutate 调用方传入的 next 项）。
		clone := *item
		clone.ID = id
		clone.DependsOn = builtin_tools.CloneStringSlice(item.DependsOn)
		merged = append(merged, &clone)
	}
	// 依赖重指：把 merged 全体指向「被去重丢弃 id」的 depends_on 改指到保留的同文案项。
	for _, item := range merged {
		if item == nil || len(item.DependsOn) == 0 || len(dropped) == 0 {
			continue
		}
		for i, dep := range item.DependsOn {
			if keepID, ok := dropped[builtin_tools.CanonicalizePlanIDToken(strings.TrimSpace(dep))]; ok {
				item.DependsOn[i] = keepID
			}
		}
	}
	if len(merged) == 0 {
		return next
	}
	return merged
}

var stepTextNormalizer = strings.NewReplacer(
	"：", ":", "（", "(", "）", ")", "，", ",",
	"。", ".", "；", ";",
)

func normalizeStepText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	s = stepTextNormalizer.Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func truncateByRunes(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if maxRunes <= 0 || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:maxRunes])) + "..."
}

func cloneAndTruncateStrings(items []string, maxItems int, maxRunesPerItem int) []string {
	if len(items) == 0 || maxItems == 0 {
		return nil
	}
	if maxItems < 0 {
		maxItems = len(items)
	}
	out := make([]string, 0, min(len(items), maxItems))
	for _, it := range items {
		if len(out) >= maxItems {
			break
		}
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		out = append(out, truncateByRunes(it, maxRunesPerItem))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (a *Agent) extractPlanToolCallsDigest() []string {
	if a == nil || len(a.stepHistory) == 0 {
		return nil
	}
	var digest []string
	seen := make(map[string]struct{})
	for _, msg := range a.stepHistory {
		if msg == nil || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc == nil || tc.Function == nil {
				continue
			}
			name := strings.TrimSpace(tc.Function.Name)
			if name == "" || name == submitPlanToolName {
				continue
			}
			argSummary := truncatePlanToolArgs(tc.Function.Arguments)
			entry := fmt.Sprintf("[%s] %s", name, argSummary)
			if _, ok := seen[entry]; ok {
				continue
			}
			seen[entry] = struct{}{}
			digest = append(digest, entry)
		}
	}
	return digest
}

func truncatePlanToolArgs(args any) string {
	s, ok := args.(string)
	if !ok {
		return ""
	}
	var argsMap map[string]any
	if json.Unmarshal([]byte(s), &argsMap) != nil {
		return truncateByRunes(s, 120)
	}
	for _, key := range []string{"path", "file", "pattern", "command", "query"} {
		if v, ok := argsMap[key]; ok {
			return fmt.Sprintf("%s=%v", key, v)
		}
	}
	return truncateByRunes(s, 120)
}

func (a *Agent) appendPlanContextRecord(res *builtin_tools.TaskPlannerResult, snapshot builtin_tools.StateSnapshot) {
	if a == nil || a.workspaceRuntime == nil || res == nil {
		return
	}
	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}
	namespace := builtin_tools.NormalizeWorkspaceNamespace(a.workspaceNamespace)
	stepID := "__plan__"
	contextKey := fmt.Sprintf("%s:%d:%s", namespace, planVersion, stepID)

	digest := builtin_tools.CloneStringSlice(res.ToolCallsDigest)
	if len(digest) == 0 {
		digest = a.extractPlanToolCallsDigest()
	}

	record := &builtin_tools.StepContextRecord{
		ContextKey:      contextKey,
		Namespace:       namespace,
		StepID:          stepID,
		PlanVersion:     planVersion,
		ShortSummary:    firstNonEmpty(strings.TrimSpace(res.Summary), strings.TrimSpace(res.Explanation)),
		KeyFacts:        builtin_tools.CloneStringSlice(res.KeyFacts),
		ToolCallsDigest: digest,
		CreatedAt:       time.Now(),
	}
	if err := a.workspaceRuntime.AppendStepContextRecords(
		[]*builtin_tools.StepContextRecord{record},
	); err != nil {
		a.emitRuntimeLog("warn", "append plan context record failed", snapshot, map[string]any{
			"event": "plan_context_append_failed",
			"error": err.Error(),
		})
	}
}

func normalizeRuntimeErrorText(err error) string {
	if err == nil {
		return ""
	}
	var exhausted *structuredoutput.ExhaustedError
	if errors.As(err, &exhausted) && exhausted != nil {
		if last := exhausted.LastAttempt(); last != nil && last.ErrorType == structuredoutput.ErrorTypeModelCallFailed && strings.TrimSpace(last.Error) != "" {
			return strings.TrimSpace(last.Error)
		}
	}
	return strings.TrimSpace(err.Error())
}

func (a *Agent) handlePhaseError(
	ctx context.Context,
	err error,
	iter, maxIterations int,
	runClient ai.ChatClient,
) (*builtin_tools.RunResult, error) {
	if tri, ok := isTurnInterruptRaised(err); ok {
		return &builtin_tools.RunResult{
			Success:          false,
			TurnID:           strings.TrimSpace(a.currentTurnID),
			TurnStatus:       string(persistv2.TurnStatusInterrupted),
			PendingInterrupt: tri.Pending(),
		}, nil
	}
	a.prepareTerminalInterrupt(err)
	_ = a.state.EnterFinalAnswer(builtin_tools.TaskStatusFailed, normalizeRuntimeErrorText(err))
	a.syncStepHistoryLayer(a.state.Snapshot())
	snapshot, faErr := a.runFinalAnswerPhase(ctx, iter, runClient)
	// Forced-failure terminal path: settle still-running sub-agent cards as
	// cancelled so they do not stay stuck on "running".
	a.cancelRunningSubAgents()
	a.cancelRunningInlineSteps()
	if faErr != nil {
		a.emitRuntimeLog("error", "final answer phase failed during error handling", snapshot, map[string]any{
			"event":          "final_answer_phase_error_in_fallback",
			"original_error": err.Error(),
			"final_error":    faErr.Error(),
		})
		a.emitter.EmitIteration(iter, maxIterations, "terminal")
		return nil, fmt.Errorf("phase error: %v; final_answer error: %w", err, faErr)
	}
	a.emitter.EmitIteration(iter, maxIterations, "terminal")
	return a.finalizeResult(snapshot), nil
}

func (a *Agent) prepareTerminalInterrupt(err error) {
	if a == nil || a.state == nil {
		return
	}
	if info := classifyExternalInterrupt(err); info != nil {
		_ = a.state.SetExternalInterrupt(info)
	}
}

func (a *Agent) runStepPhase(ctx context.Context, iter int, runClient ai.ChatClient, extraText string, taskContext *TaskContextData) error {
	_ = a.state.SetPhase(builtin_tools.AgentPhaseStep)

	// 主路径 step 完成后 CurrentStepID 仍指向已终态 step（state.go:436 注释保留）。
	// 入口前先清空，让 EnsureCurrentStep 选下个 ready 作 current。
	// degenerate case：MaxParallel=1 时也走相同代码——ResetCurrentStepIfTerminal 在终态时
	// 翻空、非终态时 no-op；不再保留 if maxParallel < 2 分叉特殊化（fallback 不变质原则，
	// 参见 plan §fallback 不变质 + [[feedback_no_atomic_ledger_tools]]）。
	_ = a.state.ResetCurrentStepIfTerminal()

	_ = a.state.EnsureCurrentStep()
	// 主路径翻 Pending→InProgress：observer 自动 emit task_item + ensureStepFileScaffold。
	// 删掉旧 prevSnapshot/prevPlan/emitTaskItemDiffs 手抓 diff 与
	// 显式 ensureStepFileScaffold 调用——参见 state_observer_emitter.go /
	// state_observer_workspace.go。
	snapshot := a.state.MarkCurrentStepInProgress()

	// Ensure the in-step transcript layer is bound to current_step_id before calling the model.
	// Otherwise the first tool transcript may be written while step id is empty, and then
	// cleared by the next sync transition.
	a.syncStepHistoryLayer(snapshot)

	currentStep := snapshot.CurrentStep()
	a.emitRuntimeLog("info", "enter step phase", snapshot, map[string]any{
		"event":        "phase_enter",
		"current_step": currentStep,
	})

	// Freeze execution lineage at step start (before prompt building).
	if _, err := a.ensureFrozenStepLineage(snapshot); err != nil {
		return err
	}
	// runStepsConcurrently 内部：
	//   1. collectInlineStepIDs(snapshot) → [current, peer1, peer2, ...]
	//   2. peers 各 spawn 后台 goroutine（spawnInlinePeer，跑 runInlineStep loop 至 terminal）
	//   3. 主路径同步跑 current 的 runInlineStep（runCtx=nil，行为同抽出前）
	// MaxParallel=1 时 peers 列表为空，仅主路径——纯串行 fallback 走相同代码路径，
	// 无 if maxParallel<2 分叉特殊化（degenerate case）。
	return a.runStepsConcurrently(ctx, runClient, iter, snapshot, extraText)
}

// executeToolCall 单条 tool_call 分发执行；runCtx 用于桶路由（同 dispatchToolCalls 注释）。
// 主路径传 nil（行为不变）；inline step 在 commit 8b 通过 runInlineStep 传真正 runCtx。
func (a *Agent) executeToolCall(ctx context.Context, runCtx *InlineStepCtx, iter int, tc *ai.FunctionTool, allowedTools map[string]struct{}) error {
	callID := strings.TrimSpace(tc.Id)
	toolName := strings.TrimSpace(tc.Function.Name)
	if toolName == "" {
		a.emitRuntimeLog("warn", "empty tool name in tool call, skipping", a.state.Snapshot(), map[string]any{
			"event":   "empty_tool_name",
			"call_id": callID,
		})
		return nil
	}
	prevSnapshot := a.state.Snapshot()
	if len(allowedTools) > 0 {
		if _, ok := allowedTools[toolName]; !ok {
			a.AICallProxyWriteToolResult(runCtx, callID, toolName, "", map[string]any{}, "", "tool not available in current phase", false)
			return nil
		}
	}

	argsMap, argErr := ParseToolArguments(tc.Function.Arguments)
	if argsMap == nil {
		argsMap = map[string]any{}
	}
	if argErr != nil {
		rawArgs := ""
		if s, ok := tc.Function.Arguments.(string); ok {
			if len(s) > 500 {
				rawArgs = s[:500] + "..."
			} else {
				rawArgs = s
			}
		}
		errMsg := fmt.Sprintf("tool args parse failed: %v\n\nThe arguments JSON you provided is malformed. Raw arguments (truncated):\n%s\n\nPlease retry the tool call with valid JSON arguments.", argErr, rawArgs)
		a.AICallProxyWriteToolResult(runCtx, callID, toolName, "", argsMap, "", errMsg, false)
		return nil
	}

	tool, exists := a.GetTool(toolName)
	if !exists || tool == nil {
		a.AICallProxyWriteToolResult(runCtx, callID, toolName, "", argsMap, "", "tool not found", false)
		return nil
	}

	isAgent := IsAgentToolForCall(ctx, tool, argsMap)
	stackDepth := 0
	if parentToolRuntime, ok := builtin_tools.GetToolRuntime(ctx); ok {
		stackDepth = parentToolRuntime.StackDepth + 1
	}
	a.emitter.EmitToolStart(iter, builtin_tools.ToolCall{
		ID:         callID,
		Name:       toolName,
		IsAgent:    isAgent,
		StackDepth: stackDepth,
		Arguments:  builtin_tools.CloneAnyMap(argsMap),
	}, effectiveStepID(runCtx, prevSnapshot))

	// Durable human-in-the-loop: raise a persisted interrupt and end the current turn.
	// This must be crash-safe and resume-safe; we snapshot the runtime state + step transcript
	// before unwinding the scheduler.
	//
	// Important: We intentionally do NOT generate a tool-result message here. The outstanding
	// tool_call_id is completed only when the user resolves the interrupt.
	if toolName == builtin_tools.HumanConfirmToolName {
		question, inputType, options, ctxMap := parseHumanConfirmArgs(argsMap)
		interruptID := "interrupt-" + uuid.NewString()

		// Persistence barrier (P0 hard requirement):
		// - runtime_state + step_history MUST be durably written (as blobs)
		// - INTERRUPT_RAISED MUST be appended with blob refs in payload
		// - snapshot must be updated successfully
		// Otherwise we must NOT enter WAITING_FOR_HUMAN, because resume would be unreliable.
		if a.v2Store == nil {
			a.emitRuntimeLog("error", "persistence store missing for human_confirm", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "human_confirm_raise",
			})
			return fmt.Errorf("persistence store is not available for human_confirm")
		}

		rawState, err := json.Marshal(a.state.Snapshot())
		if err != nil || len(rawState) == 0 {
			errText := "empty runtime_state"
			if err != nil {
				errText = err.Error()
			}
			a.emitRuntimeLog("error", "marshal runtime_state failed", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "write_blob",
				"err":    errText,
			})
			return fmt.Errorf("marshal runtime_state failed: %w", err)
		}
		runtimeRef, err := a.v2Store.WriteBlob(rawState)
		if err != nil || strings.TrimSpace(runtimeRef) == "" {
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			a.emitRuntimeLog("error", "persistence failed: write_blob(runtime_state)", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "write_blob",
				"err":    errText,
			})
			return fmt.Errorf("write runtime_state blob failed: %w", err)
		}

		rawHistory, err := json.Marshal(ai.NormalizeMsgInfoSlice(a.stepHistory))
		if err != nil || len(rawHistory) == 0 {
			errText := "empty step_history"
			if err != nil {
				errText = err.Error()
			}
			a.emitRuntimeLog("error", "marshal step_history failed", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "write_blob",
				"err":    errText,
			})
			return fmt.Errorf("marshal step_history failed: %w", err)
		}
		historyRef, err := a.v2Store.WriteBlob(rawHistory)
		if err != nil || strings.TrimSpace(historyRef) == "" {
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			a.emitRuntimeLog("error", "persistence failed: write_blob(step_history)", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "write_blob",
				"err":    errText,
			})
			return fmt.Errorf("write step_history blob failed: %w", err)
		}

		var convHistoryRef string
		if len(a.history) > 0 {
			rawConvHistory, cerr := json.Marshal(ai.NormalizeMsgInfoSlice(a.history))
			if cerr != nil || len(rawConvHistory) == 0 {
				errText := "empty conversation_history"
				if cerr != nil {
					errText = cerr.Error()
				}
				a.emitRuntimeLog("error", "marshal conversation_history failed", prevSnapshot, map[string]any{
					"kind":   "persistence",
					"action": "write_blob",
					"err":    errText,
				})
				return fmt.Errorf("marshal conversation_history failed: %w", cerr)
			}
			convHistoryRef, err = a.v2Store.WriteBlob(rawConvHistory)
			if err != nil || strings.TrimSpace(convHistoryRef) == "" {
				errText := ""
				if err != nil {
					errText = err.Error()
				}
				a.emitRuntimeLog("error", "persistence failed: write_blob(conversation_history)", prevSnapshot, map[string]any{
					"kind":   "persistence",
					"action": "write_blob",
					"err":    errText,
				})
				return fmt.Errorf("write conversation_history blob failed: %w", err)
			}
		}

		payload := map[string]any{
			"question":               question,
			"input_type":             inputType,
			"options":                options,
			"context":                ctxMap,
			"tool_call_id":           callID,
			"runtime_state_blob_ref": strings.TrimSpace(runtimeRef),
			"step_history_blob_ref":  strings.TrimSpace(historyRef),
		}
		if convHistoryRef != "" {
			payload["conversation_history_blob_ref"] = strings.TrimSpace(convHistoryRef)
		}

		ev, err := a.v2Store.AppendEvent(&persistv2.Event{
			Type:        "INTERRUPT_RAISED",
			GroupID:     strings.TrimSpace(a.currentGroupID),
			TurnID:      strings.TrimSpace(a.currentTurnID),
			InterruptID: interruptID,
			Payload:     payload,
		})
		if err != nil {
			a.emitRuntimeLog("error", "persistence failed: append_event(INTERRUPT_RAISED)", prevSnapshot, map[string]any{
				"kind":         "persistence",
				"action":       "append_event",
				"event_type":   "INTERRUPT_RAISED",
				"interrupt_id": interruptID,
				"err":          err.Error(),
			})
			return fmt.Errorf("append INTERRUPT_RAISED event failed: %w", err)
		}

		snap, lerr := a.v2Store.LoadSnapshot()
		if lerr != nil {
			a.emitRuntimeLog("error", "persistence failed: load_snapshot after interrupt raised", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "load_snapshot",
				"err":    lerr.Error(),
			})
			return fmt.Errorf("load snapshot after interrupt raised failed: %w", lerr)
		}
		if snap == nil {
			return fmt.Errorf("snapshot is nil after interrupt raised")
		}
		if rerr := persistv2.ReduceSnapshot(snap, ev); rerr != nil {
			return fmt.Errorf("reduce snapshot failed: %w", rerr)
		}
		// Redundant after reducer fix — kept as defensive double-write.
		snap.RuntimeStateBlobRef = strings.TrimSpace(runtimeRef)
		snap.StepHistoryBlobRef = strings.TrimSpace(historyRef)
		if convHistoryRef != "" {
			snap.ConversationHistoryBlobRef = strings.TrimSpace(convHistoryRef)
		}
		if serr := a.v2Store.SaveSnapshotAtomic(snap); serr != nil {
			a.emitRuntimeLog("error", "persistence failed: save_snapshot after interrupt raised", prevSnapshot, map[string]any{
				"kind":   "persistence",
				"action": "save_snapshot",
				"err":    serr.Error(),
			})
			return fmt.Errorf("save snapshot after interrupt raised failed: %w", serr)
		}

		waitSnap := a.state.UpdateTaskStatus(builtin_tools.TaskStatusUpdate{
			Task:     "等待人工确认",
			Status:   builtin_tools.TaskStatusWaiting,
			Message:  firstNonEmpty(question, "等待人工输入"),
			Progress: -1,
		})
		a.emitter.EmitStateChange(waitSnap)

		if a.workspaceRuntime != nil {
			l := a.wsLayout()
			if stepID := effectiveStepID(runCtx, prevSnapshot); l.SharedDir() != "" && stepID != "" {
				_ = appendStepTimeline(a.workspaceRuntime, stepID, &TimelineEvent{
					TS:   time.Now().UTC(),
					Type: "human_confirm",
					Key:  interruptID,
					Payload: map[string]any{
						"question": question,
						"status":   "pending",
					},
				})
			}
		}

		// Mark the tool call as "waiting" in UI so it does not stay spinning forever.
		a.emitter.EmitToolEnd(iter, builtin_tools.ToolResult{
			ID:         callID,
			Name:       toolName,
			IsAgent:    isAgent,
			StackDepth: stackDepth,
			Result:     "WAITING_FOR_HUMAN",
			Error:      "",
		}, effectiveStepID(runCtx, prevSnapshot))

		return &turnInterruptRaised{
			pending: &builtin_tools.PendingInterrupt{
				InterruptID: interruptID,
				Question:    question,
				InputType:   inputType,
				Options:     options,
				Context:     ctxMap,
			},
			toolCall: tc,
		}
	}

	callCtx := ctx
	if isAgent {
		callCtx = WithNextAgentCallInfo(ctx, strings.TrimSpace(a.cfg.AgentID), strings.TrimSpace(a.agentName))
		a.InjectAgentToolExtra(callCtx, toolName, argsMap)
	}
	sharedDir := ""
	if a.workspaceRuntime != nil {
		sharedDir = a.workspaceRuntime.SharedDir()
	}
	callCtx = builtin_tools.WithToolRuntime(callCtx, builtin_tools.ToolRuntimeInfo{
		Emitter:            a.emitter,
		RunID:              strings.TrimSpace(a.currentRunID),
		CallID:             callID,
		ToolName:           toolName,
		Iteration:          iter,
		IsAgent:            isAgent,
		StackDepth:         stackDepth,
		WorkspaceSessionID: strings.TrimSpace(a.workspaceSessionID),
		WorkspaceRootDir:   strings.TrimSpace(a.workspaceRootDir),
		WorkspaceNamespace: strings.TrimSpace(a.workspaceNamespace),
		WorkspaceSharedDir: sharedDir,
		SourceWorkingDir:   strings.TrimSpace(a.runtimeRepoContext.SourceWorkingDir),
		RepoRootDir:        strings.TrimSpace(a.runtimeRepoContext.RepoRootDir),
		IsGitRepo:          a.runtimeRepoContext.IsGitRepo,
		GitBranch:          strings.TrimSpace(a.runtimeRepoContext.Branch),
		GitRepoURL:         strings.TrimSpace(a.runtimeRepoContext.RemoteURL),
		IsGitWorktree:      a.runtimeRepoContext.IsWorktree,
		CurrentStepID:      effectiveStepID(runCtx, prevSnapshot),
	})

	// Execute 窗口统一穿工具洋葱链（tool_middleware.go）：超时装配 / Execute /
	// 超时包装在 base，截断 + 截断日志与耗时为默认中间件。链自身故障（非工具
	// 业务错误——那类进 res.ErrText 回填）直接上抛。
	res, chainErr := a.toolExecChain(callCtx, &toolExecCall{
		CallID:       callID,
		ToolName:     toolName,
		Tool:         tool,
		Args:         argsMap,
		IsAgent:      isAgent,
		StackDepth:   stackDepth,
		Iter:         iter,
		StepID:       effectiveStepID(runCtx, prevSnapshot),
		Phase:        prevSnapshot.Phase,
		PrevSnapshot: prevSnapshot,
	})
	if chainErr != nil {
		return fmt.Errorf("tool exec chain failed for %q: %w", toolName, chainErr)
	}
	out := res.Out
	errText := res.ErrText
	toolDuration := res.Duration
	outFullPath := res.OutFullPath

	// 一些前端仅展示 result 字段而忽略 error 字段；为了避免"失败但无输出"，把错误信息也放进展示结果里。
	displayOut := out
	if strings.TrimSpace(displayOut) == "" && strings.TrimSpace(errText) != "" {
		displayOut = fmt.Sprintf("Error: %s", errText)
	}
	render := buildToolResultRender(toolName, out)
	a.handleSkillToolStateSync(toolName, argsMap, out, errText, runCtx)
	a.AICallProxyWriteToolResult(runCtx, callID, toolName, tool.Description(), argsMap, render.Content, errText, isAgent)

	if stepID := effectiveStepID(runCtx, prevSnapshot); sharedDir != "" && stepID != "" {
		event := newToolCallTimelineEvent(callID, toolName, argsMap, out, errText, outFullPath, toolDuration)
		if len(render.Media) > 0 {
			event.Payload = map[string]any{"media": render.Media}
		}
		_ = appendStepTimeline(a.workspaceRuntime, stepID, event)
	}

	a.emitter.EmitToolEnd(iter, builtin_tools.ToolResult{
		ID:         callID,
		Name:       toolName,
		IsAgent:    isAgent,
		StackDepth: stackDepth,
		Result:     displayOut,
		Error:      errText,
		Media:      render.Media,
	}, effectiveStepID(runCtx, prevSnapshot))

	if toolName == builtin_tools.UpdateCurrentStepToolName {
		nextSnapshot := a.state.Snapshot()
		// update_current_step 工具内部已调 a.state.UpdateCurrentStep → observer 自动 emit
		// task_item，不再手抓 prevPlan diff（参见 state_observer_emitter.go）。

		// fix/02 补漏：peer 桶里 LLM 误调 update_current_step 时（fix/09 屏蔽前的窗口），
		// outcome attempt 必须按 runCtx 路由——否则把 peer 的 attempt 写到主 step outcome。
		stepID := effectiveStepID(runCtx, prevSnapshot)
		stepName := ""
		if current := prevSnapshot.CurrentStep(); current != nil {
			if stepID == "" {
				stepID = strings.TrimSpace(current.ID)
			}
			stepName = strings.TrimSpace(current.Step)
		}
		outcome := findOutcome(nextSnapshot.StepOutcomes, stepID)
		status := builtin_tools.PlanStepStatus(builtin_tools.ToolRuntimeValue(argsMap["status"]))
		a.writeV2StepAttemptResult(stepID, stepName, callID, status, outcome)
		a.state.SetStepOutcomeAttemptID(stepID, callID)
	}
	return nil
}

func (a *Agent) emitRuntimeLog(level string, message string, snapshot builtin_tools.StateSnapshot, extra map[string]any) {
	payload := builtin_tools.CloneAnyMap(extra)
	if payload == nil {
		payload = make(map[string]any)
	}
	payload["level"] = strings.TrimSpace(level)
	payload["message"] = strings.TrimSpace(message)
	payload["phase"] = snapshot.Phase
	payload["status"] = snapshot.Status
	payload["iteration"] = snapshot.Iteration
	payload["progress"] = snapshot.Progress
	payload["current_step_id"] = strings.TrimSpace(snapshot.CurrentStepID)
	if currentStep := snapshot.CurrentStep(); currentStep != nil {
		payload["current_step"] = currentStep
	}
	if latestInput := snapshot.LatestInput(); latestInput != nil {
		payload["latest_input"] = latestInput
	}
	if strings.TrimSpace(snapshot.StatusSummary) != "" {
		payload["status_summary"] = strings.TrimSpace(snapshot.StatusSummary)
	}
	if strings.TrimSpace(snapshot.Error) != "" {
		payload["state_error"] = strings.TrimSpace(snapshot.Error)
	}
	runtimelog.LogJSON(level, payload)
	if a == nil || a.emitter == nil {
		return
	}
	a.emitter.EmitLogPayload(payload)
}

func stepIDOf(step *builtin_tools.PlanItem) string {
	if step == nil {
		return ""
	}
	return strings.TrimSpace(step.ID)
}

// blockingStepFailure 已由 step_summary -> final_answer 的统一链路覆盖；本次重构不再提前截断。

const plannerInlineLimit = 15

func (a *Agent) applyPlannerOverflowHints(input *TaskPlannerPromptInput) {
	if input.HasSkillsTable() && countMarkdownTableRows(input.SkillsContext.Table) > plannerInlineLimit {
		path := a.writePlannerTempFile("planner_skills_index.md", input.SkillsContext.Table)
		if path != "" {
			input.SkillsOverflowPath = path
		}
	}
	if input.HasMCPTable() && countMarkdownTableRows(input.MCPContext.Table) > plannerInlineLimit {
		path := a.writePlannerTempFile("planner_mcp_index.md", input.MCPContext.Table)
		if path != "" {
			input.MCPOverflowPath = path
		}
	}
}

func countMarkdownTableRows(table string) int {
	lines := strings.Split(strings.TrimSpace(table), "\n")
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		if isMarkdownSeparatorRow(trimmed) {
			continue
		}
		count++
	}
	if count > 0 {
		count-- // 减去表头行
	}
	return count
}

func isMarkdownSeparatorRow(line string) bool {
	inner := strings.Trim(line, "| ")
	if inner == "" {
		return false
	}
	for _, ch := range inner {
		if ch != '-' && ch != ':' && ch != ' ' {
			return false
		}
	}
	return true
}

func (a *Agent) writePlannerTempFile(name, content string) string {
	if a == nil || a.workspaceRuntime == nil {
		return ""
	}
	l := a.wsLayout()
	dir := l.SharedDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, name)
	rel := filepath.ToSlash(filepath.Join(l.SharedDirRel(), name))
	if err := a.workspaceRuntime.Store().Write(rel, []byte(content)); err != nil {
		runtimelog.LogJSON("warning", map[string]any{
			"event":   "planner_overflow_file_write_failed",
			"message": "failed to write planner overflow file",
			"path":    path,
			"error":   err.Error(),
		})
		return ""
	}
	return path
}
