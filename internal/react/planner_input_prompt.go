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
	AgentRole             string
	AgentBackground       string
	AgentInstruction      string
	HandoffContext        string
	InputTimeline         string
	TaskItemsJSON         string
	ReplanContextJSON     string
	ExecutionLineJSON     string
	HasExecutionLine      bool
	WorkspaceContextsJSON string
	HasWorkspaceContexts  bool
}
