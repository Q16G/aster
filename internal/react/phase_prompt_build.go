package react

import (
	"encoding/json"
	"fmt"
	"strings"
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
		AgentRole:           strings.TrimSpace(a.cfg.Role),
		AgentBackground:     strings.TrimSpace(a.cfg.Background),
		IsSubAgent:          a.cfg.IsSubAgent,
		CurrentGoal:         payload["current_goal"],
		GoalUnderstanding:   stringFromPayload(payload, "goal_understanding"),
		CurrentPhase:        stringFromPayload(payload, "current_phase"),
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
		SkillsContext:       skillsCtx,
		HasSkillsTable:      skillsCtx != nil && skillsCtx.HasTable(),
		AvailableTools:      availableTools,
		HasAvailableTools:   len(availableTools) > 0,
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
		AgentRole:          strings.TrimSpace(a.cfg.Role),
		AgentBackground:    strings.TrimSpace(a.cfg.Background),
		Status:             payload["status"],
		StateError:         payload["state_error"],
		InputTimeline:      payload["input_timeline"],
		GoalUnderstanding:  stringFromPayload(payload, "goal_understanding"),
		PlanItems:          payload["plan_items"],
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

func prettyJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(raw)
}
