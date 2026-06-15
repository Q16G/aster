package react

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/runtimelog"
)

type stepReplanModelOutput struct {
	ShouldReplan bool   `json:"should_replan"`
	ReplanReason string `json:"replan_reason"`
	NextGoal     string `json:"next_goal"`
	// Plan 是 step_replan 直接产出的重编排 pending 步骤集（替代旧三轴输出）：
	// should_replan=true 时必填非空；只输出重规划后的 pending 步骤（保留 / 新增 / 调整），
	// completed / in_progress 项由系统自动承接、不在此复述；status 一律 pending；
	// depends_on 可引用已完成步骤 id；步骤文案可引用 OI-id 与事实板已确认值。
	Plan []*builtin_tools.PlanItem `json:"plan"`
}

func (a *Agent) runStepReplanPhase(ctx context.Context, iter int, runClient ai.ChatClient) error {
	_ = a.state.SetPhase(builtin_tools.AgentPhaseStepReplan)
	snapshot := a.state.Snapshot()
	a.emitter.EmitStateChange(snapshot)
	a.emitRuntimeLog("info", "enter step replan phase", snapshot, map[string]any{
		"event": "phase_enter",
	})

	current := snapshot.CurrentStep()
	if current == nil || strings.TrimSpace(current.ID) == "" {
		return fmt.Errorf("step_replan phase missing current step")
	}
	stepID := strings.TrimSpace(current.ID)

	rawOutcome := findOutcome(snapshot.StepOutcomes, stepID)
	if rawOutcome == nil {
		return fmt.Errorf("step_replan phase missing step outcome step_id=%s", stepID)
	}

	workspaceSharedDir := ""
	if a.workspaceRuntime != nil {
		workspaceSharedDir = strings.TrimSpace(a.workspaceRuntime.SharedDir())
	}

	// digest 归约：runtime 对 timeline 的规则归约为权威来源，先于 SimpleTask bypass 与 LLM
	// 判定 prompt 注入完成，确保所有下游路径（simple / 直达 Step / 子 Agent 回流）拿到同一
	// 归约结果；applyReplanResult 不再重复归约。
	if reduced := reduceStepTimelineToolCallsDigest(workspaceSharedDir, stepID); len(reduced) > 0 {
		rawOutcome.ToolCallsDigest = reduced
	}

	// 简单分支直通（设计 2.1）：simple 单步任务完成后跳过三轴 LLM 判定直达验收；
	// 机械落盘（plan_item 烘焙 / journal / step_contexts）由 applyReplanResult 保留执行，
	// final_answer 仍持有 should_replan 回流兜底。
	if snapshot.SimpleTask && len(snapshot.Plan) == 1 {
		a.emitRuntimeLog("info", "simple task bypasses step replan", snapshot, map[string]any{
			"event":   "step_replan_bypassed_simple",
			"step_id": stepID,
		})
		return a.applyReplanResult(stepID, nil, nil, nil, snapshot, "")
	}

	// Plan-once-execute-many gate（纯客观信号，零 LLM）：
	// 未命中任一触发条件时直接走 applyReplanResult 的 no-op 分支转入下一 step，
	// 命中则保留下方完整 step_replan LLM 路径。bypass 不渲染 prompt、不发 LLM。
	// 触发条件：
	//   1) plan 耗尽（无下一可跑 step）
	//   2) 当前 step status == failed
	//   3) 心跳：连续跳过 K 步后强制升级
	// 环境变量 STEP_REPLAN_BYPASS_DISABLED=true 时整段失效（紧急回滚开关）。
	if !stepReplanBypassDisabled() {
		escalate, reason := a.shouldEscalateStepReplan(snapshot, rawOutcome)
		if !escalate {
			a.consecutiveStepsSinceReplan++
			a.emitRuntimeLog("info", "step_replan bypassed by gate", snapshot, map[string]any{
				"event":                          "step_replan_bypassed",
				"step_id":                        stepID,
				"reason":                         "no_escalation_signal",
				"consecutive_steps_since_replan": a.consecutiveStepsSinceReplan,
			})
			return a.applyReplanResult(stepID, nil, nil, nil, snapshot, "")
		}
		a.emitRuntimeLog("info", "step_replan escalated to LLM", snapshot, map[string]any{
			"event":                          "step_replan_escalated",
			"step_id":                        stepID,
			"reason":                         reason,
			"consecutive_steps_since_replan": a.consecutiveStepsSinceReplan,
		})
		a.consecutiveStepsSinceReplan = 0
	}

	// Scheme A: 命中 gate 触发条件时（或开关关闭时）走完整 StepReplan LLM loop。
	//
	// Rationale: the old fast-path skip logic only relied on self-reported signals like
	// open_questions/warnings/unresolved, which can be under-reported and cause replan to
	// "never trigger". We intentionally trade cost for correctness here.

	stepResultPath := a.resolveStepResultPath(stepID, rawOutcome)
	// 覆盖清单超阈值时落盘并以指针入卡（persistCoverageChecklist 幂等，烘焙路径的同名调用不冲突）。
	stepCoveragePath := ""
	if rel := a.persistCoverageChecklist(stepID, rawOutcome); rel != "" {
		stepCoveragePath = filepath.Join(a.workspaceRootDir, rel)
	}
	stepContextsPath := a.resolveStepContextsPath()
	stepTranscriptPath := ""
	if ref := strings.TrimSpace(a.lastStepTranscriptBlobRef); ref != "" && a.v2Store != nil {
		stepTranscriptPath = a.v2Store.BlobPath(ref)
	}
	stepTimelinePath := ""
	if stepTimelineExists(workspaceSharedDir, stepID) {
		stepTimelinePath = filepath.Join(workspaceSharedDir, stepID, "timeline.jsonl")
	}
	openItemsPath := ""
	taskContextPath := ""
	if workspaceSharedDir != "" {
		openItemsPath = filepath.Join(workspaceSharedDir, openItemsFileName)
		taskContextPath = filepath.Join(workspaceSharedDir, taskContextFileName)
	}
	plannerJournal := readPlannerJournalForPrompt(a.workspaceRootDir, sharedFileLimitBytes(a.contextWindowTokens))
	// 仅在内联 journal 触发截断时注入路径指针——未截断时模型已看到全文，路径行属冗余 token。
	plannerJournalPath := ""
	if isTruncatedForPrompt(plannerJournal) {
		plannerJournalPath = resolvePlannerJournalPointer(a.workspaceRootDir)
	}

	skillsCtx := a.buildSkillsPromptContext(ctx, snapshot)

	fnTools, allowedTools := a.BuildFunctionTools(builtin_tools.AgentPhaseStepReplan)
	fnTools = append(fnTools, buildSubmitReplanFunctionTool())

	prompt, err := a.BuildStepReplanPrompt(map[string]any{
		"current_goal":         snapshot.CurrentGoal,
		"goal_understanding":   snapshot.GoalUnderstanding,
		"input_timeline":       snapshot.InputTimeline,
		"current_step_card":    buildReplanStepCard(current, rawOutcome, workspaceSharedDir, stepResultPath, stepCoveragePath),
		"plan_overview":        ProjectPlanItemCardsSlim(snapshot.Plan, a.workspaceRootDir),
		"planner_journal_path": plannerJournalPath,
		"planner_journal":      plannerJournal,
		"open_items_ledger":    readSharedFileForPromptWithLimit(workspaceSharedDir, openItemsFileName, sharedFileLimitBytes(a.contextWindowTokens)),
		"task_context_board":   readSharedFileForPromptWithLimit(workspaceSharedDir, taskContextFileName, sharedFileLimitBytes(a.contextWindowTokens)),
		"step_file_content":    readSharedStepFileForPrompt(workspaceSharedDir, stepID),
		"step_result_path":     stepResultPath,
		"step_contexts_path":   stepContextsPath,
		"step_transcript_path": stepTranscriptPath,
		"step_timeline_path":   stepTimelinePath,
		"open_items_path":      openItemsPath,
		"task_context_path":    taskContextPath,
		"step_file_path":       stepFileAbs(workspaceSharedDir, stepID),
		"skills_context":       skillsCtx,
		"available_tools":      functionToolsToAvailableInfo(fnTools),
	})
	if err != nil {
		return fmt.Errorf("build step replan prompt failed: %w", err)
	}

	const maxSubmitRetries = 3
	submitRetries := 0

	for round := 0; ; round++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		_ = round // 不再以 round 计数硬上限：让模型按需充分核验与落盘，runaway 防线靠 ctx 取消与 MaxIterations 兜底
		replanCtx, replanCancel := context.WithCancel(ctx)
		callResult, err := a.AICallProxy(replanCtx, iter, runClient, prompt, promptFamilyStepReplan, fnTools...)
		replanCancel()
		if err != nil {
			return fmt.Errorf("step replan AICallProxy failed: %w", err)
		}

		// Replan 允许空响应：语义为"不需要重规划"，默认继续当前计划。
		// 记 warning 便于观测「replan 零核验零落盘静默通过」的频率。
		if len(callResult.ToolCalls) == 0 {
			a.emitRuntimeLog("warning", "step replan returned no tool calls; treated as no-replan", snapshot, map[string]any{
				"event":        "step_replan_text_fallback",
				"step_id":      stepID,
				"content_size": len(strings.TrimSpace(callResult.AssistantText)),
			})
			return a.applyReplanResult(stepID, nil, nil, nil, snapshot, "")
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
				decision, parseErr := parseSubmitReplanArgs(tc.Function.Arguments)
				var mergedPlan []*builtin_tools.PlanItem
				if parseErr == nil && decision.ShouldReplan {
					// merge 后再 normalize：检查新 pending 是否依赖了 merge 后实际不存在的 id
					// 或形成环（merge 把 completed/in_progress 也作为合法依赖锚点）；失败也走
					// 同一个 retry 通道，避免静默 bail 到 phase_error → final_answer 错误终态。
					merged := mergeReplannedPlan(snapshot.Plan, decision.Plan)
					if _, normErr := builtin_tools.NormalizePlanItems(merged, true); normErr != nil {
						parseErr = fmt.Errorf("plan merge 后结构校验失败: %w（请检查 depends_on 是否引用了不存在的 id 或形成循环）", normErr)
					} else {
						mergedPlan = merged
					}
				}
				if parseErr != nil {
					submitRetries++
					if submitRetries > maxSubmitRetries {
						return fmt.Errorf("submit_plan replan failed after %d retries: %w", maxSubmitRetries, parseErr)
					}
					a.AICallProxyWriteToolResult(
						strings.TrimSpace(tc.Id), submitPlanToolName,
						"", nil, "",
						fmt.Sprintf("submit_plan 参数校验失败: %s\n请修正后重新调用 submit_plan。", parseErr.Error()),
						false,
					)
					anyUsefulTool = true
					continue
				}
				return a.applyReplanDecision(stepID, decision, mergedPlan, snapshot)
			}
			if _, ok := allowedTools[strings.TrimSpace(tc.Function.Name)]; ok {
				anyUsefulTool = true
				if err := a.executeToolCall(ctx, iter, tc, allowedTools); err != nil {
					return err
				}
			} else {
				a.AICallProxyWriteToolResult(strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name), "", nil, "", "tool not available in current phase", false)
			}
		}
		if !anyUsefulTool {
			return a.applyReplanResult(stepID, nil, nil, nil, snapshot, "")
		}
	}
}

