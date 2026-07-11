package react

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/runtimelog"
	"aster/internal/workspacefs"
)

const submitReplanToolName = "submit_replan"

// submitReplanTool 是 step_replan 阶段专属的提交工具，实现 Tool 接口。
// Execute 完成参数解析与基本校验（含 phase_assessments 一致性守卫），成功时存储结果；
// 失败时返回 error，由 executeToolCall 写入 tool result，模型自动重试。
type submitReplanTool struct {
	mu           sync.Mutex
	result       *stepReplanModelOutput
	activePhases map[string]struct{} // 本轮 frontier 的 active phase id 集，用于 phase_id 合法性校验
	pendingByID  map[string]bool     // 各 phase 是否仍有 pending step，用于 completed-with-pending 守卫
}

func newSubmitReplanTool(activePhases []*builtin_tools.PlanPhase, plan []*builtin_tools.PlanItem) *submitReplanTool {
	ids := make(map[string]struct{}, len(activePhases))
	pending := make(map[string]bool, len(activePhases))
	for _, phase := range activePhases {
		if phase == nil {
			continue
		}
		id := strings.TrimSpace(phase.ID)
		if id == "" {
			continue
		}
		ids[id] = struct{}{}
		pending[id] = builtin_tools.PhaseHasPendingStep(plan, id)
	}
	return &submitReplanTool{activePhases: ids, pendingByID: pending}
}

func (t *submitReplanTool) Name() string { return submitReplanToolName }

func (t *submitReplanTool) Description() string {
	return "完成复核与重编排后提交本轮决策；提交前账本 / 归档 / 事实板终态已成立。"
}

