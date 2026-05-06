package react

import (
	"errors"
	"fmt"
	"strings"

	openai "aster/internal/ai/openai"
	"aster/internal/builtin_tools"
	"aster/internal/structuredoutput"
)

func classifyExternalInterrupt(err error) *builtin_tools.ExternalInterrupt {
	if err == nil {
		return nil
	}

	rawError := strings.TrimSpace(normalizeRuntimeErrorText(err))
	decision := openai.BuildRetryDecision(err, nil)
	if decision.ReasonCode == "" {
		var exhausted *structuredoutput.ExhaustedError
		if errors.As(err, &exhausted) && exhausted != nil {
			if last := exhausted.LastAttempt(); last != nil && strings.TrimSpace(last.Error) != "" {
				rawError = strings.TrimSpace(last.Error)
				decision = openai.BuildRetryDecisionFromText(rawError, nil)
			}
		} else if rawError != "" {
			decision = openai.BuildRetryDecisionFromText(rawError, nil)
		}
	}
	if decision.ReasonCode == "" {
		return nil
	}

	info := &builtin_tools.ExternalInterrupt{
		ReasonCode:       strings.TrimSpace(decision.ReasonCode),
		Retryable:        decision.Retry,
		Error:            rawError,
		UserMessage:      strings.TrimSpace(decision.UserMessage),
		SuggestedActions: builtin_tools.CloneStringSlice(decision.SuggestedActions),
	}
	if info.UserMessage == "" {
		if info.Retryable {
			info.UserMessage = "外部依赖调用失败，系统已按策略自动重试。"
		} else {
			info.UserMessage = "外部依赖调用失败，本次不会自动重试。"
		}
	}
	return info
}

func externalInterruptWarning(info *builtin_tools.ExternalInterrupt) string {
	if info == nil || strings.TrimSpace(info.UserMessage) == "" {
		return ""
	}
	warning := strings.TrimSpace(info.UserMessage)
	if !info.Retryable {
		if next := firstNonEmpty(info.SuggestedActions...); next != "" {
			warning += " 建议：" + next + "。"
		}
	}
	return warning
}

func buildExternalInterruptModelOutput(snapshot builtin_tools.StateSnapshot, info *builtin_tools.ExternalInterrupt) FinalAnswerModelOutput {
	reason := ""
	if info != nil {
		reason = strings.TrimSpace(info.UserMessage)
	}
	if reason == "" && info != nil {
		reason = strings.TrimSpace(info.Error)
	}
	if reason == "" {
		reason = "外部依赖调用中断，已进入收尾阶段。"
	}

	status := builtin_tools.TaskStatusFailed
	if shouldForceCompletedOnExternalInterrupt(snapshot) {
		status = builtin_tools.TaskStatusCompleted
	}

	return FinalAnswerModelOutput{
		IsComplete:   true,
		Status:       string(status),
		Reason:       reason,
		ShouldReplan: false,
		NextGoal:     "",
		MissingItems: nil,
		Warnings:     nil,
		UserMessage:  buildExternalInterruptFinalAnswer(snapshot, info),
		References:   nil,
	}
}

