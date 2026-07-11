package react

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"aster/internal/builtin_tools"
	"aster/internal/workspacefs"
)

// planner.jsonl 的 IO 实现（M5b 自 builtin_tools 迁入 react）：
// 记录类型 PlannerJournalRecord / PlanItem / AnalysisTopic 与 PlannerJournalKind*
// 常量仍留在 builtin_tools（工具侧契约），本文件只承担读写。
// 路径经 workspacefs.Layout，IO 经 workspacefs.Store（含防穿越与 per-key 锁）。

// AppendPlannerJournalRecords 把 records 合并入当前 planner.jsonl 的 snapshot 并
// 原子重写。语义与旧的 append-only 一致——按 LoadPlannerJournal 的重放规则消化：
// kind=plan 且 plan_version 更高时全量替换（items 与 phases 一并 reset）；
// kind=step / kind=phase 按 id 覆盖。重写后磁盘上只保留最新 plan_version 的合并状态
// （item 行 kind=plan + phase 行 kind=phase），不再保留历史增量行。
// 崩溃安全靠 Store.WriteAtomic（temp file + rename 原子替换）。
// 契约：提升 plan_version 的批次里，kind=plan 行必须排在 kind=phase 行之前，
// 否则新版本 phase 行会被随后的全量 reset 冲掉。
func AppendPlannerJournalRecords(workspaceRootDir string, records []*builtin_tools.PlannerJournalRecord) error {
	if len(records) == 0 {
		return nil
	}
	store, err := workspacefs.NewLocalStore(workspaceRootDir)
	if err != nil {
		return err
	}

	// 1) 读现有 snapshot 作为合并基底（不存在视作空）。
	existingItems, existingTopics, planVersion, err := LoadPlannerJournalSnapshot(workspaceRootDir)
	if err != nil {
		return fmt.Errorf("load planner.jsonl snapshot failed: %w", err)
	}
	order := make([]string, 0, len(existingItems)+len(records))
	byID := make(map[string]*builtin_tools.PlanItem, len(existingItems)+len(records))
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
	upsert := func(item *builtin_tools.PlanItem) {
		id := strings.TrimSpace(item.ID)
		if _, exists := byID[id]; !exists {
			order = append(order, id)
		}
		byID[id] = item
	}

	phaseOrder := make([]string, 0, len(existingTopics)+len(records))
	phaseByID := make(map[string]*builtin_tools.AnalysisTopic, len(existingTopics)+len(records))
	upsertTopic := func(phase *builtin_tools.AnalysisTopic) {
		id := strings.TrimSpace(phase.ID)
		if _, exists := phaseByID[id]; !exists {
			phaseOrder = append(phaseOrder, id)
		}
		phaseByID[id] = phase
	}
	for _, phase := range existingTopics {
		if phase == nil || strings.TrimSpace(phase.ID) == "" {
			continue
		}
		upsertTopic(phase)
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

		if record.Kind == builtin_tools.PlannerJournalKindTopic {
			if record.Phase == nil || strings.TrimSpace(record.Phase.ID) == "" {
				return fmt.Errorf("invalid planner journal record: phase.id is required")
			}
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
			}
			upsertTopic(record.Phase)
			continue
		}

		if record.Kind != builtin_tools.PlannerJournalKindPlan && record.Kind != builtin_tools.PlannerJournalKindStep {
			return fmt.Errorf("invalid planner journal record: unknown kind %q", record.Kind)
		}
		if record.Item == nil || strings.TrimSpace(record.Item.ID) == "" {
			return fmt.Errorf("invalid planner journal record: item.id is required")
		}
		record.Item.StepFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.Item.StepFile)
		record.Item.ResultFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.Item.ResultFile)
		record.Item.TimelineFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.Item.TimelineFile)
		record.Item.CoverageFile = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, record.Item.CoverageFile)
		for i, ref := range record.Item.References {
			record.Item.References[i] = builtin_tools.WorkspaceArtifactPath(workspaceRootDir, ref)
		}

		switch record.Kind {
		case builtin_tools.PlannerJournalKindPlan:
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
				order = order[:0]
				byID = make(map[string]*builtin_tools.PlanItem)
				phaseOrder = phaseOrder[:0]
				phaseByID = make(map[string]*builtin_tools.AnalysisTopic)
			}
			upsert(record.Item)
		case builtin_tools.PlannerJournalKindStep:
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
		rec := &builtin_tools.PlannerJournalRecord{
			Kind:        builtin_tools.PlannerJournalKindPlan,
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
		rec := &builtin_tools.PlannerJournalRecord{
			Kind:        builtin_tools.PlannerJournalKindTopic,
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

	// 5) 原子重写：Store.WriteAtomic（temp file → rename）。
	rel := workspacefs.New(workspaceRootDir, "").PlannerJournalRel()
	if err := store.WriteAtomic(rel, buf.Bytes()); err != nil {
		return fmt.Errorf("write planner.jsonl failed: %w", err)
	}
	return nil
}

// LoadPlannerJournal 重放 planner.jsonl，返回最新 plan 状态与版本号。
// 兼容 wrapper：新读端优先用 LoadPlannerJournalSnapshot（含 phases）。
func LoadPlannerJournal(workspaceRootDir string) ([]*builtin_tools.PlanItem, int, error) {
	items, _, version, err := LoadPlannerJournalSnapshot(workspaceRootDir)
	return items, version, err
}

// LoadPlannerJournalSnapshot 重放 planner.jsonl，返回最新 plan items、phases 与版本号。
// 重放规则：kind=plan 且版本号更高时整体替换（items 与 phases 一并 reset，planner
// 提交的全量集合已含保留项）；kind=step / kind=phase 按 id 覆盖。条目保持首次出现
// 的顺序。文件不存在时返回 (nil, nil, 0, nil)。旧文件无 phase 行时 phases 为 nil，
// 由 runtime 的 SynthesizeTopicsIfMissing 兜底。
// 新写路径（AppendPlannerJournalRecords）已改为 snapshot 重写，磁盘文件只含最新
// plan_version 的合并行；但本函数对旧 session 的 append-only 文件保持兼容重放。
func LoadPlannerJournalSnapshot(workspaceRootDir string) ([]*builtin_tools.PlanItem, []*builtin_tools.AnalysisTopic, int, error) {
	store, err := workspacefs.NewLocalStore(workspaceRootDir)
	if err != nil {
		return nil, nil, 0, err
	}
	data, err := store.Read(workspacefs.New(workspaceRootDir, "").PlannerJournalRel())
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
	byID := make(map[string]*builtin_tools.PlanItem)
	phaseOrder := make([]string, 0)
	phaseByID := make(map[string]*builtin_tools.AnalysisTopic)
	reset := func() {
		order = order[:0]
		byID = make(map[string]*builtin_tools.PlanItem)
		phaseOrder = phaseOrder[:0]
		phaseByID = make(map[string]*builtin_tools.AnalysisTopic)
	}
	upsert := func(item *builtin_tools.PlanItem) {
		id := strings.TrimSpace(item.ID)
		if _, exists := byID[id]; !exists {
			order = append(order, id)
		}
		byID[id] = item
	}
	upsertTopic := func(phase *builtin_tools.AnalysisTopic) {
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
		var record builtin_tools.PlannerJournalRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, nil, 0, fmt.Errorf("unmarshal planner journal record failed: %w", err)
		}
		if record.Kind == builtin_tools.PlannerJournalKindTopic {
			if record.Phase == nil || strings.TrimSpace(record.Phase.ID) == "" {
				continue
			}
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
			}
			upsertTopic(record.Phase)
			continue
		}
		if record.Item == nil || strings.TrimSpace(record.Item.ID) == "" {
			continue
		}
		switch record.Kind {
		case builtin_tools.PlannerJournalKindPlan:
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
				reset()
			}
			upsert(record.Item)
		case builtin_tools.PlannerJournalKindStep:
			if record.PlanVersion > planVersion {
				planVersion = record.PlanVersion
			}
			upsert(record.Item)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, 0, fmt.Errorf("scan planner journal failed: %w", err)
	}

	var phases []*builtin_tools.AnalysisTopic
	if len(phaseOrder) > 0 {
		phases = make([]*builtin_tools.AnalysisTopic, 0, len(phaseOrder))
		for _, id := range phaseOrder {
			phases = append(phases, phaseByID[id])
		}
	}
	if len(order) == 0 {
		return nil, phases, planVersion, nil
	}
	out := make([]*builtin_tools.PlanItem, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, phases, planVersion, nil
}
