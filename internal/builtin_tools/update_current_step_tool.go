package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"aster/internal/runtimelog"
	"aster/internal/utils/argx"
)

type UpdateCurrentStepTool struct {
	ctx               ToolContext
	ChildAgentChecker func() []string
}

func NewUpdateCurrentStepTool(ctx ToolContext) *UpdateCurrentStepTool {
	return &UpdateCurrentStepTool{ctx: ctx}
}

func (t *UpdateCurrentStepTool) Name() string { return UpdateCurrentStepToolName }

func (t *UpdateCurrentStepTool) Description() string {
	return "step 完成或失败后调用，提交当前 step 的结构化终态与结果；这是结束 step 的唯一方式。status=completed 前须完成覆盖对账（仍有 uncovered 项时不得标 completed）。"
}

func (t *UpdateCurrentStepTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{
				"type": "string",
				"enum": []any{
					string(PlanStepCompleted),
					string(PlanStepFailed),
				},
				"description": "当前 step 的终态，只允许 completed/failed",
			},
			"summary": map[string]any{
				"type":        "string",
				"description": "可选：当前 step 的简要结论",
			},
			"display_result": map[string]any{
				"type":        "string",
				"description": "可选：面向用户的简洁结果（final answer 仍由 final_answer phase 生成；这里仅提交 step 级原始事实）",
			},
			"result": map[string]any{
				"type":        []any{"string", "object", "array"},
				"description": "可选：当前 step 的结构化结果",
				"items":       map[string]any{},
			},
			"error": map[string]any{
				"type":        "string",
				"description": "status=failed 时可选：失败原因",
			},
			"references": map[string]any{
				"type":        "array",
				"description": "可选：显式证据引用。所有文件路径必须使用绝对路径，禁止使用相对路径。",
				"items": map[string]any{
					"type": "string",
				},
			},
			"status_summary": map[string]any{
				"type":        "string",
				"description": "一句话状态总结，概括当前 step 的完成情况",
			},
			"short_summary": map[string]any{
				"type":        "string",
				"description": "2-4 句短总结，包含关键结论和结果",
			},
			"long_summary": map[string]any{
				"type":        "string",
				"description": "较完整的长总结，保留关键事实和结构化数据",
			},
			"key_facts": map[string]any{
				"type":        "array",
				"description": "关键事实数组，每条为一个独立的事实陈述",
				"items": map[string]any{
					"type": "string",
				},
			},
			"coverage_checklist": map[string]any{
				"type":        "array",
				"description": "覆盖对账清单。当 step 声明目标含全量量词或存在可枚举清单时必填：执行现场逐项物化对账，每项给出状态与依据",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"item": map[string]any{
							"type":        "string",
							"description": "对账工作项",
						},
						"status": map[string]any{
							"type":        "string",
							"enum":        []any{"verified", "uncovered", "justified_skip", "referenced_prior_coverage"},
							"description": "覆盖状态",
						},
						"evidence": map[string]any{
							"type":        "string",
							"description": "status=verified 时的工具调用证据要点",
						},
						"reason": map[string]any{
							"type":        "string",
							"description": "status=justified_skip/uncovered 时的原因",
						},
					},
					"required":             []string{"item", "status"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"status", "status_summary", "short_summary", "long_summary", "key_facts"},
		"additionalProperties": false,
	}
}

