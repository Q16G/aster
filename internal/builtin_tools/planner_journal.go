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

	"aster/internal/workspacefs"
)

const (
	// PlannerJournalKindPlan 表示 plan 提交（首次规划 / 重规划）时的全量条目落地。
	PlannerJournalKindPlan = "plan"
	// PlannerJournalKindStep 表示 step 终态时的增量条目落地（同 id 覆盖）。
	PlannerJournalKindStep = "step"
	// PlannerJournalKindPhase 表示业务 lane（PlanPhase）条目：plan 提交时随全量落地，
	// step_replan 的 phase_assessments 承接时按 phase.id 增量覆盖。
	PlannerJournalKindPhase = "phase"
)

// PlannerJournalRecord 是 planner.jsonl 的单行记录。
// 文件语义：plan 真相源；每次写入按"读旧 + 合并新 + atomic 重写"做 snapshot，
// 磁盘上只保留最新 plan_version 的合并后状态（item 行 kind=plan + phase 行 kind=phase）。
// 旧 session 的 append-only 文件仍可被 LoadPlannerJournal 重放（兼容读端）。
type PlannerJournalRecord struct {
	Kind        string     `json:"kind"`
	PlanVersion int        `json:"plan_version"`
	Item        *PlanItem  `json:"item,omitempty"`
	Phase       *PlanPhase `json:"phase,omitempty"`
	CreatedAt   time.Time  `json:"created_at,omitempty"`
}

