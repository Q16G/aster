package react

import (
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/builtin_tools"
)

func (a *Agent) BuildStepReplanPrompt(payload map[string]any) (string, error) {
	if a == nil || a.promptManager == nil {
		return "", fmt.Errorf("step replan prompt manager is nil")
	}
	var skillsCtx *SkillsPromptContext
	if sc, ok := payload["skills_context"].(*SkillsPromptContext); ok {
		skillsCtx = sc
	}
	var availableTools []AvailableToolInfo
	if at, ok := payload["available_tools"].([]AvailableToolInfo); ok {
		availableTools = at
	}
	return a.promptManager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentRole:            strings.TrimSpace(a.cfg.Role),
		AgentBackground:      strings.TrimSpace(a.cfg.Background),
		AgentInstruction:     strings.TrimSpace(a.cfg.Instruction),
		CurrentGoal:          payload["current_goal"],
		GoalUnderstanding:    stringFromPayload(payload, "goal_understanding"),
		RuntimeRepoContext:   a.runtimeRepoContext,
		InputTimeline:        payload["input_timeline"],
		CurrentStepCard:      payload["current_step_card"],
		PlanOverview:         payload["plan_overview"],
		OpenItemsLedger:      stringFromPayload(payload, "open_items_ledger"),
		TaskContextBoard:     stringFromPayload(payload, "task_context_board"),
		StepFileContent:      stringFromPayload(payload, "step_file_content"),
		StepResultPath:       stringFromPayload(payload, "step_result_path"),
		StepContextsPath:     stringFromPayload(payload, "step_contexts_path"),
		StepTranscriptPath:   stringFromPayload(payload, "step_transcript_path"),
		StepTimelinePath:     stringFromPayload(payload, "step_timeline_path"),
		OpenItemsArchivePath: stringFromPayload(payload, "open_items_archive_path"),
		SkillsContext:        skillsCtx,
		HasSkillsTable:       skillsCtx != nil && skillsCtx.HasTable(),
		AvailableTools:       availableTools,
		HasAvailableTools:    len(availableTools) > 0,
	})
}

func stringFromPayload(payload map[string]any, key string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
}

func (a *Agent) BuildFinalAnswerPrompt(payload map[string]any) (string, error) {
	if a == nil || a.promptManager == nil {
		return "", fmt.Errorf("final answer prompt manager is nil")
	}
	return a.promptManager.BuildFinalAnswerPrompt(FinalAnswerPromptInput{
		AgentRole:          strings.TrimSpace(a.cfg.Role),
		AgentBackground:    strings.TrimSpace(a.cfg.Background),
		AgentInstruction:   strings.TrimSpace(a.cfg.Instruction),
		Status:             payload["status"],
		StateError:         payload["state_error"],
		InputTimeline:      payload["input_timeline"],
		GoalUnderstanding:  stringFromPayload(payload, "goal_understanding"),
		PlanItems:          payload["plan_items"],
		OpenItemsLedger:    stringFromPayload(payload, "open_items_ledger"),
		Warnings:           payload["warnings"],
		WorkspaceSharedDir: stringFromPayload(payload, "workspace_shared_dir"),
		RuntimeRepoContext: a.runtimeRepoContext,
	})
}

// axisKind 标识三轴未决盘点的某一轴，用于从 sticky 状态取对应承载项。
type axisKind int

const (
	axisIncomplete axisKind = iota
	axisDepth
	axisNewSurfaces
)

// carriedAxisItems 从 sticky 三轴状态取出指定轴的承载项（投影为人类可读字符串：
// item + 证据附注，旧 prompt 注入与计数沿用字符串形态）；nil 安全。
func carriedAxisItems(axes *builtin_tools.ReplanAxes, kind axisKind) []string {
	if axes == nil {
		return nil
	}
	switch kind {
	case axisIncomplete:
		return builtin_tools.AxisItemStrings(axes.IncompleteItems)
	case axisDepth:
		return builtin_tools.AxisItemStrings(axes.DepthGaps)
	case axisNewSurfaces:
		return builtin_tools.AxisItemStrings(axes.NewSurfaces)
	default:
		return nil
	}
}

func prettyJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(raw)
}
