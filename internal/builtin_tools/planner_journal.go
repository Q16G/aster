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
// 文件语义：plan 真相源；每次写入按"读旧 + 合并新 + atomic 重写"做 snapshot，
// 磁盘上只保留最新 plan_version 的合并后状态（kind=plan 全量行）。
// 旧 session 的 append-only 文件仍可被 LoadPlannerJournal 重放（兼容读端）。
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

// AppendPlannerJournalRecords 把 records 合并入当前 planner.jsonl 的 snapshot 并
// 原子重写。语义与旧的 append-only 一致——按 LoadPlannerJournal 的重放规则消化：
// kind=plan 且 plan_version 更高时全量替换；kind=step 按 id 覆盖。重写后磁盘上
// 只保留最新 plan_version 的合并状态（一行一个 item，kind=plan），不再保留历史
// 增量行。崩溃安全靠 temp file + os.Rename 原子替换。
func AppendPlannerJournalRecords(workspaceRootDir string, records []*PlannerJournalRecord) error {
	if len(records) == 0 {
		return nil
	}
	absPath := WorkspacePlannerJournalFileAbs(workspaceRootDir)
	if absPath == "" {
		return fmt.Errorf("workspace root is empty")
	}

	// 1) 读现有 snapshot 作为合并基底（不存在视作空）。
	existingItems, planVersion, err := LoadPlannerJournal(workspaceRootDir)
	if err != nil {
		return fmt.Errorf("load planner.jsonl snapshot failed: %w", err)
	}
	order := make([]string, 0, len(existingItems)+len(records))
	byID := make(map[string]*PlanItem, len(existingItems)+len(records))
	for _, it := range existingItems {
		if it == nil {
			continue
		}
		id := strings.TrimSpace(it.ID)
		if id == "" {
			continue
		}
		if _, exists := byID[id]; !exists {
			order = append(order, id)
		}
		byID[id] = it
	}
	upsert := func(item *PlanItem) {
		id := strings.TrimSpace(item.ID)
		if _, exists := byID[id]; !exists {
			order = append(order, id)
		}
		byID[id] = item
	}

	// 2) 按 LoadPlannerJournal 的语义把新 records 应用进合并基底。
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

		switch record.Kind {
		case PlannerJournalKindPlan:
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
				order = order[:0]
				byID = make(map[string]*PlanItem)
			}
			upsert(record.Item)
		case PlannerJournalKindStep:
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
			}
			upsert(record.Item)
		}
	}

	// 3) 合并后无有效内容时不动文件（与旧 append-only 行为一致）。
	if planVersion <= 0 || len(order) == 0 {
		return nil
	}

	// 4) 序列化合并后的最新 snapshot——每个 item 一行 kind=plan 记录。
	var buf bytes.Buffer
	now := time.Now()
	for _, id := range order {
		item := byID[id]
		if item == nil {
			continue
		}
		rec := &PlannerJournalRecord{
			Kind:        PlannerJournalKindPlan,
			PlanVersion: planVersion,
			Item:        item,
			CreatedAt:   now,
		}
		data, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("marshal planner journal record failed: %w", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if buf.Len() == 0 {
		return nil
	}

	// 5) 原子重写：temp file → rename。
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}
	tmpPath := absPath + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write planner.jsonl tmp failed: %w", err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename planner.jsonl failed: %w", err)
	}
	return nil
}

// LoadPlannerJournal 重放 planner.jsonl，返回最新 plan 状态与版本号。
// 重放规则：kind=plan 且版本号更高时整体替换（planner 提交的全量集合已含保留项）；
// kind=step 按 id 覆盖。条目保持首次出现的顺序。文件不存在时返回 (nil, 0, nil)。
// 新写路径（AppendPlannerJournalRecords）已改为 snapshot 重写，磁盘文件只含最新
// plan_version 的合并行；但本函数对旧 session 的 append-only 文件保持兼容重放。
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
