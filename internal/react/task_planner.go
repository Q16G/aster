package react

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
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
	var exhausted *structuredoutput.ExhaustedError
	if errors.As(err, &exhausted) && exhausted != nil {
		if last := exhausted.LastAttempt(); last != nil && last.ErrorType == structuredoutput.ErrorTypeModelCallFailed {
			return nil, err
		}
	}
	// 兜底：planner 输出不合法时，回退为“无需规划”，由 runtime 走隐式单步 plan 推进。
	explanation := "planner structured output retry exhausted, fallback to direct execution"
	if raw := structuredoutput.LastResponse(err); raw != "" {
		explanation = fmt.Sprintf("%s: %s", explanation, err.Error())
		explanation = fmt.Sprintf("%s; last_response=%s", explanation, raw)
	} else if err != nil {
		explanation = fmt.Sprintf("%s: %s", explanation, err.Error())
	}
	return &builtin_tools.TaskPlannerResult{
		NeedsPlanning: false,
		Plan:          []*builtin_tools.PlanItem{},
		Explanation:   explanation,
	}, nil
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
