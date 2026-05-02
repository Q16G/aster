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

type resumeDecision string

const (
	resumeDecisionReturnFinal       resumeDecision = "return_final"
	resumeDecisionResumeSessionOnly resumeDecision = "resume_session_only"
	resumeDecisionResumeCurrentStep resumeDecision = "resume_current_step"
	resumeDecisionResumeNextStep    resumeDecision = "resume_next_step"
	resumeDecisionResumeFinalAnswer resumeDecision = "resume_final_answer"
	resumeDecisionReplanWithContext resumeDecision = "replan_with_context"
	resumeDecisionColdStart         resumeDecision = "cold_start"
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
	if planCurrent != nil && len(planCurrent.Plan) > 0 {
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
		raw, err := writer.ReadFileRel(writer.finalAssessmentFileRel(seq))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
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

func maxFinalSeqInNamespace(writer *artifactWriter) int {
	if writer == nil {
		return 0
	}
	rel := filepath.ToSlash(filepath.Join(writer.artifactsRootRel(), "final"))
	absDir := filepath.Join(writer.sessionRoot, filepath.FromSlash(rel))
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

	// 1) final_assessment.assessed_state (strongest payload for plan/outcomes)
	if finalAssessment != nil {
		payload := finalAssessment.AssessedState
		if strings.TrimSpace(string(payload.Status)) != "" {
			snapshot.Status = payload.Status
		}
		snapshot.Error = strings.TrimSpace(payload.StateError)
		snapshot.InputTimeline = payload.InputTimeline
		snapshot.NeedsPlanning = payload.NeedsPlanning
		snapshot.Plan = payload.Plan
		snapshot.PlanVersion = payload.PlanVersion
		snapshot.StepOutcomes = payload.StepOutcomes
		snapshot.Warnings = payload.Warnings
		snapshot.Unresolved = payload.Unresolved
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
		if len(snapshot.Unresolved) == 0 && len(workspaceState.Unresolved) > 0 {
			snapshot.Unresolved = builtin_tools.CloneStringSlice(workspaceState.Unresolved)
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
		if len(snapshot.Plan) == 0 && len(planCurrent.Plan) > 0 {
			snapshot.Plan = planCurrent.Plan
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
				if raw, err := writer.ReadFileRel(writer.finalAnswerFileRel(finalSeq)); err == nil {
					if text := strings.TrimSpace(string(raw)); text != "" {
						finalText = text
					}
				}
			}
			published := strings.TrimSpace(string(decision.model.PublishedOutput))
			if finalText != "" || published != "" {
				snapshot.FinalAnswer = &builtin_tools.FinalAnswer{
					Content:         strings.TrimSpace(finalText),
					Source:          "final_assessment",
					CreatedAt:       now,
					References:      builtin_tools.CloneStringSlice(decision.model.References),
					PublishedOutput: published,
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
		applyDurableOutcomesToPlan(snapshot.Plan, snapshot.StepOutcomes, workspaceState)

		// Resolve current_step_id for resume:
		// - never point to terminal steps
		// - prefer in_progress, otherwise the next runnable pending step
		snapshot.CurrentStepID = resolveResumeCurrentStepID(snapshot.Plan, snapshot.CurrentStepID)

		// Phase/progress hints: the resume decision gate will finalize, but keep a sane default.
		snapshot.Progress = builtin_tools.PlanProgress(snapshot.Plan)
		if strings.TrimSpace(snapshot.CurrentStepID) != "" {
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

func resolveResumeCurrentStepID(plan []*builtin_tools.PlanItem, preferred string) string {
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

	// 2) If the preferred step is already the next runnable pending step, keep it.
	next := strings.TrimSpace(builtin_tools.NextRunnablePlanStepID(plan))
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

		StatusSummary:       strings.TrimSpace(artifact.Raw.StatusSummary),
		ShortSummary:        strings.TrimSpace(artifact.Raw.ShortSummary),
		LongSummary:         strings.TrimSpace(artifact.Raw.LongSummary),
		KeyFacts:            cloneStringSliceOrNil(artifact.Raw.KeyFacts),
		OpenQuestions:       cloneStringSliceOrNil(artifact.Raw.OpenQuestions),
		TimelineDiffSummary: strings.TrimSpace(artifact.Raw.TimelineDiff),

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
	if strings.TrimSpace(snapshot.FinalAnswer.PublishedOutput) != "" {
		return true
	}
	return false
}

func decideResumeDecision(input string, explicitResume bool, probe durableResumeProbe) resumeDecision {
	input = strings.TrimSpace(input)
	wantsResume := explicitResume || isContinuationInput(input)

	if !wantsResume {
		if probe.HasCheckpoint {
			return resumeDecisionResumeSessionOnly
		}
		return resumeDecisionColdStart
	}

	if probe.DeliverableFinal {
		return resumeDecisionReturnFinal
	}
	if probe.Snapshot.ReplanContext != nil {
		return resumeDecisionReplanWithContext
	}
	if strings.TrimSpace(probe.InProgressStepID) != "" {
		return resumeDecisionResumeCurrentStep
	}
	if strings.TrimSpace(probe.NextRunnableStepID) != "" {
		return resumeDecisionResumeNextStep
	}
	if probe.AllStepsCompleted && probe.PlanValid {
		return resumeDecisionResumeFinalAnswer
	}
	if probe.HasCheckpoint && (!probe.PlanValid || len(probe.Snapshot.Plan) == 0) {
		return resumeDecisionReplanWithContext
	}
	if probe.HasCheckpoint {
		// Durable data exists but no runnable step: prefer replan with context over a cold start.
		return resumeDecisionReplanWithContext
	}
	return resumeDecisionColdStart
}

func applyResumeDecisionToSnapshot(snapshot builtin_tools.StateSnapshot, decision resumeDecision) builtin_tools.StateSnapshot {
	switch decision {
	case resumeDecisionReturnFinal:
		// Keep as-is.
		return snapshot
	case resumeDecisionResumeCurrentStep, resumeDecisionResumeNextStep:
		snapshot.Status = builtin_tools.TaskStatusRunning
		snapshot.Phase = builtin_tools.AgentPhaseStep
		snapshot.Error = ""
		return snapshot
	case resumeDecisionResumeFinalAnswer:
		snapshot.Status = builtin_tools.TaskStatusRunning
		snapshot.Phase = builtin_tools.AgentPhaseFinalAnswer
		snapshot.Error = ""
		snapshot.CurrentStepID = ""
		return snapshot
	case resumeDecisionReplanWithContext:
		snapshot.Status = builtin_tools.TaskStatusRunning
		snapshot.Phase = builtin_tools.AgentPhasePlan
		snapshot.Error = ""
		snapshot.CurrentStepID = ""
		return snapshot
	default:
		return snapshot
	}
}
