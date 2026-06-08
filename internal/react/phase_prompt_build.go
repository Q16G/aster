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
	return a.promptManager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentRole:              strings.TrimSpace(a.cfg.Role),
		AgentBackground:        strings.TrimSpace(a.cfg.Background),
		AgentInstruction:       strings.TrimSpace(a.cfg.Instruction),
		CurrentGoal:            payload["current_goal"],
		GoalUnderstanding:      stringFromPayload(payload, "goal_understanding"),
		WorkspaceSharedDir:     stringFromPayload(payload, "workspace_shared_dir"),
		RuntimeRepoContext:     a.runtimeRepoContext,
		InputTimeline:          payload["input_timeline"],
		CurrentStep:            payload["current_step"],
		StepOutcome:            payload["step_outcome"],
		TaskPlan:               payload["task_plan"],
		StepOutcomes:           payload["step_outcomes"],
		CarriedIncompleteItems: payload["carried_incomplete_items"],
		CarriedDepthGaps:       payload["carried_depth_gaps"],
		CarriedNewSurfaces:     payload["carried_new_surfaces"],
		StepResultPath:         stringFromPayload(payload, "step_result_path"),
		StepContextsPath:       stringFromPayload(payload, "step_contexts_path"),
		StepTranscriptPath:     stringFromPayload(payload, "step_transcript_path"),
		StepTimelinePath:       stringFromPayload(payload, "step_timeline_path"),
		SkillsContext:          skillsCtx,
		HasSkillsTable:         skillsCtx != nil && skillsCtx.HasTable(),
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
	showPlanSection, _ := payload["show_plan"].(bool)
	return a.promptManager.BuildFinalAnswerPrompt(FinalAnswerPromptInput{
		AgentRole:              strings.TrimSpace(a.cfg.Role),
		AgentBackground:        strings.TrimSpace(a.cfg.Background),
		AgentInstruction:       strings.TrimSpace(a.cfg.Instruction),
		Status:                 payload["status"],
		StateError:             payload["state_error"],
		InputTimeline:          payload["input_timeline"],
		GoalUnderstanding:      stringFromPayload(payload, "goal_understanding"),
		ShowPlanSection:        showPlanSection,
		Plan:                   payload["plan"],
		PlanVersion:            payload["plan_version"],
		StepOutcomes:           payload["step_outcomes"],
		Warnings:               payload["warnings"],
		CarriedIncompleteItems: payload["carried_incomplete_items"],
		CarriedDepthGaps:       payload["carried_depth_gaps"],
		CarriedNewSurfaces:     payload["carried_new_surfaces"],
		WorkspaceSharedDir:     stringFromPayload(payload, "workspace_shared_dir"),
		RuntimeRepoContext:     a.runtimeRepoContext,
	})
}

// axisKind 标识三轴未决盘点的某一轴，用于从 sticky 状态取对应承载项。
type axisKind int

const (
	axisIncomplete axisKind = iota
	axisDepth
	axisNewSurfaces
)

// carriedAxisItems 从 sticky 三轴状态取出指定轴的承载项；nil 安全。
func carriedAxisItems(axes *builtin_tools.ReplanAxes, kind axisKind) []string {
	if axes == nil {
		return nil
	}
	switch kind {
	case axisIncomplete:
		return axes.IncompleteItems
	case axisDepth:
		return axes.DepthGaps
	case axisNewSurfaces:
		return axes.NewSurfaces
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
