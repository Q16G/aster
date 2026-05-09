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

const historyMaxRunes = 4096

func truncateForHistory(text string, source string) string {
	if source != "step_result" {
		return text
	}
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= historyMaxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:historyMaxRunes])) + "\n\n…(完整结果已持久化到 artifact 文件)"
}

type contractStepMatch struct {
	ContractRef string
	Outcome     *builtin_tools.StepOutcome
}

func buildEligibleOutcomeMap(outcomes []*builtin_tools.StepOutcome) map[string]*builtin_tools.StepOutcome {
	m := make(map[string]*builtin_tools.StepOutcome, len(outcomes))
	for _, o := range outcomes {
		if !stepOutcomeEligibleForResultSource(o) {
			continue
		}
		sid := strings.TrimSpace(o.StepID)
		if prev, ok := m[sid]; !ok || o.UpdatedAt.After(prev.UpdatedAt) {
			m[sid] = o
		}
	}
	return m
}

// resolveContractStep selects the best contract step from the plan.
//  1. If publishContract is specified, pick the last step referencing that contract
//     with an eligible outcome. Returns nil if no match — never falls through to
//     a different contract.
//  2. If publishContract is empty, pick the last step with any OutputContractRef
//     and an eligible outcome.
func resolveContractStep(plan []*builtin_tools.PlanItem, outcomes []*builtin_tools.StepOutcome, publishContract string) *contractStepMatch {
	publishContract = strings.TrimSpace(publishContract)
	outcomeByStepID := buildEligibleOutcomeMap(outcomes)

	if publishContract != "" {
		var best *contractStepMatch
		for _, item := range plan {
			if item == nil || strings.TrimSpace(item.OutputContractRef) != publishContract {
				continue
			}
			if o, ok := outcomeByStepID[strings.TrimSpace(item.ID)]; ok {
				best = &contractStepMatch{ContractRef: publishContract, Outcome: o}
			}
		}
		return best
	}

	var best *contractStepMatch
	for _, item := range plan {
		if item == nil {
			continue
		}
		ref := strings.TrimSpace(item.OutputContractRef)
		if ref == "" {
			continue
		}
		if o, ok := outcomeByStepID[strings.TrimSpace(item.ID)]; ok {
			best = &contractStepMatch{ContractRef: ref, Outcome: o}
		}
	}
	return best
}

func latestNonEmptyStepResult(outcomes []*builtin_tools.StepOutcome) (string, bool) {
	result, ok, _ := latestNonEmptyStepResultWithPlan(outcomes, nil, "")
	return result, ok
}

func latestNonEmptyStepResultWithPlan(outcomes []*builtin_tools.StepOutcome, plan []*builtin_tools.PlanItem, publishContract string) (string, bool, bool) {
	if match := resolveContractStep(plan, outcomes, publishContract); match != nil {
		return strings.TrimSpace(match.Outcome.Result), true, false
	}
	degraded := strings.TrimSpace(publishContract) != ""

	outcomeByStepID := buildEligibleOutcomeMap(outcomes)
	var latestAny *builtin_tools.StepOutcome
	for _, o := range outcomeByStepID {
		if latestAny == nil || o.UpdatedAt.After(latestAny.UpdatedAt) {
			latestAny = o
		}
	}
	if latestAny != nil {
		return strings.TrimSpace(latestAny.Result), true, degraded
	}
	return "", false, degraded
}

func stepOutcomeEligibleForResultSource(outcome *builtin_tools.StepOutcome) bool {
	if outcome == nil || strings.TrimSpace(outcome.Result) == "" {
		return false
	}
	switch outcome.Status {
	case "", builtin_tools.StepOutcomeCompleted:
		return true
	default:
		return false
	}
}
