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
	EventTypeThink        EventType = "think"
	EventTypeToolStart    EventType = "tool_start"
	EventTypeToolEnd      EventType = "tool_end"
	EventTypeAgentEnter   EventType = "agent_enter"
	EventTypeAgentExit    EventType = "agent_exit"
	EventTypeStateChange  EventType = "state_change"
	EventTypeHumanRequest EventType = "human_request"
	EventTypeTaskPlan     EventType = "task_plan"
	EventTypeTaskItem     EventType = "task_item"
	EventTypeIteration    EventType = "iteration"
	EventTypeResult       EventType = "result"
	EventTypeToolUpdate   EventType = "tool_update"
	EventTypeRetry        EventType = "retry"
	EventTypeLog          EventType = "log"
	EventTypeStream       EventType = "stream"
	// EventTypeStreamEnd 是一次模型生成 streaming 结束的显式信号——TUI 端据此 flush
	// 对应 (agentName, stepID) 桶，不再让结构事件（phase/iteration/tool_start/
	// step_summary 等）反推「该 flush 哪个 buffer」。emitter 端在 AICallProxyStream
	// 退出 streaming 回调时主动发该事件，owner=runCtxStepID(runCtx)。
	EventTypeStreamEnd         EventType = "stream_end"
	EventTypeStepFinish        EventType = "step_finish"
	EventTypeHistoryCompacted  EventType = "history_compacted"
	EventTypeStepSummaryResult EventType = "step_summary_result"
	EventTypeStepTriageResult  EventType = "step_triage_result"
	EventTypeStepReplanResult  EventType = "step_replan_result"
	EventTypeFinalAnswerResult EventType = "final_answer_result"
	EventTypeSubAgentBgStart   EventType = "subagent_bg_start"
	EventTypeSubAgentBgEnd     EventType = "subagent_bg_end"
	// EventTypeInlineStepStart/End 是 inline step（commit 7-9 重构后的并发 step）卡片事件。
	// 与 sub_agent 的 BgStart/BgEnd 同结构但 Kind 不同——drain 路径按 Kind 分流。
	// 区分点是「是否独立 agent 实例」（sub_agent=out-of-line，inline_step=inline）而非
	// 「是否并发」——MaxParallelSteps=1 时仍走相同事件路径，名字依然成立。
	EventTypeInlineStepStart EventType = "inline_step_start"
	EventTypeInlineStepEnd   EventType = "inline_step_end"
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
	// thinkGroupIDs 按 stepID 作用域维护 thinking 分组 UUID（key=stepID，""=主路径）。
	// 并发 step 共享同一 Emitter（spawnInlinePeer 复用同一 Agent），分组键必须 per-stepID
	// 隔离——否则一个 step 的边界事件会清掉另一个并发 step 的 in-flight reasoning 分组，
	// 把连贯 reasoning 切成单 token 碎片。
	thinkGroupIDs map[string]string
}

