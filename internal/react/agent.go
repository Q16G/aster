package react

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
	"aster/internal/utils"
)

var _ builtin_tools.ToolContext = (*Agent)(nil)

type HistoryChangeType string

const (
	HistoryChangeTypeAppend  HistoryChangeType = "append"
	HistoryChangeTypeReplace HistoryChangeType = "replace"
)

type HistoryChange struct {
	Type     HistoryChangeType
	Entries  []*ai.MsgInfo
	Snapshot []*ai.MsgInfo
}

// Agent ReAct Agent 实现
type Agent struct {
	agentName     string
	cfg           *AgentConfig
	tools         *utils.OrderMapx[string, Tool]
	promptManager PromptManager
	state         *StateTracker
	// history and stepHistory are only accessed from the scheduler goroutine (runSchedulerLoop).
	// No mutex is needed as long as this single-writer invariant holds.
	history                   []*ai.MsgInfo
	stepHistory               []*ai.MsgInfo
	stepHistoryStepID         string
	stepHistoryPhase          builtin_tools.AgentPhase
	stepHistoryPlanVer        int
	lastStepTranscriptBlobRef string
	currentRunID              string
	// V2 persistence: session-scoped event store + per-turn correlation id.
	v2Store       *persistv2.Store
	currentTurnID string
	// currentGroupID is an aggregation key carried across a "logical execution chain"
	// (e.g. interrupt raise -> resolve) so UI consumers can group related events.
	currentGroupID      string
	handoff             *handoffState
	emitter             *Emitter
	workspaceSessionID  string
	workspaceRootDir    string
	sourceWorkingDir    string
	parentWorkspaceRoot string
	workspaceNamespace  string
	runtimeRepoContext  RuntimeRepoContext
	frozenLineageByStep map[string]*frozenStepLineage
	// currentTaskContext 与 identityEnv* 为 run 内稳定的 system block2 素材与缓存；
	// frozenStepParts* 是 think_act 首条 user message 的 step 入口冻结快照
	//（step 内字节恒定，使消息前缀的移动缓存断点全程命中）。
	currentTaskContext     *TaskContextData
	identityEnvPrompt      string
	identityEnvBuilt       bool
	frozenStepParts        *PromptParts
	frozenStepPartsStepID  string
	frozenStepPartsPlanVer int
	currentResultSource    ResultSource
	workspaceRuntime       builtin_tools.WorkspaceRuntime
	// stepFileGateRejections 记录 step 过程文件闸门对各 step 的已拒绝次数（有界拒绝后降级放行）。
	stepFileGateMu         sync.Mutex
	stepFileGateRejections map[string]int
	runClientMu            sync.RWMutex
	currentRunClientVal    ai.ChatClient
	finishMu               sync.Mutex
	finishHooks            []func()
	historyHookMu          sync.RWMutex
	historyChangeHook      func(change *HistoryChange)

	asyncRegistry *AsyncAgentRegistry

	// awaitBackgroundRequested 由 await_subagents 工具置位，调度循环读到后会在
	// 非终态时 park 等待后台子 Agent 完成（等待期间零模型调用），随后无条件清除。
	// 仅在调度 goroutine 上读写，无并发问题。
	awaitBackgroundRequested bool

	// resumeChildRecovery 是一个瞬态标记：仅在 resume 回合置位（ContextCarry/ContextReplan/FullResume），
	// 表示「这是一次恢复」。它只是注入中断点子 agent 现场的必要条件——是否真注入由 runPlanPhase
	// 计算的「存在 ParentStepKey 未综合进 step_outcome 的 child_agent」条件决定。判定一次后即清。
	resumeChildRecovery bool

	// contextWindowTokens 是本轮 Execute 时从 runClient 解析到的模型上下文窗口大小（tokens）。
	// 由 Execute 写入，调度循环内只读，无并发问题。用于共享区大文件的动态截断阈值计算。
	contextWindowTokens int

	// consecutiveStepsSinceReplan 是 step_replan 心跳计数器：每跳过一次完整 LLM replan +1，
	// 真正进入 LLM replan 后归 0。配合 STEP_REPLAN_HEARTBEAT_K 兜底，防止"plan 跑很久无 replan"
	// 导致的累积漂移。仅在调度 goroutine 上读写，无并发问题。
	consecutiveStepsSinceReplan int

	// lastReplanBoundaryStepID 是上一次 LLM replan 升级时的 current stepID（即"复核窗口"的右边界）。
	// runStepReplanPhase 命中升级、构造完本次窗口后写入；下一回合构造 review_window 时，
	// 窗口取 plan 中所有索引位于该边界之后且 status ∈ {completed, failed} 的 step。
	// 空串表示尚未发生过 LLM replan，等价于"边界 = -1"，窗口含全部 completed/failed step。
	// 与 consecutiveStepsSinceReplan 一致仅为运行时态、不经 durable_resume 持久化；
	// resume 后字段重置为空串 → 首次升级窗口含全部历史 completed/failed step（偏保守、可接受）。
	// 仅在调度 goroutine 上读写，无并发问题。
	lastReplanBoundaryStepID string
}

