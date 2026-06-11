package react

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type stepReplanModelOutput struct {
	ShouldReplan bool   `json:"should_replan"`
	ReplanReason string `json:"replan_reason"`
	NextGoal     string `json:"next_goal"`
	// 三轴为结构化条目（item/dimension/evidence/ledger_id/produces/consumes）；
	// AxisItem.UnmarshalJSON 兼容字符串形态，旧持久化无缝解析。
	// IncompleteItems 轴①完成度/存在性：本 step 自身声明目标内、根本没做的项，驱动补齐 replan。
	IncompleteItems []*builtin_tools.AxisItem `json:"incomplete_items"`
	// DepthGaps 轴②深度/质量：做了但不扎实的项（shallow_only 未深度确认、分析链条断裂、低价值项占位等），驱动深挖 replan。
	DepthGaps []*builtin_tools.AxisItem `json:"depth_gaps"`
	// NewSurfaces 轴③泛化扩面：对照整体任务目标的任务覆盖面全集，尚未被任何已完成工作覆盖的面，驱动扩面 replan。
	NewSurfaces []*builtin_tools.AxisItem `json:"new_surfaces"`
	// MaintenanceDirectives 落盘维护指令：核验发现的落盘缺漏（归档/账本增改/事实烘焙），
	// 由 runtime 维护执行器在进入下一节点之前机械执行（见 step_replan 账本复核与维护段）。
	MaintenanceDirectives []*builtin_tools.MaintenanceDirective `json:"maintenance_directives,omitempty"`
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

	// 简单分支直通（设计 2.1）：simple 单步任务完成后跳过三轴 LLM 判定直达验收；
	// 机械落盘（digest 归约 / plan_item 烘焙 / journal / step_contexts）由
	// applyReplanResult 保留执行，final_answer 仍持有 should_replan 回流兜底。
	if snapshot.SimpleTask && len(snapshot.Plan) == 1 {
		a.emitRuntimeLog("info", "simple task bypasses step replan", snapshot, map[string]any{
			"event":   "step_replan_bypassed_simple",
			"step_id": stepID,
		})
		return a.applyReplanResult(stepID, nil, nil, snapshot, "")
	}

	// Scheme A: always run the StepReplan LLM loop.
	//
	// Rationale: the old fast-path skip logic only relied on self-reported signals like
	// open_questions/warnings/unresolved, which can be under-reported and cause replan to
	// "never trigger". We intentionally trade cost for correctness here.

	workspaceSharedDir := ""
	if a.workspaceRuntime != nil {
		workspaceSharedDir = strings.TrimSpace(a.workspaceRuntime.SharedDir())
	}

	// digest 归约前移：判定 prompt 必须看到 runtime 权威 digest（applyReplanResult 处
	// 的归约保持兜底，二次归约幂等）。
	if reduced := reduceStepTimelineToolCallsDigest(workspaceSharedDir, stepID); len(reduced) > 0 {
		rawOutcome.ToolCallsDigest = reduced
	}

	stepResultPath := a.resolveStepResultPath(stepID, rawOutcome)
	stepContextsPath := a.resolveStepContextsPath()
	stepTranscriptPath := ""
	if ref := strings.TrimSpace(a.lastStepTranscriptBlobRef); ref != "" && a.v2Store != nil {
		stepTranscriptPath = a.v2Store.BlobPath(ref)
	}
	stepTimelinePath := ""
	if stepTimelineExists(workspaceSharedDir, stepID) {
		stepTimelinePath = filepath.Join(workspaceSharedDir, stepID, "timeline.jsonl")
	}
	archivePath := ""
	if workspaceSharedDir != "" {
		archivePath = filepath.Join(workspaceSharedDir, openItemsArchiveFileName)
	}

	skillsCtx := a.buildSkillsPromptContext(ctx, snapshot)

	fnTools, allowedTools := a.BuildFunctionTools(builtin_tools.AgentPhaseStepReplan)
	fnTools = append(fnTools, buildSubmitReplanFunctionTool())

	prompt, err := a.BuildStepReplanPrompt(map[string]any{
		"current_goal":            snapshot.CurrentGoal,
		"goal_understanding":      snapshot.GoalUnderstanding,
		"input_timeline":          snapshot.InputTimeline,
		"current_step_card":       buildReplanStepCard(current, rawOutcome, workspaceSharedDir, stepResultPath),
		"plan_overview":           buildPlanOverview(snapshot.Plan),
		"open_items_ledger":       readSharedFileForPrompt(workspaceSharedDir, openItemsFileName),
		"task_context_board":      readSharedFileForPrompt(workspaceSharedDir, taskContextFileName),
		"step_file_content":       readSharedStepFileForPrompt(workspaceSharedDir, stepID),
		"step_result_path":        stepResultPath,
		"step_contexts_path":      stepContextsPath,
		"step_transcript_path":    stepTranscriptPath,
		"step_timeline_path":      stepTimelinePath,
		"open_items_archive_path": archivePath,
		"skills_context":          skillsCtx,
		"available_tools":         functionToolsToAvailableInfo(fnTools),
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

		// 硬上限：降级提示后仍不提交，保守按「无需重规划」收尾，避免判定节点失控空转。
		if round >= judgmentExplorationBudget+judgmentGraceRounds {
			a.emitRuntimeLog("warn", "step replan exploration budget exhausted", snapshot, map[string]any{
				"event":   "step_replan_exploration_budget_exhausted",
				"step_id": stepID,
				"rounds":  round,
			})
			return a.applyReplanResult(stepID, nil, nil, snapshot, "")
		}

		replanCtx, replanCancel := context.WithCancel(ctx)
		callResult, err := a.AICallProxy(replanCtx, iter, runClient, prompt, promptFamilyStepReplan, fnTools...)
		replanCancel()
		if err != nil {
			return fmt.Errorf("step replan AICallProxy failed: %w", err)
		}

		// Replan 允许空响应：语义为"不需要重规划"，默认继续当前计划。
		if len(callResult.ToolCalls) == 0 {
			return a.applyReplanResult(stepID, nil, nil, snapshot, "")
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
				return a.applyReplanDecision(stepID, decision, snapshot)
			}
			if _, ok := allowedTools[strings.TrimSpace(tc.Function.Name)]; ok {
				anyUsefulTool = true
				// 探索预算（无损兜底的上限与降级，设计 6.3）：超出后拒绝继续回读，
				// 要求基于已得信息降级裁决（digest-only + 账本待复核），不静默丢失。
				if round >= judgmentExplorationBudget {
					a.AICallProxyWriteToolResult(
						strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name),
						"", nil, "", stepReplanBudgetNotice, false,
					)
					continue
				}
				if err := a.executeToolCall(ctx, iter, tc, allowedTools); err != nil {
					return err
				}
			} else {
				a.AICallProxyWriteToolResult(strings.TrimSpace(tc.Id), strings.TrimSpace(tc.Function.Name), "", nil, "", "tool not available in current phase", false)
			}
		}
		if !anyUsefulTool {
			return a.applyReplanResult(stepID, nil, nil, snapshot, "")
		}
	}
}

