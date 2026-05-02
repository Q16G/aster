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
	var latest *builtin_tools.StepOutcome
	for _, outcome := range outcomes {
		if outcome == nil {
			continue
		}
		if strings.TrimSpace(outcome.Result) == "" {
			continue
		}
		if latest == nil || outcome.UpdatedAt.After(latest.UpdatedAt) {
			latest = outcome
		}
	}
	if latest == nil {
		return "", false
	}
	return strings.TrimSpace(latest.Result), true
}
