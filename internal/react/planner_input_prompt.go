package react

import (
	_ "embed"
	"text/template"
)

//go:embed prompts/planner_input.prompt
var plannerInputPromptText string

var plannerInputTmpl = template.Must(
	template.New("planner_input").Parse(plannerInputPromptText),
)

type plannerInputData struct {
	HandoffContext string
	InputTimeline  string
	TaskItemsJSON  string
	// PhasesJSON 是既有业务 lane 清单（含状态），重规划回合供 planner 承接：
	// completed/blocked 项保留、pending 项续释放或显式收束。
	PhasesJSON string
	// PlannerJournalPath 是 workspace/planner.jsonl（plan 唯一真相源）的绝对路径，
	// 文件存在才注入，供卡片不足时按需回读。
	PlannerJournalPath  string
	ReplanContextJSON   string
	RecoveryContextJSON string
}
