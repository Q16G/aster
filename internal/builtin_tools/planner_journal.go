package builtin_tools

import (
	"time"
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
// 读写实现（AppendPlannerJournalRecords / LoadPlannerJournal / LoadPlannerJournalSnapshot）
// 在 react 包（M5b 迁移），本包只保留记录类型与 kind 常量契约。
type PlannerJournalRecord struct {
	Kind        string     `json:"kind"`
	PlanVersion int        `json:"plan_version"`
	Item        *PlanItem  `json:"item,omitempty"`
	Phase       *PlanPhase `json:"phase,omitempty"`
	CreatedAt   time.Time  `json:"created_at,omitempty"`
}
