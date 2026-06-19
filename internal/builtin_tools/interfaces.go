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
	EmitToolStart(iteration int, call ToolCall, stepID string)
	EmitToolEnd(iteration int, result ToolResult, stepID string)
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
	// Simple 标记简单任务（单步即可完成）：该步完成后跳过 step_replan 三轴判定
	// 直达 final_answer 验收（验收仍保留 should_replan 回流兜底）。
	Simple bool `json:"simple,omitempty"`
	// GoalUnderstanding 是 planner 对用户输入的结构化复述（核心目标/范围边界/约束/
	// 交付物与验收/显式聚焦/隐含假设/未决歧义）。它随 plan 一起落盘，并注入 step_replan，
	// 作为多轮重规划时锚定原始意图的准绳。
	GoalUnderstanding string `json:"goal_understanding,omitempty"`
	// CurrentPhase 是 planner 自决的「当前深度优先聚焦阶段/面」一句话语义描述，与
	// §覆盖面深度优先 的「面」概念对齐。step_replan 视角 B 据此把全局视角从
	// GoalUnderstanding 全集收窄为 GoalUnderstanding ∩ CurrentPhase，承载阶段闭环判定。
	// needs_planning=true && !Simple 时必填；simple/direct_response 任务豁免。
	CurrentPhase string `json:"current_phase,omitempty"`
	// Plan 阶段调查上下文，持久化后传递给后续 Step
	Summary         string   `json:"summary,omitempty"`
	ToolCallsDigest []string `json:"tool_calls_digest,omitempty"`
	KeyFacts        []string `json:"key_facts,omitempty"`
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