// 判定节点（step_replan / final_answer）的探索预算：默认消费已注入的 digest 与共享区
// 全文即可裁决，按需回读 timeline / 产物文件以「轮」为预算；超出预算拒绝继续回读并
// 要求降级裁决，再宽限若干轮仍不提交则保守收尾。
const (
	judgmentExplorationBudget = 4
	judgmentGraceRounds       = 3
)

const stepReplanBudgetNotice = "回读/探索轮次已达上限：不要再调用查证工具。" +
	"立即基于已注入与已读取的信息调用 submit_plan 裁决；无法核验的项按 digest-only 保守判定" +
	"（拿不准偏放行进对应轴），并用 maintenance_directives 的 ledger_add 写入账本待复核。"

func (a *Agent) applyReplanDecision(stepID string, decision stepReplanModelOutput, snapshot builtin_tools.StateSnapshot) error {
	var replanContext *builtin_tools.ReplanContext
	if decision.ShouldReplan {
		nextGoal := strings.TrimSpace(decision.NextGoal)
		if nextGoal == "" {
			nextGoal = strings.TrimSpace(snapshot.CurrentGoal)
		}
		replanContext = &builtin_tools.ReplanContext{
			SourceStepID:    stepID,
			Reason:          strings.TrimSpace(decision.ReplanReason),
			NextGoal:        nextGoal,
			IncompleteItems: builtin_tools.NormalizeAxisItems(decision.IncompleteItems),
			DepthGaps:       builtin_tools.NormalizeAxisItems(decision.DepthGaps),
			NewSurfaces:     builtin_tools.NormalizeAxisItems(decision.NewSurfaces),
			ReplacePending:  true,
		}
	}
	return a.applyReplanResult(stepID, &decision, replanContext, snapshot, "")
}

