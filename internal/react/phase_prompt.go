package react

import (
	_ "embed"
)

//go:embed prompts/planning_system.prompt
var planningSystemPrompt string

//go:embed prompts/step_replan_system.prompt
var stepReplanSystemPrompt string

//go:embed prompts/step_replan_user.prompt
var stepReplanUserPrompt string

//go:embed prompts/final_answer_system.prompt
var finalAnswerSystemPrompt string

//go:embed prompts/final_answer_user.prompt
var finalAnswerUserPrompt string

//go:embed prompts/agent_identity_env.prompt
var agentIdentityEnvPrompt string