// AppendPlannerJournalRecords 把 records 合并入当前 planner.jsonl 的 snapshot 并
// 原子重写。语义与旧的 append-only 一致——按 LoadPlannerJournal 的重放规则消化：
// kind=plan 且 plan_version 更高时全量替换（items 与 phases 一并 reset）；
// kind=step / kind=phase 按 id 覆盖。重写后磁盘上只保留最新 plan_version 的合并状态
// （item 行 kind=plan + phase 行 kind=phase），不再保留历史增量行。
// 崩溃安全靠 temp file + os.Rename 原子替换。
// 契约：提升 plan_version 的批次里，kind=plan 行必须排在 kind=phase 行之前，
// 否则新版本 phase 行会被随后的全量 reset 冲掉。
func AppendPlannerJournalRecords(workspaceRootDir string, records []*PlannerJournalRecord) error {
	if len(records) == 0 {
		return nil
	}
	absPath := workspacefs.New(workspaceRootDir, "").PlannerJournal()
	if absPath == "" {
		return fmt.Errorf("workspace root is empty")
	}

	// 1) 读现有 snapshot 作为合并基底（不存在视作空）。
	existingItems, existingPhases, planVersion, err := LoadPlannerJournalSnapshot(workspaceRootDir)
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

	phaseOrder := make([]string, 0, len(existingPhases)+len(records))
	phaseByID := make(map[string]*PlanPhase, len(existingPhases)+len(records))
	upsertPhase := func(phase *PlanPhase) {
		id := strings.TrimSpace(phase.ID)
		if _, exists := phaseByID[id]; !exists {
			phaseOrder = append(phaseOrder, id)
		}
		phaseByID[id] = phase
	}
	for _, phase := range existingPhases {
		if phase == nil || strings.TrimSpace(phase.ID) == "" {
			continue
		}
		upsertPhase(phase)
	}

	// 2) 按 LoadPlannerJournal 的语义把新 records 应用进合并基底。
	for _, record := range records {
		if record == nil {
			continue
		}
		record.Kind = strings.TrimSpace(record.Kind)
		if record.PlanVersion <= 0 {
			return fmt.Errorf("invalid planner journal record: plan_version is required")
		}

		if record.Kind == PlannerJournalKindPhase {
			if record.Phase == nil || strings.TrimSpace(record.Phase.ID) == "" {
				return fmt.Errorf("invalid planner journal record: phase.id is required")
			}
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
			}
			upsertPhase(record.Phase)
			continue
		}

		if record.Kind != PlannerJournalKindPlan && record.Kind != PlannerJournalKindStep {
			return fmt.Errorf("invalid planner journal record: unknown kind %q", record.Kind)
		}
		if record.Item == nil || strings.TrimSpace(record.Item.ID) == "" {
			return fmt.Errorf("invalid planner journal record: item.id is required")
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
				phaseOrder = phaseOrder[:0]
				phaseByID = make(map[string]*PlanPhase)
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

	// 4) 序列化合并后的最新 snapshot——item 行在前（kind=plan），phase 行在后
	//    （kind=phase），与「版本提升批次 plan 行在前」的重放契约一致。
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
	for _, id := range phaseOrder {
		phase := phaseByID[id]
		if phase == nil {
			continue
		}
		rec := &PlannerJournalRecord{
			Kind:        PlannerJournalKindPhase,
			PlanVersion: planVersion,
			Phase:       phase,
			CreatedAt:   now,
		}
		data, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("marshal planner journal phase record failed: %w", err)
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
// 兼容 wrapper：新读端优先用 LoadPlannerJournalSnapshot（含 phases）。
func LoadPlannerJournal(workspaceRootDir string) ([]*PlanItem, int, error) {
	items, _, version, err := LoadPlannerJournalSnapshot(workspaceRootDir)
	return items, version, err
}

// LoadPlannerJournalSnapshot 重放 planner.jsonl，返回最新 plan items、phases 与版本号。
// 重放规则：kind=plan 且版本号更高时整体替换（items 与 phases 一并 reset，planner
// 提交的全量集合已含保留项）；kind=step / kind=phase 按 id 覆盖。条目保持首次出现
// 的顺序。文件不存在时返回 (nil, nil, 0, nil)。旧文件无 phase 行时 phases 为 nil，
// 由 runtime 的 SynthesizePhasesIfMissing 兜底。
// 新写路径（AppendPlannerJournalRecords）已改为 snapshot 重写，磁盘文件只含最新
// plan_version 的合并行；但本函数对旧 session 的 append-only 文件保持兼容重放。
func LoadPlannerJournalSnapshot(workspaceRootDir string) ([]*PlanItem, []*PlanPhase, int, error) {
	absPath := workspacefs.New(workspaceRootDir, "").PlannerJournal()
	if absPath == "" {
		return nil, nil, 0, fmt.Errorf("workspace root is empty")
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, 0, nil
		}
		return nil, nil, 0, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	planVersion := 0
	order := make([]string, 0)
	byID := make(map[string]*PlanItem)
	phaseOrder := make([]string, 0)
	phaseByID := make(map[string]*PlanPhase)
	reset := func() {
		order = order[:0]
		byID = make(map[string]*PlanItem)
		phaseOrder = phaseOrder[:0]
		phaseByID = make(map[string]*PlanPhase)
	}
	upsert := func(item *PlanItem) {
		id := strings.TrimSpace(item.ID)
		if _, exists := byID[id]; !exists {
			order = append(order, id)
		}
		byID[id] = item
	}
	upsertPhase := func(phase *PlanPhase) {
		id := strings.TrimSpace(phase.ID)
		if _, exists := phaseByID[id]; !exists {
			phaseOrder = append(phaseOrder, id)
		}
		phaseByID[id] = phase
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record PlannerJournalRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, nil, 0, fmt.Errorf("unmarshal planner journal record failed: %w", err)
		}
		if record.Kind == PlannerJournalKindPhase {
			if record.Phase == nil || strings.TrimSpace(record.Phase.ID) == "" {
				continue
			}
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
			}
			upsertPhase(record.Phase)
			continue
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
		return nil, nil, 0, fmt.Errorf("scan planner journal failed: %w", err)
	}

	var phases []*PlanPhase
	if len(phaseOrder) > 0 {
		phases = make([]*PlanPhase, 0, len(phaseOrder))
		for _, id := range phaseOrder {
			phases = append(phases, phaseByID[id])
		}
	}
	if len(order) == 0 {
		return nil, phases, planVersion, nil
	}
	out := make([]*PlanItem, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, phases, planVersion, nil
}