// applyReplanDecision 仅做应用：mergedPlan 已经由主循环（runStepReplanPhase）做完
// merge + normalize 校验；should_replan=false 时 mergedPlan 为 nil。直达 Step 路径下
// 不构造带三轴的 ReplanContext。
func (a *Agent) applyReplanDecision(stepID string, decision stepReplanModelOutput, mergedPlan []*builtin_tools.PlanItem, snapshot builtin_tools.StateSnapshot) error {
	if !decision.ShouldReplan {
		return a.applyReplanResult(stepID, &decision, nil, nil, snapshot, "")
	}
	return a.applyReplanResult(stepID, &decision, mergedPlan, nil, snapshot, "")
}

// applyReplanResult 收尾 step_replan 阶段。三类入参互斥地决定下一步流转：
//   - newPlan != nil：本 step 内直接重编排（StepReplan → Step 直达），不再回流 Plan
//   - replanContext != nil：仅由 checkChildAgentsCompletion 构造，子 Agent 仍在跑时回流 Plan
//   - 二者皆 nil：无重编排，按 plan 中下一个可跑 step 继续，否则收尾走 final_answer
//
// step_replan 不再写 sticky `UnresolvedAxes`（三轴输出已删）；该字段仅由 final_answer
// 自身评估写入并由 planner 兜底消费，与本路径无关。
func (a *Agent) applyReplanResult(stepID string, modelOut *stepReplanModelOutput, newPlan []*builtin_tools.PlanItem, replanContext *builtin_tools.ReplanContext, snapshot builtin_tools.StateSnapshot, artifactDir string) error {
	current := snapshot.CurrentStep()

	nextPhase := builtin_tools.AgentPhaseFinalAnswer
	nextRunnableStepID := ""
	planForNextRunnable := snapshot.Plan
	if newPlan != nil {
		// 重编排直达 Step：以新 plan 计算下一个可跑 step；通常就是新增的第一条 pending。
		planForNextRunnable = newPlan
		if candidate := strings.TrimSpace(builtin_tools.NextRunnablePlanStepID(planForNextRunnable)); candidate != "" {
			nextRunnableStepID = candidate
			nextPhase = builtin_tools.AgentPhaseStep
		}
	} else if replanContext != nil {
		nextPhase = builtin_tools.AgentPhasePlan
	} else if candidate := strings.TrimSpace(builtin_tools.NextRunnablePlanStepID(snapshot.Plan)); candidate != "" {
		nextRunnableStepID = candidate
		nextPhase = builtin_tools.AgentPhaseStep
	}

	if nextPhase == builtin_tools.AgentPhaseFinalAnswer {
		if rc := a.checkChildAgentsCompletion(); rc != nil {
			replanContext = rc
			nextPhase = builtin_tools.AgentPhasePlan
		}
	}

	summaryGoal := ""
	if newPlan != nil && modelOut != nil {
		summaryGoal = strings.TrimSpace(modelOut.NextGoal)
	} else if replanContext != nil {
		summaryGoal = strings.TrimSpace(replanContext.NextGoal)
	}

	var replanWarnings []string
	if replanContext != nil {
		replanWarnings = replanContext.Warnings
	}

	rawOutcome := findOutcome(snapshot.StepOutcomes, stepID)

	// tool_calls_digest 已在 runStepReplanPhase 入口完成 runtime 归约（写入 rawOutcome）；
	// 同一 phase 内 rawOutcome 是同一指针，此处直接信任，不再重复读 timeline。

	contextKey := a.resolveStepContextKey(stepID, rawOutcome, snapshot)

	var timelineFile string
	if a.workspaceRuntime != nil && stepTimelineExists(a.workspaceRuntime.SharedDir(), stepID) {
		timelineFile = stepTimelineRelPath(stepID)
	}
	// step 过程文件（think_act 按三节契约维护）：存在才填指针；旧布局 fallback 兼容老 session。
	var stepFile string
	if a.workspaceRuntime != nil {
		if stepFileExists(a.workspaceRuntime.SharedDir(), stepID) {
			stepFile = stepFileRelPath(stepID)
		} else if legacyStepFileExists(a.workspaceRuntime.SharedDir(), stepID) {
			stepFile = fmt.Sprintf("shared/%s/step.md", stepID)
		}
	}
	coverageFile := a.persistCoverageChecklist(stepID, rawOutcome)

	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}

	prevPlan := builtin_tools.ClonePlanItems(snapshot.Plan)
	snapshot = a.state.ApplyStepReplan(stepID, stepReplanUpdate{
		ArtifactDir:       artifactDir,
		ContextKey:        contextKey,
		TimelineFile:      timelineFile,
		StepFile:          stepFile,
		CoverageFile:      coverageFile,
		Namespace:         builtin_tools.NormalizeWorkspaceNamespace(a.workspaceNamespace),
		PlanVersion:       planVersion,
		TranscriptBlobRef: a.lastStepTranscriptBlobRef,
		CurrentGoal:       summaryGoal,
		Warnings:          replanWarnings,
		ReplanContext:     replanContext,
		NewPlan:           newPlan,
		NextPhase:         nextPhase,
	})
	a.lastStepTranscriptBlobRef = ""

	a.appendStepContextRecord(stepID, snapshot)
	// step 烘焙记录归属旧 plan_version（snapshot.PlanVersion 在 NewPlan 路径下已经 ++，
	// 这里显式用 planVersion 这个本轮入参对应的旧值，避免 step / plan 两类记录的 plan_version 错配）。
	a.appendPlannerJournalStepRecordAt(stepID, snapshot, planVersion)
	if newPlan != nil {
		// 重编排直达 Step：plan 真相源（planner.jsonl）按新计划全量重写一条 plan 级记录，
		// 对齐 ApplyPlanAndEmit 的持久化纪律；UI / 下游通过 EmitTaskPlan 看见新 plan。
		a.appendPlannerJournalFullPlan(snapshot)
	}

	a.emitter.EmitStateChange(snapshot)

	if rawOutcome != nil {
		a.emitter.EmitStepSummaryResult(stepID, strings.TrimSpace(current.Step), rawOutcome)
	}
	if modelOut != nil {
		a.emitter.EmitStepReplanResult(stepID, strings.TrimSpace(current.Step), modelOut)
	}
	if newPlan != nil && a.emitter != nil && modelOut != nil {
		a.emitter.EmitTaskPlan(snapshot.Plan, strings.TrimSpace(modelOut.ReplanReason))
		emitTaskItemDiffs(a.emitter, prevPlan, snapshot.Plan, snapshot.CurrentStepID, strings.TrimSpace(modelOut.ReplanReason))
	}

	a.emitRuntimeLog("info", "step replan completed", snapshot, map[string]any{
		"event":         "step_replan_completed",
		"step_id":       stepID,
		"next_phase":    nextPhase,
		"next_step_id":  nextRunnableStepID,
		"should_replan": newPlan != nil,
		"replan_via":    replanViaLabel(newPlan, replanContext),
		"plan_size":     len(snapshot.Plan),
		"artifact_dir":  artifactDir,
	})
	return nil
}

