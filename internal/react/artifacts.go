package react

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"

	"aster/internal/builtin_tools"
)

//go:embed templates/step_summary.md.tmpl
var stepSummaryMDTemplate string

type artifactWriter struct {
	runtime     builtin_tools.WorkspaceRuntime
	sessionRoot string
	namespace   string
	stepTmpl    *template.Template
}

type artifactWriterOption func(*artifactWriterConfig)

type artifactWriterConfig struct {
	namespace string
	sessionID string
}

func WithArtifactNamespace(namespace string) artifactWriterOption {
	return func(cfg *artifactWriterConfig) {
		if cfg == nil {
			return
		}
		cfg.namespace = sanitizeArtifactNamespace(namespace)
	}
}

func NewArtifactWriter(sessionRoot string, opts ...artifactWriterOption) (*artifactWriter, error) {
	cfg := &artifactWriterConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	runtime, err := newLocalWorkspaceRuntime(cfg.sessionID, sessionRoot, cfg.namespace)
	if err != nil {
		return nil, err
	}
	return newArtifactWriter(runtime)
}

func newArtifactWriter(runtime builtin_tools.WorkspaceRuntime) (*artifactWriter, error) {
	if runtime == nil {
		return nil, fmt.Errorf("workspace runtime is nil")
	}
	sessionRoot := strings.TrimSpace(runtime.RootDir())
	if sessionRoot == "" {
		return nil, fmt.Errorf("workspace session root is empty")
	}
	tmpl, err := template.New("step_summary_md").Parse(stepSummaryMDTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse step summary template failed: %w", err)
	}
	writer := &artifactWriter{
		runtime:     runtime,
		sessionRoot: sessionRoot,
		namespace:   sanitizeArtifactNamespace(runtime.Namespace()),
		stepTmpl:    tmpl,
	}
	if writer.namespace != "" && (strings.HasPrefix(writer.namespace, "/") || strings.Contains(writer.namespace, "..")) {
		return nil, fmt.Errorf("invalid workspace namespace: %q", writer.namespace)
	}
	return writer, nil
}

func (w *artifactWriter) writeFileRel(relPath string, content []byte) error {
	if w == nil {
		return fmt.Errorf("artifact writer is nil")
	}
	if w.runtime == nil {
		return fmt.Errorf("workspace runtime is nil")
	}
	return w.runtime.WriteFileRel(relPath, content)
}

func (w *artifactWriter) ReadFileRel(relPath string) ([]byte, error) {
	if w == nil {
		return nil, fmt.Errorf("artifact writer is nil")
	}
	if w.runtime == nil {
		return nil, fmt.Errorf("workspace runtime is nil")
	}
	return w.runtime.ReadFileRel(relPath)
}

func (w *artifactWriter) artifactsRootRel() string {
	if w == nil {
		return ""
	}
	if strings.TrimSpace(w.namespace) == "" {
		return "artifacts"
	}
	return filepath.ToSlash(filepath.Join("artifacts", w.namespace))
}

func (w *artifactWriter) planCurrentFileRel() string {
	return filepath.ToSlash(filepath.Join(w.artifactsRootRel(), "plan", "current.json"))
}

func (w *artifactWriter) planHistoryFileRel(planVersion int) string {
	if planVersion <= 0 {
		planVersion = 1
	}
	return filepath.ToSlash(filepath.Join(w.artifactsRootRel(), "plan", "history", strconv.Itoa(planVersion)+".json"))
}

func (w *artifactWriter) sharedStepArtifactDirRel() string {
	if w == nil {
		return ""
	}
	if strings.TrimSpace(w.namespace) == "" {
		return "shared/step_artifacts"
	}
	return filepath.ToSlash(filepath.Join("shared", w.namespace, "step_artifacts"))
}

type stepArtifactPlan struct {
	PlanVersion int
	StepKey     string
	StepID      string

	ArtifactID  string
	ArtifactDir string
	SummaryFile string
	ResultFile  string
}

