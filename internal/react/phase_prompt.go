package react

import (
	_ "embed"
)

//go:embed prompts/step_summary.prompt
var stepSummaryPrompt string

//go:embed prompts/final_answer.prompt
var finalAnswerPrompt string

//go:embed prompts/reducer.prompt
var reducerPrompt string
