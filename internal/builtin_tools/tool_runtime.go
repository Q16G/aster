package builtin_tools

import (
	"context"
	"strings"

	"aster/internal/utils/argx"
)

type toolRuntimeContextKey struct{}

type ToolRuntimeInfo struct {
	Emitter    Emitter
	RunID      string
	CallID     string
	ToolName   string
	Iteration  int
	IsAgent    bool
	StackDepth int

	WorkspaceSessionID string
	WorkspaceRootDir   string
	WorkspaceNamespace string
	WorkspaceSharedDir string
	CurrentStepID      string
}

func WithToolRuntime(ctx context.Context, info ToolRuntimeInfo) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	info.RunID = strings.TrimSpace(info.RunID)
	info.CallID = strings.TrimSpace(info.CallID)
	info.ToolName = strings.TrimSpace(info.ToolName)
	info.WorkspaceSessionID = strings.TrimSpace(info.WorkspaceSessionID)
	info.WorkspaceRootDir = strings.TrimSpace(info.WorkspaceRootDir)
	info.WorkspaceNamespace = strings.TrimSpace(info.WorkspaceNamespace)
	info.WorkspaceSharedDir = strings.TrimSpace(info.WorkspaceSharedDir)
	info.CurrentStepID = strings.TrimSpace(info.CurrentStepID)
	return context.WithValue(ctx, toolRuntimeContextKey{}, info)
}

func GetToolRuntime(ctx context.Context) (ToolRuntimeInfo, bool) {
	if ctx == nil {
		return ToolRuntimeInfo{}, false
	}
	info, ok := ctx.Value(toolRuntimeContextKey{}).(ToolRuntimeInfo)
	if !ok || info.Emitter == nil {
		return ToolRuntimeInfo{}, false
	}
	return info, true
}

func EmitToolRuntimeLog(ctx context.Context, level string, message string, extra map[string]any) {
	info, ok := GetToolRuntime(ctx)
	if !ok {
		return
	}

	payload := CloneAnyMap(extra)
	if payload == nil {
		payload = make(map[string]any)
	}

	level = strings.TrimSpace(strings.ToLower(level))
	message = strings.TrimSpace(message)
	if level != "" {
		payload["level"] = level
	}
	if message != "" {
		payload["message"] = message
	}
	if info.CallID != "" {
		payload["call_id"] = info.CallID
	}
	if info.ToolName != "" {
		payload["tool_name"] = info.ToolName
	}
	if info.Iteration > 0 {
		payload["iteration"] = info.Iteration
	}
	payload["is_agent"] = info.IsAgent
	payload["stack_depth"] = info.StackDepth
	if _, exists := payload["status"]; !exists {
		payload["status"] = "running"
	}
	info.Emitter.EmitToolUpdate(payload)
}

func EmitToolRuntimeInfo(ctx context.Context, message string, extra map[string]any) {
	EmitToolRuntimeLog(ctx, "info", message, extra)
}

func EmitToolRuntimeWarning(ctx context.Context, message string, extra map[string]any) {
	EmitToolRuntimeLog(ctx, "warning", message, extra)
}

func EmitToolRuntimeError(ctx context.Context, message string, extra map[string]any) {
	EmitToolRuntimeLog(ctx, "error", message, extra)
}

func EmitToolRuntimeProgress(ctx context.Context, phase string, message string, extra map[string]any) {
	payload := CloneAnyMap(extra)
	if payload == nil {
		payload = make(map[string]any)
	}
	if strings.TrimSpace(phase) != "" {
		payload["phase"] = strings.TrimSpace(phase)
	}
	EmitToolRuntimeInfo(ctx, message, payload)
}

func ToolRuntimeLabel(exe string, args []string) string {
	command := strings.TrimSpace(exe)
	if len(args) > 0 {
		command = strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
	}
	return strings.TrimSpace(command)
}

func ToolRuntimeValue(value any) string {
	return argx.Text(value)
}
