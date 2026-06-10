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
	// IncompleteItems 轴①完成度/存在性：本 step 自身声明目标内、根本没做的项，驱动补齐 replan。
	IncompleteItems []string `json:"incomplete_items"`
	// DepthGaps 轴②深度/质量：做了但不扎实的项（shallow_only 未深度确认、分析链条断裂、低价值项占位等），驱动深挖 replan。
	DepthGaps []string `json:"depth_gaps"`
	// NewSurfaces 轴③泛化扩面：对照整体任务目标的任务覆盖面全集，尚未被任何已完成工作覆盖的面，驱动扩面 replan。
	NewSurfaces []string `json:"new_surfaces"`
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

	// Scheme A: always run the StepReplan LLM loop.
	//
	// Rationale: the old fast-path skip logic only relied on self-reported signals like
	// open_questions/warnings/unresolved, which can be under-reported and cause replan to
	// "never trigger". We intentionally trade cost for correctness here.

	stepResultPath := a.resolveStepResultPath(stepID, rawOutcome)
	stepContextsPath := a.resolveStepContextsPath()
	stepTranscriptPath := ""
	if ref := strings.TrimSpace(a.lastStepTranscriptBlobRef); ref != "" && a.v2Store != nil {
		stepTranscriptPath = a.v2Store.BlobPath(ref)
	}
	stepTimelinePath := ""
	if a.workspaceRuntime != nil {
		sd := a.workspaceRuntime.SharedDir()
		if stepTimelineExists(sd, stepID) {
			stepTimelinePath = filepath.Join(sd, stepID, "timeline.jsonl")
		}
	}

	skillsCtx := a.buildSkillsPromptContext(ctx, snapshot)

	// 注入 prompt 前，为各 step outcome 补上绝对路径的 timeline_file（对齐 final_answer）。
	// 仅增强注入投影，不改 state（state 的回填仍由 applyReplanResult 用相对路径完成）。
	enrichedOutcomes := snapshot.StepOutcomes
	enrichedCurrent := rawOutcome
	if a.workspaceRuntime != nil {
		sharedDir := a.workspaceRuntime.SharedDir()
		enrichedOutcomes = make([]*builtin_tools.StepOutcome, len(snapshot.StepOutcomes))
		for i, o := range snapshot.StepOutcomes {
			if o == nil {
				continue
			}
			clone := *o // 浅拷贝：只写标量 TimelineFile，不触碰 slice 字段
			if clone.StepID != "" && stepTimelineExists(sharedDir, clone.StepID) {
				clone.TimelineFile = filepath.Join(sharedDir, clone.StepID, "timeline.jsonl")
			}
			enrichedOutcomes[i] = &clone
		}
		if c := findOutcome(enrichedOutcomes, stepID); c != nil {
			enrichedCurrent = c
		}
	}

	workspaceSharedDir := ""
	if a.workspaceRuntime != nil {
		workspaceSharedDir = strings.TrimSpace(a.workspaceRuntime.SharedDir())
	}

	fnTools, allowedTools := a.BuildFunctionTools(builtin_tools.AgentPhaseStepReplan)
	fnTools = append(fnTools, buildSubmitReplanFunctionTool())

	prompt, err := a.BuildStepReplanPrompt(map[string]any{
		"current_goal":             snapshot.CurrentGoal,
		"goal_understanding":       snapshot.GoalUnderstanding,
		"workspace_shared_dir":     workspaceSharedDir,
		"input_timeline":           snapshot.InputTimeline,
		"current_step":             current,
		"step_outcome":             enrichedCurrent,
		"task_plan":                snapshot.Plan,
		"step_outcomes":            enrichedOutcomes,
		"carried_incomplete_items": carriedAxisItems(snapshot.UnresolvedAxes, axisIncomplete),
		"carried_depth_gaps":       carriedAxisItems(snapshot.UnresolvedAxes, axisDepth),
		"carried_new_surfaces":     carriedAxisItems(snapshot.UnresolvedAxes, axisNewSurfaces),
		"step_result_path":         stepResultPath,
		"step_contexts_path":       stepContextsPath,
		"step_transcript_path":     stepTranscriptPath,
		"step_timeline_path":       stepTimelinePath,
		"skills_context":           skillsCtx,
		"available_tools":          functionToolsToAvailableInfo(fnTools),
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

		replanCtx, replanCancel := context.WithCancel(ctx)
		callResult, err := a.AICallProxy(replanCtx, iter, runClient, prompt, "", fnTools...)
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
			IncompleteItems: normalizeStringSlice(decision.IncompleteItems),
			DepthGaps:       normalizeStringSlice(decision.DepthGaps),
			NewSurfaces:     normalizeStringSlice(decision.NewSurfaces),
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
	coverageFile := a.persistCoverageChecklist(stepID, rawOutcome)

	planVersion := snapshot.PlanVersion
	if planVersion <= 0 {
		planVersion = 1
	}

	snapshot = a.state.ApplyStepReplan(stepID, stepReplanUpdate{
		ArtifactDir:       artifactDir,
		ContextKey:        contextKey,
		TimelineFile:      timelineFile,
		CoverageFile:      coverageFile,
		Namespace:         builtin_tools.NormalizeWorkspaceNamespace(a.workspaceNamespace),
		PlanVersion:       planVersion,
		TranscriptBlobRef: a.lastStepTranscriptBlobRef,
		CurrentGoal:       summaryGoal,
		Warnings:          replanWarnings,
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
						"items":       map[string]any{"type": "string"},
						"description": "轴①存在性/完成度：本 step 声明目标范围内、根本没做或仍悬而未决的项，驱动补齐。不含'做了但不扎实'（属 depth_gaps），也不含本 step 之外的新维度/全集遗漏（属 new_surfaces）。",
					},
					"depth_gaps": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "轴②深度/质量：做了但不扎实的项（shallow_only 未深度确认 / 分析链条断裂 / 悬而未决判断 / 低价值项占位 / 抽样冒充全量）。",
					},
					"new_surfaces": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "轴③泛化扩面：对照 GOAL_UNDERSTANDING 意图半径内、与用户核心目标语义相关的任务覆盖面全集（如 recon 检出且落在用户意图内的模块/接口），尚未被任何已完成工作覆盖的面；范围是整个任务而非当前 step，视角是任务覆盖完整性而非单点深挖。入列时按原则2.2 轻量去重（默认偏放行）：只剔除明确同 (维度×工作项) 对、前提未变、已扎实覆盖的重叠，同工作项不同维度/前序从未触及的新项/前提变化复测一律保留，拿不准也保留（禁止整方向折叠误杀新项）；已覆盖但浅的转入 depth_gaps。受 GOAL_UNDERSTANDING 范围边界约束（原则6 默认恒生效），意图外/明确不做项不计入此处、降级沉回 open_items.md 的 `## 不可解局限` 区。含原则5.1 逐项未覆盖的清单项、原则7 升进的可行动新面，驱动扩面 replan。",
					},
				},
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
	if result.ShouldReplan && strings.TrimSpace(result.NextGoal) == "" {
		return stepReplanModelOutput{}, fmt.Errorf("submit_plan replan: should_replan=true but next_goal is empty")
	}
	return result, nil
}
