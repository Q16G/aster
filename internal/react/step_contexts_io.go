package react

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"aster/internal/builtin_tools"
	"aster/internal/workspacefs"
)

// step_contexts.jsonl 的 IO 实现（M5b 自 builtin_tools 迁入 react）：
// 记录类型 StepContextRecord 与 NormalizeWorkspaceNamespace / WorkspaceArtifactPath
// 仍留在 builtin_tools（工具侧契约），本文件只承担读写。
// 路径经 workspacefs.Layout，IO 经 workspacefs.Store（含防穿越与 per-key 锁）。

func AppendWorkspaceStepContextRecords(workspaceRootDir string, records []*builtin_tools.StepContextRecord) error {
	if len(records) == 0 {
		return nil
	}
	store, err := workspacefs.NewLocalStore(workspaceRootDir)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	for _, record := range records {
		if record == nil {
			continue
		}
		record.ContextKey = strings.TrimSpace(record.ContextKey)
		record.Namespace = builtin_tools.NormalizeWorkspaceNamespace(record.Namespace)
		record.StepID = strings.TrimSpace(record.StepID)
		record.StepKey = strings.TrimSpace(record.StepKey)
		record.AgentProfile = strings.TrimSpace(record.AgentProfile)
		record.SummaryFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.SummaryFile)
		record.ResultFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.ResultFile)
		record.TimelineFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.TimelineFile)
		for i, ref := range record.References {
			record.References[i] = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, ref)
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

	rel := workspacefs.New(workspaceRootDir, "").StepContextsRel()
	if err := store.Append(rel, buf.Bytes()); err != nil {
		return fmt.Errorf("append step_contexts.jsonl failed: %w", err)
	}
	return nil
}

// LoadWorkspaceStepContextRecords loads step context records from workspace/step_contexts.jsonl.
//
// If limit > 0, it returns at most the last `limit` records (in original order).
func LoadWorkspaceStepContextRecords(workspaceRootDir string, limit int) ([]*builtin_tools.StepContextRecord, error) {
	store, err := workspacefs.NewLocalStore(workspaceRootDir)
	if err != nil {
		return nil, err
	}

	data, err := store.Read(workspacefs.New(workspaceRootDir, "").StepContextsRel())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	if limit <= 0 {
		out := make([]*builtin_tools.StepContextRecord, 0)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var record builtin_tools.StepContextRecord
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				return nil, fmt.Errorf("unmarshal step context record failed: %w", err)
			}
			if strings.TrimSpace(record.ContextKey) == "" {
				continue
			}
			record.SummaryFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.SummaryFile)
			record.ResultFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.ResultFile)
			record.TimelineFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.TimelineFile)
			for i, ref := range record.References {
				record.References[i] = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, ref)
			}
			out = append(out, &record)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan step context records failed: %w", err)
		}
		return out, nil
	}

	// Ring buffer for the last `limit` records.
	ring := make([]*builtin_tools.StepContextRecord, 0, limit)
	seen := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record builtin_tools.StepContextRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, fmt.Errorf("unmarshal step context record failed: %w", err)
		}
		if strings.TrimSpace(record.ContextKey) == "" {
			continue
		}
		record.SummaryFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.SummaryFile)
		record.ResultFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.ResultFile)
		record.TimelineFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.TimelineFile)
		for i, ref := range record.References {
			record.References[i] = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, ref)
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
	out := make([]*builtin_tools.StepContextRecord, 0, len(ring))
	out = append(out, ring[start:]...)
	out = append(out, ring[:start]...)
	return out, nil
}