// replanViaLabel 标注本轮 replan 的来源路径，便于运行日志追踪：
//   - direct: step_replan 内部直接产出新 plan，StepReplan → Step 直达
//   - child_agents: 子 Agent 仍在跑触发的兜底回流 Plan
//   - none: 无 replan
func replanViaLabel(newPlan []*builtin_tools.PlanItem, rc *builtin_tools.ReplanContext) string {
	switch {
	case newPlan != nil:
		return "direct"
	case rc != nil:
		return "child_agents"
	default:
		return "none"
	}
}

// shouldEscalateStepReplan 用纯客观信号判定本次 step_replan 是否需要升级为完整 LLM 调用。
// 三条触发条件按检查代价从低到高排列：
//   - step_error：当前 step 失败，必须 replan 调整路线
//   - heartbeat：连续跳过 K 步后强制升级，防止 plan 越走越偏
//   - plan_exhausted：plan 中无下一可跑 step，必须 replan 补充
//
// 返回 (false, "") 表示可以跳过 LLM 调用直接进入下一 step。
func (a *Agent) shouldEscalateStepReplan(snapshot builtin_tools.StateSnapshot, rawOutcome *builtin_tools.StepOutcome) (bool, string) {
	if rawOutcome != nil && rawOutcome.Status == builtin_tools.StepOutcomeFailed {
		return true, "step_error"
	}
	if k := stepReplanHeartbeatK(); k > 0 && a.consecutiveStepsSinceReplan >= k {
		return true, "heartbeat"
	}
	if strings.TrimSpace(builtin_tools.NextRunnablePlanStepID(snapshot.Plan)) == "" {
		return true, "plan_exhausted"
	}
	return false, ""
}

