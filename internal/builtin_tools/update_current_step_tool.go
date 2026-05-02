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
	ctx ToolContext
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
				"description": "可选：显式证据引用。建议填写文件路径、文档路径或其他可追溯产物标识；runtime 会在后续阶段补充 artifact 路径。",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required":             []string{"status"},
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

	summary := ToolRuntimeValue(args["summary"])
	displayResult := ToolRuntimeValue(args["display_result"])
	result, err := normalizeToolTextOrJSON(args["result"])
	if err != nil {
		return "", err
	}
	errText := ToolRuntimeValue(args["error"])
	references := normalizeToolStringSlice(args["references"])

	prev := t.ctx.Snapshot()
	target := prev.CurrentStep()
	if target == nil {
		return "", fmt.Errorf("current step is empty, wait for runtime planning first")
	}

	artifactDir, summaryFile, resultFile := resolveStepArtifactPaths(prev.PlanVersion, strings.TrimSpace(target.ID))

	snapshot := t.ctx.UpdateCurrentStep(CurrentStepUpdate{
		Status:        status,
		Summary:       summary,
		DisplayResult: displayResult,
		Result:        result,
		Error:         errText,
		References:    references,
	})
	t.ctx.GetEmitter().EmitStateChange(snapshot)
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