func (w *artifactWriter) PlanStepArtifactRel(planVersion int, stepKey string, stepID string) (*stepArtifactPlan, error) {
	if w == nil {
		return nil, fmt.Errorf("artifact writer is nil")
	}
	stepKey = strings.TrimSpace(stepKey)
	if stepKey == "" {
		return nil, fmt.Errorf("step key is empty")
	}
	stepID = strings.TrimSpace(stepID)

	artifactDir := w.sharedStepArtifactDirRel()
	if artifactDir == "" {
		return nil, fmt.Errorf("invalid step artifact dir")
	}

	suffix := sanitizeArtifactKey(stepID)
	if strings.TrimSpace(stepID) == "" {
		suffix = "step"
	}
	artifactID := uuid.NewString() + "_" + suffix
	summaryFile := filepath.ToSlash(filepath.Join(artifactDir, artifactID+".summary.md"))
	resultFile := filepath.ToSlash(filepath.Join(artifactDir, artifactID+".result.json"))

	if planVersion <= 0 {
		planVersion = 1
	}
	return &stepArtifactPlan{
		PlanVersion: planVersion,
		StepKey:     stepKey,
		StepID:      stepID,
		ArtifactID:  artifactID,
		ArtifactDir: artifactDir,
		SummaryFile: summaryFile,
		ResultFile:  resultFile,
	}, nil
}

func (w *artifactWriter) finalDirRel(finalSeq int) string {
	if finalSeq <= 0 {
		return ""
	}
	return filepath.ToSlash(filepath.Join(w.artifactsRootRel(), "final", strconv.Itoa(finalSeq)))
}

func (w *artifactWriter) finalAnswerFileRel(finalSeq int) string {
	dir := w.finalDirRel(finalSeq)
	if dir == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join(dir, "final_answer.md"))
}

func (w *artifactWriter) finalAssessmentFileRel(finalSeq int) string {
	dir := w.finalDirRel(finalSeq)
	if dir == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Join(dir, "final_assessment.json"))
}

func (w *artifactWriter) LoadWorkspaceState() (*builtin_tools.WorkspaceState, error) {
	if w == nil || w.runtime == nil {
		return nil, fmt.Errorf("artifact writer is nil")
	}
	return w.runtime.LoadWorkspaceState()
}

func (w *artifactWriter) WriteWorkspaceState(state *builtin_tools.WorkspaceState) error {
	if w == nil || w.runtime == nil {
		return fmt.Errorf("artifact writer is nil")
	}
	return w.runtime.SaveWorkspaceState(state)
}

func (w *artifactWriter) loadWorkspaceReferences() ([]*builtin_tools.WorkspaceReferenceRecord, error) {
	if w == nil || w.runtime == nil {
		return nil, fmt.Errorf("artifact writer is nil")
	}
	return w.runtime.LoadWorkspaceReferences()
}

func (w *artifactWriter) LoadPlanCurrentCheckpoint() (*planCurrentCheckpoint, error) {
	if w == nil {
		return nil, fmt.Errorf("artifact writer is nil")
	}
	data, err := w.ReadFileRel(w.planCurrentFileRel())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var checkpoint planCurrentCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("unmarshal plan current checkpoint failed: %w", err)
	}
	return &checkpoint, nil
}

func (w *artifactWriter) appendWorkspaceReferences(records []*builtin_tools.WorkspaceReferenceRecord) error {
	if w == nil || w.runtime == nil {
		return fmt.Errorf("artifact writer is nil")
	}
	return w.runtime.AppendWorkspaceReferences(records)
}

