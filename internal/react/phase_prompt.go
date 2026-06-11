package react

import (
	_ "embed"
)

// planningSystemPrompt 是 plan / step_replan 两相位共用的 system prompt 单文件，
// 由 IS_REPLAN_PHASE 在渲染期分阶段展开（plan=任务规划，step_replan=交付复核+重编排）。
//
//go:embed prompts/planning_system.prompt
var planningSystemPrompt string

//go:embed prompts/step_replan_user.prompt
var stepReplanUserPrompt string

//go:embed prompts/final_answer_system.prompt
var finalAnswerSystemPrompt string

//go:embed prompts/final_answer_user.prompt
var finalAnswerUserPrompt string

//go:embed prompts/agent_identity_env.prompt
var agentIdentityEnvPrompt string
