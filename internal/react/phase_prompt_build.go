package react

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (a *Agent) BuildStepSummaryPrompt(payload map[string]any) (string, error) {
	if a == nil || a.promptManager == nil {
		return "", fmt.Errorf("step summary prompt manager is nil")
	}

	var hasSummaryPolicy bool
	var summaryPolicyName, summaryPolicyDetail string
	if a.cfg != nil {
		snap := a.state.Snapshot()
		if currentStep := snap.CurrentStep(); currentStep != nil && currentStep.OutputContractRef != "" {
			if c := a.cfg.LookupOutputContract(currentStep.OutputContractRef); c != nil && c.SummaryPolicy != "" {
				hasSummaryPolicy = true
				summaryPolicyName = c.Name
				summaryPolicyDetail = c.SummaryPolicy
			}
		}
	}

	return a.promptManager.BuildStepSummaryPrompt(StepSummaryPromptInput{
		InputTimeline:       payload["input_timeline"],
		CurrentGoal:         payload["current_goal"],
		CurrentStep:         payload["current_step"],
		TaskPlan:            payload["task_plan"],
		RawOutcome:          payload["raw_outcome"],
		StepWindow:          payload["step_window"],
		TimelineDiff:        payload["timeline_diff"],
		References:          payload["references"],
		Artifacts:           payload["artifacts"],
		Warnings:            payload["warnings"],
		Unresolved:          payload["unresolved"],
		HasSummaryPolicy:    hasSummaryPolicy,
		SummaryPolicyName:   summaryPolicyName,
		SummaryPolicyDetail: summaryPolicyDetail,
	})
}

func (a *Agent) BuildFinalAnswerPrompt(payload map[string]any) (string, error) {
	if a == nil || a.promptManager == nil {
		return "", fmt.Errorf("final answer prompt manager is nil")
	}
	showPlanSection, _ := payload["show_plan"].(bool)
	publish := a.currentFinalAnswerPublishConfig()
	publishedOutputRequired := publish != nil && publish.Strict && publish.Contract != nil
	publishedOutputName := ""
	publishedOutputSchema := ""
	publishedOutputExample := ""
	if publish != nil && publish.Contract != nil {
		publishedOutputName = strings.TrimSpace(publish.Contract.Name)
		publishedOutputSchema = strings.TrimSpace(publish.Contract.Schema)
		publishedOutputExample = strings.TrimSpace(publish.Contract.Example)
	}
	return a.promptManager.BuildFinalAnswerPrompt(FinalAnswerPromptInput{
		Status:                  payload["status"],
		StateError:              payload["state_error"],
		InputTimeline:           payload["input_timeline"],
		ShowPlanSection:         showPlanSection,
		Plan:                    payload["plan"],
		PlanVersion:             payload["plan_version"],
		StepOutcomes:            payload["step_outcomes"],
		Warnings:                payload["warnings"],
		Unresolved:              payload["unresolved"],
		PublishedOutputRequired: publishedOutputRequired,
		PublishedOutputName:     publishedOutputName,
		PublishedOutputSchema:   publishedOutputSchema,
		PublishedOutputExample:  publishedOutputExample,
	})
}

func (a *Agent) BuildReducerPrompt(payload map[string]any) (string, error) {
	if a == nil || a.promptManager == nil {
		return "", fmt.Errorf("reducer prompt manager is nil")
	}
	return a.promptManager.BuildReducerPrompt(ReducerPromptInput{
		StepID:          payload["step_id"],
		InputTimeline:   payload["input_timeline"],
		CurrentStep:     payload["current_step"],
		RawTimelineDiff: payload["raw_timeline_diff"],
		DiffSummaryHint: payload["diff_summary_hint"],
		References:      payload["references"],
		Artifacts:       payload["artifacts"],
	})
}

func prettyJSON(value any) string {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "null"
	}
	return string(raw)
}
