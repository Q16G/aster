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
	// history 仍由 scheduler goroutine 单写（plan/replan/final_answer phase）。
	// stepHistory / stepHistoryStepID / stepHistoryPhase / stepHistoryPlanVer 为
	// 老的「单桶」字段——commit 2 阶段保留，由 commit 3 改造 AICallProxy 签名后统一
	// 切到 stepHistories 多桶并清理。
	//
	// stepHistories 是新的「按 stepID 分桶」存储：每个 inline step 桶由其自身 goroutine
	// 单写；stepHistoryMu 仅保护 map 增删（取桶 / 增删桶），不保护桶内 slice 写。
	// MaxParallelSteps=1 时单桶 fallback 走相同代码路径（map 仅 1 个 entry），不保留分叉。
	history                   []*ai.MsgInfo
	stepHistory               []*ai.MsgInfo
	stepHistoryStepID         string
	stepHistoryPhase          builtin_tools.AgentPhase
	stepHistoryPlanVer        int
	stepHistories             map[string]*stepHistoryBucket
	stepHistoryMu             sync.Mutex
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
	// frozenStepCache 是 think_act 首条 user message 的「按 (stepID, planVer) 分桶」
	// 冻结快照（step 内字节恒定，使消息前缀的移动缓存断点全程命中）。
	//
	// **多 peer 并发安全**：旧版用 3 个独立 frozen 字段（*PromptParts + stepID + planVer）
	// 单字段无锁——主路径 + 多 peer 同时调 thinkActPartsForStep 会 race + 缓存抖动
	// （peer A 写 s2 顶掉 peer B 的 s3）。改 map + RWMutex 后每个 (stepID, planVer)
	// 独立 entry，并发 peer 互不串扰，与 streamKey / thinkBufKey 同款分桶哲学。
	currentTaskContext *TaskContextData
	// identityEnv* run 内稳定（agent 启动后不变直到下次 run）。多 peer 并发
	// thinkActPartsForStep 会同时调 identityEnvBlock，旧版无锁导致 race。
	// identityEnvMu 保护双字段，double-check 模式见 identityEnvBlock。
	identityEnvMu       sync.RWMutex
	identityEnvPrompt   string
	identityEnvBuilt    bool
	frozenStepCache     *frozenStepPromptCache
	currentResultSource ResultSource
	workspaceRuntime    builtin_tools.WorkspaceRuntime
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

	// agentFactory 在 AgentFactory.Build 路径里为非 sub_agent 的根 Agent 注入，
	// 供 X2 step_fanout.spawnRemoteStep 派发远程 step 时复用同一 factory 构造 child。
	// sub_agent 自身的 Agent 实例不持有 factory，避免嵌套 spawn。
	agentFactory *AgentFactory

	// requestPool 引用 AgentFactory 持有的 ai 请求 limiter。
	// AICallProxy / runStructuredOutputWithRetry 入口把它注入 ctx，
	// ai.ChatExWithOptions / ChatStreamWithOptions 在统一入口 Acquire/Release。
	// nil 时（未经 factory.Build 的实例，主要是单测）AI 请求不受限。
	requestPool *AgentRequestPool

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

	// journaledStepIDs 记录已固化（烘焙 + 写 planner.jsonl 的 kind=step 记录）的 step，
	// 按 step_id 单维去重（不带 plan_version——每个 step 的 kind=step 记录只在它完成那刻落盘
	// 一次、归属完成时的 plan_version；重规划把已完成 step 并入新 plan 时不应在新版本下重复落盘）。
	// X2 滚动收尾扫描（finalizeUnjournaledTerminalSteps）与 applyReplanResult 共用它做幂等：
	// 任一路径固化某 step 后登记，另一路径见已登记即跳过，保证每个终态 step 恰好落盘一次。
	// 仅在调度 goroutine 上读写（收尾扫描与 step_replan 都在调度 goroutine），无并发问题；
	// 运行时态、不经 durable_resume 持久化，每轮 Execute 重置。
	journaledStepIDs map[string]struct{}
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
		agentName:       name,
		cfg:             cfg,
		tools:           utils.NewOrderMapx[string, Tool](),
		promptManager:   cfg.PromptManager,
		state:           NewStateTracker(),
		handoff:         &handoffState{},
		stepHistories:   make(map[string]*stepHistoryBucket),
		frozenStepCache: newFrozenStepPromptCache(),
	}

	if cfg.Emitter == nil {
		return nil, fmt.Errorf("emitter is required")
	}
	agent.emitter = cfg.Emitter

	// 把 emitter 绑成 PlanItemChange observer：mutator 翻 PlanItem.Status 时自动 emit
	// task_item / inline_step_start / inline_step_end。替代散落各处的 emitTaskItemDiffs
	// 与 EmitJSON(EventTypeInlineStepStart/End) 显式调用。
	agent.state.RegisterObserver(newEmitterStateObserver(agent))
	// workspaceRuntime observer：PlanItem Pending→InProgress 自动 ensureStepFileScaffold。
	// observer 持 agent 活引用，在回调时按 a.workspaceRuntime 现读——allow workspaceRuntime
	// 由 Execute 注入（在 Agent 构造时仍为 nil）。
	agent.state.RegisterObserver(newWorkspaceStateObserver(agent))

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

