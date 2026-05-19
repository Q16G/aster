package react

import (
	"aster/internal/builtin_tools"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// EventType 事件类型
type EventType string

const (
	EventTypeThink             EventType = "think"
	EventTypeToolStart         EventType = "tool_start"
	EventTypeToolEnd           EventType = "tool_end"
	EventTypeAgentEnter        EventType = "agent_enter"
	EventTypeAgentExit         EventType = "agent_exit"
	EventTypeStateChange       EventType = "state_change"
	EventTypeHumanRequest      EventType = "human_request"
	EventTypeTaskPlan          EventType = "task_plan"
	EventTypeTaskItem          EventType = "task_item"
	EventTypeIteration         EventType = "iteration"
	EventTypeResult            EventType = "result"
	EventTypeToolUpdate        EventType = "tool_update"
	EventTypeRetry             EventType = "retry"
	EventTypeLog               EventType = "log"
	EventTypeStream            EventType = "stream"
	EventTypeStepFinish        EventType = "step_finish"
	EventTypeHistoryCompacted  EventType = "history_compacted"
	EventTypeStepSummaryResult EventType = "step_summary_result"
	EventTypeStepReplanResult  EventType = "step_replan_result"
	EventTypeFinalAnswerResult EventType = "final_answer_result"
)

// AgentOutputEvent 统一的事件结构
type AgentOutputEvent struct {
	Type      EventType      `json:"type"`
	AgentID   string         `json:"agent_id,omitempty"`
	AgentName string         `json:"agent_name,omitempty"`
	NodeID    string         `json:"node_id,omitempty"`
	EventID   string         `json:"event_id,omitempty"`
	GroupID   string         `json:"group_id,omitempty"`
	Iteration int            `json:"iteration,omitempty"`
	SeqID     uint64         `json:"seq_id,omitempty"`
	Timestamp int64          `json:"timestamp"`
	IsJSON    bool           `json:"is_json,omitempty"`
	Content   string         `json:"content,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// BaseEmitterFunc 底层事件接收函数
type BaseEmitterFunc func(e *AgentOutputEvent) error

// EventProcessor 事件处理器（中间件）
type EventProcessor func(e *AgentOutputEvent) *AgentOutputEvent

// Emitter 事件发射器
type Emitter struct {
	mu             sync.RWMutex
	id             string
	agentName      string
	baseEmitter    BaseEmitterFunc
	processorStack []EventProcessor
	seqID          *atomic.Uint64
	thinkGroupID   string
}

// NewEmitter 创建事件发射器
func NewEmitter(id string, agentName string, emitter BaseEmitterFunc) *Emitter {
	return &Emitter{
		id:          id,
		agentName:   agentName,
		baseEmitter: emitter,
		seqID:       &atomic.Uint64{},
	}
}

// NewDummyEmitter 创建空发射器（用于测试）
func NewDummyEmitter() *Emitter {
	return NewEmitter("", "", nil)
}

// PushProcessor 添加事件处理器。
func (e *Emitter) PushProcessor(processor EventProcessor) *Emitter {
	if e == nil || processor == nil {
		return e
	}
	e.mu.Lock()
	e.processorStack = append(e.processorStack, processor)
	e.mu.Unlock()
	return e
}

// PopProcessor 移除最后一个事件处理器。
func (e *Emitter) PopProcessor() *Emitter {
	if e == nil {
		return e
	}
	e.mu.Lock()
	if len(e.processorStack) > 0 {
		e.processorStack = e.processorStack[:len(e.processorStack)-1]
	}
	e.mu.Unlock()
	return e
}

// Emit 发射事件
func (e *Emitter) Emit(event *AgentOutputEvent) error {
	if e == nil || event == nil {
		return nil
	}

	if event.SeqID == 0 && e.seqID != nil {
		event.SeqID = e.seqID.Add(1)
	}

	event.AgentID = e.id
	event.AgentName = e.agentName
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixMilli()
	}
	// event_id is record-unique; UI should use group_id for aggregation.
	if strings.TrimSpace(event.EventID) == "" {
		if event.SeqID > 0 {
			event.EventID = fmt.Sprintf("%s:%d", event.AgentID, event.SeqID)
		} else {
			event.EventID = uuid.NewString()
		}
	}

	e.mu.RLock()
	processors := e.processorStack
	baseEmitter := e.baseEmitter
	e.mu.RUnlock()

	for _, processor := range processors {
		if processor != nil {
			event = processor(event)
		}
	}

	if baseEmitter != nil {
		return baseEmitter(event)
	}
	return nil
}

// EmitJSON 发射 JSON 事件
func (e *Emitter) EmitJSON(eventType EventType, nodeID string, payload map[string]any) {
	content, _ := json.Marshal(payload)
	e.Emit(&AgentOutputEvent{
		Type:    eventType,
		NodeID:  nodeID,
		IsJSON:  true,
		Content: string(content),
		Payload: payload,
	})
}

func (e *Emitter) EnsureThinkGroupID() string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.thinkGroupID == "" {
		e.thinkGroupID = uuid.NewString()
	}
	return e.thinkGroupID
}

func (e *Emitter) ResetThinkGroupID() {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.thinkGroupID = ""
	e.mu.Unlock()
}

// EmitThink 发射思考事件
func (e *Emitter) EmitThink(iteration int, content string, thinkContent string, reasoningContent string, toolCalls any, finishReason string) {
	groupID := e.EnsureThinkGroupID()
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeThink,
		NodeID:    "think",
		GroupID:   groupID,
		Iteration: iteration,
		Payload: map[string]any{
			"content":           content,
			"think_content":     thinkContent,
			"reasoning_content": reasoningContent,
			"tool_calls":        toolCalls,
			"finish_reason":     finishReason,
		},
	})
	if strings.TrimSpace(finishReason) != "" {
		e.ResetThinkGroupID()
	}
}

func (e *Emitter) EmitStream(iteration int, delta string) {
	e.ResetThinkGroupID()
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeStream,
		NodeID:    "stream",
		Iteration: iteration,
		Content:   delta,
	})
}

func (e *Emitter) EmitStepFinish(iteration int, payload map[string]any) {
	e.ResetThinkGroupID()
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeStepFinish,
		NodeID:    "step_finish",
		Iteration: iteration,
		Payload:   payload,
	})
}

// EmitToolStart 发射工具开始事件
func (e *Emitter) EmitToolStart(iteration int, call builtin_tools.ToolCall) {
	e.ResetThinkGroupID()
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeToolStart,
		NodeID:    "tool:" + call.Name,
		Iteration: iteration,
		Payload: map[string]any{
			"call_id":     call.ID,
			"tool_name":   call.Name,
			"is_agent":    call.IsAgent,
			"stack_depth": call.StackDepth,
			"arguments":   call.Arguments,
		},
	})
}

// EmitToolEnd 发射工具结束事件
func (e *Emitter) EmitToolEnd(iteration int, result builtin_tools.ToolResult) {
	e.ResetThinkGroupID()
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeToolEnd,
		NodeID:    "tool:" + result.Name,
		Iteration: iteration,
		Payload: map[string]any{
			"call_id":     result.ID,
			"tool_name":   result.Name,
			"is_agent":    result.IsAgent,
			"stack_depth": result.StackDepth,
			"result":      result.Result,
			"error":       result.Error,
		},
	})
}

// EmitStateChange 发射状态变更事件
func (e *Emitter) EmitStateChange(snapshot builtin_tools.StateSnapshot) {
	e.ResetThinkGroupID()
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeStateChange,
		NodeID:    "state",
		Iteration: snapshot.Iteration,
		Payload: map[string]any{
			"phase":              snapshot.Phase,
			"status":             snapshot.Status,
			"current_goal":       snapshot.CurrentGoal,
			"current_step_id":    snapshot.CurrentStepID,
			"input_timeline":     snapshot.InputTimeline,
			"progress":           snapshot.Progress,
			"status_summary":     snapshot.StatusSummary,
			"final_answer":       snapshot.FinalAnswer,
			"error":              snapshot.Error,
			"external_interrupt": snapshot.ExternalInterrupt,
			"warnings":           snapshot.Warnings,
			"unresolved":         snapshot.Unresolved,
			"plan_version":       snapshot.PlanVersion,
		},
	})
}

// EmitHumanRequest 发射人工请求事件
func (e *Emitter) EmitHumanRequest(iteration int, requestID string, question string, context map[string]any) {
	e.ResetThinkGroupID()
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeHumanRequest,
		NodeID:    "human_request",
		Iteration: iteration,
		Payload: map[string]any{
			"request_id": requestID,
			"question":   question,
			"context":    context,
		},
	})
}

// EmitTaskPlan 发射任务计划事件
func (e *Emitter) EmitTaskPlan(plan []*builtin_tools.PlanItem, explanation string) {
	e.ResetThinkGroupID()
	e.Emit(&AgentOutputEvent{
		Type:   EventTypeTaskPlan,
		NodeID: "task_plan",
		Payload: map[string]any{
			"plan":        plan,
			"explanation": explanation,
		},
	})
}

func (e *Emitter) EmitTaskItem(item *builtin_tools.PlanItem, prevStatus builtin_tools.PlanStepStatus, index int, explanation string) {
	e.ResetThinkGroupID()
	if item == nil {
		return
	}
	id := strings.TrimSpace(item.ID)
	step := strings.TrimSpace(item.Step)
	if step == "" {
		return
	}
	e.Emit(&AgentOutputEvent{
		Type:   EventTypeTaskItem,
		NodeID: "task_item",
		Payload: map[string]any{
			"id":          id,
			"step":        step,
			"status":      item.Status,
			"prev_status": prevStatus,
			"index":       index,
			"depends_on":  item.DependsOn,
			"explanation": strings.TrimSpace(explanation),
		},
	})
}

// EmitIteration 发射迭代事件
func (e *Emitter) EmitIteration(current int, max int, description string) {
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeIteration,
		NodeID:    "iteration",
		Iteration: current,
		Payload: map[string]any{
			"current":     current,
			"max":         max,
			"description": description,
		},
	})
}

// EmitResult 发射结果事件
func (e *Emitter) EmitResult(result any, success bool) {
	e.Emit(&AgentOutputEvent{
		Type:   EventTypeResult,
		NodeID: "result",
		Payload: map[string]any{
			"result":  result,
			"success": success,
		},
	})
}

func (e *Emitter) EmitToolUpdate(payload map[string]any) {
	if payload == nil {
		payload = make(map[string]any)
	}
	nodeID := "tool:update"
	if toolName := builtin_tools.ToolRuntimeValue(payload["tool_name"]); toolName != "" {
		nodeID = "tool:update:" + toolName
	}
	e.Emit(&AgentOutputEvent{
		Type:    EventTypeToolUpdate,
		NodeID:  nodeID,
		Payload: builtin_tools.CloneAnyMap(payload),
	})
}

func (e *Emitter) EmitRetry(attempt int, maxAttempts int, delay time.Duration, next time.Time, message string) {
	e.Emit(&AgentOutputEvent{
		Type:   EventTypeRetry,
		NodeID: "retry",
		Payload: map[string]any{
			"attempt":      attempt,
			"max_attempts": maxAttempts,
			"delay_ms":     delay.Milliseconds(),
			"next_unix_ms": next.UnixMilli(),
			"message":      strings.TrimSpace(message),
		},
	})
}

func (e *Emitter) EmitLogPayload(payload map[string]any) {
	if payload == nil {
		payload = make(map[string]any)
	}
	e.Emit(&AgentOutputEvent{
		Type:    EventTypeLog,
		NodeID:  "log",
		Payload: builtin_tools.CloneAnyMap(payload),
	})
}

// EmitLog 发射日志事件
func (e *Emitter) EmitLog(level string, message string) {
	e.EmitLogPayload(map[string]any{
		"level":   level,
		"message": message,
	})
}

// EmitInfo 发射 info 级别日志
func (e *Emitter) EmitInfo(message string) {
	e.EmitLog("info", message)
}

// EmitWarning 发射 warning 级别日志
func (e *Emitter) EmitWarning(message string) {
	e.EmitLog("warning", message)
}

// EmitError 发射 error 级别日志
func (e *Emitter) EmitError(message string) {
	e.EmitLog("error", message)
}

func (e *Emitter) EmitStepSummaryResult(stepID string, stepName string, outcome *builtin_tools.StepOutcome) {
	e.ResetThinkGroupID()
	if outcome == nil {
		return
	}
	e.Emit(&AgentOutputEvent{
		Type:   EventTypeStepSummaryResult,
		NodeID: "step_summary_result",
		Payload: map[string]any{
			"step_id":           strings.TrimSpace(stepID),
			"step_name":         strings.TrimSpace(stepName),
			"short_summary":     strings.TrimSpace(outcome.ShortSummary),
			"long_summary":      strings.TrimSpace(outcome.LongSummary),
			"key_facts":         outcome.KeyFacts,
			"open_questions":    outcome.OpenQuestions,
			"tool_calls_digest": strings.Join(outcome.ToolCallsDigest, "\n"),
			"references":        outcome.References,
		},
	})
}

func (e *Emitter) EmitStepReplanResult(stepID string, stepName string, result *stepReplanModelOutput) {
	e.ResetThinkGroupID()
	if result == nil {
		return
	}
	e.Emit(&AgentOutputEvent{
		Type:   EventTypeStepReplanResult,
		NodeID: "step_replan_result",
		Payload: map[string]any{
			"step_id":       strings.TrimSpace(stepID),
			"step_name":     strings.TrimSpace(stepName),
			"should_replan": result.ShouldReplan,
			"replan_reason": strings.TrimSpace(result.ReplanReason),
			"next_goal":     strings.TrimSpace(result.NextGoal),
			"missing_items": normalizeStringSlice(result.MissingItems),
			"warnings":      normalizeStringSlice(result.Warnings),
		},
	})
}

func (e *Emitter) EmitFinalAnswerResult(answer *builtin_tools.FinalAnswer) {
	e.ResetThinkGroupID()
	if answer == nil || strings.TrimSpace(answer.Content) == "" {
		return
	}
	e.Emit(&AgentOutputEvent{
		Type:   EventTypeFinalAnswerResult,
		NodeID: "final_answer_result",
		Payload: map[string]any{
			"content":    strings.TrimSpace(answer.Content),
			"source":     strings.TrimSpace(answer.Source),
			"references": answer.References,
		},
	})
}

func (e *Emitter) EmitHistoryCompacted(payload map[string]any) {
	if payload == nil {
		payload = make(map[string]any)
	}
	e.Emit(&AgentOutputEvent{
		Type:    EventTypeHistoryCompacted,
		NodeID:  "history:compacted",
		Payload: payload,
	})
}
