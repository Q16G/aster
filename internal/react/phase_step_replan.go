package react

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/runtimelog"
)

const submitReplanToolName = "submit_replan"

// submitReplanTool 是 step_replan 阶段专属的提交工具，实现 Tool 接口。
// Execute 完成参数解析与基本校验，成功时存储三轴结果；
// 失败时返回 error，由 executeToolCall 写入 tool result，模型自动重试。
type submitReplanTool struct {
	mu     sync.Mutex
	result *stepReplanModelOutput
}

func newSubmitReplanTool() *submitReplanTool {
	return &submitReplanTool{}
}

func (t *submitReplanTool) Name() string { return submitReplanToolName }

func (t *submitReplanTool) Description() string {
	return "完成复核与重编排后提交本轮决策；提交前账本 / 归档 / 事实板终态已成立。"
}

func (t *submitReplanTool) Parameters() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"should_replan", "replan_reason", "next_goal"},
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
			"incomplete_items": map[string]any{
				"type":        "array",
				"description": "轴①存在性：本 step 声明目标范围内、根本没做或仍悬而未决的项，驱动补齐。不含「做了但不扎实」（属 depth_gaps）。",
				"items":       map[string]any{"type": "string"},
			},
			"depth_gaps": map[string]any{
				"type":        "array",
				"description": "轴②深度/质量：本 step 声明目标内、做了但不扎实的项（static_only 未确认 / sink 未追到 source / 悬而未决判断 / 水货占位挤掉高价值分析 / 抽样冒充全量），驱动深挖。即使轴①为空也须独立判定。",
				"items":       map[string]any{"type": "string"},
			},
			"new_surfaces": map[string]any{
				"type":        "array",
				"description": "轴③泛化扩面：对照 GOAL_UNDERSTANDING 意图半径内、与用户核心目标语义相关的资产/攻击面全集，尚未被任何已完成工作覆盖的面；范围是整个任务而非当前 step。入列时轻量去重偏放行：只剔除明确同 (检查维度×资产) 对、前提未变、已扎实覆盖的重叠，拿不准保留；已覆盖但浅的转入 depth_gaps。受 GOAL_UNDERSTANDING 范围边界约束，意图外/明确不做项降级沉回 `## 不可解局限`。",
				"items":       map[string]any{"type": "string"},
			},
		},
	}
}

func (t *submitReplanTool) Execute(_ context.Context, args map[string]any) (string, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("submit_replan: marshal args failed: %w", err)
	}
	var result stepReplanModelOutput
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("submit_replan: parse args failed: %w", err)
	}
	if result.ShouldReplan && strings.TrimSpace(result.NextGoal) == "" {
		return "", fmt.Errorf("submit_replan: should_replan=true but next_goal is empty")
	}
	r := result
	t.mu.Lock()
	t.result = &r
	t.mu.Unlock()
	return `{"ok":true}`, nil
}

func (t *submitReplanTool) getResult() *stepReplanModelOutput {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.result
}