// NewReActAgent 创建 ReAct Agent
func NewReActAgent(name string, aiClient ai.ChatClient, opts ...Option) (*Agent, error) {
	if aiClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}

	cfg := defaultAgentConfig(aiClient)
	for _, opt := range opts {
		opt(cfg)
	}
	cfg.Tools = dedupToolsByName(cfg.Tools)
	if cfg.PromptManager == nil {
		manager, err := newDefaultPromptManager()
		if err != nil {
			return nil, err
		}
		cfg.PromptManager = manager
	}

	if cfg.HistoryCompressor == nil {
		budget := resolveContextBudget(cfg.AIClient)
		triggerTokens := budget.TriggerTokens
		if triggerTokens <= 0 {
			triggerTokens = budget.UsableInputTokens
		}
		if triggerTokens > 0 {
			cfg.HistoryCompressor = NewAIHistoryCompressorWithTokenBudget(
				triggerTokens,
				cfg.HistoryCompressKeepLastRounds,
			)
		}
	}
	if compressor, ok := cfg.HistoryCompressor.(*AIHistoryCompressor); ok && compressor != nil {
		compressor.promptManager = cfg.PromptManager
	}
	if cfg.StepHistoryCompressKeepLastRounds < 0 {
		cfg.StepHistoryCompressKeepLastRounds = 0
	}
	if cfg.StepHistoryCompressKeepLastRounds == 0 {
		cfg.StepHistoryCompressKeepLastRounds = cfg.HistoryCompressKeepLastRounds
		if cfg.StepHistoryCompressKeepLastRounds <= 0 {
			cfg.StepHistoryCompressKeepLastRounds = 5
		}
	}
	if cfg.StepHistoryCompressTriggerRatio <= 0 || cfg.StepHistoryCompressTriggerRatio > 1 {
		cfg.StepHistoryCompressTriggerRatio = 0.90
	}
	if cfg.StepHistoryToolResultMaxRunes <= 0 {
		cfg.StepHistoryToolResultMaxRunes = 1024
	}
	if cfg.StepHistoryCompactor == nil {
		cfg.StepHistoryCompactor = NewAIStepHistoryCompactor(
			cfg.StepHistoryCompressTriggerRatio,
			cfg.StepHistoryCompressKeepLastRounds,
			cfg.StepHistoryToolResultMaxRunes,
			cfg.PromptManager,
		)
	}
	if cfg.TaskPlanner == nil {
		cfg.TaskPlanner = NewDefaultTaskPlanner(aiClient, cfg.PromptManager)
	}

	agent := &Agent{
		agentName:     name,
		cfg:           cfg,
		tools:         utils.NewOrderMapx[string, Tool](),
		promptManager: cfg.PromptManager,
		state:         NewStateTracker(),
		handoff:       &handoffState{},
	}

	if cfg.Emitter == nil {
		return nil, fmt.Errorf("emitter is required")
	}
	agent.emitter = cfg.Emitter

	if len(cfg.InitialHistory) > 0 {
		agent.history = make([]*ai.MsgInfo, 0, len(cfg.InitialHistory))
		for _, m := range cfg.InitialHistory {
			if m == nil {
				continue
			}
			agent.history = append(agent.history, m)
		}
	}

	// 平台级内置工具：状态回写、任务状态查询所有 Agent 共享；human_confirm 仅顶层注册（见下）。
	ucsTool := builtin_tools.NewUpdateCurrentStepTool(agent)
	ucsTool.ChildAgentChecker = agent.runningChildAgentNames
	ucsTool.StepFileChecker = agent.checkStepFileProgress
	if err := agent.registerTool(ucsTool); err != nil {
		return nil, err
	}
	if err := agent.registerTool(builtin_tools.NewTaskStatusQueryTool(agent)); err != nil {
		return nil, err
	}
	// human_confirm 只在顶层 agent 注册：子 agent 发起的 durable interrupt 永远到不了人类，
	// 请求只会挂起直到 ctx 取消并把整个子 agent 标记为失败。
	if !cfg.IsSubAgent {
		if err := agent.registerTool(builtin_tools.NewHumanConfirmTool(agent)); err != nil {
			return nil, err
		}
	}
	if cfg.BashTool != nil {
		bashTool := builtin_tools.NewBashTool(agent, cfg.BashTool.PermCtx, cfg.BashTool.SessionAL)
		if err := agent.registerTool(bashTool); err != nil {
			return nil, err
		}
	}
	if cfg.PowerShellTool != nil {
		psTool := builtin_tools.NewPowerShellTool(agent, cfg.PowerShellTool.PermCtx, cfg.PowerShellTool.SessionAL)
		if err := agent.registerTool(psTool); err != nil {
			return nil, err
		}
	}

	for _, tool := range cfg.Tools {
		if tool == nil {
			continue
		}
		if err := agent.registerTool(tool); err != nil {
			return nil, err
		}
	}

	return agent, nil
}