func (w *artifactWriter) nextSequenceFromRelDir(relDir string) (int, error) {
	if w == nil {
		return 0, fmt.Errorf("artifact writer is nil")
	}
	relDir = filepath.ToSlash(strings.TrimSpace(relDir))
	if relDir == "" {
		return 0, fmt.Errorf("artifact sequence dir is empty")
	}
	absDir := filepath.Join(w.sessionRoot, filepath.FromSlash(relDir))
	entries, err := os.ReadDir(absDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 1, nil
		}
		return 0, err
	}
	maxSeq := 0
	for _, entry := range entries {
		if !entry.IsDir() {
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
	return maxSeq + 1, nil
}

func (w *artifactWriter) PersistPlanArtifacts(snapshot builtin_tools.StateSnapshot, sessionID string, explanation string) error {
	if w == nil {
		return fmt.Errorf("artifact writer is nil")
	}

	state, err := w.LoadWorkspaceState()
	if err != nil {
		return err
	}
	latestFinalSeq := 0
	if state != nil && state.LatestFinalSeq > 0 {
		latestFinalSeq = state.LatestFinalSeq
	}

	checkpoint := buildPlanCurrentCheckpoint(snapshot, sessionID, explanation, latestFinalSeq)
	if err := w.WritePlanCurrentCheckpoint(checkpoint); err != nil {
		return err
	}
	// Plan history is versioned: record the initial plan checkpoint at each plan_version.
	if err := w.writePlanHistoryCheckpoint(checkpoint); err != nil {
		return err
	}

	state.SessionID = strings.TrimSpace(sessionID)
	state.Status = snapshot.Status
	state.CurrentPlanVersion = snapshot.PlanVersion
	state.CurrentStepKey = strings.TrimSpace(snapshot.CurrentStepID)
	state.Warnings = builtin_tools.CloneStringSlice(snapshot.Warnings)
	state.Unresolved = builtin_tools.CloneStringSlice(snapshot.Unresolved)
	state.ReplanContext = builtin_tools.CloneReplanContext(snapshot.ReplanContext)
	state.ActiveSkillNames = builtin_tools.CloneStringSlice(snapshot.ActiveSkillNames)
	state.ActiveMCPServers = builtin_tools.CloneStringSlice(snapshot.ActiveMCPServers)
	state.UpdatedAt = time.Now()
	return w.WriteWorkspaceState(state)
}

// PersistRuntimeCheckpoint writes a durable "current execution" checkpoint without creating a new plan history entry.
// It is used for step-start (in_progress) checkpoints and other lightweight sync points.
func (w *artifactWriter) PersistRuntimeCheckpoint(snapshot builtin_tools.StateSnapshot, sessionID string, explanation string) error {
	if w == nil {
		return fmt.Errorf("artifact writer is nil")
	}

	state, err := w.LoadWorkspaceState()
	if err != nil {
		return err
	}
	if state == nil {
		state = &builtin_tools.WorkspaceState{}
	}

	state.SessionID = strings.TrimSpace(sessionID)
	state.Status = snapshot.Status
	state.CurrentPlanVersion = snapshot.PlanVersion
	state.CurrentStepKey = strings.TrimSpace(snapshot.CurrentStepID)
	state.Warnings = builtin_tools.CloneStringSlice(snapshot.Warnings)
	state.Unresolved = builtin_tools.CloneStringSlice(snapshot.Unresolved)
	state.ReplanContext = builtin_tools.CloneReplanContext(snapshot.ReplanContext)
	state.ActiveSkillNames = builtin_tools.CloneStringSlice(snapshot.ActiveSkillNames)
	state.ActiveMCPServers = builtin_tools.CloneStringSlice(snapshot.ActiveMCPServers)
	state.UpdatedAt = time.Now()
	if err := w.WriteWorkspaceState(state); err != nil {
		return err
	}

	latestFinalSeq := 0
	if state.LatestFinalSeq > 0 {
		latestFinalSeq = state.LatestFinalSeq
	}

	checkpoint := buildPlanCurrentCheckpoint(snapshot, sessionID, explanation, latestFinalSeq)
	if prev, err := w.LoadPlanCurrentCheckpoint(); err == nil && prev != nil {
		if checkpoint.Explanation == "" {
			checkpoint.Explanation = strings.TrimSpace(prev.Explanation)
		}
		if checkpoint.CurrentGoal == "" {
			checkpoint.CurrentGoal = strings.TrimSpace(prev.CurrentGoal)
		}
		if len(checkpoint.InputTimeline) == 0 {
			checkpoint.InputTimeline = prev.InputTimeline
		}
		if checkpoint.LatestFinalSeq <= 0 {
			checkpoint.LatestFinalSeq = prev.LatestFinalSeq
		}
	}
	return w.WritePlanCurrentCheckpoint(checkpoint)
}

func buildPlanCurrentCheckpoint(snapshot builtin_tools.StateSnapshot, sessionID string, explanation string, latestFinalSeq int) planCurrentCheckpoint {
	return planCurrentCheckpoint{
		SessionID:        strings.TrimSpace(sessionID),
		Phase:            snapshot.Phase,
		PlanVersion:      snapshot.PlanVersion,
		CurrentStepID:    strings.TrimSpace(snapshot.CurrentStepID),
		Status:           snapshot.Status,
		UpdatedAt:        time.Now(),
		Explanation:      strings.TrimSpace(explanation),
		Plan:             snapshot.Plan,
		Warnings:         snapshot.Warnings,
		Unresolved:       snapshot.Unresolved,
		ReplanContext:    builtin_tools.CloneReplanContext(snapshot.ReplanContext),
		StatusSummary:    strings.TrimSpace(snapshot.StatusSummary),
		CurrentGoal:      strings.TrimSpace(snapshot.CurrentGoal),
		InputTimeline:    snapshot.InputTimeline,
		ActiveSkillNames: builtin_tools.CloneStringSlice(snapshot.ActiveSkillNames),
		ActiveMCPServers: builtin_tools.CloneStringSlice(snapshot.ActiveMCPServers),
		LatestFinalSeq:   latestFinalSeq,
	}
}

func (w *artifactWriter) WritePlanCurrentCheckpoint(checkpoint planCurrentCheckpoint) error {
	if w == nil {
		return fmt.Errorf("artifact writer is nil")
	}
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan artifacts failed: %w", err)
	}
	data = append(data, '\n')
	if err := w.writeFileRel(w.planCurrentFileRel(), data); err != nil {
		return fmt.Errorf("write current plan artifact failed: %w", err)
	}
	return nil
}