func buildExternalInterruptFinalAnswer(snapshot builtin_tools.StateSnapshot, info *builtin_tools.ExternalInterrupt) string {
	if info == nil {
		return ""
	}

	completed := completedStepSummaries(snapshot)
	incomplete := incompleteStepSummaries(snapshot)
	if len(incomplete) == 0 {
		incomplete = []string{"- 当前没有待执行的剩余步骤。"}
	}

	var b strings.Builder
	b.WriteString("## 当前可交付结果\n\n")
	b.WriteString("### 已完成的步骤 / 可用产物\n")
	if len(completed) == 0 {
		b.WriteString("- 暂无已完成步骤。\n")
	} else {
		b.WriteString(strings.Join(completed, "\n"))
		b.WriteString("\n")
	}

	b.WriteString("\n### 未完成的步骤\n")
	b.WriteString(strings.Join(incomplete, "\n"))
	b.WriteString("\n")

	b.WriteString("\n### 中断原因\n")
	b.WriteString("- " + strings.TrimSpace(info.UserMessage) + "\n")
	if raw := strings.TrimSpace(info.Error); raw != "" {
		b.WriteString("- 原始错误：" + raw + "\n")
	}

	b.WriteString("\n### 重试说明\n")
	if info.Retryable {
		b.WriteString("- 该错误属于可重试问题，系统已按策略自动重试；在达到重试上限后仍未恢复，因此提前结束本次运行。\n")
	} else {
		b.WriteString("- 该错误被判定为不可重试的外部依赖问题，因此系统没有继续自动重试，而是直接进入收尾阶段交付当前可用结果。\n")
	}

	b.WriteString("\n### 建议下一步\n")
	if len(info.SuggestedActions) == 0 {
		b.WriteString("- 处理外部依赖问题后，重新执行未完成步骤。\n")
	} else {
		for _, item := range info.SuggestedActions {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			b.WriteString("- " + item + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func completedStepSummaries(snapshot builtin_tools.StateSnapshot) []string {
	outcomesByID := latestStepOutcomeByID(snapshot.StepOutcomes)
	lines := make([]string, 0, len(snapshot.Plan))
	seen := make(map[string]struct{}, len(snapshot.Plan))
	for _, item := range snapshot.Plan {
		if item == nil {
			continue
		}
		stepID := strings.TrimSpace(item.ID)
		if stepID == "" {
			continue
		}
		seen[stepID] = struct{}{}
		outcome := outcomesByID[stepID]
		if !stepOutcomeCompleted(outcome) && item.Status != builtin_tools.PlanStepCompleted {
			continue
		}
		lines = append(lines, "- "+formatCompletedStep(item, outcome))
	}
	for stepID, outcome := range outcomesByID {
		if _, ok := seen[stepID]; ok || !stepOutcomeCompleted(outcome) {
			continue
		}
		lines = append(lines, "- "+formatCompletedStep(nil, outcome))
	}
	return lines
}

func incompleteStepSummaries(snapshot builtin_tools.StateSnapshot) []string {
	outcomesByID := latestStepOutcomeByID(snapshot.StepOutcomes)
	lines := make([]string, 0, len(snapshot.Plan))
	for _, item := range snapshot.Plan {
		if item == nil {
			continue
		}
		outcome := outcomesByID[strings.TrimSpace(item.ID)]
		if stepOutcomeCompleted(outcome) || item.Status == builtin_tools.PlanStepCompleted {
			continue
		}
		label := strings.TrimSpace(item.Step)
		if label == "" {
			label = strings.TrimSpace(item.ID)
		}
		status := strings.TrimSpace(string(item.Status))
		if status == "" {
			status = "pending"
		}
		lines = append(lines, fmt.Sprintf("- %s（当前状态：%s）", label, status))
	}
	return lines
}

func latestStepOutcomeByID(outcomes []*builtin_tools.StepOutcome) map[string]*builtin_tools.StepOutcome {
	out := make(map[string]*builtin_tools.StepOutcome, len(outcomes))
	for _, outcome := range outcomes {
		if outcome == nil {
			continue
		}
		stepID := strings.TrimSpace(outcome.StepID)
		if stepID == "" {
			continue
		}
		prev := out[stepID]
		if prev == nil || outcome.UpdatedAt.After(prev.UpdatedAt) {
			out[stepID] = outcome
		}
	}
	return out
}

func stepOutcomeCompleted(outcome *builtin_tools.StepOutcome) bool {
	if outcome == nil {
		return false
	}
	switch outcome.Status {
	case "", builtin_tools.StepOutcomeCompleted:
		return true
	default:
		return false
	}
}

func formatCompletedStep(item *builtin_tools.PlanItem, outcome *builtin_tools.StepOutcome) string {
	label := ""
	if item != nil {
		label = strings.TrimSpace(item.Step)
	}
	if label == "" && outcome != nil {
		label = strings.TrimSpace(outcome.StepID)
	}
	if label == "" {
		label = "未命名步骤"
	}
	detail := ""
	if outcome != nil {
		switch {
		case strings.TrimSpace(outcome.ResultFile) != "":
			detail = "产物：" + strings.TrimSpace(outcome.ResultFile)
		case strings.TrimSpace(outcome.DisplayResult) != "":
			detail = truncateByRunes(strings.TrimSpace(outcome.DisplayResult), 96)
		case strings.TrimSpace(outcome.Summary) != "":
			detail = truncateByRunes(strings.TrimSpace(outcome.Summary), 96)
		}
	}
	if detail == "" {
		return label
	}
	return fmt.Sprintf("%s（%s）", label, detail)
}

func shouldForceCompletedOnExternalInterrupt(snapshot builtin_tools.StateSnapshot) bool {
	return len(completedStepSummaries(snapshot)) > 0
}

func applyExternalInterruptDecision(snapshot builtin_tools.StateSnapshot, decision finalAnswerDecision, info *builtin_tools.ExternalInterrupt) finalAnswerDecision {
	if info == nil {
		return decision
	}

	decision.model.IsComplete = true
	decision.model.ShouldReplan = false
	decision.model.NextGoal = ""
	decision.model.MissingItems = nil
	decision.isTerminal = true
	if shouldForceCompletedOnExternalInterrupt(snapshot) {
		decision.status = builtin_tools.TaskStatusCompleted
		decision.model.Status = string(builtin_tools.TaskStatusCompleted)
		if strings.TrimSpace(decision.model.Reason) == "" {
			decision.model.Reason = "外部中断后已交付当前可用结果。"
		}
	} else {
		decision.status = builtin_tools.TaskStatusFailed
		decision.model.Status = string(builtin_tools.TaskStatusFailed)
		if strings.TrimSpace(decision.model.Reason) == "" {
			decision.model.Reason = "外部中断导致任务未完成。"
		}
	}
	return decision
}
