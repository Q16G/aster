package react

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/memory"
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
	memory        *memory.TimelineMemory
	promptManager PromptManager
	state         *StateTracker
	history       []*ai.MsgInfo
	// StepHistory stores the in-step tool calling transcript (assistant messages + tool results).
	// It is cleared when the current step changes so tool outputs do not leak across steps.
	stepHistory        []*ai.MsgInfo
	stepHistoryStepID  string
	stepHistoryPhase   builtin_tools.AgentPhase
	stepHistoryPlanVer int
	currentRunID       string
	// V2 persistence: session-scoped event store + per-turn correlation id.
	v2Store       *persistv2.Store
	currentTurnID string
	// currentGroupID is an aggregation key carried across a "logical execution chain"
	// (e.g. interrupt raise -> resolve) so UI consumers can group related events.
	currentGroupID            string
	handoff                   *handoffState
	emitter                   *Emitter
	workspaceSessionID        string
	workspaceRootDir          string
	workspaceNamespace        string
	frozenLineageByStep       map[string]*frozenStepLineage
	currentResultSource       ResultSource
	currentPublishContract    string
	currentFinalAnswerPublish *FinalAnswerPublishConfig
	workspaceRuntime          builtin_tools.WorkspaceRuntime
	runClientMu               sync.RWMutex
	currentRunClientVal       ai.ChatClient
	finishMu                  sync.Mutex
	finishHooks               []func()
	historyHookMu             sync.RWMutex
	historyChangeHook         func(change *HistoryChange)
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

	// 平台级内置工具：状态回写和人工确认，所有 Agent 共享。
	ucsTool := builtin_tools.NewUpdateCurrentStepTool(agent)
	ucsTool.ContractLookup = func(name string) *builtin_tools.OutputContract {
		return agent.cfg.LookupOutputContract(name)
	}
	if err := agent.registerTool(ucsTool); err != nil {
		return nil, err
	}
	if err := agent.registerTool(builtin_tools.NewTaskStatusQueryTool(agent)); err != nil {
		return nil, err
	}
	if err := agent.registerTool(builtin_tools.NewHumanConfirmTool(agent)); err != nil {
		return nil, err
	}
	if cfg.BashTool != nil {
		bashTool := builtin_tools.NewBashTool(agent, cfg.BashTool.PermCtx, cfg.BashTool.SessionAL)
		if err := agent.registerTool(bashTool); err != nil {
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

	var memOpts []memory.TimelineOption
	if cfg.MemoryTriggerBytes >= 0 {
		memOpts = append(memOpts, memory.WithTriggerBytes(cfg.MemoryTriggerBytes))
	} else {
		budget := resolveContextBudget(aiClient)
		triggerTokens := budget.TriggerTokens
		if triggerTokens <= 0 {
			triggerTokens = budget.UsableInputTokens
		}
		if triggerTokens > 0 {
			memOpts = append(memOpts, memory.WithTriggerBytes(triggerTokens*defaultCharsPerToken))
		}
	}
	if cfg.MemoryKeepLastItems >= 0 {
		memOpts = append(memOpts, memory.WithKeepLastItems(cfg.MemoryKeepLastItems))
	}
	agent.memory = memory.NewTimeLine(
		context.Background(),
		aiClient,
		func() string {
			return ""
		},
		memOpts...,
	)

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

// AddMemoryAssistantOutput 实现 ToolContext 接口
func (a *Agent) AddMemoryAssistantOutput(content string) {
	if a == nil || a.memory == nil {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	_ = a.memory.AddItem(generateRandomString(8), memory.NewAssistantOutputItem(content))
	a.memory.TryCompressAsync()
}