func (w *artifactWriter) writePlanHistoryCheckpoint(checkpoint planCurrentCheckpoint) error {
	if w == nil {
		return fmt.Errorf("artifact writer is nil")
	}
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan artifacts failed: %w", err)
	}
	data = append(data, '\n')
	if err := w.writeFileRel(w.planHistoryFileRel(checkpoint.PlanVersion), data); err != nil {
		return fmt.Errorf("write plan history artifact failed: %w", err)
	}
	return nil
}

type stepSummaryTemplateData struct {
	StepID          string
	Step            string
	Status          string
	UpdatedAt       string
	StatusSummary   string
	ShortSummary    string
	LongSummary     string
	KeyFacts        []string
	OpenQuestions   []string
	References      []string
	ToolCallsDigest []string
}

type stepArtifactRecord struct {
	ArtifactID   string
	ArtifactDir  string
	SummaryFile  string
	ResultFile   string
	ReferenceIDs []string
}

func (w *artifactWriter) PersistStepArtifacts(snapshot builtin_tools.StateSnapshot, sessionID string, agentProfile string, planItem *builtin_tools.PlanItem, outcome *builtin_tools.StepOutcome, window *StepWindow, plan *stepArtifactPlan) (*stepArtifactRecord, error) {
	if w == nil {
		return nil, fmt.Errorf("artifact writer is nil")
	}
	if planItem == nil || outcome == nil {
		return nil, fmt.Errorf("planItem/outcome is nil")
	}
	if plan == nil {
		return nil, fmt.Errorf("step artifact plan is nil")
	}
	stepKey := strings.TrimSpace(planItem.ID)
	if stepKey == "" {
		stepKey = strings.TrimSpace(outcome.StepID)
	}
	if stepKey == "" {
		return nil, fmt.Errorf("step key is empty")
	}

	effectivePlanVersion := snapshot.PlanVersion
	if effectivePlanVersion <= 0 {
		effectivePlanVersion = 1
	}
	if plan.PlanVersion != effectivePlanVersion || strings.TrimSpace(plan.StepKey) != stepKey {
		return nil, fmt.Errorf("step artifact plan mismatch: plan_version=%d step_key=%q", plan.PlanVersion, plan.StepKey)
	}
	artifactID := strings.TrimSpace(plan.ArtifactID)
	artifactDir := strings.TrimSpace(plan.ArtifactDir)
	summaryFile := strings.TrimSpace(plan.SummaryFile)
	resultFile := strings.TrimSpace(plan.ResultFile)
	if artifactID == "" || artifactDir == "" || summaryFile == "" || resultFile == "" {
		return nil, fmt.Errorf("invalid step artifact paths")
	}

	var md bytes.Buffer
	if w.stepTmpl == nil {
		return nil, fmt.Errorf("step summary template is nil")
	}
	data := stepSummaryTemplateData{
		StepID:          strings.TrimSpace(outcome.StepID),
		Step:            strings.TrimSpace(planItem.Step),
		Status:          strings.TrimSpace(string(outcome.Status)),
		UpdatedAt:       outcome.UpdatedAt.Format(time.RFC3339),
		StatusSummary:   strings.TrimSpace(outcome.StatusSummary),
		ShortSummary:    strings.TrimSpace(outcome.ShortSummary),
		LongSummary:     strings.TrimSpace(outcome.LongSummary),
		KeyFacts:        outcome.KeyFacts,
		OpenQuestions:   outcome.OpenQuestions,
		References:      outcome.References,
		ToolCallsDigest: outcome.ToolCallsDigest,
	}
	if err := w.stepTmpl.Execute(&md, data); err != nil {
		return nil, fmt.Errorf("render step summary template failed: %w", err)
	}
	if err := w.writeFileRel(summaryFile, md.Bytes()); err != nil {
		return nil, fmt.Errorf("write step summary failed: %w", err)
	}

	resultPayload := map[string]any{
		"session_id":    strings.TrimSpace(sessionID),
		"plan_version":  plan.PlanVersion,
		"step_id":       strings.TrimSpace(outcome.StepID),
		"step_key":      stepKey,
		"artifact_id":   artifactID,
		"context_key":   strings.TrimSpace(outcome.ContextKey),
		"step":          strings.TrimSpace(planItem.Step),
		"status":        strings.TrimSpace(string(outcome.Status)),
		"updated_at":    outcome.UpdatedAt,
		"step_window":   window,
		"references":    outcome.References,
		"agent_profile": strings.TrimSpace(agentProfile),
		"raw": map[string]any{
			"summary":        strings.TrimSpace(outcome.Summary),
			"display_result": strings.TrimSpace(outcome.DisplayResult),
			"result":         strings.TrimSpace(outcome.Result),
			"error":          strings.TrimSpace(outcome.Error),
			"references":     outcome.References,
			"artifact_id":    artifactID,
			"artifact_dir":   artifactDir,
			"summary_file":   summaryFile,
			"result_file":    resultFile,
			"context_key":    strings.TrimSpace(outcome.ContextKey),
			"status_summary": strings.TrimSpace(outcome.StatusSummary),
			"short_summary":  strings.TrimSpace(outcome.ShortSummary),
			"long_summary":   strings.TrimSpace(outcome.LongSummary),
			"key_facts":      outcome.KeyFacts,
			"open_questions": outcome.OpenQuestions,
		},
	}
	rawJSON, err := json.MarshalIndent(resultPayload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal step result failed: %w", err)
	}
	rawJSON = append(rawJSON, '\n')
	if err := w.writeFileRel(resultFile, rawJSON); err != nil {
		return nil, fmt.Errorf("write step result failed: %w", err)
	}

	refIDs, err := w.syncReferences(BuildStepReferences(outcome.References, artifactDir, summaryFile, resultFile), workspaceReferenceMeta{
		StepKey:      stepKey,
		AgentProfile: agentProfile,
		CreatedAt:    time.Now(),
	})
	if err != nil {
		return nil, err
	}

	state, err := w.LoadWorkspaceState()
	if err != nil {
		return nil, err
	}
	if state.LatestStepOutcomes == nil {
		state.LatestStepOutcomes = make(map[string]*builtin_tools.WorkspaceStepOutcomePointer)
	}
	state.SessionID = strings.TrimSpace(sessionID)
	state.Status = snapshot.Status
	state.CurrentPlanVersion = plan.PlanVersion
	state.CurrentStepKey = stepKey
	state.Warnings = builtin_tools.CloneStringSlice(snapshot.Warnings)
	state.Unresolved = builtin_tools.CloneStringSlice(snapshot.Unresolved)
	state.ReplanContext = builtin_tools.CloneReplanContext(snapshot.ReplanContext)
	state.ActiveReferenceIDs = mergeUniqueStrings(state.ActiveReferenceIDs, refIDs)
	state.LatestStepOutcomes[stepKey] = &builtin_tools.WorkspaceStepOutcomePointer{
		PlanVersion: plan.PlanVersion,
		StepKey:     stepKey,
		ArtifactID:  artifactID,
		Status:      outcome.Status,
		ArtifactDir: artifactDir,
		SummaryFile: summaryFile,
		ResultFile:  resultFile,
		ContextKey:  strings.TrimSpace(outcome.ContextKey),
		UpdatedAt:   time.Now(),
	}
	state.UpdatedAt = time.Now()
	if err := w.WriteWorkspaceState(state); err != nil {
		return nil, err
	}

	// Keep artifacts/plan/current.json in sync as a real durable checkpoint.
	checkpointSnapshot := snapshot
	checkpointSnapshot.CurrentStepID = strings.TrimSpace(builtin_tools.NextRunnablePlanStepID(snapshot.Plan))
	checkpointSnapshot.StatusSummary = firstNonEmpty(
		strings.TrimSpace(outcome.StatusSummary),
		strings.TrimSpace(outcome.ShortSummary),
		strings.TrimSpace(outcome.Summary),
		strings.TrimSpace(outcome.DisplayResult),
		strings.TrimSpace(snapshot.StatusSummary),
	)
	checkpoint := buildPlanCurrentCheckpoint(checkpointSnapshot, sessionID, "", state.LatestFinalSeq)
	if prev, err := w.LoadPlanCurrentCheckpoint(); err == nil && prev != nil {
		if checkpoint.Explanation == "" {
			checkpoint.Explanation = strings.TrimSpace(prev.Explanation)
		}
		if checkpoint.CurrentGoal == "" {
			checkpoint.CurrentGoal = strings.TrimSpace(prev.CurrentGoal)
		}
		if len(checkpoint.InputTimeline) == 0 {
			checkpoint.InputTimeline = prev.InputTimeline
		}
		if checkpoint.LatestFinalSeq <= 0 {
			checkpoint.LatestFinalSeq = prev.LatestFinalSeq
		}
	}
	_ = w.WritePlanCurrentCheckpoint(checkpoint)

	return &stepArtifactRecord{
		ArtifactID:   artifactID,
		ArtifactDir:  artifactDir,
		SummaryFile:  summaryFile,
		ResultFile:   resultFile,
		ReferenceIDs: refIDs,
	}, nil
}