func (a *Agent) applyReplanResult(stepID string, modelOut *stepReplanModelOutput, replanContext *builtin_tools.ReplanContext, snapshot builtin_tools.StateSnapshot, artifactDir string) error {
	current := snapshot.CurrentStep()

	nextPhase := builtin_tools.AgentPhaseFinalAnswer
	nextRunnableStepID := ""
	if replanContext != nil {
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
	if replanContext != nil {
		summaryGoal = strings.TrimSpace(replanContext.NextGoal)
	}

	// 维护指令先于状态推进执行（进入下一节点之前），保证下游读到最新共享区；
	// 执行 warnings 并入本轮 Warnings 注入下游（失败不阻塞、显式可见）。
	var maintenanceWarnings []string
	if modelOut != nil {
		maintenanceWarnings = a.executeMaintenanceDirectives(stepID, modelOut.MaintenanceDirectives)
	}

	var replanWarnings []string
	var replanAxes *builtin_tools.ReplanAxes
	if replanContext != nil {
		replanWarnings = replanContext.Warnings
		// replanContext != nil 时写三轴 sticky 状态；nil 时不写，保留上一次滚下来的三轴。
		replanAxes = &builtin_tools.ReplanAxes{
			IncompleteItems: replanContext.IncompleteItems,
			DepthGaps:       replanContext.DepthGaps,
			NewSurfaces:     replanContext.NewSurfaces,
		}
	}

	rawOutcome := findOutcome(snapshot.StepOutcomes, stepID)

	// tool_calls_digest 以 runtime 对 timeline 的规则归约为权威来源；模型自报仅在
	// timeline 缺失（如无任何工具调用）时作兜底。
	if rawOutcome != nil && a.workspaceRuntime != nil {
		if reduced := reduceStepTimelineToolCallsDigest(a.workspaceRuntime.SharedDir(), stepID); len(reduced) > 0 {
			rawOutcome.ToolCallsDigest = reduced
		}
	}

	contextKey := a.resolveStepContextKey(stepID, rawOutcome, snapshot)

	var timelineFile string
	if a.workspaceRuntime != nil && stepTimelineExists(a.workspaceRuntime.SharedDir(), stepID) {
		timelineFile = stepTimelineRelPath(stepID)
	}
	// step 过程文件（think_act 按 6.4 模板维护）：存在才填指针。
	var stepFile string
	if a.workspaceRuntime != nil && stepSharedFileExists(a.workspaceRuntime.SharedDir(), stepID, "step.md") {
		stepFile = fmt.Sprintf("shared/%s/step.md", stepID)
	}
	coverageFile := a.persistCoverageChecklist(stepID, rawOutcome)

	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}

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
		Warnings:          append(replanWarnings, maintenanceWarnings...),
		UnresolvedAxes:    replanAxes,
		ReplanContext:     replanContext,
		NextPhase:         nextPhase,
	})
	a.lastStepTranscriptBlobRef = ""

	a.appendStepContextRecord(stepID, snapshot)
	a.appendPlannerJournalStepRecord(stepID, snapshot)

	a.emitter.EmitStateChange(snapshot)

	if rawOutcome != nil {
		a.emitter.EmitStepSummaryResult(stepID, strings.TrimSpace(current.Step), rawOutcome)
	}
	if modelOut != nil {
		a.emitter.EmitStepReplanResult(stepID, strings.TrimSpace(current.Step), modelOut)
	}

	a.emitRuntimeLog("info", "step replan completed", snapshot, map[string]any{
		"event":         "step_replan_completed",
		"step_id":       stepID,
		"next_phase":    nextPhase,
		"next_step_id":  nextRunnableStepID,
		"should_replan": replanContext != nil,
		"artifact_dir":  artifactDir,
	})
	return nil
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

