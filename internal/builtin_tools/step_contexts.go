package builtin_tools

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const workspaceStepContextsRelPath = "workspace/step_contexts.jsonl"

func NormalizeWorkspaceNamespace(namespace string) string {
	namespace = filepath.ToSlash(strings.TrimSpace(namespace))
	namespace = strings.Trim(namespace, "/")
	if namespace == "" {
		return "root"
	}
	return namespace
}

func WorkspaceStepContextsFileAbs(workspaceRootDir string) string {
	workspaceRootDir = strings.TrimSpace(workspaceRootDir)
	if workspaceRootDir == "" {
		return ""
	}
	return filepath.Join(workspaceRootDir, filepath.FromSlash(workspaceStepContextsRelPath))
}

func WorkspaceArtifactPath(workspaceRootDir string, filePath string) string {
	workspaceRootDir = strings.TrimSpace(workspaceRootDir)
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return ""
	}
	localPath := filepath.Clean(filepath.FromSlash(filePath))
	if filepath.IsAbs(localPath) {
		return filepath.ToSlash(localPath)
	}
	if workspaceRootDir == "" {
		return filepath.ToSlash(localPath)
	}
	return filepath.ToSlash(filepath.Join(workspaceRootDir, localPath))
}

func AppendWorkspaceStepContextRecords(workspaceRootDir string, records []*StepContextRecord) error {
	if len(records) == 0 {
		return nil
	}
	absPath := WorkspaceStepContextsFileAbs(workspaceRootDir)
	if absPath == "" {
		return fmt.Errorf("workspace root is empty")
	}

	var buf bytes.Buffer
	for _, record := range records {
		if record == nil {
			continue
		}
		record.ContextKey = strings.TrimSpace(record.ContextKey)
		record.Namespace = NormalizeWorkspaceNamespace(record.Namespace)
		record.StepID = strings.TrimSpace(record.StepID)
		record.StepKey = strings.TrimSpace(record.StepKey)
		record.AgentProfile = strings.TrimSpace(record.AgentProfile)
		record.SummaryFile = WorkspaceArtifactPath(workspaceRootDir, record.SummaryFile)
		record.ResultFile = WorkspaceArtifactPath(workspaceRootDir, record.ResultFile)
		record.TimelineFile = WorkspaceArtifactPath(workspaceRootDir, record.TimelineFile)
		for i, ref := range record.References {
			record.References[i] = WorkspaceArtifactPath(workspaceRootDir, ref)
		}
		if record.ContextKey == "" || record.StepID == "" || record.PlanVersion <= 0 {
			return fmt.Errorf("invalid step context record: context_key/step_id/plan_version is required")
		}

		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal step context record failed: %w", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if buf.Len() == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(absPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open step_contexts.jsonl failed: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("append step_contexts.jsonl failed: %w", err)
	}
	return nil
}

// LoadWorkspaceStepContextRecords loads step context records from workspace/step_contexts.jsonl.
//
// If limit > 0, it returns at most the last `limit` records (in original order).
func LoadWorkspaceStepContextRecords(workspaceRootDir string, limit int) ([]*StepContextRecord, error) {
	absPath := WorkspaceStepContextsFileAbs(workspaceRootDir)
	if absPath == "" {
		return nil, fmt.Errorf("workspace root is empty")
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	if limit <= 0 {
		out := make([]*StepContextRecord, 0)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var record StepContextRecord
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				return nil, fmt.Errorf("unmarshal step context record failed: %w", err)
			}
			if strings.TrimSpace(record.ContextKey) == "" {
				continue
			}
			record.SummaryFile = WorkspaceArtifactPath(workspaceRootDir, record.SummaryFile)
			record.ResultFile = WorkspaceArtifactPath(workspaceRootDir, record.ResultFile)
			record.TimelineFile = WorkspaceArtifactPath(workspaceRootDir, record.TimelineFile)
			for i, ref := range record.References {
				record.References[i] = WorkspaceArtifactPath(workspaceRootDir, ref)
			}
			out = append(out, &record)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan step context records failed: %w", err)
		}
		return out, nil
	}

	// Ring buffer for the last `limit` records.
	ring := make([]*StepContextRecord, 0, limit)
	seen := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record StepContextRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("unmarshal step context record failed: %w", err)
		}
		if strings.TrimSpace(record.ContextKey) == "" {
			continue
		}
		record.SummaryFile = WorkspaceArtifactPath(workspaceRootDir, record.SummaryFile)
		record.ResultFile = WorkspaceArtifactPath(workspaceRootDir, record.ResultFile)
		record.TimelineFile = WorkspaceArtifactPath(workspaceRootDir, record.TimelineFile)
		for i, ref := range record.References {
			record.References[i] = WorkspaceArtifactPath(workspaceRootDir, ref)
		}
		rec := record
		if len(ring) < limit {
			ring = append(ring, &rec)
		} else {
			ring[seen%limit] = &rec
		}
		seen++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan step context records failed: %w", err)
	}
	if len(ring) == 0 {
		return nil, nil
	}
	if seen <= limit {
		return ring, nil
	}
	start := seen % limit
	out := make([]*StepContextRecord, 0, len(ring))
	out = append(out, ring[start:]...)
	out = append(out, ring[:start]...)
	return out, nil
}