func (t *submitReplanTool) Parameters() any {
	return map[string]any{
		"type":     "object",
		"required": []string{"should_replan", "replan_reason", "phase_assessments"},
		"properties": map[string]any{
			"should_replan": map[string]any{
				"type":        "boolean",
				"description": "本轮 frontier 内任一 active phase 仍有可行动缺口（continue）且未被现有 pending 步骤完整覆盖时为 true，否则 false。存在任一 status=continue 的 phase 时本字段必须为 true。",
			},
			"replan_reason": map[string]any{
				"type":        "string",
				"description": "should_replan=true 时填一句人类可读的跨 phase 缺口总括；false 时填空字符串。",
			},
			"phase_assessments": map[string]any{
				"type":        "array",
				"description": "对本轮 frontier 内每个 active phase 独立给出一条评估。逐个判定，不要合并多个 phase。",
				"items": map[string]any{
					"type":     "object",
					"required": []string{"phase_id", "status"},
					"properties": map[string]any{
						"phase_id": map[string]any{
							"type":        "string",
							"description": "被评估的 active phase id，必须是本轮 frontier 内的有效 active phase。",
						},
						"status": map[string]any{
							"type":        "string",
							"enum":        []string{"continue", "completed", "blocked"},
							"description": "continue=该 lane 仍有 in-phase 深度可推进，下一轮 planner 继续释放其 step（要求 should_replan=true）；completed=该 lane 已闭环（其下不得残留 pending step，可测的都测了、可深入的都深入了）；blocked=该 lane 受阻，其下 pending step 将被收敛为 skipped，阻因须记入账本。",
						},
						"reason": map[string]any{
							"type":        "string",
							"description": "该 phase 判定的一句话依据。",
						},
						"incomplete_items": map[string]any{
							"type":        "array",
							"description": "轴①存在性（限本 phase 范围内）：该 lane 内根本没做或仍悬而未决的项，驱动补齐。不含「做了但不扎实」（属 depth_gaps）。跨 phase 的新面只登账本「待承接」，不进此字段。",
							"items":       map[string]any{"type": "string"},
						},
						"depth_gaps": map[string]any{
							"type":        "array",
							"description": "轴②深度/质量（限本 phase 范围内）：该 lane 内做了但不扎实的项（" + builtin_tools.DepthSmellsEnumeration + "），驱动深挖。即使轴①为空也须独立判定。",
							"items":       map[string]any{"type": "string"},
						},
						"new_surfaces": map[string]any{
							"type":        "array",
							"description": "轴③泛化扩面（限本 phase 范围内）：该 lane 理论全量覆盖集与已完成工作的差集；跨 phase 的同级新面由 role 全量核验只登账本「待承接」不入此字段。受 GOAL_UNDERSTANDING 范围边界约束。",
							"items":       map[string]any{"type": "string"},
						},
					},
				},
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
	if result.ShouldReplan && strings.TrimSpace(result.ReplanReason) == "" {
		return "", fmt.Errorf("submit_replan: should_replan=true but replan_reason is empty")
	}

	hasContinue := false
	assessed := make(map[string]struct{}, len(result.PhaseAssessments))
	for _, assess := range result.PhaseAssessments {
		if assess == nil {
			continue
		}
		phaseID := builtin_tools.CanonicalizePlanIDToken(assess.PhaseID)
		if phaseID == "" {
			return "", fmt.Errorf("submit_replan: phase_assessments[].phase_id is required")
		}
		assess.PhaseID = phaseID
		if _, ok := t.activePhases[phaseID]; !ok {
			return "", fmt.Errorf("submit_replan: phase_id %q 不属于本轮 frontier 的 active phase，只能评估当前活跃 lane", phaseID)
		}
		if _, dup := assessed[phaseID]; dup {
			return "", fmt.Errorf("submit_replan: phase %q 被重复评估，每个 active phase 只提交一条 assessment", phaseID)
		}
		assessed[phaseID] = struct{}{}
		switch assess.Status {
		case builtin_tools.PhaseAssessContinue:
			hasContinue = true
		case builtin_tools.PhaseAssessCompleted:
			// D7a①：completed 的 lane 不容残留 pending step（completed=已闭环语义）。
			if t.pendingByID[phaseID] {
				return "", fmt.Errorf("submit_replan: phase %q 判为 completed 但其下仍有 pending step——completed 表示 lane 已闭环，请改判 continue（继续推进）或先让 pending step 落定", phaseID)
			}
		case builtin_tools.PhaseAssessBlocked:
		default:
			return "", fmt.Errorf("submit_replan: phase %q 的 status 非法（须为 continue|completed|blocked）", phaseID)
		}
	}
	// 完整性校验（review 补充）：本轮每个 active phase 都必须恰有一条 assessment，
	// 否则漏评的 lane 状态不推进——下游依赖它的 lane 会被永久锁住、或全 step terminal
	// 时被静默跳过至 final_answer。ACTIVE_PHASES 为空（simple/历史 session）时豁免。
	if len(t.activePhases) > 0 && len(assessed) < len(t.activePhases) {
		var missing []string
		for id := range t.activePhases {
			if _, ok := assessed[id]; !ok {
				missing = append(missing, id)
			}
		}
		sort.Strings(missing)
		return "", fmt.Errorf("submit_replan: 本轮 active phase 未全部评估，缺失 %v——每个 active lane 必须逐一给出 continue|completed|blocked，不得遗漏", missing)
	}
	// D7a②：存在 continue 的 phase ⇒ should_replan 必须为 true，否则 continue 语义被静默丢弃。
	if hasContinue && !result.ShouldReplan {
		return "", fmt.Errorf("submit_replan: 存在 status=continue 的 phase，should_replan 必须为 true（继续推进该 lane 需回流 planner 释放 step）")
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
	ShouldReplan bool   `json:"should_replan"`
	ReplanReason string `json:"replan_reason"`
	// PhaseAssessments 是对本轮 frontier 内各 active phase 的逐个评估（continue/completed/blocked
	// + 该 phase 范围内三轴缺口）。runtime 机械承接：completed/blocked 写回 PlanPhase.Status，
	// blocked 联动 skip 其下 pending step；continue 汇总三轴回流 planner 继续释放 step。
	PhaseAssessments []*builtin_tools.PhaseAssessment `json:"phase_assessments,omitempty"`
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

	var wsl workspacefs.Layout
	if a.workspaceRuntime != nil {
		wsl = a.wsLayout()
	}

	// digest 归约：runtime 对 timeline 的规则归约为权威来源，先于 SimpleTask bypass 与 LLM
	// 判定 prompt 注入完成，确保所有下游路径（simple / 直达 Step / 子 Agent 回流）拿到同一
	// 归约结果；applyReplanResult 不再重复归约。
	if reduced := reduceStepTimelineToolCallsDigest(a.workspaceRuntime, stepID); len(reduced) > 0 {
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
	//   1) plan 耗尽（无下一可跑 task_item）—— 默认主触发：当前 phase 释放的批次跑完即复核
	//   2) 当前 step status == failed —— 硬失败即时升级
	//   3) 心跳：连续跳过 K 步后强制升级（默认 K=-1 禁用；K=0 每步；K>0 折中）
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
	// 窗口为自上次复核以来的全部 step；默认 K<0（纯 per-batch）时为整批，K=0（per-step）时稳定为 1 张卡。
	priorBoundaryStepID := a.lastReplanBoundaryStepID
	reviewWin := a.buildReviewWindow(snapshot, priorBoundaryStepID, a.workspaceRuntime)
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
	if wsl.SharedDir() != "" {
		openItemsPath = wsl.OpenItems()
		taskContextPath = wsl.TaskContext()
	}
	// 统一 PromptContext：内存字段与共享区文件字段全部经动态 preview 上限投影（M2 接线）。
	pc := a.buildPromptContext(snapshot, stepID)
	// 仅在内联 journal 触发截断时注入路径指针——未截断时模型已看到全文，路径行属冗余 token。
	plannerJournalPath := ""
	if isTruncatedForPrompt(pc.PlannerJournal) {
		plannerJournalPath = resolvePlannerJournalPointer(a.workspaceRootDir)
	}
	// 当前 step 的 transcript blob 路径属于"最后一卡"的辅助维度，整体指针下沉到 reviewWin 不便表达，
	// 仍以顶层路径形式注入（最后一卡 = current）。
	stepTranscriptPath := ""
	if ref := strings.TrimSpace(a.lastStepTranscriptBlobRef); ref != "" && a.v2Store != nil {
		stepTranscriptPath = a.v2Store.BlobPath(ref)
	}

	skillsCtx := a.buildSkillsPromptContext(ctx, snapshot)

	activePhases := builtin_tools.ActivePhases(snapshot.Phases)
	submitTool := newSubmitReplanTool(activePhases, snapshot.Plan)
	if err := a.registerTool(submitTool); err != nil {
		return fmt.Errorf("register submit_replan tool: %w", err)
	}
	defer a.unregisterTool(submitReplanToolName)

	fnTools, allowedTools := a.BuildFunctionTools(nil, builtin_tools.AgentPhaseStepReplan)

	prompt, err := a.BuildStepReplanPrompt(map[string]any{
		"current_goal":           snapshot.CurrentGoal,
		"goal_understanding":     pc.GoalUnderstanding,
		"active_phases":          activePhases,
		"input_timeline":         pc.InputTimeline,
		"review_window":          reviewWin,
		"plan_overview":          pc.Plan,
		"planner_journal_path":   plannerJournalPath,
		"planner_journal":        pc.PlannerJournal,
		"open_items_ledger":      pc.OpenItemsLedger,
		"task_context_board":     pc.TaskContextBoard,
		"step_file_content":      pc.StepFileContent,
		"step_contexts_path":     stepContextsPath,
		"step_transcript_path":   stepTranscriptPath,
		"open_items_path":        openItemsPath,
		"task_context_path":      taskContextPath,
		"step_file_path":         wsl.StepFile(stepID),
		"prior_boundary_step_id": priorBoundaryStepID,
		"skills_context":         skillsCtx,
		"available_tools":        functionToolsToAvailableInfo(fnTools),
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
		callResult, err := a.AICallProxy(replanCtx, nil, iter, runClient, prompt, promptFamilyStepReplan, fnTools...)
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
				if err := a.executeToolCall(ctx, nil, iter, tc, allowedTools); err != nil {
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
				if err := a.executeToolCall(ctx, nil, iter, tc, allowedTools); err != nil {
					return err
				}
			} else {
				a.AICallProxyWriteToolResult(nil, strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name), "", nil, "", "tool not available in current phase", false)
			}
		}
		if !anyUsefulTool {
			return a.applyReplanResult(stepID, nil, nil, nil, snapshot, "")
		}
	}
}

// applyReplanDecision 把 step_replan 的复核结论承接进 phase 状态并构造 ReplanContext。
// 机械承接顺序（D7）：先 ApplyPhaseAssessments（completed/blocked 写回 PlanPhase.Status +
// blocked 联动 skip 其下 pending step + append kind=phase journal），再据 should_replan
// 决定是否回流 planner。
//
// 职责反转：step_replan 只维护账本 + 逐 phase 报告 assessment，不选下一个 phase；
// 各 lane 继续释放 step 还是收束由 planner 读 phase_assessments 自决。
func (a *Agent) applyReplanDecision(stepID string, decision stepReplanModelOutput, snapshot builtin_tools.StateSnapshot) error {
	// 先承接 phase 评估：写回 phase 状态、blocked 联动 skip、增量落 journal。
	if changed, updated := a.state.ApplyPhaseAssessments(decision.PhaseAssessments); len(changed) > 0 {
		snapshot = updated
		a.appendPlannerJournalPhaseRecords(changed, snapshot.PlanVersion)
	}

	if !decision.ShouldReplan {
		return a.applyReplanResult(stepID, &decision, nil, nil, snapshot, "")
	}

	// 汇总 continue 型 phase 的三轴缺口回流 planner（sticky ReplanContext 轴 = 各 lane 并集）。
	var incomplete, depth, surfaces []string
	for _, assess := range decision.PhaseAssessments {
		if assess == nil || assess.Status != builtin_tools.PhaseAssessContinue {
			continue
		}
		incomplete = append(incomplete, assess.IncompleteItems...)
		depth = append(depth, assess.DepthGaps...)
		surfaces = append(surfaces, assess.NewSurfaces...)
	}
	rc := &builtin_tools.ReplanContext{
		SourceStepID:     stepID,
		Reason:           decision.ReplanReason,
		IncompleteItems:  builtin_tools.NewAxisItems(incomplete),
		DepthGaps:        builtin_tools.NewAxisItems(depth),
		NewSurfaces:      builtin_tools.NewAxisItems(surfaces),
		ReplacePending:   true,
		PhaseAssessments: builtin_tools.ClonePhaseAssessments(decision.PhaseAssessments),
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
		// 重编排直达 Step：以新 plan 计算下一个可跑 frontier step；通常就是新增的第一条 pending。
		planForNextRunnable = newPlan
		if candidate := strings.TrimSpace(builtin_tools.NextFrontierPlanStepID(planForNextRunnable, snapshot.Phases)); candidate != "" {
			nextRunnableStepID = candidate
			nextPhase = builtin_tools.AgentPhaseStep
		}
	} else if replanContext != nil {
		nextPhase = builtin_tools.AgentPhasePlan
	} else if candidate := strings.TrimSpace(builtin_tools.NextFrontierPlanStepID(snapshot.Plan, snapshot.Phases)); candidate != "" {
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
		summaryGoal = strings.TrimSpace(modelOut.ReplanReason)
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
	if stepTimelineExists(a.workspaceRuntime, stepID) {
		timelineFile = stepTimelineRelPath(stepID)
	}
	// step 过程文件（think_act 按三节契约维护）：存在才填指针；旧布局 fallback 兼容老 session。
	var stepFile string
	if a.workspaceRuntime != nil {
		l := a.wsLayout()
		if stepFileExists(l, stepID) {
			stepFile = stepFileRelPath(stepID)
		} else if legacyStepFileExists(l, stepID) {
			stepFile = l.LegacyStepFileRel(stepID)
		}
	}
	coverageFile := a.persistCoverageChecklist(stepID, rawOutcome)

	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}

	// 防御断言：NewPlan 整盘替换前不应再有 in_progress inline peer 在跑——调度器的 X2 并发
	// 屏障（runtime_scheduler 进 step_replan 前 awaitRunningInlineSteps）已保证这点。若此处仍
	// 命中，说明屏障被绕过，整盘替换会与 peer 完成回写竞态（结果丢失 / plan_version 错配），
	// 落 error 日志以便定位（不阻断，交由 UpdateInlineStep 的终态守卫兜底）。
	if newPlan != nil && a.asyncRegistry != nil && a.asyncRegistry.HasRunningInlineSteps() {
		a.emitRuntimeLog("error", "step_replan applying NewPlan while inline peers still running", snapshot, map[string]any{
			"event":          "step_replan_newplan_inline_race",
			"step_id":        stepID,
			"running_inline": a.asyncRegistry.RunningInlineSteps(),
		})
	}

	// ApplyStepReplan 内部 diff → observer 自动 emit task_item，不再手抓 prevPlan。
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
	// 登记：current step 已由本路径固化落盘。X2 滚动收尾扫描（finalizeUnjournaledTerminalSteps）
	// 跳过 current、且对已登记者 no-op，故此 step 后续不再被扫描重复落盘。
	a.markStepJournaled(stepID)
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
		a.emitter.EmitTaskPlan(snapshot.Plan, snapshot.Phases, strings.TrimSpace(modelOut.ReplanReason))
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
	if strings.TrimSpace(builtin_tools.NextFrontierPlanStepID(snapshot.Plan, snapshot.Phases)) == "" {
		// 防御：frontier 枯竭（无可派发 ready）只代表“这批 frontier 跑完”，不代表“这批全落定”。
		// 仍有 in_progress inline peer 在跑时不升级——否则会基于不完整状态做 LLM 重规划。
		// 调度器的 frontier barrier 已先 await peer，正常流程下此处 HasRunningInlineSteps 必为 false；
		// 此条为纵深防御，屏障被绕过时退化为“跳过本轮、下一轮 peer 落定再升级”。
		if a.asyncRegistry != nil && a.asyncRegistry.HasRunningInlineSteps() {
			return false, ""
		}
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
// 默认 -1（禁用心跳，纯 per-batch：只在没有可跑 task_item 时即 plan_exhausted、或 step_error 升级），
// 可通过环境变量 STEP_REPLAN_HEARTBEAT_K 调整。触发语义：k>=0 且 consecutiveStepsSinceReplan >= K
// 时升级——K<0（默认）禁用心跳；K=0 退回每步复核（per-step）；K>0（如 3）为每 K 步的折中节流。
// 无论 K 取值，plan_exhausted（当前批次跑完、无可跑 task_item）与 step_error 恒触发。
func stepReplanHeartbeatK() int {
	const defaultK = -1
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
	l := a.wsLayout()
	if info, err := a.workspaceRuntime.Store().Stat(l.StepCoverageRel(stepID)); err == nil && !info.IsDir() {
		return l.StepCoverageRel(stepID)
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
	l := a.wsLayout()
	if err := a.workspaceRuntime.Store().Write(l.StepCoverageRel(stepID), data); err != nil {
		a.emitRuntimeLog("warn", "persist coverage checklist failed", a.state.Snapshot(), map[string]any{
			"event": "coverage_checklist_persist_failed", "step_id": stepID, "error": err.Error(),
		})
		return ""
	}
	return l.StepCoverageRel(stepID)
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
	if err := AppendPlannerJournalRecords(a.workspaceRuntime.RootDir(), []*builtin_tools.PlannerJournalRecord{
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
	return a.wsLayout().StepContexts()
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

// reviewWindowMaxCardsBaseline 是区间多卡软上限的下界。窗口可能远超 heartbeat K，
// 须截断保最新 N 张并在模板提示更早 step 从 PLANNER_JOURNAL / PLAN_OVERVIEW 回读。
// 实际上限由 reviewWindowMaxCards() 动态计算，避免反直觉的频繁截断。
const reviewWindowMaxCardsBaseline = 8

// reviewWindowMaxCardsBatchCeiling 是 per-batch（K<0）模式下窗口的硬上限：正常批次应被整批
// 覆盖（窗口=批次规模），但 resume 跨越大 plan 时窗口可能爆量，须截断到此上限并由 journal
// 指针兜底，防止 token 爆炸。（后续可改为按 contextWindow 缩放。）
const reviewWindowMaxCardsBatchCeiling = 32

// reviewWindowMaxCards 计算复核窗口的卡片软上限，total 为本次窗口实际收集到的卡片数：
//   - K<0（per-batch，默认）：clamp(total, baseline=8, ceiling=32)——覆盖整批，超大批截断到 ceiling。
//   - K==0（per-step）：baseline（窗口本就 1 张）。
//   - K>0（折中心跳）：max(K+3, baseline)，"+3" 为 plan_exhausted 多带尾部 step 的安全余量。
func reviewWindowMaxCards(total int) int {
	k := stepReplanHeartbeatK()
	if k < 0 {
		cap := total
		if cap < reviewWindowMaxCardsBaseline {
			cap = reviewWindowMaxCardsBaseline
		}
		if cap > reviewWindowMaxCardsBatchCeiling {
			cap = reviewWindowMaxCardsBatchCeiling
		}
		return cap
	}
	if k == 0 {
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

func buildReplanStepCard(current *builtin_tools.PlanItem, outcome *builtin_tools.StepOutcome, rt WorkspaceRuntime, resultPath, coveragePath string) *replanStepCard {
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
	if stepTimelineExists(rt, card.ID) {
		card.TimelineFile = rt.Layout().StepTimeline(card.ID)
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
func (a *Agent) buildReviewWindow(snapshot builtin_tools.StateSnapshot, boundaryStepID string, rt WorkspaceRuntime) *reviewWindow {
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
	maxCards := reviewWindowMaxCards(total)
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
		card := buildReplanStepCard(item, outcome, rt,
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
	l := a.wsLayout()
	absPath := l.TaskContext()
	if absPath == "" {
		return
	}
	if _, err := a.workspaceRuntime.Store().Stat(l.TaskContextRel()); err == nil {
		return
	}
	if err := a.workspaceRuntime.WriteFileRel(l.TaskContextRel(), []byte(taskContextSkeleton)); err != nil {
		runtimelog.LogJSON("warning", map[string]any{
			"event": "task_context_skeleton_write_failed",
			"path":  absPath,
			"error": err.Error(),
		})
	}
}

// readSharedFileOptional 与 readSharedFileForPrompt 同源读取共享区文件，但所有"无内容"分支
// （runtime 缺位 / 文件不存在 / 文件为空）一律返回 ""，让外层的 `HAS_XXX` gate（基于
// strings.TrimSpace != ""）正确判定"是否注入"。
//
// 读取经 rt.Store() 走 per-key 锁保护（task_context.md / open_items.md 与并发写者串行化）。
//
// 短路顺序保持与旧版语义一致：name 或 sharedDir 任一为空 → 不发起任何 IO，直接返回 ""。
// （review P1-5：避免 sharedDir == "" 时悄悄走 runtime IO 产生额外失败调用。）
func readSharedFileOptional(rt WorkspaceRuntime, name string) string {
	if rt == nil {
		return ""
	}
	l := rt.Layout()
	if l.SharedDir() == "" || strings.TrimSpace(name) == "" {
		return ""
	}
	data, err := rt.Store().Read(filepath.ToSlash(filepath.Join(l.SharedDirRel(), name)))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readSharedStepFileForPrompt(rt WorkspaceRuntime, stepID string, limitTokens int) string {
	if rt == nil {
		return ""
	}
	l := rt.Layout()
	if stepFileExists(l, stepID) {
		data, err := rt.Store().Read(l.StepFileRel(stepID))
		if err != nil {
			return ""
		}
		return previewStepFileForPrompt(string(data), l.StepFile(stepID), limitTokens)
	}
	// 旧布局 shared/<stepID>/step.md fallback（老 session resume）。
	if !legacyStepFileExists(l, stepID) {
		return ""
	}
	data, err := rt.Store().Read(l.LegacyStepFileRel(stepID))
	if err != nil {
		return ""
	}
	return previewStepFileForPrompt(string(data), l.LegacyStepFile(stepID), limitTokens)
}

// previewStepFileForPrompt 对 step 文件内容应用统一 preview；空内容保持返回空串
// （调用方以空串作缺失 gate，不能替换成「(文件为空)」占位）。
func previewStepFileForPrompt(raw, absPath string, limitTokens int) string {
	content := strings.TrimSpace(raw)
	if content == "" {
		return ""
	}
	return previewForPrompt(content, absPath, limitTokens)
}
