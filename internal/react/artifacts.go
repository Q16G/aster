package react

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aster/internal/builtin_tools"
)

type artifactWriter struct {
	runtime     builtin_tools.WorkspaceRuntime
	sessionRoot string
	namespace   string
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
	writer := &artifactWriter{
		runtime:     runtime,
		sessionRoot: sessionRoot,
		namespace:   sanitizeArtifactNamespace(runtime.Namespace()),
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

func sanitizeArtifactNamespace(namespace string) string {
	namespace = filepath.ToSlash(strings.TrimSpace(namespace))
	namespace = strings.Trim(namespace, "/")
	if namespace == "" {
		return ""
	}
	// Keep it best-effort: caller should provide a pre-sanitized relative namespace.
	return namespace
}