// stepReplanBypassDisabled 是 plan-once-execute-many gate 的紧急回滚开关。
// 置位时所有 step 都走完整 LLM replan（等价于旧行为）。
func stepReplanBypassDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STEP_REPLAN_BYPASS_DISABLED"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// stepReplanHeartbeatK 返回心跳兜底阈值：连续跳过 K 步后强制升级一次完整 replan。
// 默认 5，可通过环境变量 STEP_REPLAN_HEARTBEAT_K 调整。<=0 视作禁用心跳。
func stepReplanHeartbeatK() int {
	const defaultK = 5
	v := strings.TrimSpace(os.Getenv("STEP_REPLAN_HEARTBEAT_K"))
	if v == "" {
		return defaultK
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultK
	}
	return n
}

func (a *Agent) checkChildAgentsCompletion() *builtin_tools.ReplanContext {
	running := a.runningChildAgentNames()
	if len(running) == 0 {
		return nil
	}
	return &builtin_tools.ReplanContext{
		Reason:         "child agents still running",
		Warnings:       running,
		ReplacePending: false,
	}
}

func (a *Agent) runningChildAgentNames() []string {
	if a.workspaceRuntime == nil {
		return nil
	}
	state, err := a.workspaceRuntime.LoadWorkspaceState()
	if err != nil || state == nil || len(state.ChildAgents) == 0 {
		return nil
	}
	var running []string
	for name, ptr := range state.ChildAgents {
		if ptr == nil {
			continue
		}
		if ptr.Status == "completed" || ptr.Status == "failed" {
			continue
		}
		running = append(running, name)
	}
	return running
}

