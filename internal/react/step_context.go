package react

import (
	"sort"
	"strings"
	"time"

	"aster/internal/builtin_tools"
)

type stepContextView struct {
	StepID          string   `json:"step_id,omitempty"`
	Step            string   `json:"step,omitempty"`
	Status          string   `json:"status,omitempty"`
	UpdatedAt       string   `json:"updated_at,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	DisplayResult   string   `json:"display_result,omitempty"`
	Result          string   `json:"result,omitempty"`
	Error           string   `json:"error,omitempty"`
	References      []string `json:"references,omitempty"`
	StatusSummary   string   `json:"status_summary,omitempty"`
	ShortSummary    string   `json:"short_summary,omitempty"`
	LongSummary     string   `json:"long_summary,omitempty"`
	KeyFacts        []string `json:"key_facts,omitempty"`
	OpenQuestions   []string `json:"open_questions,omitempty"`
	ToolCallsDigest []string `json:"tool_calls_digest,omitempty"`
	SummaryFile     string   `json:"summary_file,omitempty"`
	ResultFile      string   `json:"result_file,omitempty"`
	ContextKey      string   `json:"context_key,omitempty"`
	TimelineFile    string   `json:"timeline_file,omitempty"`
}

func collectStepContextViews(plan []*builtin_tools.PlanItem, outcomes []*builtin_tools.StepOutcome, completedOnly bool) []stepContextView {
	outcomesByID := latestStepOutcomeByID(outcomes)
	if len(outcomesByID) == 0 {
		return nil
	}

	planByID := make(map[string]*builtin_tools.PlanItem, len(plan))
	for _, item := range plan {
		if item == nil {
			continue
		}
		stepID := strings.TrimSpace(item.ID)
		if stepID == "" {
			continue
		}
		planByID[stepID] = item
	}

	views := make([]stepContextView, 0, len(outcomesByID))
	seen := make(map[string]struct{}, len(outcomesByID))
	appendView := func(item *builtin_tools.PlanItem, outcome *builtin_tools.StepOutcome) {
		if outcome == nil {
			return
		}
		if completedOnly && !stepOutcomeCompleted(outcome) {
			return
		}

		view := stepContextView{
			StepID:          strings.TrimSpace(outcome.StepID),
			Status:          strings.TrimSpace(string(outcome.Status)),
			UpdatedAt:       outcome.UpdatedAt.Format(time.RFC3339),
			Summary:         strings.TrimSpace(outcome.Summary),
			DisplayResult:   strings.TrimSpace(outcome.DisplayResult),
			Result:          strings.TrimSpace(outcome.Result),
			Error:           strings.TrimSpace(outcome.Error),
			References:      builtin_tools.CloneStringSlice(outcome.References),
			StatusSummary:   strings.TrimSpace(outcome.StatusSummary),
			ShortSummary:    strings.TrimSpace(outcome.ShortSummary),
			LongSummary:     strings.TrimSpace(outcome.LongSummary),
			KeyFacts:        builtin_tools.CloneStringSlice(outcome.KeyFacts),
			OpenQuestions:   builtin_tools.CloneStringSlice(outcome.OpenQuestions),
			ToolCallsDigest: builtin_tools.CloneStringSlice(outcome.ToolCallsDigest),
			SummaryFile:     strings.TrimSpace(outcome.SummaryFile),
			ResultFile:      strings.TrimSpace(outcome.ResultFile),
			ContextKey:      strings.TrimSpace(outcome.ContextKey),
		}
		if item != nil {
			view.Step = strings.TrimSpace(item.Step)
		}
		if view.Step == "" {
			view.Step = view.StepID
		}
		if len(view.References) == 0 {
			view.References = nil
		}
		if len(view.KeyFacts) == 0 {
			view.KeyFacts = nil
		}
		if len(view.OpenQuestions) == 0 {
			view.OpenQuestions = nil
		}
		if len(view.ToolCallsDigest) == 0 {
			view.ToolCallsDigest = nil
		}
		views = append(views, view)
	}

	for _, item := range plan {
		if item == nil {
			continue
		}
		stepID := strings.TrimSpace(item.ID)
		if stepID == "" {
			continue
		}
		outcome := outcomesByID[stepID]
		if outcome == nil {
			continue
		}
		appendView(item, outcome)
		seen[stepID] = struct{}{}
	}

	extraIDs := make([]string, 0, len(outcomesByID))
	for stepID, outcome := range outcomesByID {
		if outcome == nil {
			continue
		}
		if _, ok := seen[stepID]; ok {
			continue
		}
		if completedOnly && !stepOutcomeCompleted(outcome) {
			continue
		}
		extraIDs = append(extraIDs, stepID)
	}
	sort.Slice(extraIDs, func(i, j int) bool {
		left := outcomesByID[extraIDs[i]]
		right := outcomesByID[extraIDs[j]]
		if left == nil || right == nil {
			return extraIDs[i] < extraIDs[j]
		}
		if left.UpdatedAt.Equal(right.UpdatedAt) {
			return extraIDs[i] < extraIDs[j]
		}
		return left.UpdatedAt.Before(right.UpdatedAt)
	})

	for _, stepID := range extraIDs {
		appendView(planByID[stepID], outcomesByID[stepID])
	}

	if len(views) == 0 {
		return nil
	}
	return views
}

func collectCompletedStepContextViews(plan []*builtin_tools.PlanItem, outcomes []*builtin_tools.StepOutcome) []stepContextView {
	return collectStepContextViews(plan, outcomes, true)
}

func collectAllStepContextViews(plan []*builtin_tools.PlanItem, outcomes []*builtin_tools.StepOutcome) []stepContextView {
	return collectStepContextViews(plan, outcomes, false)
}

func renderCompletedStepHandoffContext(plan []*builtin_tools.PlanItem, outcomes []*builtin_tools.StepOutcome) string {
	views := collectCompletedStepContextViews(plan, outcomes)
	if len(views) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("已完成步骤上下文（按 plan 顺序）：\n")
	for _, view := range views {
		stepLabel := strings.TrimSpace(view.Step)
		if stepLabel == "" {
			stepLabel = view.StepID
		}
		if stepLabel == "" {
			stepLabel = "未命名步骤"
		}
		b.WriteString("- [")
		b.WriteString(view.StepID)
		b.WriteString("] ")
		b.WriteString(truncateByRunes(stepLabel, 120))

		statusSummary := firstNonEmpty(view.StatusSummary, view.ShortSummary, view.LongSummary, view.Summary, view.DisplayResult)
		shortSummary := firstNonEmpty(view.ShortSummary, view.StatusSummary, view.Summary, view.DisplayResult, view.LongSummary)
		var parts []string
		if statusSummary != "" {
			parts = append(parts, "status_summary: "+truncateByRunes(statusSummary, 240))
		}
		if shortSummary != "" && shortSummary != statusSummary {
			parts = append(parts, "short_summary: "+truncateByRunes(shortSummary, 240))
		}
		if view.SummaryFile != "" {
			parts = append(parts, "summary_file: "+view.SummaryFile)
		}
		if view.ResultFile != "" {
			parts = append(parts, "result_file: "+view.ResultFile)
		}
		if view.ContextKey != "" {
			parts = append(parts, "context_key: "+view.ContextKey)
		}
		if len(parts) > 0 {
			b.WriteString(" | ")
			b.WriteString(strings.Join(parts, " | "))
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}