func (t *UpdateCurrentStepTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.ctx == nil {
		return "", fmt.Errorf("tool context is nil")
	}

	status := PlanStepStatus(ToolRuntimeValue(args["status"]))
	switch status {
	case PlanStepCompleted, PlanStepFailed:
	default:
		return "", fmt.Errorf("invalid status: %s", ToolRuntimeValue(args["status"]))
	}

	if status == PlanStepCompleted && t.ChildAgentChecker != nil {
		if running := t.ChildAgentChecker(); len(running) > 0 {
			return "", fmt.Errorf(
				"cannot mark step as completed: child agents still running: %s. "+
					"Wait for all child agents to finish before calling update_current_step",
				strings.Join(running, ", "),
			)
		}
	}

	summary := ToolRuntimeValue(args["summary"])
	displayResult := ToolRuntimeValue(args["display_result"])
	result, err := normalizeToolTextOrJSON(args["result"])
	if err != nil {
		return "", err
	}
	errText := ToolRuntimeValue(args["error"])
	references := normalizeToolStringSlice(args["references"])
	statusSummary := ToolRuntimeValue(args["status_summary"])
	shortSummary := ToolRuntimeValue(args["short_summary"])
	longSummary := ToolRuntimeValue(args["long_summary"])
	keyFacts := normalizeToolStringSlice(args["key_facts"])
	coverageChecklist, err := normalizeCoverageChecklist(args["coverage_checklist"])
	if err != nil {
		return "", err
	}

	prev := t.ctx.Snapshot()
	target := prev.CurrentStep()
	if target == nil {
		return "", fmt.Errorf("current step is empty, wait for runtime planning first")
	}

	artifactDir := resolveStepArtifactDir(prev.PlanVersion, strings.TrimSpace(target.ID))

	snapshot := t.ctx.UpdateCurrentStep(CurrentStepUpdate{
		Status:            status,
		Summary:           summary,
		DisplayResult:     displayResult,
		Result:            result,
		Error:             errText,
		References:        references,
		StatusSummary:     statusSummary,
		ShortSummary:      shortSummary,
		LongSummary:       longSummary,
		KeyFacts:          keyFacts,
		CoverageChecklist: coverageChecklist,
	})
	t.ctx.GetEmitter().EmitStateChange(snapshot)
	EmitToolRuntimeInfo(ctx, "step result ready", map[string]any{
		"presentation":   "step_result",
		"step_id":        strings.TrimSpace(target.ID),
		"step_name":      strings.TrimSpace(target.Step),
		"step_status":    status,
		"display_result": displayResult,
		"summary":        summary,
		"error":          errText,
	})
	logLevel := "info"
	logMessage := "current step completed"
	if status == PlanStepFailed {
		logLevel = "warning"
		logMessage = "current step failed"
	}
	payload := map[string]any{
		"level":           logLevel,
		"message":         logMessage,
		"event":           "step_updated",
		"step_id":         strings.TrimSpace(target.ID),
		"step":            strings.TrimSpace(target.Step),
		"status":          status,
		"summary":         summary,
		"display_result":  displayResult,
		"error":           errText,
		"references":      references,
		"artifact_dir":    artifactDir,
		"phase":           snapshot.Phase,
		"current_step_id": snapshot.CurrentStepID,
		"progress":        snapshot.Progress,
		"result_present":  strings.TrimSpace(result) != "",
	}
	runtimelog.LogJSON(logLevel, payload)

	out, _ := json.Marshal(map[string]any{
		"ok":              true,
		"step_id":         strings.TrimSpace(target.ID),
		"status":          status,
		"current_step_id": snapshot.CurrentStepID,
		"artifact_dir":    artifactDir,
	})
	return string(out), nil
}

func normalizeToolStringSlice(value any) []string {
	return argx.StringSlice(value)
}

func normalizeCoverageChecklist(value any) ([]CoverageChecklistItem, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal coverage_checklist failed: %w", err)
	}
	var items []CoverageChecklistItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("invalid coverage_checklist: %w", err)
	}
	out := items[:0]
	for _, item := range items {
		item.Item = strings.TrimSpace(item.Item)
		item.Status = strings.TrimSpace(item.Status)
		if item.Item == "" || item.Status == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// resolveStepArtifactDir 返回 step 产物目录；summary_file 双写已废弃（产出并入
// shared/step_<stepID>.md，指针走 plan_item.step_file）。
func resolveStepArtifactDir(planVersion int, stepID string) string {
	if planVersion <= 0 || strings.TrimSpace(stepID) == "" {
		return ""
	}
	return "shared/step_artifacts"
}