func (a *Agent) resolveStepContextKey(stepID string, outcome *builtin_tools.StepOutcome, snapshot builtin_tools.StateSnapshot) string {
	if outcome != nil {
		if ck := strings.TrimSpace(outcome.ContextKey); ck != "" {
			return ck
		}
	}
	namespace := builtin_tools.NormalizeWorkspaceNamespace(a.workspaceNamespace)
	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}
	return fmt.Sprintf("%s:%d:%s", namespace, planVersion, stepID)
}

func (a *Agent) appendStepContextRecord(stepID string, snapshot builtin_tools.StateSnapshot) {
	if a.workspaceRuntime == nil {
		return
	}
	outcome := findOutcome(snapshot.StepOutcomes, stepID)
	if outcome == nil {
		return
	}
	record := outcome.ToContextRecord()
	record.CreatedAt = time.Now()
	if err := a.workspaceRuntime.AppendStepContextRecords(
		[]*builtin_tools.StepContextRecord{record},
	); err != nil {
		a.emitRuntimeLog("warn", "append step context record failed", snapshot, map[string]any{
			"event":   "step_context_append_failed",
			"step_id": stepID,
			"error":   err.Error(),
		})
	}
}

const (
	coverageChecklistInlineMaxItems = 30
	coverageChecklistInlineMaxBytes = 2048
)

// persistCoverageChecklist 在覆盖清单超阈值时落地 shared/<stepID>/coverage.json 并返回相对路径；
// 未超阈值或无清单时返回空（保持内联）。落盘失败只记日志，不阻塞 step 收尾。
func (a *Agent) persistCoverageChecklist(stepID string, outcome *builtin_tools.StepOutcome) string {
	if a.workspaceRuntime == nil || outcome == nil || len(outcome.CoverageChecklist) == 0 {
		return ""
	}
	data, err := json.MarshalIndent(outcome.CoverageChecklist, "", "  ")
	if err != nil {
		return ""
	}
	if len(outcome.CoverageChecklist) <= coverageChecklistInlineMaxItems && len(data) <= coverageChecklistInlineMaxBytes {
		return ""
	}
	dir := filepath.Join(a.workspaceRuntime.SharedDir(), stepID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.emitRuntimeLog("warn", "persist coverage checklist failed", a.state.Snapshot(), map[string]any{
			"event": "coverage_checklist_persist_failed", "step_id": stepID, "error": err.Error(),
		})
		return ""
	}
	if err := os.WriteFile(filepath.Join(dir, "coverage.json"), data, 0o644); err != nil {
		a.emitRuntimeLog("warn", "persist coverage checklist failed", a.state.Snapshot(), map[string]any{
			"event": "coverage_checklist_persist_failed", "step_id": stepID, "error": err.Error(),
		})
		return ""
	}
	return fmt.Sprintf("shared/%s/coverage.json", stepID)
}

// appendPlannerJournalStepRecordAt 在 step 终态产出烘焙完成后，把该 plan_item 增量
// append 到 planner.jsonl（plan 真相源；同 id 最后一条胜出）。
// planVersion 由调用方显式指定（避免 NewPlan 路径下读 snapshot.PlanVersion 已经 ++ 后
// 把当前 step 的烘焙记录错标为新 plan_version）。
func (a *Agent) appendPlannerJournalStepRecordAt(stepID string, snapshot builtin_tools.StateSnapshot, planVersion int) {
	if a.workspaceRuntime == nil {
		return
	}
	var item *builtin_tools.PlanItem
	for _, it := range snapshot.Plan {
		if it != nil && strings.TrimSpace(it.ID) == stepID {
			item = it
			break
		}
	}
	if item == nil {
		return
	}
	if planVersion <= 0 {
		planVersion = 1
	}
	// 浅拷贝 + 克隆会被路径归一化原地改写的 slice，避免 append 时把 state 内的相对路径改成绝对路径。
	clone := *item
	clone.References = builtin_tools.CloneStringSlice(item.References)
	if err := builtin_tools.AppendPlannerJournalRecords(a.workspaceRuntime.RootDir(), []*builtin_tools.PlannerJournalRecord{
		{Kind: builtin_tools.PlannerJournalKindStep, PlanVersion: planVersion, Item: &clone},
	}); err != nil {
		a.emitRuntimeLog("warn", "append planner journal step record failed", snapshot, map[string]any{
			"event":   "planner_journal_step_append_failed",
			"step_id": stepID,
			"error":   err.Error(),
		})
	}
}

