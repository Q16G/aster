package react

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"aster/internal/builtin_tools"
)

type durableResumeProbe struct {
	HasCheckpoint bool

	WorkspaceRootDir   string
	WorkspaceNamespace string

	PlanCurrent     *planCurrentCheckpoint
	WorkspaceState  *builtin_tools.WorkspaceState
	FinalAssessment *FinalAssessmentArtifact
	FinalSeq        int

	Snapshot builtin_tools.StateSnapshot

	PlanValid          bool
	DeliverableFinal   bool
	InProgressStepID   string
	NextRunnableStepID string
	AllStepsTerminal   bool
	AllStepsCompleted  bool
}

func probeDurableResume(workspaceRootDir string, workspaceNamespace string) (durableResumeProbe, error) {
	workspaceRootDir = strings.TrimSpace(workspaceRootDir)
	workspaceNamespace = strings.TrimSpace(workspaceNamespace)

	probe := durableResumeProbe{
		WorkspaceRootDir:   workspaceRootDir,
		WorkspaceNamespace: workspaceNamespace,
	}
	if workspaceRootDir == "" {
		return probe, nil
	}

	runtime, err := newLocalWorkspaceRuntime("", workspaceRootDir, workspaceNamespace)
	if err != nil {
		return probe, err
	}

	writer, err := newArtifactWriter(runtime)
	if err != nil {
		return probe, err
	}

	planCurrent, _ := writer.LoadPlanCurrentCheckpoint()
	workspaceState, _ := writer.LoadWorkspaceState()
	finalAssessment, finalSeq, _ := LoadLatestFinalAssessment(writer, workspaceState, planCurrent)

	probe.PlanCurrent = planCurrent
	probe.WorkspaceState = workspaceState
	probe.FinalAssessment = finalAssessment
	probe.FinalSeq = finalSeq

	snapshot, planValid := synthesizeResumeSnapshot(writer, planCurrent, workspaceState, finalAssessment, finalSeq)
	probe.Snapshot = snapshot
	probe.PlanValid = planValid
	probe.HasCheckpoint = hasAnyCheckpoint(planCurrent, workspaceState, finalAssessment, snapshot)

	if planValid && len(snapshot.Plan) > 0 {
		probe.AllStepsTerminal = builtin_tools.AllPlanStepsTerminal(snapshot.Plan)
		probe.AllStepsCompleted = builtin_tools.AllPlanStepsCompleted(snapshot.Plan)
		probe.NextRunnableStepID = strings.TrimSpace(builtin_tools.NextRunnablePlanStepID(snapshot.Plan))
		for _, it := range snapshot.Plan {
			if it == nil {
				continue
			}
			if it.Status == builtin_tools.PlanStepInProgress {
				probe.InProgressStepID = strings.TrimSpace(it.ID)
				break
			}
		}
	}

	probe.DeliverableFinal = isDeliverableFinal(snapshot)
	return probe, nil
}

func hasAnyCheckpoint(planCurrent *planCurrentCheckpoint, workspaceState *builtin_tools.WorkspaceState, finalAssessment *FinalAssessmentArtifact, snapshot builtin_tools.StateSnapshot) bool {
	if finalAssessment != nil {
		return true
	}
	if planCurrent != nil && planCurrent.PlanVersion > 0 {
		return true
	}
	if workspaceState != nil && (len(workspaceState.LatestStepOutcomes) > 0 || workspaceState.LatestFinalSeq > 0) {
		return true
	}
	if len(snapshot.Plan) > 0 || len(snapshot.StepOutcomes) > 0 || snapshot.FinalAnswer != nil {
		return true
	}
	return false
}

func LoadLatestFinalAssessment(writer *artifactWriter, workspaceState *builtin_tools.WorkspaceState, planCurrent *planCurrentCheckpoint) (*FinalAssessmentArtifact, int, error) {
	if writer == nil {
		return nil, 0, nil
	}

	candidates := make([]int, 0, 4)
	addSeq := func(seq int) {
		if seq <= 0 {
			return
		}
		for _, existing := range candidates {
			if existing == seq {
				return
			}
		}
		candidates = append(candidates, seq)
	}
	if workspaceState != nil {
		addSeq(workspaceState.LatestFinalSeq)
	}
	if planCurrent != nil {
		addSeq(planCurrent.LatestFinalSeq)
	}
	if seq := maxFinalSeqInNamespace(writer); seq > 0 {
		addSeq(seq)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i] > candidates[j]
	})

	for _, seq := range candidates {
		raw, err := readFinalArtifactWithLegacy(writer, writer.finalAssessmentFileRel(seq), writer.layout.LegacyFinalAssessmentRel(seq))
		if err != nil {
			// Non-fatal: resume can still work without final assessment.
			continue
		}
		var artifact FinalAssessmentArtifact
		if err := json.Unmarshal(raw, &artifact); err != nil {
			continue
		}
		return &artifact, seq, nil
	}
	return nil, 0, nil
}

