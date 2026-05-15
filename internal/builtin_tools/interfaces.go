package builtin_tools

import (
	"context"

	"aster/internal/ai"
)

type StateReader interface {
	Snapshot() StateSnapshot
}

type PlanManager interface {
	StateReader
	UpdatePlan(plan []*PlanItem, explanation string, needsPlanning bool) StateSnapshot
	UpdateCurrentStep(update CurrentStepUpdate) StateSnapshot
}

type TaskStateManager interface {
	PlanManager
	UpdateTaskStatus(update TaskStatusUpdate) StateSnapshot
}

// Emitter 内置工具依赖的事件发射接口（由 react.Emitter 实现）
type Emitter interface {
	EmitThink(iteration int, content string, thinkContent string, reasoningContent string, toolCalls any, finishReason string)
	EmitToolStart(iteration int, call ToolCall)
	EmitToolEnd(iteration int, result ToolResult)
	EmitStateChange(snapshot StateSnapshot)
	EmitTaskPlan(plan []*PlanItem, explanation string)
	EmitHumanRequest(iteration int, requestID string, question string, context map[string]any)
	EmitIteration(current int, max int, description string)
	EmitResult(result any, success bool)
	EmitToolUpdate(payload map[string]any)
	EmitLog(level string, message string)
	EmitInfo(message string)
	EmitWarning(message string)
	EmitError(message string)
}

// OnHumanInputFunc 人工输入回调
type OnHumanInputFunc func(ctx context.Context, question string, context map[string]any) (answer string, err error)

// TaskPlannerResult 任务规划结果
type TaskPlannerResult struct {
	NeedsPlanning  bool        `json:"needs_planning"`
	Plan           []*PlanItem `json:"plan,omitempty"`
	Explanation    string      `json:"explanation,omitempty"`
	DirectResponse string      `json:"direct_response,omitempty"`
}

// TaskPlanner 任务规划器接口
type TaskPlanner interface {
	Plan(ctx context.Context, input string) (*TaskPlannerResult, error)
}

type ToolContext interface {
	TaskStateManager

	GetEmitter() Emitter
	ApplyPlanAndEmit(ctx context.Context, plan []*PlanItem, explanation string, needsPlanning bool) StateSnapshot
	GetTaskPlanner() TaskPlanner
	GetAIClient() ai.ChatClient
	GetHistory() []*ai.MsgInfo
	GetOnHumanInput() OnHumanInputFunc
}