type finalArtifactRecord struct {
	FinalSeq            int
	FinalAnswerFile     string
	FinalAssessmentFile string
}

func (w *artifactWriter) PersistFinalArtifacts(snapshot builtin_tools.StateSnapshot, sessionID string, assessmentPayload any, finalContent string) (*finalArtifactRecord, error) {
	if w == nil {
		return nil, fmt.Errorf("artifact writer is nil")
	}
	finalSeq, err := w.nextSequenceFromRelDir(filepath.ToSlash(filepath.Join(w.artifactsRootRel(), "final")))
	if err != nil {
		return nil, fmt.Errorf("resolve final seq failed: %w", err)
	}
	finalAssessmentFile := w.finalAssessmentFileRel(finalSeq)
	raw, err := json.MarshalIndent(assessmentPayload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal final assessment failed: %w", err)
	}
	raw = append(raw, '\n')
	if err := w.writeFileRel(finalAssessmentFile, raw); err != nil {
		return nil, fmt.Errorf("write final assessment failed: %w", err)
	}

	finalAnswerFile := ""
	finalContent = strings.TrimSpace(finalContent)
	if finalContent != "" {
		finalAnswerFile = w.finalAnswerFileRel(finalSeq)
		if err := w.writeFileRel(finalAnswerFile, []byte(finalContent+"\n")); err != nil {
			return nil, fmt.Errorf("write final answer failed: %w", err)
		}
	}

	state, err := w.LoadWorkspaceState()
	if err != nil {
		return nil, err
	}
	state.SessionID = strings.TrimSpace(sessionID)
	state.Status = snapshot.Status
	state.CurrentPlanVersion = snapshot.PlanVersion
	state.CurrentStepKey = strings.TrimSpace(snapshot.CurrentStepID)
	state.Warnings = builtin_tools.CloneStringSlice(snapshot.Warnings)
	state.Unresolved = builtin_tools.CloneStringSlice(snapshot.Unresolved)
	state.ReplanContext = builtin_tools.CloneReplanContext(snapshot.ReplanContext)
	state.ActiveSkillNames = builtin_tools.CloneStringSlice(snapshot.ActiveSkillNames)
	state.ActiveMCPServers = builtin_tools.CloneStringSlice(snapshot.ActiveMCPServers)
	state.LatestFinalSeq = finalSeq
	state.UpdatedAt = time.Now()
	if err := w.WriteWorkspaceState(state); err != nil {
		return nil, err
	}

	// Sync plan/current.json with the final checkpoint so resume can trust it.
	checkpoint := buildPlanCurrentCheckpoint(snapshot, sessionID, "", finalSeq)
	if prev, err := w.LoadPlanCurrentCheckpoint(); err == nil && prev != nil {
		if checkpoint.Explanation == "" {
			checkpoint.Explanation = strings.TrimSpace(prev.Explanation)
		}
		if checkpoint.CurrentGoal == "" {
			checkpoint.CurrentGoal = strings.TrimSpace(prev.CurrentGoal)
		}
		if len(checkpoint.InputTimeline) == 0 {
			checkpoint.InputTimeline = prev.InputTimeline
		}
	}
	_ = w.WritePlanCurrentCheckpoint(checkpoint)

	return &finalArtifactRecord{
		FinalSeq:            finalSeq,
		FinalAnswerFile:     finalAnswerFile,
		FinalAssessmentFile: finalAssessmentFile,
	}, nil
}