// NewEmitter 创建事件发射器
func NewEmitter(id string, agentName string, emitter BaseEmitterFunc) *Emitter {
	return &Emitter{
		id:            id,
		agentName:     agentName,
		baseEmitter:   emitter,
		seqID:         &atomic.Uint64{},
		thinkGroupIDs: make(map[string]string),
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

// EnsureThinkGroupID 返回（必要时新建）stepID 作用域下的 thinking 分组 UUID。
// stepID="" 为主路径；peer 走 runCtx.StepID。并发 step 各自独立 entry，互不串扰。
func (e *Emitter) EnsureThinkGroupID(stepID string) string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.thinkGroupIDs == nil {
		e.thinkGroupIDs = make(map[string]string)
	}
	if e.thinkGroupIDs[stepID] == "" {
		e.thinkGroupIDs[stepID] = uuid.NewString()
	}
	return e.thinkGroupIDs[stepID]
}

// ResetThinkGroupID 重置**指定 stepID 作用域**的 thinking 分组——使该 step 的下一个
// EmitThink 起新 ThinkingPart，且**不影响**其它并发 step 的 in-flight 分组。
//
// **不变量（reset 边界 = 该 step 真正 reasoning 段终结 / 真正阶段切换）**：
//
// 只在下面这些场景被调用（均携带 stepID）：
//   - 终结类：EmitThink finishReason 非空 / EmitStepFinish / EmitStepSummaryResult /
//     EmitStepReplanResult
//   - 切换类：EmitToolStart / EmitToolEnd
//
// **禁止边界**（曾被错误使用、commit 后移除）：
//   - 每 token Content stream（EmitStream）—— deepseek 等 reasoning 模型在同 chunk
//     内同时吐 ReasoningContent + Content 是正常行为，不构成 reasoning 段终结。
//     旧实现 EmitStream 调 reset → 让下一段 reasoning 起新 GroupID → ThinkingPart
//     被切碎为 200 条 1-3 token 的独立 part（主屏「Thinking: -b/rowser/.」碎片化症状）
//   - observer 细粒度 plan_item 状态变化（EmitTaskItem）—— peer 状态变化与主路径
//     thinking 段边界正交，主路径 thinking 中间不应被 peer 边界事件切断
//   - 全局 observer 状态翻转（EmitStateChange）/ 人工请求（EmitHumanRequest）/ 计划
//     重建（EmitTaskPlan）—— 这些事件与任何 step 的 reasoning 段边界正交，且在并发下
//     高频触发，曾全局 reset 把别的 step 的 reasoning 切碎；现一律不 reset，真边界由
//     tool / step_finish / summary / replan（均 per-step）覆盖。
//
// 新增 emit 路径时若不确定要不要 reset：默认**不调用** reset，除非你的事件语义
// 上确实是「该 step 一段 reasoning 段终结」。turn 级终结用 ResetAllThinkGroupIDs。
func (e *Emitter) ResetThinkGroupID(stepID string) {
	if e == nil {
		return
	}
	e.mu.Lock()
	delete(e.thinkGroupIDs, stepID)
	e.mu.Unlock()
}

// ResetAllThinkGroupIDs 清空所有作用域的 thinking 分组——仅用于 turn 级终结
// （EmitFinalAnswerResult），此时所有 step 的 reasoning 都应封口。
func (e *Emitter) ResetAllThinkGroupIDs() {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.thinkGroupIDs = make(map[string]string)
	e.mu.Unlock()
}

// EmitThink 发射思考事件。stepID 标记 inline_step 归属（peer 走 runCtx.StepID；
// 主路径 / 非 step phase 传空字符串），TUI 用于 ThinkingPart.StepID + idxByStepID。
func (e *Emitter) EmitThink(iteration int, content string, thinkContent string, reasoningContent string, toolCalls any, finishReason string, stepID string) {
	groupID := e.EnsureThinkGroupID(stepID)
	payload := map[string]any{
		"content":           content,
		"think_content":     thinkContent,
		"reasoning_content": reasoningContent,
		"tool_calls":        toolCalls,
		"finish_reason":     finishReason,
	}
	if stepID != "" {
		payload["step_id"] = stepID
	}
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeThink,
		NodeID:    "think",
		GroupID:   groupID,
		Iteration: iteration,
		Payload:   payload,
	})
	if strings.TrimSpace(finishReason) != "" {
		e.ResetThinkGroupID(stepID)
	}
}

// EmitStream 发射流式 token 事件。stepID 标记该流归属哪个 inline_step（peer 走
// runCtx.StepID；主路径 / 非 step phase 传空字符串）。空字符串 stepID 不入 payload，
// 与 EmitThink/EmitToolStart 对称——保证 TUI 端 payload["step_id"] 仅当 peer 出现，
// stream buffer / TextPart 据此做 (agentName, stepID) 隔离与 idxByStepID 索引。
func (e *Emitter) EmitStream(iteration int, delta string, stepID string) {
	// **不调** ResetThinkGroupID：每个 content chunk 不构成 reasoning 段终结。
	// 旧实现这里 reset 会让交错 reasoning/content 的 deepseek 等模型把 200 个 1-3
	// token 的 ThinkingPart 切碎渲染。详见 ResetThinkGroupID doc 的「禁止边界」。
	event := &AgentOutputEvent{
		Type:      EventTypeStream,
		NodeID:    "stream",
		Iteration: iteration,
		Content:   delta,
	}
	if stepID != "" {
		event.Payload = map[string]any{"step_id": stepID}
	}
	e.Emit(event)
}

// EmitStreamEnd 发射「一次模型生成 streaming 结束」的显式信号。owner 通过 stepID
// 字段标记（runCtxStepID(runCtx)，主路径 ""，peer 路径 peerStepID）。TUI 端据此
// 唯一触发 (agentName, stepID) 桶的 flush——不再让 phase/iteration/tool_start/
// step_summary 等结构事件反推该 flush 谁。
//
// 调用纪律：每个 streaming AICallProxy 退出 streaming 回调时**必须**调用一次本方法，
// 无论是否真正产生 stream token（保证 TUI 端有终结信号；空 buffer flush 是 no-op）。
func (e *Emitter) EmitStreamEnd(iteration int, stepID string) {
	event := &AgentOutputEvent{
		Type:      EventTypeStreamEnd,
		NodeID:    "stream_end",
		Iteration: iteration,
	}
	if stepID != "" {
		event.Payload = map[string]any{"step_id": stepID}
	}
	e.Emit(event)
}

func (e *Emitter) EmitStepFinish(iteration int, payload map[string]any, stepID string) {
	e.ResetThinkGroupID(stepID)
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeStepFinish,
		NodeID:    "step_finish",
		Iteration: iteration,
		Payload:   payload,
	})
}

