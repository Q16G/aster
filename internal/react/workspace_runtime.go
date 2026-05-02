package react

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aster/internal/builtin_tools"
)

type localWorkspaceRuntime struct {
	sessionID string
	rootDir   string
	namespace string
}

var _ builtin_tools.WorkspaceRuntime = (*localWorkspaceRuntime)(nil)

func newLocalWorkspaceRuntime(sessionID string, rootDir string, namespace string) (builtin_tools.WorkspaceRuntime, error) {
	rootDir = normalizeWorkspaceRootDir(rootDir)
	if strings.TrimSpace(rootDir) == "" {
		return nil, fmt.Errorf("workspace root dir is empty")
	}
	return &localWorkspaceRuntime{
		sessionID: strings.TrimSpace(sessionID),
		rootDir:   rootDir,
		namespace: builtin_tools.NormalizeWorkspaceNamespace(namespace),
	}, nil
}

func (w *localWorkspaceRuntime) SessionID() string {
	if w == nil {
		return ""
	}
	return w.sessionID
}

func (w *localWorkspaceRuntime) RootDir() string {
	if w == nil {
		return ""
	}
	return w.rootDir
}

func (w *localWorkspaceRuntime) Namespace() string {
	if w == nil {
		return ""
	}
	return builtin_tools.NormalizeWorkspaceNamespace(w.namespace)
}

func (w *localWorkspaceRuntime) SharedDir() string {
	if w == nil || w.rootDir == "" {
		return ""
	}
	return filepath.Join(w.rootDir, "shared")
}

func (w *localWorkspaceRuntime) ReadFileRel(relPath string) ([]byte, error) {
	absPath, err := w.resolveAbsPath(relPath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(absPath)
}

func (w *localWorkspaceRuntime) WriteFileRel(relPath string, content []byte) error {
	absPath, err := w.resolveAbsPath(relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(absPath, content, 0o644)
}

func (w *localWorkspaceRuntime) LoadWorkspaceState() (*builtin_tools.WorkspaceState, error) {
	data, err := w.ReadFileRel(filepath.ToSlash(filepath.Join("workspace", "state.json")))
	if err != nil {
		if os.IsNotExist(err) {
			return &builtin_tools.WorkspaceState{
				SessionID:          strings.TrimSpace(w.SessionID()),
				LatestStepOutcomes: make(map[string]*builtin_tools.WorkspaceStepOutcomePointer),
				ChildAgents:        make(map[string]*builtin_tools.WorkspaceChildAgentPointer),
			}, nil
		}
		return nil, err
	}
	var state builtin_tools.WorkspaceState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal workspace state: %w", err)
	}
	if state.LatestStepOutcomes == nil {
		state.LatestStepOutcomes = make(map[string]*builtin_tools.WorkspaceStepOutcomePointer)
	}
	if state.ChildAgents == nil {
		state.ChildAgents = make(map[string]*builtin_tools.WorkspaceChildAgentPointer)
	}
	if state.SessionID == "" {
		state.SessionID = strings.TrimSpace(w.SessionID())
	}
	return &state, nil
}

func (w *localWorkspaceRuntime) SaveWorkspaceState(state *builtin_tools.WorkspaceState) error {
	if state == nil {
		return fmt.Errorf("workspace state is nil")
	}
	if state.LatestStepOutcomes == nil {
		state.LatestStepOutcomes = make(map[string]*builtin_tools.WorkspaceStepOutcomePointer)
	}
	if state.ChildAgents == nil {
		state.ChildAgents = make(map[string]*builtin_tools.WorkspaceChildAgentPointer)
	}
	if strings.TrimSpace(state.SessionID) == "" {
		state.SessionID = strings.TrimSpace(w.SessionID())
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workspace state: %w", err)
	}
	data = append(data, '\n')
	return w.WriteFileRel(filepath.ToSlash(filepath.Join("workspace", "state.json")), data)
}

func (w *localWorkspaceRuntime) LoadWorkspaceReferences() ([]*builtin_tools.WorkspaceReferenceRecord, error) {
	data, err := w.ReadFileRel(filepath.ToSlash(filepath.Join("workspace", "references.jsonl")))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	out := make([]*builtin_tools.WorkspaceReferenceRecord, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record builtin_tools.WorkspaceReferenceRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("unmarshal workspace reference: %w", err)
		}
		record.RefID = strings.TrimSpace(record.RefID)
		if record.RefID == "" {
			continue
		}
		out = append(out, &record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan workspace references: %w", err)
	}
	return out, nil
}

func (w *localWorkspaceRuntime) AppendWorkspaceReferences(refs []*builtin_tools.WorkspaceReferenceRecord) error {
	if len(refs) == 0 {
		return nil
	}
	var buf bytes.Buffer
	for _, record := range refs {
		if record == nil || strings.TrimSpace(record.RefID) == "" {
			continue
		}
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal workspace reference: %w", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if buf.Len() == 0 {
		return nil
	}
	absPath, err := w.resolveAbsPath(filepath.ToSlash(filepath.Join("workspace", "references.jsonl")))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open workspace references: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("append workspace references: %w", err)
	}
	return nil
}

func (w *localWorkspaceRuntime) LoadStepContextRecords(limit int) ([]*builtin_tools.StepContextRecord, error) {
	return builtin_tools.LoadWorkspaceStepContextRecords(w.RootDir(), limit)
}

func (w *localWorkspaceRuntime) AppendStepContextRecords(records []*builtin_tools.StepContextRecord) error {
	return builtin_tools.AppendWorkspaceStepContextRecords(w.RootDir(), records)
}

func (w *localWorkspaceRuntime) ArtifactWritePath(relPath string) (artifactPath string, absPath string, err error) {
	return builtin_tools.WorkspaceArtifactWritePath(w.RootDir(), w.Namespace(), relPath)
}

func (w *localWorkspaceRuntime) resolveAbsPath(relPath string) (string, error) {
	rootDir := strings.TrimSpace(w.RootDir())
	if rootDir == "" {
		return "", fmt.Errorf("workspace root dir is empty")
	}
	relPath = filepath.ToSlash(strings.TrimSpace(relPath))
	if relPath == "" {
		return "", fmt.Errorf("workspace relative path is empty")
	}
	if strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, "\\") {
		return "", fmt.Errorf("workspace relative path must be relative")
	}
	localPath := filepath.Clean(filepath.FromSlash(relPath))
	if localPath == "." || localPath == "" {
		return "", fmt.Errorf("workspace relative path is empty")
	}
	if filepath.IsAbs(localPath) || localPath == ".." || strings.HasPrefix(localPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace relative path escapes root")
	}
	absPath, err := filepath.Abs(filepath.Join(rootDir, localPath))
	if err != nil {
		return "", fmt.Errorf("resolve workspace file path: %w", err)
	}
	workspaceAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root dir: %w", err)
	}
	relToRoot, err := filepath.Rel(workspaceAbs, absPath)
	if err != nil {
		return "", fmt.Errorf("resolve workspace file rel: %w", err)
	}
	relToRoot = filepath.Clean(relToRoot)
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("workspace file path escapes root")
	}
	return absPath, nil
}
