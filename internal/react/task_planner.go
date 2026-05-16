package react

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// PlannerPromptBuilder is an optional interface that TaskPlanner implementations
// can satisfy to enable tool-assisted planning via AICallProxy instead of
// the legacy structuredoutput.RunWithRetry path.
type PlannerPromptBuilder interface {
	BuildPrompt(input string) (string, error)
}

// DefaultTaskPlanner 默认任务规划器
type DefaultTaskPlanner struct {
	aiClient      ai.ChatClient
	promptManager PromptManager
}

// NewDefaultTaskPlanner 创建默认任务规划器
func NewDefaultTaskPlanner(aiClient ai.ChatClient, promptManagers ...PromptManager) *DefaultTaskPlanner {
	var promptManager PromptManager
	if len(promptManagers) > 0 {
		promptManager = promptManagers[0]
	}
	if promptManager == nil {
		promptManager, _ = newDefaultPromptManager()
	}
	return &DefaultTaskPlanner{aiClient: aiClient, promptManager: promptManager}
}

//go:embed prompts/task_planner.prompt
var taskPlanPrompt string

// BuildPrompt builds the planner prompt text without calling the AI model.
// This enables runPlanPhase to use AICallProxy (with tools) instead of structuredoutput.RunWithRetry.
func (p *DefaultTaskPlanner) BuildPrompt(input string) (string, error) {
	if p == nil || p.promptManager == nil {
		return "", fmt.Errorf("task planner prompt manager is nil")
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("input is required")
	}
	return p.promptManager.BuildTaskPlannerPrompt(input)
}

// Plan implements the TaskPlanner interface for backward compatibility with test mocks
// that do not implement PlannerPromptBuilder. Production code always uses
// runPlanPhaseWithTools via the PlannerPromptBuilder path.
func (p *DefaultTaskPlanner) Plan(ctx context.Context, input string) (*builtin_tools.TaskPlannerResult, error) {
	return nil, fmt.Errorf("DefaultTaskPlanner.Plan is deprecated; use PlannerPromptBuilder path")
}