func dedupToolsByName(tools []Tool) []Tool {
	if len(tools) == 0 {
		return tools
	}

	last := make(map[string]int, len(tools))
	for i, tool := range tools {
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Name())
		if name == "" {
			continue
		}
		last[name] = i
	}

	out := make([]Tool, 0, len(tools))
	for i, tool := range tools {
		if tool == nil {
			continue
		}
		name := strings.TrimSpace(tool.Name())
		if name == "" {
			out = append(out, tool)
			continue
		}
		if last[name] != i {
			continue
		}
		out = append(out, tool)
	}
	return out
}

// ensureAsyncRegistry lazily creates the AsyncAgentRegistry.
// Safe to call from the scheduler goroutine only (single-writer invariant).
func (a *Agent) ensureAsyncRegistry() *AsyncAgentRegistry {
	if a.asyncRegistry == nil {
		a.asyncRegistry = NewAsyncAgentRegistry()
	}
	return a.asyncRegistry
}

func (a *Agent) setCurrentRunClient(c ai.ChatClient) {
	a.runClientMu.Lock()
	defer a.runClientMu.Unlock()
	a.currentRunClientVal = c
}

func (a *Agent) getCurrentRunClient() ai.ChatClient {
	a.runClientMu.RLock()
	defer a.runClientMu.RUnlock()
	if a.currentRunClientVal != nil {
		return a.currentRunClientVal
	}
	if a.cfg != nil {
		return a.cfg.AIClient
	}
	return nil
}

func (a *Agent) Name() string {
	if a == nil {
		return ""
	}
	return a.agentName
}

// State 返回当前状态快照
func (a *Agent) State() builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.Snapshot()
}

func (a *Agent) replaceState(snapshot builtin_tools.StateSnapshot) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.Replace(snapshot)
}

func (a *Agent) ReplaceState(snapshot builtin_tools.StateSnapshot) builtin_tools.StateSnapshot {
	return a.replaceState(snapshot)
}

// History 返回历史消息
func (a *Agent) History() []*ai.MsgInfo {
	if a == nil || len(a.history) == 0 {
		return nil
	}
	return a.history
}

func (a *Agent) resetStepHistory() {
	if a == nil {
		return
	}
	a.stepHistory = nil
	a.stepHistoryStepID = ""
	a.stepHistoryPhase = ""
	a.stepHistoryPlanVer = 0
}