func (a *Agent) resolveStepResultPath(stepID string, outcome *builtin_tools.StepOutcome) string {
	if a == nil || a.v2Store == nil {
		return ""
	}
	if outcome != nil {
		if aid := strings.TrimSpace(outcome.AttemptID); aid != "" {
			p, err := a.v2Store.StepAttemptResultPath(stepID, aid)
			if err == nil {
				return p
			}
		}
	}
	return ""
}

func (a *Agent) resolveStepContextsPath() string {
	if a == nil {
		return ""
	}
	return builtin_tools.WorkspaceStepContextsFileAbs(a.workspaceRootDir)
}

func findOutcome(outcomes []*builtin_tools.StepOutcome, stepID string) *builtin_tools.StepOutcome {
	for _, o := range outcomes {
		if o != nil && strings.TrimSpace(o.StepID) == stepID {
			return o
		}
	}
	return nil
}

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// replanStepCard 是当前 step 产出的判定视图（plan_item 卡片形态：内联小字段 + 指针），
// 替代旧的 STEP_OUTCOME 全量注入。
type replanStepCard struct {
	ID                string                                `json:"id"`
	Step              string                                `json:"step"`
	Status            string                                `json:"status"`
	StatusSummary     string                                `json:"status_summary,omitempty"`
	ShortSummary      string                                `json:"short_summary,omitempty"`
	KeyFacts          []string                              `json:"key_facts,omitempty"`
	OpenQuestions     []string                              `json:"open_questions,omitempty"`
	ToolCallsDigest   []string                              `json:"tool_calls_digest,omitempty"`
	CoverageChecklist []builtin_tools.CoverageChecklistItem `json:"coverage_checklist,omitempty"`
	References        []string                              `json:"references,omitempty"`
	Error             string                                `json:"error,omitempty"`
	ResultFile        string                                `json:"result_file,omitempty"`
	TimelineFile      string                                `json:"timeline_file,omitempty"`
	CoverageFile      string                                `json:"coverage_file,omitempty"`
}

func buildReplanStepCard(current *builtin_tools.PlanItem, outcome *builtin_tools.StepOutcome, sharedDir, resultPath, coveragePath string) *replanStepCard {
	if current == nil || outcome == nil {
		return nil
	}
	card := &replanStepCard{
		ID:                strings.TrimSpace(current.ID),
		Step:              strings.TrimSpace(current.Step),
		Status:            strings.TrimSpace(string(outcome.Status)),
		StatusSummary:     strings.TrimSpace(outcome.StatusSummary),
		ShortSummary:      strings.TrimSpace(outcome.ShortSummary),
		KeyFacts:          outcome.KeyFacts,
		OpenQuestions:     outcome.OpenQuestions,
		ToolCallsDigest:   outcome.ToolCallsDigest,
		CoverageChecklist: outcome.CoverageChecklist,
		References:        outcome.References,
		Error:             strings.TrimSpace(outcome.Error),
		ResultFile:        strings.TrimSpace(resultPath),
		CoverageFile:      strings.TrimSpace(coveragePath),
	}
	// 清单已落盘指针化时内联只截留前 N 条，完整清单顺 coverage_file 回读。
	if card.CoverageFile != "" && len(card.CoverageChecklist) > coverageChecklistInlineMaxItems {
		card.CoverageChecklist = card.CoverageChecklist[:coverageChecklistInlineMaxItems]
	}
	if stepTimelineExists(sharedDir, card.ID) {
		card.TimelineFile = filepath.Join(sharedDir, card.ID, "timeline.jsonl")
	}
	return card
}

// sharedFileLimitBytes 根据 contextWindowTokens 计算共享区大文件的注入字节上限：
//
//	有效上限 = min(20KB, contextWindow * 0.40 * charsPerToken)
//
// contextWindowTokens <= 0 时用默认上限 defaultContextWindowTokens。
func sharedFileLimitBytes(contextWindowTokens int) int {
	const hardLimitBytes = 20 * 1024           // 20 KB
	const dynamicRatio = 0.40                  // 40% 上下文
	const bytesPerToken = defaultCharsPerToken // 4 bytes/token（保守估算）
	cw := contextWindowTokens
	if cw <= 0 {
		cw = defaultContextWindowTokens
	}
	dynamic := int(float64(cw) * dynamicRatio * bytesPerToken)
	if dynamic < hardLimitBytes {
		return dynamic
	}
	return hardLimitBytes
}

// readSharedFileForPrompt 读取共享区文件全文用于注入；缺失时返回占位说明（不报错）。
// 占位字符串供 step_replan_user.prompt 的 OPEN_ITEMS_LEDGER / TASK_CONTEXT_BOARD 显式注入，
// 告知模型文件状态——这是 step_replan 阶段的设计意图。需要"缺失即留空"语义的调用方（如
// task_planner 的 TaskContextBoard，HAS_TASK_CONTEXT_BOARD gate 仅判空串）改用
// readSharedFileOptional，避免占位字符串被当事实板内容渲染。
func readSharedFileForPrompt(sharedDir, name string) string {
	return readSharedFileForPromptWithLimit(sharedDir, name, 0)
}

