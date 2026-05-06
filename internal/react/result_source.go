package react

import (
	"strings"

	"aster/internal/builtin_tools"
)

type ResultSource string

const (
	ResultSourceFinalAnswer      ResultSource = "final_answer"
	ResultSourceLatestStepResult ResultSource = "latest_step_result"
)

func normalizeResultSource(source ResultSource) ResultSource {
	switch ResultSource(strings.TrimSpace(string(source))) {
	case ResultSourceLatestStepResult:
		return ResultSourceLatestStepResult
	case ResultSourceFinalAnswer:
		fallthrough
	default:
		return ResultSourceFinalAnswer
	}
}

func latestNonEmptyStepResult(outcomes []*builtin_tools.StepOutcome) (string, bool) {
	return latestNonEmptyStepResultWithPlan(outcomes, nil, "")
}

func latestNonEmptyStepResultWithPlan(outcomes []*builtin_tools.StepOutcome, plan []*builtin_tools.PlanItem, publishContract string) (string, bool) {
	publishContract = strings.TrimSpace(publishContract)

	outcomeByStepID := make(map[string]*builtin_tools.StepOutcome, len(outcomes))
	for _, o := range outcomes {
		if o == nil || strings.TrimSpace(o.Result) == "" {
			continue
		}
		sid := strings.TrimSpace(o.StepID)
		if prev, ok := outcomeByStepID[sid]; !ok || o.UpdatedAt.After(prev.UpdatedAt) {
			outcomeByStepID[sid] = o
		}
	}

	// If a publish contract is specified, only match steps referencing that exact contract.
	if publishContract != "" {
		var bestTarget *builtin_tools.StepOutcome
		for _, item := range plan {
			if item == nil || strings.TrimSpace(item.OutputContractRef) != publishContract {
				continue
			}
			if o, ok := outcomeByStepID[strings.TrimSpace(item.ID)]; ok {
				bestTarget = o
			}
		}
		if bestTarget != nil {
			return strings.TrimSpace(bestTarget.Result), true
		}
	}

	// No publish contract, or no match: pick the last contract step by plan position.
	var bestContract *builtin_tools.StepOutcome
	for _, item := range plan {
		if item == nil || strings.TrimSpace(item.OutputContractRef) == "" {
			continue
		}
		if o, ok := outcomeByStepID[strings.TrimSpace(item.ID)]; ok {
			bestContract = o
		}
	}
	if bestContract != nil {
		return strings.TrimSpace(bestContract.Result), true
	}

	// Fallback: latest non-empty result by time.
	var latestAny *builtin_tools.StepOutcome
	for _, o := range outcomeByStepID {
		if latestAny == nil || o.UpdatedAt.After(latestAny.UpdatedAt) {
			latestAny = o
		}
	}
	if latestAny != nil {
		return strings.TrimSpace(latestAny.Result), true
	}
	return "", false
}