// appendPlannerJournalStepRecord 在 step 终态产出烘焙完成后，把该 plan_item 增量 append 到
// planner.jsonl（plan 真相源；同 id 最后一条胜出）。
func (a *Agent) appendPlannerJournalStepRecord(stepID string, snapshot builtin_tools.StateSnapshot) {
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
	planVersion := snapshot.PlanVersion
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
	OpenItemIDs       []string                              `json:"open_item_ids,omitempty"`
	References        []string                              `json:"references,omitempty"`
	Error             string                                `json:"error,omitempty"`
	ResultFile        string                                `json:"result_file,omitempty"`
	TimelineFile      string                                `json:"timeline_file,omitempty"`
}

func buildReplanStepCard(current *builtin_tools.PlanItem, outcome *builtin_tools.StepOutcome, sharedDir, resultPath string) *replanStepCard {
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
		OpenItemIDs:       outcome.OpenItemIDs,
		References:        outcome.References,
		Error:             strings.TrimSpace(outcome.Error),
		ResultFile:        strings.TrimSpace(resultPath),
	}
	if stepTimelineExists(sharedDir, card.ID) {
		card.TimelineFile = filepath.Join(sharedDir, card.ID, "timeline.jsonl")
	}
	return card
}

type planOverviewEntry struct {
	ID        string   `json:"id"`
	Step      string   `json:"step"`
	Status    string   `json:"status"`
	DependsOn []string `json:"depends_on,omitempty"`
}

func buildPlanOverview(plan []*builtin_tools.PlanItem) []planOverviewEntry {
	out := make([]planOverviewEntry, 0, len(plan))
	for _, item := range plan {
		if item == nil {
			continue
		}
		out = append(out, planOverviewEntry{
			ID:        strings.TrimSpace(item.ID),
			Step:      strings.TrimSpace(item.Step),
			Status:    strings.TrimSpace(string(item.Status)),
			DependsOn: item.DependsOn,
		})
	}
	return out
}

// readSharedFileForPrompt 读取共享区文件全文用于注入；缺失时返回占位说明（不报错）。
func readSharedFileForPrompt(sharedDir, name string) string {
	if sharedDir == "" {
		return "(共享区不可用)"
	}
	data, err := os.ReadFile(filepath.Join(sharedDir, name))
	if err != nil {
		return "(文件尚不存在)"
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "(文件为空)"
	}
	return content
}