// EmitToolStart 发射工具开始事件。stepID 用于 TUI 把 tool_result 归到归属的
// inline_step 卡片下（commit 11 idxByStepID 消费）。主路径和 plan/replan 等
// 非 step phase 传空字符串。
func (e *Emitter) EmitToolStart(iteration int, call builtin_tools.ToolCall, stepID string) {
	e.ResetThinkGroupID(stepID)
	payload := map[string]any{
		"call_id":     call.ID,
		"tool_name":   call.Name,
		"is_agent":    call.IsAgent,
		"stack_depth": call.StackDepth,
		"arguments":   call.Arguments,
	}
	if stepID != "" {
		payload["step_id"] = stepID
	}
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeToolStart,
		NodeID:    "tool:" + call.Name,
		Iteration: iteration,
		Payload:   payload,
	})
}

// EmitToolEnd 发射工具结束事件。stepID 同 EmitToolStart——peer 桶 tool 走 runCtx.StepID，
// 主路径走 snapshot.CurrentStepID。
func (e *Emitter) EmitToolEnd(iteration int, result builtin_tools.ToolResult, stepID string) {
	e.ResetThinkGroupID(stepID)
	payload := map[string]any{
		"call_id":     result.ID,
		"tool_name":   result.Name,
		"is_agent":    result.IsAgent,
		"stack_depth": result.StackDepth,
		"result":      result.Result,
		"error":       result.Error,
	}
	if len(result.Media) > 0 {
		payload["media"] = result.Media
	}
	if stepID != "" {
		payload["step_id"] = stepID
	}
	e.Emit(&AgentOutputEvent{
		Type:      EventTypeToolEnd,
		NodeID:    "tool:" + result.Name,
		Iteration: iteration,
		Payload:   payload,
	})
}

// EmitStateChange 发射状态变更事件。
// **不调** ResetThinkGroupID：状态翻转与任何 step 的 reasoning 段边界正交，且作为全局
// observer 在并发下高频触发，曾全局 reset 把别的 step 的 reasoning 切碎。详见
// ResetThinkGroupID doc 的「禁止边界」。
func (e *Emitter) EmitStateChange(snapshot builtin_tools.StateSnapshot) {
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
			"unresolved_axes":    snapshot.UnresolvedAxes,
			"plan_version":       snapshot.PlanVersion,
		},
	})
}

// EmitHumanRequest 发射人工请求事件。
// **不调** ResetThinkGroupID：人工请求不是某段 reasoning 的终结边界（且本方法不携带
// stepID）；真正的 step 边界由 tool / step_finish 等覆盖。详见 ResetThinkGroupID doc。
func (e *Emitter) EmitHumanRequest(iteration int, requestID string, question string, context map[string]any) {
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

// EmitTaskPlan 发射任务计划事件。phases 是业务 lane 清单，供 TUI 按 lane 分组展示 step。
// **不调** ResetThinkGroupID：计划重建是结构事件，与逐 step reasoning 边界正交（与
// EmitTaskItem 同类）。详见 ResetThinkGroupID doc 的「禁止边界」。
func (e *Emitter) EmitTaskPlan(plan []*builtin_tools.PlanItem, phases []*builtin_tools.AnalysisTopic, explanation string) {
	e.Emit(&AgentOutputEvent{
		Type:   EventTypeTaskPlan,
		NodeID: "task_plan",
		Payload: map[string]any{
			"plan":        plan,
			"phases":      phases,
			"explanation": explanation,
		},
	})
}

func (e *Emitter) EmitTaskItem(item *builtin_tools.PlanItem, prevStatus builtin_tools.PlanStepStatus, index int, explanation string) {
	// **不调** ResetThinkGroupID：observer 模式下任意 plan_item 状态变化都触发本事件
	// （peer 完成、新 peer 开始、主路径 step 翻 in_progress 等），与主路径 thinking 段
	// 边界正交，不应切断 ThinkingPart。详见 ResetThinkGroupID doc 的「禁止边界」。
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
	e.ResetThinkGroupID(stepID)
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
	e.ResetThinkGroupID(stepID)
	if result == nil {
		return
	}
	completed, blocked, cont := 0, 0, 0
	for _, assess := range result.TopicAssessments {
		if assess == nil {
			continue
		}
		switch assess.Status {
		case builtin_tools.TopicAssessCompleted:
			completed++
		case builtin_tools.TopicAssessBlocked:
			blocked++
		case builtin_tools.TopicAssessContinue:
			cont++
		}
	}
	e.Emit(&AgentOutputEvent{
		Type:   EventTypeStepReplanResult,
		NodeID: "step_replan_result",
		Payload: map[string]any{
			"step_id":                strings.TrimSpace(stepID),
			"step_name":              strings.TrimSpace(stepName),
			"should_replan":          result.ShouldReplan,
			"replan_reason":          strings.TrimSpace(result.ReplanReason),
			"phase_assessments_size": len(result.TopicAssessments),
			"phase_continue_size":    cont,
			"phase_completed_size":   completed,
			"phase_blocked_size":     blocked,
		},
	})
}

func (e *Emitter) EmitFinalAnswerResult(answer *builtin_tools.FinalAnswer) {
	e.ResetAllThinkGroupIDs()
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
