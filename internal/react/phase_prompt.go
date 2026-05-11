package react

import (
	_ "embed"
)

//go:embed prompts/step_replan.prompt
var stepReplanPrompt string

//go:embed prompts/final_answer.prompt
var finalAnswerPrompt string