type workspaceReferenceMeta struct {
	CallID       string
	StepKey      string
	AgentProfile string
	CreatedAt    time.Time
}

func (w *artifactWriter) syncReferences(refs []string, meta workspaceReferenceMeta) ([]string, error) {
	refs = mergeUniqueStrings(nil, refs)
	if len(refs) == 0 {
		return nil, nil
	}
	existing, err := w.loadWorkspaceReferences()
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]string, len(existing))
	for _, record := range existing {
		if record == nil {
			continue
		}
		key := workspaceReferenceKey(record)
		if key == "" {
			continue
		}
		if _, exists := byKey[key]; !exists {
			byKey[key] = strings.TrimSpace(record.RefID)
		}
	}

	refIDs := make([]string, 0, len(refs))
	newRecords := make([]*builtin_tools.WorkspaceReferenceRecord, 0)
	nextSeq := len(existing) + 1
	for _, raw := range refs {
		record := workspaceReferenceFromRaw(raw, meta)
		if record == nil {
			continue
		}
		key := workspaceReferenceKey(record)
		if existingID := strings.TrimSpace(byKey[key]); existingID != "" {
			refIDs = append(refIDs, existingID)
			continue
		}
		record.RefID = fmt.Sprintf("ref-%06d", nextSeq)
		nextSeq++
		byKey[key] = record.RefID
		refIDs = append(refIDs, record.RefID)
		newRecords = append(newRecords, record)
	}
	if err := w.appendWorkspaceReferences(newRecords); err != nil {
		return nil, err
	}
	return mergeUniqueStrings(nil, refIDs), nil
}