// persistBucketTranscriptBlob 把 inline peer 桶的 transcript 一次性写入主 v2Store。
// 与 persistStepTranscriptBlob 的差异：
//   - 显式接 bucket 参数，读 bucket.msgs 而非 a.stepHistory（peer 跑的是自己桶）
//   - 单 writer：peer goroutine 走到 terminal 时调（loop 已退出，桶 msgs 不再写）；
//     主路径不竞争同一 blob
//   - 不更新 a.lastStepTranscriptBlobRef（那是主路径专用字段）；返回 ref 由调用方
//     塞到 StepAttemptResult.TranscriptBlobRef 让 outcome 知道指向何处
//
// 调用方契约：在 spawnInlinePeer goroutine 内、Complete 之前、dropBucket 之前调用。
// 失败返回 ""，由调用方决定是否当 result.Error。
func (a *Agent) persistBucketTranscriptBlob(bucket *stepHistoryBucket) string {
	if a == nil || a.v2Store == nil || bucket == nil || len(bucket.msgs) == 0 {
		return ""
	}
	raw, err := json.Marshal(ai.NormalizeMsgInfoSlice(bucket.msgs))
	if err != nil {
		a.emitPersistenceWarning("marshal_inline_bucket_transcript", err)
		return ""
	}
	ref, err := a.v2Store.WriteBlob(raw)
	if err != nil {
		a.emitPersistenceWarning("write_blob_inline_bucket_transcript", err)
		return ""
	}
	return strings.TrimSpace(ref)
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
	currentPhase := currentPhase(snapshot, a.effectiveWaveWidth())
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

// maxParallelSteps 返回同层 ready step 最大并发数（含主路径）。
// nil 防御 + 默认 1（向后兼容串行）。配置为 0 / 负数时也回退到 1。
func (a *Agent) maxParallelSteps() int {
	if a == nil || a.cfg == nil {
		return 1
	}
	if a.cfg.MaxParallelSteps < 1 {
		return 1
	}
	return a.cfg.MaxParallelSteps
}

// maxParallelChains 链间维度乘数。nil 防御 + 默认 1（不放大）。0 / 负数回退到 1。
func (a *Agent) maxParallelChains() int {
	if a == nil || a.cfg == nil {
		return 1
	}
	if a.cfg.MaxParallelChains < 1 {
		return 1
	}
	return a.cfg.MaxParallelChains
}

// effectiveWaveWidth 有效波宽 E = max(1,N_step) × max(1,N_chain)：运行时 inline peer
// 同波并发上限（含主路径），同时也是全局 AI 请求池容量的依据。N_chain=1 时退化为
// maxParallelSteps()，行为与引入链间维度前完全一致。
func (a *Agent) effectiveWaveWidth() int {
	return a.maxParallelSteps() * a.maxParallelChains()
}
