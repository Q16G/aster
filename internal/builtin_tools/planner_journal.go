package builtin_tools

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const workspacePlannerJournalRelPath = "workspace/planner.jsonl"

const (
	// PlannerJournalKindPlan 表示 plan 提交（首次规划 / 重规划）时的全量条目落地。
	PlannerJournalKindPlan = "plan"
	// PlannerJournalKindStep 表示 step 终态时的增量条目落地（同 id 覆盖）。
	PlannerJournalKindStep = "step"
)

// PlannerJournalRecord 是 planner.jsonl 的单行记录。
// 文件语义：plan 真相源，append-only；最新状态 = 按 id 取最后一条。
type PlannerJournalRecord struct {
	Kind        string    `json:"kind"`
	PlanVersion int       `json:"plan_version"`
	Item        *PlanItem `json:"item"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

func WorkspacePlannerJournalFileAbs(workspaceRootDir string) string {
	workspaceRootDir = strings.TrimSpace(workspaceRootDir)
	if workspaceRootDir == "" {
		return ""
	}
	return filepath.Join(workspaceRootDir, filepath.FromSlash(workspacePlannerJournalRelPath))
}

func AppendPlannerJournalRecords(workspaceRootDir string, records []*PlannerJournalRecord) error {
	if len(records) == 0 {
		return nil
	}
	absPath := WorkspacePlannerJournalFileAbs(workspaceRootDir)
	if absPath == "" {
		return fmt.Errorf("workspace root is empty")
	}

	var buf bytes.Buffer
	for _, record := range records {
		if record == nil || record.Item == nil {
			continue
		}
		record.Kind = strings.TrimSpace(record.Kind)
		if record.Kind != PlannerJournalKindPlan && record.Kind != PlannerJournalKindStep {
			return fmt.Errorf("invalid planner journal record: unknown kind %q", record.Kind)
		}
		if strings.TrimSpace(record.Item.ID) == "" || record.PlanVersion <= 0 {
			return fmt.Errorf("invalid planner journal record: item.id/plan_version is required")
		}
		record.Item.StepFile = WorkspaceArtifactPath(workspaceRootDir, record.Item.StepFile)
		record.Item.ResultFile = WorkspaceArtifactPath(workspaceRootDir, record.Item.ResultFile)
		record.Item.TimelineFile = WorkspaceArtifactPath(workspaceRootDir, record.Item.TimelineFile)
		record.Item.CoverageFile = WorkspaceArtifactPath(workspaceRootDir, record.Item.CoverageFile)
		for i, ref := range record.Item.References {
			record.Item.References[i] = WorkspaceArtifactPath(workspaceRootDir, ref)
		}
		if record.CreatedAt.IsZero() {
			record.CreatedAt = time.Now()
		}

		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("marshal planner journal record failed: %w", err)
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
		return fmt.Errorf("open planner.jsonl failed: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("append planner.jsonl failed: %w", err)
	}
	return nil
}

// LoadPlannerJournal 重放 planner.jsonl，返回最新 plan 状态与版本号。
// 重放规则：kind=plan 且版本号更高时整体替换（planner 提交的全量集合已含保留项）；
// kind=step 按 id 覆盖。条目保持首次出现的顺序。文件不存在时返回 (nil, 0, nil)。
func LoadPlannerJournal(workspaceRootDir string) ([]*PlanItem, int, error) {
	absPath := WorkspacePlannerJournalFileAbs(workspaceRootDir)
	if absPath == "" {
		return nil, 0, fmt.Errorf("workspace root is empty")
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	planVersion := 0
	order := make([]string, 0)
	byID := make(map[string]*PlanItem)
	reset := func() {
		order = order[:0]
		byID = make(map[string]*PlanItem)
	}
	upsert := func(item *PlanItem) {
		id := strings.TrimSpace(item.ID)
		if _, exists := byID[id]; !exists {
			order = append(order, id)
		}
		byID[id] = item
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record PlannerJournalRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, 0, fmt.Errorf("unmarshal planner journal record failed: %w", err)
		}
		if record.Item == nil || strings.TrimSpace(record.Item.ID) == "" {
			continue
		}
		switch record.Kind {
		case PlannerJournalKindPlan:
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
				reset()
			}
			upsert(record.Item)
		case PlannerJournalKindStep:
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
			}
			upsert(record.Item)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("scan planner journal failed: %w", err)
	}
	if len(order) == 0 {
		return nil, planVersion, nil
	}
	out := make([]*PlanItem, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, planVersion, nil
}