// readSharedFileForPromptWithLimit 与 readSharedFileForPrompt 相同，但超过 limitBytes 时在
// 尾部追加截断提示（含绝对文件路径），让模型自主决策是否用文件工具读取完整内容。
// limitBytes <= 0 时不截断（同 readSharedFileForPrompt）。
func readSharedFileForPromptWithLimit(sharedDir, name string, limitBytes int) string {
	if sharedDir == "" {
		return "(共享区不可用)"
	}
	return readFileForPromptWithLimit(filepath.Join(sharedDir, name), limitBytes)
}

// readFileForPromptWithLimit 是 readSharedFileForPromptWithLimit 的核心实现，接受绝对路径，
// 供非共享区文件（例如 workspace/planner.jsonl）复用同一套读取+截断+占位策略。
func readFileForPromptWithLimit(absPath string, limitBytes int) string {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "(文件尚不存在)"
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "(文件为空)"
	}
	if limitBytes > 0 && len(content) > limitBytes {
		// 先找换行符：尽量在完整记录边界截断。
		// 搜索范围限制在 [limitBytes/2, limitBytes)，防止截点太靠前导致内容损失过多。
		cutByte := limitBytes
		if i := strings.LastIndexByte(content[:limitBytes], '\n'); i >= limitBytes/2 {
			cutByte = i
		}
		// 确保截断点落在 UTF-8 字符边界（从 cutByte 向前找合法起始字节）。
		for cutByte > 0 && content[cutByte]&0xC0 == 0x80 {
			cutByte--
		}
		truncated := content[:cutByte]
		return truncated + "\n\n（[截断] 仅显示前 " +
			formatBytes(cutByte) + "。完整内容见文件：" + absPath + "，如需全量数据请用文件工具读取。）"
	}
	return content
}

// isTruncatedForPrompt 判定 readFileForPromptWithLimit 产出的文本是否被超限截断。
// 截断尾部含固定标记串「（[截断]」（见 readFileForPromptWithLimit 实现），未截断或
// 文件不存在的占位说明（如「(文件尚不存在)」/「(文件为空)」）一律返回 false。
func isTruncatedForPrompt(content string) bool {
	return strings.Contains(content, "（[截断]")
}

// readPlannerJournalForPrompt 读取 workspace/planner.jsonl 全文用于注入。
// 与 readSharedFileForPromptWithLimit 共用截断与占位策略；workspaceRootDir 空或
// planner.jsonl 不存在时返回占位提示，让模型识别状态。
func readPlannerJournalForPrompt(workspaceRootDir string, limitBytes int) string {
	root := strings.TrimSpace(workspaceRootDir)
	if root == "" {
		return "(workspace 不可用)"
	}
	absPath := builtin_tools.WorkspacePlannerJournalFileAbs(root)
	if absPath == "" {
		return "(workspace 不可用)"
	}
	return readFileForPromptWithLimit(absPath, limitBytes)
}

// formatBytes 把字节数格式化为人类可读字符串（仅用于截断提示）。
func formatBytes(n int) string {
	if n >= 1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
	}
	if n >= 1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%d B", n)
}

// taskContextSkeleton 是 planner 冷启时 task_context.md 的初始骨架——仅含两节空标题
// （`## 输入事实` / `## 执行中补充`），为 LLM 提供入板锚点。骨架本身视作零内容快照，
// 不写入任何具体值（包括路径、技术栈、凭据等），符合 prompt_validate.md 第 6 条；
// 完整的"提交前须成立"终态由 planner 在提交计划前补齐。
const taskContextSkeleton = "## 输入事实\n\n## 执行中补充\n"

// ensureTaskContextSkeleton 在顶层 planner 首次进入时确保 task_context.md 存在。
// 文件已存在则不动；不存在则写入两节空标题骨架，避免冷启时 LLM 收到完全空白的
// 事实板上下文、无现成结构可参照。任何 IO 错误仅记录 warn，不阻塞规划流程。
func (a *Agent) ensureTaskContextSkeleton() {
	if a == nil || a.workspaceRuntime == nil {
		return
	}
	sharedDir := a.workspaceRuntime.SharedDir()
	if strings.TrimSpace(sharedDir) == "" {
		return
	}
	absPath := filepath.Join(sharedDir, taskContextFileName)
	if _, err := os.Stat(absPath); err == nil {
		return
	}
	relPath := filepath.ToSlash(filepath.Join("shared", taskContextFileName))
	if err := a.workspaceRuntime.WriteFileRel(relPath, []byte(taskContextSkeleton)); err != nil {
		runtimelog.LogJSON("warning", map[string]any{
			"event": "task_context_skeleton_write_failed",
			"path":  absPath,
			"error": err.Error(),
		})
	}
}

