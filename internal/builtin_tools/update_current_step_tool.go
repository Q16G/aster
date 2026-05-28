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
	return "写入当前 step 的终态与结果。"
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
			"open_questions": map[string]any{
				"type":        "array",
				"description": "未决问题数组，记录信息不足或需要后续确认的事项",
				"items": map[string]any{
					"type": "string",
				},
			},
			"tool_calls_digest": map[string]any{
				"type":        "array",
				"description": "本 step 工具调用摘要数组，每条格式：[工具名] 关键参数摘要 → 结果要点",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required":             []string{"status", "status_summary", "short_summary", "long_summary", "key_facts", "open_questions", "tool_calls_digest"},
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
	openQuestions := normalizeToolStringSlice(args["open_questions"])
	toolCallsDigest := normalizeToolStringSlice(args["tool_calls_digest"])

	prev := t.ctx.Snapshot()
	target := prev.CurrentStep()
	if target == nil {
		return "", fmt.Errorf("current step is empty, wait for runtime planning first")
	}

	artifactDir, summaryFile, resultFile := resolveStepArtifactPaths(prev.PlanVersion, strings.TrimSpace(target.ID))

	snapshot := t.ctx.UpdateCurrentStep(CurrentStepUpdate{
		Status:          status,
		Summary:         summary,
		DisplayResult:   displayResult,
		Result:          result,
		Error:           errText,
		References:      references,
		StatusSummary:   statusSummary,
		ShortSummary:    shortSummary,
		LongSummary:     longSummary,
		KeyFacts:        keyFacts,
		OpenQuestions:   openQuestions,
		ToolCallsDigest: toolCallsDigest,
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
		"summary_file":    summaryFile,
		"result_file":     resultFile,
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
		"summary_file":    summaryFile,
		"result_file":     resultFile,
	})
	return string(out), nil
}

func normalizeToolStringSlice(value any) []string {
	return argx.StringSlice(value)
}

func resolveStepArtifactPaths(planVersion int, stepID string) (artifactDir string, summaryFile string, resultFile string) {
	stepID = strings.TrimSpace(stepID)
	if planVersion <= 0 || stepID == "" {
		return "", "", ""
	}
	artifactDir = "shared/step_artifacts"
	return artifactDir, summaryFile, resultFile
}
