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

func latestNonEmptyStepResult(outcomes []*builtin_tools.StepOutcome) (string, bool) {
	result, ok := latestNonEmptyStepResultWithPlan(outcomes, nil)
	return result, ok
}

func latestNonEmptyStepResultWithPlan(outcomes []*builtin_tools.StepOutcome, plan []*builtin_tools.PlanItem) (string, bool) {
	outcomeByStepID := buildEligibleOutcomeMap(outcomes)
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
