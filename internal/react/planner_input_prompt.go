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
	// PlannerJournalPath 是 workspace/planner.jsonl（plan 唯一真相源）的绝对路径，
	// 文件存在才注入，供卡片不足时按需回读。
	PlannerJournalPath  string
	ReplanContextJSON   string
	RecoveryContextJSON string
}