// readFinalArtifactWithLegacy 先读新布局 rel，缺失时回退旧 artifacts/root/ 布局
//（legacyRel 为空表示无回退，例如子 namespace）。
func readFinalArtifactWithLegacy(writer *artifactWriter, rel, legacyRel string) ([]byte, error) {
	raw, err := writer.ReadFileRel(rel)
	if err == nil {
		return raw, nil
	}
	if !os.IsNotExist(err) || legacyRel == "" {
		return nil, err
	}
	return writer.ReadFileRel(legacyRel)
}

func maxFinalSeqInNamespace(writer *artifactWriter) int {
	if writer == nil {
		return 0
	}
	maxSeq := maxFinalSeqInDir(filepath.Join(writer.sessionRoot, filepath.FromSlash(writer.layout.FinalRootRel())))
	// 旧布局 artifacts/root/final 一并纳入（存量 session resume 序号不回退）。
	if legacyRel := writer.layout.LegacyFinalRootRel(); legacyRel != "" {
		if legacyMax := maxFinalSeqInDir(filepath.Join(writer.sessionRoot, filepath.FromSlash(legacyRel))); legacyMax > maxSeq {
			maxSeq = legacyMax
		}
	}
	return maxSeq
}

func maxFinalSeqInDir(absDir string) int {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return 0
	}
	maxSeq := 0
	for _, entry := range entries {
		if entry == nil || !entry.IsDir() {
			continue
		}
		seq, err := strconv.Atoi(strings.TrimSpace(entry.Name()))
		if err != nil || seq <= 0 {
			continue
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	return maxSeq
}

func synthesizeResumeSnapshot(writer *artifactWriter, planCurrent *planCurrentCheckpoint, workspaceState *builtin_tools.WorkspaceState, finalAssessment *FinalAssessmentArtifact, finalSeq int) (builtin_tools.StateSnapshot, bool) {
	now := time.Now()
	snapshot := builtin_tools.StateSnapshot{
		Phase:     builtin_tools.AgentPhasePlan,
		Status:    builtin_tools.TaskStatusPreparing,
		UpdatedAt: now,
	}

	// 0) workspace/planner.jsonl —— plan 唯一真相源（step 终态即时 append，比任何
	//    checkpoint 快照都新）。journal 缺失（旧 session）时回退 assessed_state。
	if writer != nil {
		if items, phases, version, err := LoadPlannerJournalSnapshot(writer.sessionRoot); err == nil && len(items) > 0 {
			snapshot.Plan = items
			snapshot.Phases = phases
			snapshot.PlanVersion = version
		}
	}

	// 1) final_assessment.assessed_state (strongest payload for outcomes; plan 仅作
	//    journal 缺失时的回退来源)
	if finalAssessment != nil {
		payload := finalAssessment.AssessedState
		if strings.TrimSpace(string(payload.Status)) != "" {
			snapshot.Status = payload.Status
		}
		snapshot.Error = strings.TrimSpace(payload.StateError)
		snapshot.InputTimeline = payload.InputTimeline
		snapshot.NeedsPlanning = payload.NeedsPlanning
		if len(snapshot.Plan) == 0 {
			snapshot.Plan = payload.Plan
			snapshot.PlanVersion = payload.PlanVersion
		}
		if len(snapshot.Phases) == 0 {
			snapshot.Phases = builtin_tools.ClonePlanPhases(payload.Phases)
		}
		snapshot.StepOutcomes = payload.StepOutcomes
		snapshot.ExternalInterrupt = builtin_tools.CloneExternalInterrupt(payload.ExternalInterrupt)
		snapshot.Warnings = payload.Warnings
		snapshot.UnresolvedAxes = builtin_tools.CloneReplanAxes(payload.UnresolvedAxes)
		snapshot.ReplanContext = builtin_tools.CloneReplanContext(payload.ReplanContext)
		snapshot.ActiveSkillNames = builtin_tools.CloneStringSlice(payload.ActiveSkillNames)
		snapshot.ActiveMCPServers = builtin_tools.CloneStringSlice(payload.ActiveMCPServers)
	}

	// 2) workspace/state.json + latest pointers (mostly indices)
	if workspaceState != nil {
		if strings.TrimSpace(string(snapshot.Status)) == "" && strings.TrimSpace(string(workspaceState.Status)) != "" {
			snapshot.Status = workspaceState.Status
		}
		if snapshot.PlanVersion <= 0 && workspaceState.CurrentPlanVersion > 0 {
			snapshot.PlanVersion = workspaceState.CurrentPlanVersion
		}
		if len(snapshot.Warnings) == 0 && len(workspaceState.Warnings) > 0 {
			snapshot.Warnings = builtin_tools.CloneStringSlice(workspaceState.Warnings)
		}
		if builtin_tools.ReplanAxesEmpty(snapshot.UnresolvedAxes) && !builtin_tools.ReplanAxesEmpty(workspaceState.UnresolvedAxes) {
			snapshot.UnresolvedAxes = builtin_tools.CloneReplanAxes(workspaceState.UnresolvedAxes)
		}
		if snapshot.ReplanContext == nil && workspaceState.ReplanContext != nil {
			snapshot.ReplanContext = builtin_tools.CloneReplanContext(workspaceState.ReplanContext)
		}
		if len(snapshot.ActiveSkillNames) == 0 && len(workspaceState.ActiveSkillNames) > 0 {
			snapshot.ActiveSkillNames = builtin_tools.CloneStringSlice(workspaceState.ActiveSkillNames)
		}
		if len(snapshot.ActiveMCPServers) == 0 && len(workspaceState.ActiveMCPServers) > 0 {
			snapshot.ActiveMCPServers = builtin_tools.CloneStringSlice(workspaceState.ActiveMCPServers)
		}
	}

	// 3) plan/current.json (durable skeleton)
	if planCurrent != nil {
		if snapshot.Phase == "" || snapshot.Phase == builtin_tools.AgentPhasePlan {
			if planCurrent.Phase != "" {
				snapshot.Phase = planCurrent.Phase
			}
		}
		if snapshot.PlanVersion <= 0 && planCurrent.PlanVersion > 0 {
			snapshot.PlanVersion = planCurrent.PlanVersion
		}
		if strings.TrimSpace(string(snapshot.Status)) == "" && strings.TrimSpace(string(planCurrent.Status)) != "" {
			snapshot.Status = planCurrent.Status
		}
		if strings.TrimSpace(snapshot.StatusSummary) == "" && strings.TrimSpace(planCurrent.StatusSummary) != "" {
			snapshot.StatusSummary = strings.TrimSpace(planCurrent.StatusSummary)
		}
		if strings.TrimSpace(snapshot.CurrentGoal) == "" && strings.TrimSpace(planCurrent.CurrentGoal) != "" {
			snapshot.CurrentGoal = strings.TrimSpace(planCurrent.CurrentGoal)
		}
		if strings.TrimSpace(snapshot.GoalUnderstanding) == "" && strings.TrimSpace(planCurrent.GoalUnderstanding) != "" {
			snapshot.GoalUnderstanding = strings.TrimSpace(planCurrent.GoalUnderstanding)
		}
		if len(snapshot.Phases) == 0 && len(planCurrent.Phases) > 0 {
			snapshot.Phases = builtin_tools.ClonePlanPhases(planCurrent.Phases)
		}
		if len(snapshot.InputTimeline) == 0 && len(planCurrent.InputTimeline) > 0 {
			snapshot.InputTimeline = planCurrent.InputTimeline
		}
		if len(snapshot.ActiveMCPServers) == 0 && len(planCurrent.ActiveMCPServers) > 0 {
			snapshot.ActiveMCPServers = builtin_tools.CloneStringSlice(planCurrent.ActiveMCPServers)
		}
		if strings.TrimSpace(snapshot.CurrentStepID) == "" && strings.TrimSpace(planCurrent.CurrentStepID) != "" {
			snapshot.CurrentStepID = strings.TrimSpace(planCurrent.CurrentStepID)
		}
		if snapshot.ReplanContext == nil && planCurrent.ReplanContext != nil {
			snapshot.ReplanContext = builtin_tools.CloneReplanContext(planCurrent.ReplanContext)
		}
		if len(snapshot.ActiveSkillNames) == 0 && len(planCurrent.ActiveSkillNames) > 0 {
			snapshot.ActiveSkillNames = builtin_tools.CloneStringSlice(planCurrent.ActiveSkillNames)
		}
	}

	// Fill goal from timeline if still missing.
	if strings.TrimSpace(snapshot.CurrentGoal) == "" && snapshot.ReplanContext != nil {
		snapshot.CurrentGoal = strings.TrimSpace(snapshot.ReplanContext.NextGoal)
	}
	if strings.TrimSpace(snapshot.CurrentGoal) == "" && len(snapshot.InputTimeline) > 0 {
		last := snapshot.InputTimeline[len(snapshot.InputTimeline)-1]
		if last != nil {
			snapshot.CurrentGoal = strings.TrimSpace(last.Content)
		}
	}

	// Best-effort final answer hydration from final_assessment.json.
	// Note: assessed_state.status is the status *before* final decision is applied. The terminal status
	// should be derived from assessment.status/is_complete instead.
	if finalAssessment != nil && finalSeq > 0 {
		decision := normalizeFinalAnswerDecision(finalAssessment.Assessment)
		if decision.isTerminal {
			snapshot.Status = decision.status
			snapshot.Phase = builtin_tools.AgentPhaseFinalAnswer
		}

		if decision.isTerminal {
			finalText := strings.TrimSpace(decision.model.UserMessage)
			if writer != nil {
				if raw, err := readFinalArtifactWithLegacy(writer, writer.finalAnswerFileRel(finalSeq), writer.layout.LegacyFinalAnswerRel(finalSeq)); err == nil {
					if text := strings.TrimSpace(string(raw)); text != "" {
						finalText = text
					}
				}
			}
			if finalText != "" {
				snapshot.FinalAnswer = &builtin_tools.FinalAnswer{
					Content:    strings.TrimSpace(finalText),
					Source:     "final_assessment",
					CreatedAt:  now,
					References: builtin_tools.CloneStringSlice(decision.model.References),
				}
				if strings.TrimSpace(snapshot.StatusSummary) == "" {
					snapshot.StatusSummary = firstNonEmpty(strings.TrimSpace(decision.model.Reason), strings.TrimSpace(finalText))
				}
			}
		}
	}

	// Hydrate step outcomes from workspace pointers if final_assessment didn't contain them.
	if len(snapshot.StepOutcomes) == 0 && workspaceState != nil && len(workspaceState.LatestStepOutcomes) > 0 {
		snapshot.StepOutcomes = loadStepOutcomesFromPointers(writer, workspaceState.LatestStepOutcomes)
	}

	planValid := false
	if len(snapshot.Plan) > 0 {
		normalized, err := builtin_tools.NormalizePlanItems(snapshot.Plan, true)
		if err == nil && len(normalized) > 0 {
			planValid = true
			snapshot.Plan = normalized
			builtin_tools.HydratePlanRelations(snapshot.Plan)
		}
	}

	if planValid {
		// 旧数据无 phases（journal 无 phase 行、checkpoint 无 phases 字段）时
		// 合成 synthetic phase 并把缺挂靠的 item 收编，保证 frontier 调度有效。
		snapshot.Phases = builtin_tools.SynthesizePhasesIfMissing(snapshot.Plan, snapshot.Phases, snapshot.CurrentGoal)
		// blocked phase 的 step-skip 只落内存不落 journal——恢复时重放一次，让被 blocked
		// 收束的 lane 下 pending step 自愈为 skipped（含跨 phase 下游传递），
		// 防止 journal 里残留的 stale pending step 在 resume 后复活执行。
		builtin_tools.SkipStepsOfBlockedPhases(snapshot.Plan, snapshot.Phases)

		applyDurableOutcomesToPlan(snapshot.Plan, snapshot.StepOutcomes, workspaceState)

		// Resolve current_step_id for resume:
		// - never point to terminal steps
		// - prefer in_progress, otherwise the next runnable frontier step（带 phase 门，
		//   不选被 blocked/未解锁 lane 下的 step）
		snapshot.CurrentStepID = resolveResumeCurrentStepID(snapshot.Plan, snapshot.Phases, snapshot.CurrentStepID)

		// Phase/progress hints: the resume decision gate will finalize, but keep a sane default.
		snapshot.Progress = builtin_tools.PlanProgress(snapshot.Plan)
		if snapshot.ReplanContext != nil {
			// When a replan context is pending, the plan phase must run first
			// so the planner can incorporate the replan directives before
			// the scheduler advances to the next step.
			snapshot.Phase = builtin_tools.AgentPhasePlan
			if strings.TrimSpace(string(snapshot.Status)) == "" || snapshot.Status == builtin_tools.TaskStatusPreparing {
				snapshot.Status = builtin_tools.TaskStatusRunning
			}
		} else if strings.TrimSpace(snapshot.CurrentStepID) != "" {
			snapshot.Phase = builtin_tools.AgentPhaseStep
			if strings.TrimSpace(string(snapshot.Status)) == "" || snapshot.Status == builtin_tools.TaskStatusPreparing {
				snapshot.Status = builtin_tools.TaskStatusRunning
			}
		} else if builtin_tools.AllPlanStepsTerminal(snapshot.Plan) {
			snapshot.Phase = builtin_tools.AgentPhaseFinalAnswer
		}
	}

	snapshot.UpdatedAt = now
	return snapshot, planValid
}

func resolveResumeCurrentStepID(plan []*builtin_tools.PlanItem, phases []*builtin_tools.PlanPhase, preferred string) string {
	preferred = strings.TrimSpace(preferred)
	if len(plan) == 0 {
		return ""
	}

	// 1) If there is an in_progress step, always resume it.
	for _, it := range plan {
		if it == nil {
			continue
		}
		if it.Status == builtin_tools.PlanStepInProgress {
			return strings.TrimSpace(it.ID)
		}
	}

	// 2) 选下一个 frontier step（带 phase 解锁门），不选被 blocked / 依赖未解锁 lane 下的 step。
	next := strings.TrimSpace(builtin_tools.NextFrontierPlanStepID(plan, phases))
	if preferred != "" && preferred == next {
		return preferred
	}
	if next != "" {
		return next
	}
	return ""
}

func loadStepOutcomesFromPointers(writer *artifactWriter, pointers map[string]*builtin_tools.WorkspaceStepOutcomePointer) []*builtin_tools.StepOutcome {
	if len(pointers) == 0 || writer == nil {
		return nil
	}
	type pair struct {
		key string
		ptr *builtin_tools.WorkspaceStepOutcomePointer
	}
	items := make([]pair, 0, len(pointers))
	for k, ptr := range pointers {
		if strings.TrimSpace(k) == "" || ptr == nil {
			continue
		}
		items = append(items, pair{key: strings.TrimSpace(k), ptr: ptr})
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i].ptr
		right := items[j].ptr
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		// Keep deterministic order: updated_at desc, then step_key.
		if left.UpdatedAt.Equal(right.UpdatedAt) {
			return strings.TrimSpace(items[i].key) < strings.TrimSpace(items[j].key)
		}
		return left.UpdatedAt.After(right.UpdatedAt)
	})

	out := make([]*builtin_tools.StepOutcome, 0, len(items))
	for _, item := range items {
		ptr := item.ptr
		if ptr == nil {
			continue
		}
		resultFile := strings.TrimSpace(ptr.ResultFile)
		if resultFile == "" {
			continue
		}
		raw, err := writer.ReadFileRel(resultFile)
		if err != nil {
			continue
		}
		var artifact stepResultArtifact
		if err := json.Unmarshal(raw, &artifact); err != nil {
			continue
		}
		outcome := stepOutcomeFromResultArtifact(&artifact)
		if outcome == nil {
			continue
		}
		out = append(out, outcome)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stepOutcomeFromResultArtifact(artifact *stepResultArtifact) *builtin_tools.StepOutcome {
	if artifact == nil {
		return nil
	}
	stepID := strings.TrimSpace(artifact.StepID)
	if stepID == "" {
		stepID = strings.TrimSpace(artifact.StepKey)
	}
	if stepID == "" {
		return nil
	}

	status := builtin_tools.StepOutcomeCompleted
	switch strings.ToLower(strings.TrimSpace(artifact.Status)) {
	case string(builtin_tools.StepOutcomeFailed):
		status = builtin_tools.StepOutcomeFailed
	default:
		status = builtin_tools.StepOutcomeCompleted
	}

	outcome := &builtin_tools.StepOutcome{
		StepID:        stepID,
		Status:        status,
		UpdatedAt:     artifact.UpdatedAt,
		Summary:       strings.TrimSpace(artifact.Raw.Summary),
		DisplayResult: strings.TrimSpace(artifact.Raw.DisplayResult),
		Result:        strings.TrimSpace(artifact.Raw.Result),
		Error:         strings.TrimSpace(artifact.Raw.Error),
		References:    normalizeReferences(artifact.References),

		StatusSummary: strings.TrimSpace(artifact.Raw.StatusSummary),
		ShortSummary:  strings.TrimSpace(artifact.Raw.ShortSummary),
		LongSummary:   strings.TrimSpace(artifact.Raw.LongSummary),
		KeyFacts:      cloneStringSliceOrNil(artifact.Raw.KeyFacts),
		OpenQuestions: cloneStringSliceOrNil(artifact.Raw.OpenQuestions),

		ArtifactDir: strings.TrimSpace(artifact.Raw.ArtifactDir),
		SummaryFile: strings.TrimSpace(artifact.Raw.SummaryFile),
		ResultFile:  strings.TrimSpace(artifact.Raw.ResultFile),
		ContextKey:  strings.TrimSpace(artifact.Raw.ContextKey),
	}
	if outcome.ArtifactDir == "" {
		outcome.ArtifactDir = strings.TrimSpace(artifact.Raw.ArtifactDir)
	}
	if outcome.ContextKey == "" {
		outcome.ContextKey = strings.TrimSpace(artifact.ContextKey)
	}
	return outcome
}

func applyDurableOutcomesToPlan(plan []*builtin_tools.PlanItem, outcomes []*builtin_tools.StepOutcome, workspaceState *builtin_tools.WorkspaceState) {
	if len(plan) == 0 {
		return
	}

	terminalByID := make(map[string]builtin_tools.PlanStepStatus, len(outcomes))
	for _, outcome := range outcomes {
		if outcome == nil {
			continue
		}
		stepID := strings.TrimSpace(outcome.StepID)
		if stepID == "" {
			continue
		}
		switch outcome.Status {
		case builtin_tools.StepOutcomeCompleted:
			terminalByID[stepID] = builtin_tools.PlanStepCompleted
		case builtin_tools.StepOutcomeFailed:
			terminalByID[stepID] = builtin_tools.PlanStepFailed
		}
	}
	if workspaceState != nil && len(workspaceState.LatestStepOutcomes) > 0 {
		for stepID, ptr := range workspaceState.LatestStepOutcomes {
			stepID = strings.TrimSpace(stepID)
			if stepID == "" || ptr == nil {
				continue
			}
			if _, exists := terminalByID[stepID]; exists {
				continue
			}
			switch ptr.Status {
			case builtin_tools.StepOutcomeCompleted:
				terminalByID[stepID] = builtin_tools.PlanStepCompleted
			case builtin_tools.StepOutcomeFailed:
				terminalByID[stepID] = builtin_tools.PlanStepFailed
			}
		}
	}

	for _, item := range plan {
		if item == nil {
			continue
		}
		stepID := strings.TrimSpace(item.ID)
		if stepID == "" {
			continue
		}
		if terminal, ok := terminalByID[stepID]; ok {
			item.Status = terminal
		}
	}

	// Ensure downstream blocked nodes are skipped.
	_ = builtin_tools.PropagateSkippedPlanSteps(plan)
}

func isDeliverableFinal(snapshot builtin_tools.StateSnapshot) bool {
	if snapshot.Status != builtin_tools.TaskStatusCompleted {
		return false
	}
	if snapshot.FinalAnswer == nil {
		return false
	}
	if strings.TrimSpace(snapshot.FinalAnswer.Content) != "" {
		return true
	}
	return false
}

func rehydrateFromProbe(probe durableResumeProbe) builtin_tools.StateSnapshot {
	snapshot := probe.Snapshot
	if snapshot.Phase == "" {
		snapshot.Phase = inferPhaseFromProbe(probe)
	}
	snapshot.Status = builtin_tools.TaskStatusRunning
	snapshot.Error = ""
	return snapshot
}

func inferPhaseFromProbe(probe durableResumeProbe) builtin_tools.AgentPhase {
	if probe.AllStepsCompleted && probe.PlanValid {
		return builtin_tools.AgentPhaseFinalAnswer
	}
	if probe.InProgressStepID != "" || probe.NextRunnableStepID != "" {
		return builtin_tools.AgentPhaseStep
	}
	return builtin_tools.AgentPhasePlan
}