func workspaceReferenceFromRaw(raw string, meta workspaceReferenceMeta) *builtin_tools.WorkspaceReferenceRecord {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	record := &builtin_tools.WorkspaceReferenceRecord{
		Kind:         "note",
		Title:        raw,
		CallID:       strings.TrimSpace(meta.CallID),
		StepKey:      strings.TrimSpace(meta.StepKey),
		AgentProfile: strings.TrimSpace(meta.AgentProfile),
		CreatedAt:    meta.CreatedAt,
		Metadata:     map[string]any{"content_excerpt": raw},
	}
	switch {
	case strings.HasPrefix(raw, "http://"), strings.HasPrefix(raw, "https://"):
		record.Kind = "uri"
		record.URI = raw
		record.Title = raw
	case strings.HasPrefix(raw, "artifacts/"):
		record.Kind = "artifact"
		record.ArtifactPath = raw
		record.Title = filepath.Base(raw)
	case strings.Contains(raw, "/"), strings.HasPrefix(raw, "."):
		record.Kind = "file"
		record.FilePath = raw
		record.Title = filepath.Base(raw)
	default:
		record.Kind = "note"
	}
	if strings.TrimSpace(record.Title) == "" {
		record.Title = raw
	}
	return record
}

func workspaceReferenceKey(record *builtin_tools.WorkspaceReferenceRecord) string {
	if record == nil {
		return ""
	}
	excerpt := ""
	if record.Metadata != nil {
		if v, ok := record.Metadata["content_excerpt"].(string); ok {
			excerpt = v
		}
	}
	return strings.Join([]string{
		strings.TrimSpace(record.Kind),
		strings.TrimSpace(record.URI),
		strings.TrimSpace(record.FilePath),
		strings.TrimSpace(record.ArtifactPath),
		strings.TrimSpace(excerpt),
		strings.TrimSpace(record.CallID),
		strings.TrimSpace(record.StepKey),
		strings.TrimSpace(record.AgentProfile),
	}, "|")
}

func mergeUniqueStrings(base []string, extra []string) []string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(base)+len(extra))
	out := make([]string, 0, len(base)+len(extra))
	for _, item := range append(append([]string{}, base...), extra...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
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

func sanitizeArtifactKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "unknown"
	}
	key = strings.ReplaceAll(key, "\\", "_")
	key = strings.ReplaceAll(key, "/", "_")
	return key
}

func sanitizeArtifactNamespace(namespace string) string {
	namespace = filepath.ToSlash(strings.TrimSpace(namespace))
	namespace = strings.Trim(namespace, "/")
	if namespace == "" {
		return ""
	}
	// Keep it best-effort: caller should provide a pre-sanitized relative namespace.
	return namespace
}

func BuildStepReferences(explicit []string, artifactDir string, summaryFile string, resultFile string) []string {
	refs := make([]string, 0, len(explicit)+3)
	add := func(items ...string) {
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			refs = append(refs, item)
		}
	}

	add(artifactDir, summaryFile, resultFile)
	add(explicit...)
	return normalizeStringSlice(refs)
}
