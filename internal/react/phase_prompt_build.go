package react

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (a *Agent) BuildStepReplanPrompt(payload map[string]any) (string, error) {
	if a == nil || a.promptManager == nil {
		return "", fmt.Errorf("step replan prompt manager is nil")
	}
	return a.promptManager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentInstruction: strings.TrimSpace(a.cfg.Instruction),
		CurrentGoal:      payload["current_goal"],
		CurrentStep:      payload["current_step"],
		StepOutcome:      payload["step_outcome"],
		TaskPlan:         payload["task_plan"],
		StepOutcomes:     payload["step_outcomes"],
		Warnings:         payload["warnings"],
		Unresolved:       payload["unresolved"],
		StepResultPath:   stringFromPayload(payload, "step_result_path"),
		StepContextsPath: stringFromPayload(payload, "step_contexts_path"),
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
		AgentInstruction: strings.TrimSpace(a.cfg.Instruction),
		Status:           payload["status"],
		StateError:       payload["state_error"],
		InputTimeline:    payload["input_timeline"],
		ShowPlanSection:  showPlanSection,
		Plan:             payload["plan"],
		PlanVersion:      payload["plan_version"],
		StepOutcomes:     payload["step_outcomes"],
		Warnings:         payload["warnings"],
		Unresolved:       payload["unresolved"],
	})
}

func prettyJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(raw)
}
