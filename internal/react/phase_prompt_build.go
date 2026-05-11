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
	return a.promptManager.BuildStepReplanPrompt(StepReplanPromptInput{
		CurrentGoal:  payload["current_goal"],
		CurrentStep:  payload["current_step"],
		StepOutcome:  payload["step_outcome"],
		TaskPlan:     payload["task_plan"],
		StepOutcomes: payload["step_outcomes"],
		Warnings:     payload["warnings"],
		Unresolved:   payload["unresolved"],
	})
}

func (a *Agent) BuildFinalAnswerPrompt(payload map[string]any) (string, error) {
	if a == nil || a.promptManager == nil {
		return "", fmt.Errorf("final answer prompt manager is nil")
	}
	snap := a.state.Snapshot()
	showPlanSection, _ := payload["show_plan"].(bool)
	publish := a.currentFinalAnswerPublishConfig()
	publishedOutputRequired := publish != nil && publish.Strict && publish.Contract != nil
	publishedOutputName := ""
	publishedOutputSchema := ""
	publishedOutputExample := ""
	hasSummaryPolicy := false
	summaryPolicyName := ""
	summaryPolicyDetail := ""
	if publish != nil && publish.Contract != nil {
		publishedOutputName = strings.TrimSpace(publish.Contract.Name)
		publishedOutputSchema = strings.TrimSpace(publish.Contract.Schema)
		publishedOutputExample = strings.TrimSpace(publish.Contract.Example)
	}
	if a.cfg != nil {
		if contract := a.lookupFinalAnswerOutputContract(snap); contract != nil && strings.TrimSpace(contract.SummaryPolicy) != "" {
			hasSummaryPolicy = true
			summaryPolicyName = strings.TrimSpace(contract.Name)
			summaryPolicyDetail = strings.TrimSpace(contract.SummaryPolicy)
		}
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
		HasSummaryPolicy:        hasSummaryPolicy,
		SummaryPolicyName:       summaryPolicyName,
		SummaryPolicyDetail:     summaryPolicyDetail,
		PublishedOutputRequired: publishedOutputRequired,
		PublishedOutputName:     publishedOutputName,
		PublishedOutputSchema:   publishedOutputSchema,
		PublishedOutputExample:  publishedOutputExample,
	})
}

func (a *Agent) lookupFinalAnswerOutputContract(snapshot builtin_tools.StateSnapshot) *builtin_tools.OutputContract {
	if a == nil || a.cfg == nil {
		return nil
	}
	if name := strings.TrimSpace(a.currentPublishContract); name != "" {
		if c := a.cfg.LookupOutputContract(name); c != nil {
			return c
		}
	}
	match := resolveContractStep(snapshot.Plan, snapshot.StepOutcomes, "")
	if match == nil {
		return nil
	}
	return a.cfg.LookupOutputContract(match.ContractRef)
}

func prettyJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(raw)
}
