package react

import (
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/builtin_tools"
)

func (a *Agent) BuildStepReplanPrompt(payload map[string]any) (PromptParts, error) {
	if a == nil || a.promptManager == nil {
		return PromptParts{}, fmt.Errorf("step replan prompt manager is nil")
	}
	var skillsCtx *SkillsPromptContext
	if sc, ok := payload["skills_context"].(*SkillsPromptContext); ok {
		skillsCtx = sc
	}
	var availableTools []AvailableToolInfo
	if at, ok := payload["available_tools"].([]AvailableToolInfo); ok {
		availableTools = at
	}
	parts, err := a.promptManager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentProfile:        AgentProfile{AgentRole: strings.TrimSpace(a.cfg.Role), AgentBackground: strings.TrimSpace(a.cfg.Background)},
		RunFlags:            RunFlags{IsSubAgent: a.cfg.IsSubAgent},
		CapabilityIndex:     CapabilityIndex{SkillsContext: skillsCtx, AvailableTools: availableTools},
		CurrentGoal:         payload["current_goal"],
		GoalUnderstanding:   stringFromPayload(payload, "goal_understanding"),
		ActiveTopics:        payload["active_topics"],
		InputTimeline:       payload["input_timeline"],
		ReviewWindow:        payload["review_window"],
		PlanOverview:        payload["plan_overview"],
		PriorBoundaryStepID: stringFromPayload(payload, "prior_boundary_step_id"),
		OpenItemsLedger:     stringFromPayload(payload, "open_items_ledger"),
		TaskContextBoard:    stringFromPayload(payload, "task_context_board"),
		StepFileContent:     stringFromPayload(payload, "step_file_content"),
		StepContextsPath:    stringFromPayload(payload, "step_contexts_path"),
		StepTranscriptPath:  stringFromPayload(payload, "step_transcript_path"),
		OpenItemsPath:       stringFromPayload(payload, "open_items_path"),
		TaskContextPath:     stringFromPayload(payload, "task_context_path"),
		StepFilePath:        stringFromPayload(payload, "step_file_path"),
		PlannerJournalPath:  stringFromPayload(payload, "planner_journal_path"),
		PlannerJournal:      stringFromPayload(payload, "planner_journal"),
	})
	if err != nil {
		return PromptParts{}, err
	}
	parts.SystemAgent = a.identityEnvBlock()
	return parts, nil
}

func stringFromPayload(payload map[string]any, key string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

func (a *Agent) BuildFinalAnswerPrompt(payload map[string]any) (PromptParts, error) {
	if a == nil || a.promptManager == nil {
		return PromptParts{}, fmt.Errorf("final answer prompt manager is nil")
	}
	parts, err := a.promptManager.BuildFinalAnswerPrompt(FinalAnswerPromptInput{
		AgentProfile:       AgentProfile{AgentRole: strings.TrimSpace(a.cfg.Role), AgentBackground: strings.TrimSpace(a.cfg.Background)},
		Status:             payload["status"],
		StateError:         payload["state_error"],
		InputTimeline:      payload["input_timeline"],
		GoalUnderstanding:  stringFromPayload(payload, "goal_understanding"),
		PlanItems:          payload["plan_items"],
		Topics:             payload["topics"],
		PlannerJournalPath: stringFromPayload(payload, "planner_journal_path"),
		OpenItemsLedger:    stringFromPayload(payload, "open_items_ledger"),
		Warnings:           payload["warnings"],
	})
	if err != nil {
		return PromptParts{}, err
	}
	parts.SystemAgent = a.identityEnvBlock()
	return parts, nil
}

// activeTopicsNonEmpty 判断 ACTIVE_PHASES 注入是否含至少一个 phase（供模板 HAS 分支）。
func activeTopicsNonEmpty(value any) bool {
	switch v := value.(type) {
	case []*builtin_tools.AnalysisTopic:
		return len(v) > 0
	case nil:
		return false
	default:
		return false
	}
}

func prettyJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(raw)
}

// stringOrJSON 注入值已是 preview 字符串（PromptContext 产出）时原样注入；
// 兼容旧调用方传结构化值时按 prettyJSON 序列化。
func stringOrJSON(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return prettyJSON(value)
}