func (a *Agent) persistStepTranscriptBlob() {
	if a == nil || a.v2Store == nil || len(a.stepHistory) == 0 {
		return
	}
	// If step history was compacted mid-step, we keep the first full transcript snapshot
	// for step_replan/backtracking. Do not overwrite it with a later compacted view.
	if strings.TrimSpace(a.lastStepTranscriptBlobRef) != "" {
		return
	}
	raw, err := json.Marshal(ai.NormalizeMsgInfoSlice(a.stepHistory))
	if err != nil {
		a.emitPersistenceWarning("marshal_step_transcript", err)
		return
	}
	ref, err := a.v2Store.WriteBlob(raw)
	if err != nil {
		a.emitPersistenceWarning("write_blob_step_transcript", err)
		return
	}
	a.lastStepTranscriptBlobRef = strings.TrimSpace(ref)
}

func (a *Agent) persistInFlightStepHistory() {
	if a == nil || a.v2Store == nil {
		return
	}

	var runtimeRef, stepRef, convRef string

	if a.state != nil {
		rawState, err := json.Marshal(a.state.Snapshot())
		if err == nil && len(rawState) > 0 {
			ref, werr := a.v2Store.WriteBlob(rawState)
			if werr != nil {
				a.emitPersistenceWarning("inflight_write_blob_runtime_state", werr)
			} else {
				runtimeRef = strings.TrimSpace(ref)
			}
		} else if err != nil {
			a.emitPersistenceWarning("inflight_marshal_runtime_state", err)
		}
	}

	if len(a.stepHistory) > 0 {
		rawStep, err := json.Marshal(ai.NormalizeMsgInfoSlice(a.stepHistory))
		if err == nil && len(rawStep) > 0 {
			ref, werr := a.v2Store.WriteBlob(rawStep)
			if werr != nil {
				a.emitPersistenceWarning("inflight_write_blob_step_history", werr)
			} else {
				stepRef = strings.TrimSpace(ref)
			}
		} else if err != nil {
			a.emitPersistenceWarning("inflight_marshal_step_history", err)
		}
	}

	if len(a.history) > 0 {
		rawConv, err := json.Marshal(ai.NormalizeMsgInfoSlice(a.history))
		if err == nil && len(rawConv) > 0 {
			ref, werr := a.v2Store.WriteBlob(rawConv)
			if werr != nil {
				a.emitPersistenceWarning("inflight_write_blob_conversation_history", werr)
			} else {
				convRef = strings.TrimSpace(ref)
			}
		} else if err != nil {
			a.emitPersistenceWarning("inflight_marshal_conversation_history", err)
		}
	}

	if runtimeRef == "" && stepRef == "" && convRef == "" {
		return
	}

	snap, err := a.v2Store.LoadSnapshot()
	if err != nil {
		a.emitPersistenceWarning("inflight_load_snapshot", err)
		return
	}
	if snap == nil {
		snap = &persistv2.Snapshot{}
	}
	if runtimeRef != "" {
		snap.RuntimeStateBlobRef = runtimeRef
	}
	if stepRef != "" {
		snap.StepHistoryBlobRef = stepRef
	}
	if convRef != "" {
		snap.ConversationHistoryBlobRef = convRef
	}
	if err := a.v2Store.SaveSnapshotAtomic(snap); err != nil {
		a.emitPersistenceWarning("inflight_save_snapshot", err)
	}
}

func (a *Agent) ensureStepHistoryForStep(stepID string) {
	if a == nil {
		return
	}
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		a.resetStepHistory()
		return
	}
	if strings.TrimSpace(a.stepHistoryStepID) == stepID {
		return
	}
	// New step: clear the previous step transcript.
	a.stepHistory = nil
	a.lastStepTranscriptBlobRef = "" // prevent stale ref from associating with the wrong step (no replan follows direct step switch)
	a.stepHistoryStepID = stepID
}