func readSharedStepFileForPrompt(sharedDir, stepID string) string {
	if !stepSharedFileExists(sharedDir, stepID, "step.md") {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(sharedDir, stepID, "step.md"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func buildSubmitReplanFunctionTool() *ai.FunctionTool {
	return &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        submitPlanToolName,
			Description: "当你完成评估、准备好输出重规划决策时，调用此工具提交。参数即为决策的结构化内容。",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"should_replan", "replan_reason", "next_goal", "incomplete_items", "depth_gaps", "new_surfaces"},
				"properties": map[string]any{
					"should_replan": map[string]any{
						"type":        "boolean",
						"description": "是否需要回流重新规划。仅当出现新攻击面/缺口且 agent 仍能继续执行补齐时为 true。",
					},
					"replan_reason": map[string]any{
						"type":        "string",
						"description": "should_replan=false 时填空字符串；true 时填一句人类可读的总括说明。",
					},
					"next_goal": map[string]any{
						"type":        "string",
						"description": "should_replan=false 时填空字符串；true 时填明确的下一轮目标，不要写「等待用户输入」。",
					},
					"incomplete_items": map[string]any{
						"type":        "array",
						"items":       submitReplanAxisItemSchema(),
						"description": "轴①存在性/完成度：本 step 声明目标范围内、根本没做或仍悬而未决的项，驱动补齐。不含'做了但不扎实'（属 depth_gaps），也不含本 step 之外的新维度/全集遗漏（属 new_surfaces）。",
					},
					"depth_gaps": map[string]any{
						"type":        "array",
						"items":       submitReplanAxisItemSchema(),
						"description": "轴②深度/质量：做了但不扎实的项（shallow_only 未深度确认 / 分析链条断裂 / 悬而未决判断 / 低价值项占位 / 抽样冒充全量），驱动深挖。即使轴①为空也须独立判定。",
					},
					"new_surfaces": map[string]any{
						"type":        "array",
						"items":       submitReplanAxisItemSchema(),
						"description": "轴③泛化扩面：对照 GOAL_UNDERSTANDING 意图半径内的任务覆盖面全集，尚未被任何已完成工作覆盖的面；范围是整个任务而非当前 step。入列前轻量去重（默认偏放行：仅剔除同 (维度×工作项) 对且前提未变且已扎实覆盖的重叠，新项/新维度/前提变化复测/拿不准一律保留，禁止整方向折叠）；已覆盖但浅的转 depth_gaps。受意图半径约束（恒生效），意图外项不计入、改用 maintenance_directives 的 ledger_add 降级落账本不可解局限区。含声明产出清单逐项比对出的未覆盖项与账本复核升进的可行动项。",
					},
					"maintenance_directives": map[string]any{
						"type":        "array",
						"description": "可选：核验发现的落盘缺漏维护指令，由 runtime 在进入下一节点之前机械执行。类型：ledger_add（账本新增，target 留空自动取号）/ ledger_update（target=OI-id，content=更新批注）/ archive_item（target=OI-id 闭环归档，evidence=闭环证据）/ context_bake（content 写入 task_context 执行中补充）/ merge_staging（提示暂存区待归并）。",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"type": map[string]any{
									"type": "string",
									"enum": []any{"archive_item", "ledger_add", "ledger_update", "merge_staging", "context_bake"},
								},
								"target":   map[string]any{"type": "string", "description": "OI-id 或 task_context 节名（按类型）"},
								"content":  map[string]any{"type": "string"},
								"evidence": map[string]any{"type": "string"},
							},
							"required":             []string{"type"},
							"additionalProperties": false,
						},
					},
				},
			},
		},
	}
}

// submitReplanAxisItemSchema 是三轴结构化条目的 function-calling schema（附录 A.2）。
// evidence 必填承载 evidence-grounded 判据；ledger_id 是与账本的对账键。
func submitReplanAxisItemSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"item", "evidence"},
		"properties": map[string]any{
			"item": map[string]any{
				"type":        "string",
				"description": "工作项描述；引用了事实板已确认具体值的对象时把值内联进文本本身，下游无需重新发现。",
			},
			"evidence": map[string]any{
				"type":        "string",
				"description": "触发该条目的观测事实锚点：digest 行 / 账本 OI-id / 事实板条目 / 清单项 / 角色职责维度。无观测锚点的纯类比扩散不得入轴。",
			},
			"dimension": map[string]any{
				"type":        "string",
				"description": "可选：检查维度标签，供跨 step 去重的 (维度×工作项) 对照。",
			},
			"ledger_id": map[string]any{
				"type":        "string",
				"description": "可选：对应账本条目的 OI-id（对账键）；本轮 ledger_add 新增且未取号的可留空。",
			},
			"produces": map[string]any{
				"type":        "string",
				"description": "可选：该项预期产物描述，供 planner 产物-消费依赖排序。",
			},
			"consumes": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "可选：该项依赖的前置产物列表。",
			},
		},
		"additionalProperties": false,
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
	if result.ShouldReplan && strings.TrimSpace(result.NextGoal) == "" {
		return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: should_replan=true but next_goal is empty")
	}
	return result, nil
}