type stepReplanModelOutput struct {
	ShouldReplan    bool     `json:"should_replan"`
	ReplanReason    string   `json:"replan_reason"`
	NextGoal        string   `json:"next_goal"`
	IncompleteItems []string `json:"incomplete_items,omitempty"` // 轴①存在性缺口
	DepthGaps       []string `json:"depth_gaps,omitempty"`       // 轴②深度缺口
	NewSurfaces     []string `json:"new_surfaces,omitempty"`     // 轴③扩面缺口
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
	// 构造复核窗口：以上一次 LLM replan 边界为左侧开区间，含本回合 current 在内的所有
	// completed/failed step 进入窗口（最右为本回合，Latest=true）。
	// bypass 关闭时（每步走 LLM）窗口稳定为 1 张卡（边界=上一步）。
	priorBoundaryStepID := a.lastReplanBoundaryStepID
	reviewWin := a.buildReviewWindow(snapshot, priorBoundaryStepID, workspaceSharedDir)
	// 窗口构造完毕后把边界推进到本回合 stepID，供下一次升级使用。
	a.lastReplanBoundaryStepID = stepID

	// Scheme A: 命中 gate 触发条件时（或开关关闭时）走完整 StepReplan LLM loop。
	//
	// Rationale: the old fast-path skip logic only relied on self-reported signals like
	// open_questions/warnings/unresolved, which can be under-reported and cause replan to
	// "never trigger". We intentionally trade cost for correctness here.

	// 全局指针/共享区路径（与窗口无关，保留在顶层 payload）：
	stepContextsPath := a.resolveStepContextsPath()
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
	// 当前 step 的 transcript blob 路径属于"最后一卡"的辅助维度，整体指针下沉到 reviewWin 不便表达，
	// 仍以顶层路径形式注入（最后一卡 = current）。
	stepTranscriptPath := ""
	if ref := strings.TrimSpace(a.lastStepTranscriptBlobRef); ref != "" && a.v2Store != nil {
		stepTranscriptPath = a.v2Store.BlobPath(ref)
	}

	skillsCtx := a.buildSkillsPromptContext(ctx, snapshot)

	submitTool := newSubmitReplanTool()
	if err := a.registerTool(submitTool); err != nil {
		return fmt.Errorf("register submit_replan tool: %w", err)
	}
	defer a.unregisterTool(submitReplanToolName)

	fnTools, allowedTools := a.BuildFunctionTools(builtin_tools.AgentPhaseStepReplan)

	prompt, err := a.BuildStepReplanPrompt(map[string]any{
		"current_goal":            snapshot.CurrentGoal,
		"goal_understanding":      snapshot.GoalUnderstanding,
		"input_timeline":          snapshot.InputTimeline,
		"review_window":           reviewWin,
		"plan_overview":           ProjectPlanItemCardsSlim(snapshot.Plan, a.workspaceRootDir),
		"planner_journal_path":    plannerJournalPath,
		"planner_journal":         plannerJournal,
		"open_items_ledger":       readSharedFileForPromptWithLimit(workspaceSharedDir, openItemsFileName, sharedFileLimitBytes(a.contextWindowTokens)),
		"task_context_board":      readSharedFileForPromptWithLimit(workspaceSharedDir, taskContextFileName, sharedFileLimitBytes(a.contextWindowTokens)),
		"step_file_content":       readSharedStepFileForPrompt(workspaceSharedDir, stepID),
		"step_contexts_path":      stepContextsPath,
		"step_transcript_path":    stepTranscriptPath,
		"open_items_path":         openItemsPath,
		"task_context_path":       taskContextPath,
		"step_file_path":          stepFileAbs(workspaceSharedDir, stepID),
		"prior_boundary_step_id":  priorBoundaryStepID,
		"skills_context":          skillsCtx,
		"available_tools":         functionToolsToAvailableInfo(fnTools),
	})
	if err != nil {
		return fmt.Errorf("build step replan prompt failed: %w", err)
	}

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
			if tc.Function.Name == submitReplanToolName {
				if err := a.executeToolCall(ctx, iter, tc, allowedTools); err != nil {
					return err
				}
				decision := submitTool.getResult()
				if decision == nil {
					// Execute 返回 error，executeToolCall 已写 tool result，模型自动重试。
					anyUsefulTool = true
					continue
				}
				return a.applyReplanDecision(stepID, *decision, snapshot)
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

// applyReplanDecision 根据三轴决策构造 ReplanContext 路由回 planner；
// should_replan=false 时无重编排，继续执行下一个可跑 step 或走向 final_answer。
func (a *Agent) applyReplanDecision(stepID string, decision stepReplanModelOutput, snapshot builtin_tools.StateSnapshot) error {
	if !decision.ShouldReplan {
		return a.applyReplanResult(stepID, &decision, nil, nil, snapshot, "")
	}
	rc := &builtin_tools.ReplanContext{
		SourceStepID:    stepID,
		Reason:          decision.ReplanReason,
		NextGoal:        decision.NextGoal,
		IncompleteItems: builtin_tools.NewAxisItems(decision.IncompleteItems),
		DepthGaps:       builtin_tools.NewAxisItems(decision.DepthGaps),
		NewSurfaces:     builtin_tools.NewAxisItems(decision.NewSurfaces),
		ReplacePending:  true,
	}
	return a.applyReplanResult(stepID, &decision, nil, rc, snapshot, "")
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
	if k := stepReplanHeartbeatK(); k >= 0 && a.consecutiveStepsSinceReplan >= k {
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
// 默认 2，可通过环境变量 STEP_REPLAN_HEARTBEAT_K 调整。
// 触发语义：consecutiveStepsSinceReplan >= K 时升级——K=0 即每步必触发（零跳过容忍），
// K=2 即跳过 2 步后第 3 步触发（默认）。负值视作禁用心跳，仅靠 plan_exhausted / step_error 升级。
func stepReplanHeartbeatK() int {
	const defaultK = 2
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

// resolveCoverageFile 返回覆盖清单的相对路径（`shared/<stepID>/coverage.json`），只读不写：
//   - 清单为空或工作区缺失：返回 ""；
//   - 清单可内联（数量与字节都未超阈值）：返回 ""，调用方继续内联渲染；
//   - 清单需落盘但文件已存在（stat 命中）：直接返回 rel 路径，**跳过 WriteFile**；
//   - 清单需落盘但文件缺失：兜底调 persistCoverageChecklist 写一次再返回 rel。
//
// 设计意图：review_window 多卡场景下，历史卡的 outcome 已经 freeze（step 完成 + outcome 烘焙后
// 不再变更），重复 MarshalIndent/WriteFile 是无收益的 IO（还会刷历史文件 mtime，干扰外部观察）。
// latest 卡仍走 persistCoverageChecklist 强制写一次（latest 的 outcome 在本回合刚生成可能尚未落盘）。
func (a *Agent) resolveCoverageFile(stepID string, outcome *builtin_tools.StepOutcome) string {
	if a == nil || a.workspaceRuntime == nil || outcome == nil || len(outcome.CoverageChecklist) == 0 {
		return ""
	}
	if len(outcome.CoverageChecklist) <= coverageChecklistInlineMaxItems {
		data, err := json.MarshalIndent(outcome.CoverageChecklist, "", "  ")
		if err == nil && len(data) <= coverageChecklistInlineMaxBytes {
			return ""
		}
	}
	abs := filepath.Join(a.workspaceRuntime.SharedDir(), stepID, "coverage.json")
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		return fmt.Sprintf("shared/%s/coverage.json", stepID)
	}
	return a.persistCoverageChecklist(stepID, outcome)
}

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

// replanStepCard 是 step 产出的判定视图（plan_item 卡片形态：内联小字段 + 指针），
// 替代旧的 STEP_OUTCOME 全量注入。区间复核（review_window）下被批量生成，
// Latest=true 用于标识本回合刚跑完的那一张卡（区间最右）。
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
	Latest            bool                                  `json:"latest,omitempty"`
}

// reviewWindowMaxCardsBaseline 是区间多卡软上限的下界。plan_exhausted 触发时窗口可能远超
// heartbeat K，须截断保最新 N 张并在模板提示更早 step 从 PLANNER_JOURNAL / PLAN_OVERVIEW 回读。
// 实际上限由 reviewWindowMaxCards() 动态计算，跟随 STEP_REPLAN_HEARTBEAT_K 联动，避免用户调大 K
// 时（如 K=10）反而频繁截断的反直觉行为。
const reviewWindowMaxCardsBaseline = 8

// reviewWindowMaxCards 计算复核窗口的卡片软上限：
//
//	上限 = max(heartbeat_K + 3, baseline=8)
//
// "+3" 是安全余量：K 心跳触发后通常窗口正好 K+1 张，但 plan_exhausted 时可能多带几个尾部 step。
// 心跳禁用（K<0）或 K=0（每步触发、单卡窗口）时退化为 baseline。
func reviewWindowMaxCards() int {
	k := stepReplanHeartbeatK()
	if k <= 0 {
		return reviewWindowMaxCardsBaseline
	}
	if dyn := k + 3; dyn > reviewWindowMaxCardsBaseline {
		return dyn
	}
	return reviewWindowMaxCardsBaseline
}

// reviewWindow 是 review_window_cards 的渲染容器：携带截断元信息（共多少张/展示多少张）
// 供模板顶部提示展示，模板可通过 .Cards 迭代每张卡。
type reviewWindow struct {
	Cards        []*replanStepCard `json:"cards"`
	TotalCards   int               `json:"total_cards"`
	OmittedCount int               `json:"omitted_count,omitempty"`
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

// buildReviewWindow 收集"自上次 LLM replan 边界以来已完成的 step"区间多卡（含本回合 current）。
//
// 边界语义：boundaryStepID 是上一次 LLM replan 触发那一刻的 current stepID（含），
// 窗口取 plan 中所有索引 > boundary 且 status ∈ {completed, failed} 的 item。
// boundary 为空 / 找不到时回退为 -1，窗口含全部 completed/failed step（首跑或 resume 场景）。
// 最后一张卡的 Latest=true，模板据此渲染"↑ 刚完成"提示。
//
// 超过 reviewWindowMaxCards 时截断保最新 N 张并写入 OmittedCount，模板提示更早 step
// 走 PLANNER_JOURNAL / PLAN_OVERVIEW 回读，避免 plan_exhausted 跨越全 plan 时 token 爆炸。
func (a *Agent) buildReviewWindow(snapshot builtin_tools.StateSnapshot, boundaryStepID string, sharedDir string) *reviewWindow {
	plan := snapshot.Plan
	if len(plan) == 0 {
		return &reviewWindow{}
	}
	boundaryIdx := -1
	if bid := strings.TrimSpace(boundaryStepID); bid != "" {
		for i, it := range plan {
			if it != nil && strings.TrimSpace(it.ID) == bid {
				boundaryIdx = i
				break
			}
		}
	}
	var windowItems []*builtin_tools.PlanItem
	for i := boundaryIdx + 1; i < len(plan); i++ {
		it := plan[i]
		if it == nil {
			continue
		}
		switch it.Status {
		case builtin_tools.PlanStepCompleted, builtin_tools.PlanStepFailed:
			windowItems = append(windowItems, it)
		}
	}
	if len(windowItems) == 0 {
		return &reviewWindow{}
	}
	total := len(windowItems)
	omitted := 0
	maxCards := reviewWindowMaxCards()
	if total > maxCards {
		omitted = total - maxCards
		windowItems = windowItems[total-maxCards:]
	}
	cards := make([]*replanStepCard, 0, len(windowItems))
	lastIdx := len(windowItems) - 1
	for i, item := range windowItems {
		stepID := strings.TrimSpace(item.ID)
		outcome := findOutcome(snapshot.StepOutcomes, stepID)
		if outcome == nil {
			// 没有 outcome 的 completed/failed 项极罕见（理论上 step 完成必写 outcome）；
			// 留指针级别条目避免静默丢失：用 status_summary 占位，下游可由账本/journal 补全。
			cards = append(cards, &replanStepCard{
				ID:            stepID,
				Step:          strings.TrimSpace(item.Step),
				Status:        strings.TrimSpace(string(item.Status)),
				StatusSummary: "outcome missing; consult planner_journal / plan_overview",
			})
			continue
		}
		// latest 卡（区间最右）：outcome 在本回合刚生成，走强制 persist 保证落盘最新；
		// 历史卡：outcome 已 freeze，走 resolveCoverageFile 复用既有文件、跳过冗余 WriteFile。
		var coverageRel string
		if i == lastIdx {
			coverageRel = a.persistCoverageChecklist(stepID, outcome)
		} else {
			coverageRel = a.resolveCoverageFile(stepID, outcome)
		}
		card := buildReplanStepCard(item, outcome, sharedDir,
			a.resolveStepResultPath(stepID, outcome),
			a.absolutizeCoverageRel(coverageRel),
		)
		if card == nil {
			continue
		}
		cards = append(cards, card)
	}
	if len(cards) == 0 {
		return &reviewWindow{}
	}
	cards[len(cards)-1].Latest = true
	return &reviewWindow{
		Cards:        cards,
		TotalCards:   total,
		OmittedCount: omitted,
	}
}

// absolutizeCoverageRel 把 persistCoverageChecklist / resolveCoverageFile 返回的相对路径
// （`shared/<stepID>/coverage.json`）拼成绝对路径，与 resolveStepResultPath 返回的绝对路径风格对齐——
// 模型按 prompt 内指针回读时不再受 cwd 影响。空串保持空串。
func (a *Agent) absolutizeCoverageRel(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" || a == nil {
		return ""
	}
	if filepath.IsAbs(rel) {
		return rel
	}
	if a.workspaceRootDir == "" {
		return rel
	}
	return filepath.Join(a.workspaceRootDir, rel)
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