func (a *Agent) syncStepHistoryLayer(snapshot builtin_tools.StateSnapshot) {
	if a == nil {
		return
	}
	currentPhase := currentPhase(snapshot)
	prevPhase := a.stepHistoryPhase
	prevStepID := strings.TrimSpace(a.stepHistoryStepID)
	prevPlanVer := a.stepHistoryPlanVer
	prevLayerLen := len(a.stepHistory)

	if currentPhase != builtin_tools.AgentPhaseStep {
		if prevStepID != "" || prevLayerLen > 0 {
			a.persistStepTranscriptBlob()
			a.emitRuntimeLog("info", "step history layer cleared", snapshot, map[string]any{
				"event":                   "step_history_layer_transition",
				"history_transition_name": "leave_step_phase_clear",
				"previous_phase":          prevPhase,
				"next_phase":              currentPhase,
				"previous_step_id":        prevStepID,
				"next_step_id":            "",
				"previous_plan_version":   prevPlanVer,
				"next_plan_version":       snapshot.PlanVersion,
				"previous_layer_messages": prevLayerLen,
				"next_layer_messages":     0,
			})
		}
		a.resetStepHistory()
		return
	}

	stepID := strings.TrimSpace(snapshot.CurrentStepID)
	if stepID == "" {
		if current := snapshot.CurrentStep(); current != nil {
			stepID = strings.TrimSpace(current.ID)
		}
	}

	transitionName := ""
	switch {
	case prevStepID == "" && stepID != "":
		transitionName = "enter_step_attach"
	case prevPlanVer != 0 && prevPlanVer != snapshot.PlanVersion && prevStepID == stepID:
		transitionName = "plan_changed_reset"
	case prevPlanVer != 0 && prevPlanVer != snapshot.PlanVersion && prevStepID != stepID:
		transitionName = "plan_changed_step_switch"
	case prevStepID != "" && prevStepID != stepID:
		transitionName = "step_switch_reset"
	}

	a.ensureStepHistoryForStep(stepID)
	a.stepHistoryPhase = currentPhase
	a.stepHistoryPlanVer = snapshot.PlanVersion

	if transitionName != "" {
		a.emitRuntimeLog("info", "step history layer switched", snapshot, map[string]any{
			"event":                   "step_history_layer_transition",
			"history_transition_name": transitionName,
			"previous_phase":          prevPhase,
			"next_phase":              currentPhase,
			"previous_step_id":        prevStepID,
			"next_step_id":            stepID,
			"previous_plan_version":   prevPlanVer,
			"next_plan_version":       snapshot.PlanVersion,
			"previous_layer_messages": prevLayerLen,
			"next_layer_messages":     len(a.stepHistory),
		})
	}
}

// SetHistory 设置历史消息
func (a *Agent) SetHistory(history []*ai.MsgInfo) {
	if a == nil {
		return
	}
	if len(history) == 0 {
		a.history = nil
		a.notifyHistoryReplace()
		return
	}
	cloned := make([]*ai.MsgInfo, 0, len(history))
	for _, msg := range history {
		if msg == nil {
			continue
		}
		cloned = append(cloned, msg)
	}
	a.history = cloned
	a.notifyHistoryReplace()
}

func (a *Agent) SetHistoryChangeHook(hook func(change *HistoryChange)) {
	if a == nil {
		return
	}
	a.historyHookMu.Lock()
	a.historyChangeHook = hook
	a.historyHookMu.Unlock()
}

func (a *Agent) notifyHistoryAppend(entries ...*ai.MsgInfo) {
	if a == nil || len(entries) == 0 {
		return
	}
	a.historyHookMu.RLock()
	hook := a.historyChangeHook
	a.historyHookMu.RUnlock()
	if hook == nil {
		return
	}
	normalized := NormalizeHistoryMsgInfos(entries)
	if len(normalized) == 0 {
		return
	}
	hook(&HistoryChange{
		Type:    HistoryChangeTypeAppend,
		Entries: normalized,
	})
}

func (a *Agent) notifyHistoryReplace() {
	if a == nil {
		return
	}
	a.historyHookMu.RLock()
	hook := a.historyChangeHook
	a.historyHookMu.RUnlock()
	if hook == nil {
		return
	}
	hook(&HistoryChange{
		Type:     HistoryChangeTypeReplace,
		Snapshot: NormalizeHistoryMsgInfos(a.history),
	})
}

func NormalizeHistoryMsgInfos(items []*ai.MsgInfo) []*ai.MsgInfo {
	return ai.NormalizeMsgInfoSlice(items)
}

// AddFinishHook 添加完成钩子
func (a *Agent) AddFinishHook(fn func()) {
	if a == nil || fn == nil {
		return
	}
	a.finishMu.Lock()
	a.finishHooks = append(a.finishHooks, fn)
	a.finishMu.Unlock()
}

func (a *Agent) runFinishHooks() {
	if a == nil {
		return
	}

	a.finishMu.Lock()
	hooks := append([]func(){}, a.finishHooks...)
	a.finishHooks = nil
	a.finishMu.Unlock()

	for _, fn := range hooks {
		if fn == nil {
			continue
		}
		func() {
			defer func() { _ = recover() }()
			fn()
		}()
	}
}

