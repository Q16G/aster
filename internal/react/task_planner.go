package react

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/jsonextractor"
	"aster/internal/structuredoutput"
)

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

// Plan 执行任务规划
func (p *DefaultTaskPlanner) Plan(ctx context.Context, input string) (*builtin_tools.TaskPlannerResult, error) {
	if p == nil || p.aiClient == nil {
		return nil, fmt.Errorf("task planner not initialized")
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("input is required")
	}

	if p.promptManager == nil {
		return nil, fmt.Errorf("task prompt manager is nil")
	}
	prompt, err := p.promptManager.BuildTaskPlannerPrompt(input)
	if err != nil {
		return nil, fmt.Errorf("build task plan prompt failed: %w", err)
	}
	retryResult, err := structuredoutput.RunWithRetry(ctx, p.aiClient, "planner", prompt, structuredoutput.Config{}, parseTaskPlannerResult)
	if err == nil {
		return &retryResult.Value, nil
	}
	// planner structured output 重试耗尽时直接失败（fail fast），避免将 last_response 或内部诊断信息回传给用户。
	// 需要诊断时可通过 structuredoutput 日志（response_excerpt / last_response）排查。
	return nil, fmt.Errorf("planner structured output retry exhausted: %w", err)
}

func parseTaskPlannerResult(raw string) (builtin_tools.TaskPlannerResult, error) {
	var zero builtin_tools.TaskPlannerResult
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return zero, structuredoutput.MissingJSONObjectError("planner output is empty")
	}
	stdjsons, rawCandidates := jsonextractor.ExtractJSONWithRaw(raw)
	if len(stdjsons) == 0 && len(rawCandidates) == 0 {
		return zero, structuredoutput.MissingJSONObjectError("planner output missing json object")
	}

	candidates := buildJSONCandidates(raw)
	var lastErr error
	for _, candidate := range candidates {
		var result builtin_tools.TaskPlannerResult
		if err := json.Unmarshal([]byte(candidate), &result); err != nil {
			lastErr = err
			continue
		}
		return result, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("planner output invalid json")
	}
	return zero, structuredoutput.UnmarshalFailedError(lastErr)
}