// readSharedFileOptional 与 readSharedFileForPrompt 同源读取共享区文件，但所有"无内容"分支
// （sharedDir 为空 / 文件不存在 / 文件为空）一律返回 ""，让外层的 `HAS_XXX` gate（基于
// strings.TrimSpace != ""）正确判定"是否注入"。
func readSharedFileOptional(sharedDir, name string) string {
	if strings.TrimSpace(sharedDir) == "" || strings.TrimSpace(name) == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(sharedDir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readSharedStepFileForPrompt(sharedDir, stepID string) string {
	if stepFileExists(sharedDir, stepID) {
		data, err := os.ReadFile(stepFileAbs(sharedDir, stepID))
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	// 旧布局 shared/<stepID>/step.md fallback（老 session resume）。
	if !legacyStepFileExists(sharedDir, stepID) {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(sharedDir, stepID, "step.md"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// buildSubmitReplanFunctionTool 构造 step_replan 阶段的 submit_plan 工具。
// 参数契约真相源在 schema；本函数 description 字段仅承担调用时机判据。
func buildSubmitReplanFunctionTool() *ai.FunctionTool {
	return &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        submitPlanToolName,
			Description: "完成复核与重编排后提交本轮决策；提交前账本 / 归档 / 事实板终态已成立。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"should_replan", "replan_reason", "next_goal", "plan"},
				"properties": map[string]any{
					"should_replan": map[string]any{
						"type":        "boolean",
						"description": "存在可行动缺口且未被现有 pending 步骤完整覆盖时为 true，否则 false。",
					},
					"replan_reason": map[string]any{
						"type":        "string",
						"description": "should_replan=true 时填一句人类可读的缺口总括；false 时填空字符串。",
					},
					"next_goal": map[string]any{
						"type":        "string",
						"description": "should_replan=true 时填明确的下一轮目标；false 时填空字符串。",
					},
					"plan": map[string]any{
						"type":        "array",
						"description": "重编排后的完整 pending 步骤集。should_replan=true 时非空；false 时为空数组。步骤文案可引用账本条目 OI-id 与事实板已确认值；不得出现能力索引中的名称。",
						"items":       submitReplanPlanItemSchema(),
					},
				},
			},
		},
	}
}

// submitReplanPlanItemSchema 是 step_replan 提交的 plan item schema。
// status 收窄为 pending：已完成 / 进行中项由系统自动承接、不在 plan 中复述。
func submitReplanPlanItemSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"id", "step", "status", "depends_on"},
		"properties": map[string]any{
			"id":   map[string]any{"type": "string", "description": "步骤唯一标识，不得为空或重复。"},
			"step": map[string]any{"type": "string", "description": "一条 step 是一个可独立验收的成果单元，标记完成时只声明一件被验证达成的事；一条描述若声明了多件各自可独立验收、各自可独立失败的事，按每件成果拆成多条 step。粒度上限：一条 step 约 3 次工具调用完成、>5 必拆；step 文案出现\"并且/同时/+/以及\"等并列连接，或试图合并多个独立检查类目（如同时涉及 XSS、SSRF、CORS 等不同类目）→ 视为粒度过大必须拆分；同一动作作用于多个对象按对象逐条拆，不在一条内列清单。不得为空，可引用 OI-id 与已确认事实值。"},
			"status": map[string]any{
				"type":        "string",
				"enum":        []string{"pending"},
				"description": "重编排步骤一律 pending。已完成 / 进行中项由系统自动承接，不在 plan 中复述。",
			},
			"depends_on": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "前置依赖的步骤 id 列表，可引用已完成步骤 id；不得引用无效 id 或形成循环依赖。",
			},
		},
	}
}

func parseSubmitReplanArgs(args any) (stepReplanModelOutput, error) {
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
			return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: marshal args failed: %w", err)
		}
	}
	var result stepReplanModelOutput
	if err := json.Unmarshal(data, &result); err != nil {
		return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: parse args failed: %w", err)
	}
	if result.ShouldReplan {
		if strings.TrimSpace(result.NextGoal) == "" {
			return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: should_replan=true but next_goal is empty")
		}
		if len(result.Plan) == 0 {
			return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: should_replan=true but plan is empty（请直接产出重编排后的 pending 步骤集，completed 项无需复述）")
		}
		// 单 plan 内的最小本地校验：每条 item 必须有 id / step / status 三个非空字段，
		// 且 id 在 new plan 内不重复。merge 后的跨 plan 校验（depends_on 引用未知 id /
		// 环依赖）由主循环 runStepReplanPhase 用 NormalizePlanItems 兜底——这里不能用
		// NormalizePlanItems 直接校验，因为新 plan 的 depends_on 可能合法地引用旧 plan
		// 的 completed step（id 在合并后才出现）。
		seenIDs := make(map[string]struct{}, len(result.Plan))
		for _, item := range result.Plan {
			if item == nil {
				return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: plan 含空条目")
			}
			id := strings.TrimSpace(item.ID)
			if id == "" {
				return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: plan 项 id 为空")
			}
			if _, dup := seenIDs[id]; dup {
				return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: plan 项 id 重复: %q", id)
			}
			seenIDs[id] = struct{}{}
			if strings.TrimSpace(item.Step) == "" {
				return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: plan 项 %q 的 step 为空", id)
			}
			if strings.TrimSpace(string(item.Status)) == "" {
				return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: plan 项 %q 的 status 为空", id)
			}
			// runtime 兜底（语义口径由 schema 承担）：重编排项必须为 pending；
			// completed / in_progress 项由系统自动承接，模型若复述会被 mergeReplannedPlan 静默吞噉。
			if builtin_tools.PlanStepStatus(strings.TrimSpace(string(item.Status))) != builtin_tools.PlanStepPending {
				return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: plan 项 %q status 必须为 pending（当前 %q）", id, item.Status)
			}
		}
	}
	return result, nil
}