func (a *Agent) registerTool(tool Tool) error {
	if tool == nil {
		return nil
	}
	name := strings.TrimSpace(tool.Name())
	if name == "" {
		return fmt.Errorf("tool name is empty")
	}
	a.tools.Set(name, tool)
	return nil
}

func (a *Agent) unregisterTool(name string) {
	if a == nil || a.tools == nil {
		return
	}
	a.tools.Delete(strings.TrimSpace(name))
}

func (a *Agent) unregisterToolsByPrefix(prefix string) []string {
	if a == nil || a.tools == nil {
		return nil
	}
	var removed []string
	for _, name := range a.tools.Keys() {
		if strings.HasPrefix(name, prefix) {
			a.tools.Delete(name)
			removed = append(removed, name)
		}
	}
	return removed
}

// GetTool 获取工具
func (a *Agent) GetTool(name string) (Tool, bool) {
	if a == nil || a.tools == nil {
		return nil, false
	}
	tool, ok := a.tools.Get(strings.TrimSpace(name))
	return tool, ok
}

// Tools 返回所有工具
func (a *Agent) Tools() map[string]Tool {
	if a == nil || a.tools == nil {
		return nil
	}
	out := make(map[string]Tool, a.tools.Len())
	a.tools.ForEach(func(name string, tool Tool) {
		out[name] = tool
	})
	return out
}

// ==================== ToolContext 接口实现 ====================

// Snapshot 实现 StateReader 接口
func (a *Agent) Snapshot() builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.Snapshot()
}

// UpdatePlan 实现 PlanManager 接口
func (a *Agent) UpdatePlan(plan []*builtin_tools.PlanItem, explanation string, needsPlanning bool) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.UpdatePlan(plan, explanation, needsPlanning)
}

// SetGoalUnderstanding 记录 planner 对原始输入的结构化理解。
func (a *Agent) SetGoalUnderstanding(understanding string) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.SetGoalUnderstanding(understanding)
}

func (a *Agent) UpdateCurrentStep(update builtin_tools.CurrentStepUpdate) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.UpdateCurrentStep(update)
}

// UpdateTaskStatus 实现 TaskStateManager 接口
func (a *Agent) UpdateTaskStatus(update builtin_tools.TaskStatusUpdate) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.UpdateTaskStatus(update)
}

func (a *Agent) SetCurrentGoal(goal string) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.SetCurrentGoal(goal)
}

func (a *Agent) AppendInputTimeline(content string) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.AppendInputTimeline(content)
}

func (a *Agent) SetPhase(phase builtin_tools.AgentPhase) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.SetPhase(phase)
}

func (a *Agent) EnsureCurrentStep() builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.EnsureCurrentStep()
}

func (a *Agent) SetFinalAnswer(content string, source string) builtin_tools.StateSnapshot {
	if a == nil || a.state == nil {
		return builtin_tools.StateSnapshot{}
	}
	return a.state.SetFinalAnswer(content, source)
}

// GetTaskPlanner 实现 ToolContext 接口
func (a *Agent) GetTaskPlanner() builtin_tools.TaskPlanner {
	if a == nil || a.cfg == nil {
		return nil
	}
	if a.cfg.TaskPlanner != nil {
		return a.cfg.TaskPlanner
	}
	return NewDefaultTaskPlanner(a.cfg.AIClient, a.promptManager)
}

func (a *Agent) GetEmitter() builtin_tools.Emitter {
	if a == nil {
		return nil
	}
	return a.emitter
}

// GetAIClient 实现 ToolContext 接口
func (a *Agent) GetAIClient() ai.ChatClient {
	if a == nil || a.cfg == nil {
		return nil
	}
	return a.cfg.AIClient
}

// GetHistory 实现 ToolContext 接口
func (a *Agent) GetHistory() []*ai.MsgInfo {
	if a == nil {
		return nil
	}
	return a.history
}

// GetOnHumanInput 实现 ToolContext 接口
func (a *Agent) GetOnHumanInput() builtin_tools.OnHumanInputFunc {
	if a == nil || a.cfg == nil {
		return nil
	}
	return a.cfg.OnHumanInput
}
