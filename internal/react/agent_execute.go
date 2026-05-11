package react

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/memory"
	"aster/internal/structuredoutput"
	"aster/internal/utils"
)

type ExecuteOption func(*ExecuteConfig)

type ExecuteConfig struct {
	extraText                  string
	taskContext                *TaskContextData
	structuredOutputRetryCount *int
	runID                      string
	workspaceRuntime           builtin_tools.WorkspaceRuntime
	initialState               *builtin_tools.StateSnapshot
	resumeExecutionIntent      bool
	forceColdStart             bool
	resumeOnly                 bool
	resultSource               ResultSource
	publishContract            string
	finalAnswerPublish         *FinalAnswerPublishConfig
}

func normalizeWorkspaceRootDir(rootDir string) string {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return ""
	}
	absRoot, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil {
		return rootDir
	}
	return absRoot
}

func WithExtraText(text string) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		cfg.extraText = text
	}
}

func WithTaskContext(data *TaskContextData) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		cfg.taskContext = data
	}
}

func WithExecuteRunID(runID string) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		cfg.runID = strings.TrimSpace(runID)
	}
}

func WithExecuteStructuredOutputRetryCount(n int) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil || n <= 0 {
			return
		}
		cfg.structuredOutputRetryCount = &n
	}
}

func WithWorkspaceRuntime(runtime builtin_tools.WorkspaceRuntime) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil || runtime == nil {
			return
		}
		cfg.workspaceRuntime = runtime
	}
}

func WithWorkspaceSession(sessionID string, rootDir string) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		runtime, err := newLocalWorkspaceRuntime(sessionID, rootDir, "")
		if err != nil {
			return
		}
		cfg.workspaceRuntime = runtime
	}
}

func WithInitialStateBootstrap(snapshot builtin_tools.StateSnapshot) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		cp := snapshot
		cfg.initialState = &cp
	}
}

// WithResumeExecutionIntent signals the runtime that the caller intends to continue a previous
// execution (if durable checkpoints exist in the workspace).
func WithResumeExecutionIntent() ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		cfg.resumeExecutionIntent = true
	}
}

func WithForceColdStart() ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		cfg.forceColdStart = true
	}
}

func WithResumeOnly() ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		cfg.resumeOnly = true
	}
}

// WithSkipIntentPrelude is kept for backward compatibility; it's now a no-op
// since the Intent Prelude system has been removed.
func WithSkipIntentPrelude() ExecuteOption {
	return func(cfg *ExecuteConfig) {}
}

func WithResultSource(source ResultSource) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		cfg.resultSource = normalizeResultSource(source)
	}
}

func WithPublishContract(name string) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		cfg.publishContract = strings.TrimSpace(name)
	}
}

func WithFinalAnswerPublishConfig(publish *FinalAnswerPublishConfig) ExecuteOption {
	return func(cfg *ExecuteConfig) {
		if cfg == nil {
			return
		}
		cfg.finalAnswerPublish = publish
	}
}

// Execute 执行 Agent
func (a *Agent) Execute(ctx context.Context, input string, opts ...ExecuteOption) (*builtin_tools.RunResult, error) {
	if a == nil || a.cfg == nil || a.cfg.AIClient == nil {
		return nil, fmt.Errorf("agent not initialized")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, fmt.Errorf("input is required")
	}
	defer a.runFinishHooks()
	var runResult *builtin_tools.RunResult

	cfg := &ExecuteConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	extraText := cfg.extraText
	taskContext := cfg.taskContext
	workspaceRuntime := cfg.workspaceRuntime
	if workspaceRuntime == nil {
		workspaceRootDir := ""
		if tempDir, err := os.MkdirTemp("", "sastpro-react-workspace-*"); err == nil {
			workspaceRootDir = normalizeWorkspaceRootDir(tempDir)
		} else if wd, err := os.Getwd(); err == nil {
			workspaceRootDir = normalizeWorkspaceRootDir(wd)
		}
		if strings.TrimSpace(workspaceRootDir) == "" {
			return nil, fmt.Errorf("workspace root dir is empty")
		}
		localRuntime, err := newLocalWorkspaceRuntime("", workspaceRootDir, "")
		if err != nil {
			return nil, err
		}
		workspaceRuntime = localRuntime
	}
	a.workspaceRuntime = workspaceRuntime
	a.workspaceSessionID = strings.TrimSpace(workspaceRuntime.SessionID())
	a.workspaceRootDir = normalizeWorkspaceRootDir(workspaceRuntime.RootDir())
	a.workspaceNamespace = builtin_tools.NormalizeWorkspaceNamespace(workspaceRuntime.Namespace())
	if sharedDir := workspaceRuntime.SharedDir(); sharedDir != "" {
		_ = os.MkdirAll(sharedDir, 0o755)
	}
	ctx = structuredoutput.WithConfig(ctx, a.resolveStructuredOutputConfig(cfg))

	runClient, resolveErr := a.resolveAIClient(ctx)
	if resolveErr != nil {
		return nil, fmt.Errorf("resolve ai client failed: %w", resolveErr)
	}
	a.setCurrentRunClient(runClient)

	runBudget := resolveContextBudget(runClient)
	if compressor, ok := a.cfg.HistoryCompressor.(*AIHistoryCompressor); ok && compressor != nil {
		triggerTokens := runBudget.TriggerTokens
		if triggerTokens <= 0 {
			triggerTokens = runBudget.UsableInputTokens
		}
		a.cfg.HistoryCompressor = NewAIHistoryCompressorWithTokenBudget(
			triggerTokens,
			compressor.keepLastRounds,
		)
		if recreated, ok := a.cfg.HistoryCompressor.(*AIHistoryCompressor); ok && recreated != nil {
			recreated.promptManager = a.promptManager
		}
	}

	maxIterations := a.cfg.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 1
	}

	// A reused agent should keep accumulated history, but each top-level Execute
	// starts a fresh runtime state machine for the current turn.
	a.currentRunID = strings.TrimSpace(cfg.runID)
	if a.currentRunID == "" {
		a.currentRunID = generateAgentRunID()
	}
	a.currentResultSource = normalizeResultSource(cfg.resultSource)
	a.currentPublishContract = strings.TrimSpace(cfg.publishContract)
	a.currentFinalAnswerPublish = normalizeFinalAnswerPublishConfig(cfg.finalAnswerPublish)

	// Resume decision: explicit caller intent + checkpoint data, no input text parsing.
	probe, _ := probeDurableResume(a.workspaceRootDir, a.workspaceNamespace)
	resume := cfg.resumeExecutionIntent && probe.HasCheckpoint && !cfg.forceColdStart

	if a.state != nil {
		if resume {
			rehydrated := rehydrateFromProbe(probe)
			_ = a.state.Replace(rehydrated)
		} else {
			a.state.Reset()
		}
		_ = a.state.AppendInputTimeline(input)
	}
	a.bootstrapWorkspaceState(cfg.initialState)
	a.frozenLineageByStep = nil
	a.resetRunMemory(ctx, extraText, runClient)

	userMsg := ai.NewUserMsgInfo(input)
	a.history = append(a.history, userMsg)
	a.notifyHistoryAppend(userMsg)

	// Terminal short-circuit: only when explicitly resumeOnly and the checkpoint has a deliverable final.
	// Use probe.Snapshot (which preserves the original completed status) instead of the
	// rehydrated state snapshot — rehydrateFromProbe resets status to Running for the
	// general resume path, but the return_final shortcut must honour the original terminal
	// status so that finalizeResult returns success.
	if resume && cfg.resumeOnly && probe.DeliverableFinal && a.state != nil {
		probeSnapshot := probe.Snapshot
		if probeSnapshot.FinalAnswer != nil {
			finalText := strings.TrimSpace(probeSnapshot.FinalAnswer.Content)
			if finalText == "" {
				finalText = strings.TrimSpace(probeSnapshot.FinalAnswer.PublishedOutput)
			}
			if finalText != "" {
				historyText := truncateForHistory(finalText, strings.TrimSpace(probeSnapshot.FinalAnswer.Source))
				msg := ai.NewAIMsgInfo(historyText)
				a.history = append(a.history, msg)
				a.notifyHistoryAppend(msg)
			}
		}
		return a.finalizeResult(probeSnapshot), nil
	}

	runResult, err := a.runSchedulerLoop(ctx, runClient, extraText, taskContext, maxIterations)
	if err != nil {
		return nil, err
	}
	return runResult, nil
}

func (a *Agent) bootstrapWorkspaceState(initial *builtin_tools.StateSnapshot) {
	if a == nil || a.state == nil {
		return
	}

	merged := a.state.Snapshot()
	changed := false

	if len(merged.ActiveSkillNames) == 0 || len(merged.ActiveMCPServers) == 0 {
		if state, err := a.loadWorkspaceBootstrapState(); err == nil && state != nil {
			if len(merged.ActiveSkillNames) == 0 && len(state.ActiveSkillNames) > 0 {
				merged.ActiveSkillNames = builtin_tools.CloneStringSlice(state.ActiveSkillNames)
				changed = true
			}
			if len(merged.ActiveMCPServers) == 0 && len(state.ActiveMCPServers) > 0 {
				merged.ActiveMCPServers = builtin_tools.CloneStringSlice(state.ActiveMCPServers)
				changed = true
			}
		}
	}

	if initial != nil {
		if len(initial.ActiveSkillNames) > 0 && !equalStringSets(merged.ActiveSkillNames, initial.ActiveSkillNames) {
			merged.ActiveSkillNames = builtin_tools.CloneStringSlice(initial.ActiveSkillNames)
			changed = true
		}
		if len(initial.ActiveMCPServers) > 0 && !equalStringSets(merged.ActiveMCPServers, initial.ActiveMCPServers) {
			merged.ActiveMCPServers = builtin_tools.CloneStringSlice(initial.ActiveMCPServers)
			changed = true
		}
	}

	if changed {
		_ = a.state.Replace(merged)
		_ = a.persistBootstrapWorkspaceState(merged)
	}
}

func (a *Agent) loadWorkspaceBootstrapState() (*builtin_tools.WorkspaceState, error) {
	if a == nil || a.workspaceRuntime == nil {
		return nil, nil
	}
	return a.workspaceRuntime.LoadWorkspaceState()
}

func (a *Agent) persistBootstrapWorkspaceState(snapshot builtin_tools.StateSnapshot) error {
	if a == nil || a.workspaceRuntime == nil {
		return nil
	}
	state, err := a.workspaceRuntime.LoadWorkspaceState()
	if err != nil || state == nil {
		return err
	}
	state.SessionID = firstNonEmpty(strings.TrimSpace(state.SessionID), strings.TrimSpace(a.workspaceSessionID))
	state.ActiveSkillNames = builtin_tools.CloneStringSlice(snapshot.ActiveSkillNames)
	state.ActiveMCPServers = builtin_tools.CloneStringSlice(snapshot.ActiveMCPServers)
	state.UpdatedAt = time.Now()
	return a.workspaceRuntime.SaveWorkspaceState(state)
}

func equalStringSets(aVals, bVals []string) bool {
	left := normalizeSkillNames(aVals)
	right := normalizeSkillNames(bVals)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (a *Agent) finalizeResult(snapshot builtin_tools.StateSnapshot) *builtin_tools.RunResult {
	// Canceled is unconditionally a failure — step results are irrelevant.
	if snapshot.Status == builtin_tools.TaskStatusCanceled {
		msg := ""
		if snapshot.FinalAnswer != nil {
			msg = strings.TrimSpace(snapshot.FinalAnswer.Content)
		}
		if msg == "" {
			msg = strings.TrimSpace(snapshot.StatusSummary)
		}
		if msg == "" {
			msg = "canceled"
		}
		return &builtin_tools.RunResult{Success: false, Error: msg}
	}

	if normalizeResultSource(a.currentResultSource) == ResultSourceLatestStepResult {
		// Runtime-forced failures (max iterations, phase errors) set snapshot.Error;
		// model-assessed failures do not. Only short-circuit on step result when the
		// runtime has NOT forced a failure.
		runtimeForcedFail := snapshot.Status == builtin_tools.TaskStatusFailed &&
			strings.TrimSpace(snapshot.Error) != ""
		if !runtimeForcedFail && snapshot.ExternalInterrupt == nil {
			if result, ok, degraded := latestNonEmptyStepResultWithPlan(snapshot.StepOutcomes, snapshot.Plan, a.currentPublishContract); ok {
				if degraded {
					a.emitRuntimeLog("warning", "publish contract fallback: no plan step matched contract, using latest step result", snapshot, map[string]any{
						"event":            "publish_contract_fallback",
						"publish_contract": a.currentPublishContract,
					})
				}
				return &builtin_tools.RunResult{Success: true, Result: result}
			}
		}
		if snapshot.Status == builtin_tools.TaskStatusCompleted && snapshot.ExternalInterrupt == nil {
			a.emitRuntimeLog("warning", "result_source=latest_step_result: no step result produced, falling through to final_answer", snapshot, map[string]any{
				"event":            "step_result_missing_fallback",
				"publish_contract": a.currentPublishContract,
			})
		}
	}

	switch snapshot.Status {
	case builtin_tools.TaskStatusCompleted:
		if a.requiresPublishedOutput() {
			if snapshot.FinalAnswer != nil {
				published := strings.TrimSpace(snapshot.FinalAnswer.PublishedOutput)
				if published != "" {
					return &builtin_tools.RunResult{Success: true, Result: published}
				}
			}
			return &builtin_tools.RunResult{
				Success: false,
				Error:   "final_answer.published_output is required but missing",
			}
		}
		result := ""
		if snapshot.FinalAnswer != nil {
			result = strings.TrimSpace(snapshot.FinalAnswer.Content)
		}
		return &builtin_tools.RunResult{Success: true, Result: result}
	case builtin_tools.TaskStatusCanceled:
		msg := ""
		if snapshot.FinalAnswer != nil {
			msg = strings.TrimSpace(snapshot.FinalAnswer.Content)
		}
		if msg == "" {
			msg = strings.TrimSpace(snapshot.StatusSummary)
		}
		if msg == "" {
			msg = "canceled"
		}
		return &builtin_tools.RunResult{Success: false, Error: msg}
	default:
		errText := ""
		if snapshot.FinalAnswer != nil {
			errText = strings.TrimSpace(snapshot.FinalAnswer.Content)
		}
		if errText == "" {
			errText = strings.TrimSpace(snapshot.Error)
		}
		if errText == "" {
			errText = strings.TrimSpace(snapshot.StatusSummary)
		}
		if errText == "" {
			errText = "failed"
		}
		return &builtin_tools.RunResult{Success: false, Error: errText}
	}
}

func (a *Agent) resetRunMemory(ctx context.Context, extraText string, runClient ai.ChatClient) {
	if a == nil {
		return
	}
	var memOpts []memory.TimelineOption
	if a.cfg != nil {
		if a.cfg.MemoryTriggerBytes >= 0 {
			memOpts = append(memOpts, memory.WithTriggerBytes(a.cfg.MemoryTriggerBytes))
		} else if runClient != nil {
			budget := resolveContextBudget(runClient)
			triggerTokens := budget.TriggerTokens
			if triggerTokens <= 0 {
				triggerTokens = budget.UsableInputTokens
			}
			if triggerTokens > 0 {
				memOpts = append(memOpts, memory.WithTriggerBytes(triggerTokens*defaultCharsPerToken))
			}
		}
		if a.cfg.MemoryKeepLastItems >= 0 {
			memOpts = append(memOpts, memory.WithKeepLastItems(a.cfg.MemoryKeepLastItems))
		}
		if runClient == nil {
			runClient = a.cfg.AIClient
		}
	}
	a.memory = memory.NewTimeLine(
		ctx,
		runClient,
		func() string { return strings.TrimSpace(extraText) },
		memOpts...,
	)
	a.handoff = &handoffState{
		differ: memory.NewTimelineMemoryDiffer(a.memory),
	}
}

func (a *Agent) BuildFunctionTools(phase builtin_tools.AgentPhase) ([]*ai.FunctionTool, map[string]struct{}) {
	if a == nil || a.tools == nil || a.tools.Len() == 0 {
		return nil, nil
	}
	tools := make([]*ai.FunctionTool, 0, a.tools.Len())
	allowed := make(map[string]struct{}, a.tools.Len())
	a.tools.ForEach(func(_ string, tool Tool) {
		if tool == nil {
			return
		}
		name := strings.TrimSpace(tool.Name())
		if !a.toolEnabledInPhase(name, phase) {
			return
		}
		allowed[name] = struct{}{}
		tools = append(tools, &ai.FunctionTool{
			Type: "function",
			Function: &ai.FunctionDetail{
				Name:        name,
				Description: tool.Description(),
				Parameters:  relaxToolParametersSchema(tool.Parameters()),
			},
		})
	})
	return tools, allowed
}

func (a *Agent) toolEnabledInPhase(toolName string, phase builtin_tools.AgentPhase) bool {
	switch phase {
	case builtin_tools.AgentPhaseStep:
		switch toolName {
		case builtin_tools.TaskStatusQueryToolName:
			return false
		default:
			return true
		}
	default:
		return false
	}
}

type contextAwareClientFactory interface {
	CreateClientContext(ctx context.Context, modelID string) (ai.ChatClient, error)
}

func (a *Agent) resolveAIClient(ctx context.Context) (ai.ChatClient, error) {
	if a == nil || a.cfg == nil {
		return nil, fmt.Errorf("agent not initialized")
	}

	factory := a.cfg.AIClientFactory
	if factory == nil {
		if a.cfg.AIClient == nil {
			return nil, fmt.Errorf("ai client is nil")
		}
		return a.cfg.AIClient, nil
	}

	modelID := strings.TrimSpace(a.cfg.ModelID)
	if modelID == "" {
		if client := factory.DefaultClient(); client != nil {
			return client, nil
		}
		if a.cfg.AIClient != nil {
			return a.cfg.AIClient, nil
		}
		return nil, fmt.Errorf("default ai client is nil")
	}

	if contextualFactory, ok := factory.(contextAwareClientFactory); ok {
		client, err := contextualFactory.CreateClientContext(ctx, modelID)
		if err != nil {
			return nil, err
		}
		if client != nil {
			return client, nil
		}
		return nil, fmt.Errorf("client factory returned nil client for model_id=%s", modelID)
	}

	client := factory.CreateClient(modelID)
	if client != nil {
		return client, nil
	}
	return nil, fmt.Errorf("client factory returned nil client for model_id=%s", modelID)
}

type aiCallProxyResult struct {
	ToolCalls     []*ai.FunctionTool
	AssistantText string
	FinishReason  string
	Compaction    *HistoryCompactionResult
}

func (a *Agent) AICallProxy(ctx context.Context, iter int, runClient ai.ChatClient, prompt string, tools ...*ai.FunctionTool) (*aiCallProxyResult, error) {
	if a == nil || a.cfg == nil {
		return nil, fmt.Errorf("agent not initialized")
	}
	if runClient == nil {
		runClient = a.cfg.AIClient
	}
	if runClient == nil {
		return nil, fmt.Errorf("ai client is nil")
	}

	systemMsg := ai.NewSystemMsgInfo(prompt)
	msgs := make([]*ai.MsgInfo, 0, 1+len(a.history)+len(a.stepHistory))
	msgs = append(msgs, systemMsg)
	msgs = append(msgs, a.history...)
	msgs = append(msgs, a.stepHistory...)
	requestOptions := a.buildPromptRequestOptions(promptFamilyThinkAct, prompt, true, tools...)

	if streamingClient, ok := runClient.(ai.StreamingChatClient); ok {
		return a.AICallProxyStream(ctx, iter, runClient, streamingClient, msgs, requestOptions, tools...)
	}

	var choices []*ai.ChatChoices
	var err error

	choices, err = ai.ChatExWithOptions(ctx, runClient, msgs, requestOptions, tools...)

	if err != nil {
		return nil, err
	}
	if len(choices) == 0 {
		return &aiCallProxyResult{}, nil
	}

	choice := choices[0]
	if choice == nil || choice.Message == nil {
		return &aiCallProxyResult{}, nil
	}

	return a.finalizeAIChoice(ctx, iter, runClient, choice, requestOptions, true)
}

func (a *Agent) AICallProxyStream(ctx context.Context, iter int, runClient ai.ChatClient, streamingClient ai.StreamingChatClient, msgs []*ai.MsgInfo, requestOptions *ai.RequestOptions, tools ...*ai.FunctionTool) (*aiCallProxyResult, error) {
	if streamingClient == nil {
		return &aiCallProxyResult{}, nil
	}

	var (
		contentBuilder   strings.Builder
		reasoningBuilder strings.Builder
		toolCalls        []*ai.FunctionTool
		finishReason     string
	)

	err := ai.ChatStreamWithOptions(ctx, streamingClient, msgs, requestOptions, func(delta *ai.StreamDelta, done bool) error {
		if done || delta == nil {
			return nil
		}
		if delta.ReasoningContent != "" {
			reasoningBuilder.WriteString(delta.ReasoningContent)
			a.emitter.EmitThink(iter, "", delta.ReasoningContent, reasoningBuilder.String(), nil, delta.FinishReason)
		}
		if delta.Content != "" {
			contentBuilder.WriteString(delta.Content)
			a.emitter.EmitStream(iter, delta.Content)
		}
		if len(delta.ToolCalls) > 0 {
			toolCalls = mergeFunctionToolDeltas(toolCalls, delta.ToolCalls)
		}
		if delta.FinishReason != "" {
			finishReason = delta.FinishReason
		}
		return nil
	}, tools...)
	if err != nil {
		return nil, err
	}

	msg := ai.NewAIMsgInfo(contentBuilder.String())
	msg.ReasoningOutput = reasoningBuilder.String()
	msg.ToolCalls = toolCalls
	if usageProvider, ok := runClient.(ai.TokenUsageProvider); ok {
		msg.Usage = usageProvider.LastTokenUsage()
	}

	return a.finalizeAIChoice(ctx, iter, runClient, &ai.ChatChoices{
		Index:        0,
		Message:      msg,
		Usage:        msg.Usage,
		FinishReason: finishReason,
	}, requestOptions, false)
}

func (a *Agent) finalizeAIChoice(ctx context.Context, iter int, runClient ai.ChatClient, choice *ai.ChatChoices, requestOptions *ai.RequestOptions, emitSummaryThink bool) (*aiCallProxyResult, error) {
	if choice == nil || choice.Message == nil {
		return &aiCallProxyResult{}, nil
	}

	msg := choice.Message
	if msg != nil && msg.Usage == nil && choice.Usage != nil {
		msg.Usage = ai.NormalizeTokenUsagePtr(choice.Usage)
	}
	content := ""
	if msg.Content != nil {
		if s, ok := msg.Content.(string); ok {
			content = s
		}
	}
	if emitSummaryThink && msg.ReasoningOutput != "" {
		a.emitter.EmitThink(iter, content, msg.ReasoningOutput, msg.ReasoningOutput, msg.ToolCalls, choice.FinishReason)
	}

	stepUsage := utils.BuildUsageSummary(runClient, msg.Usage)
	stepPayload := map[string]any{
		"content":           content,
		"reasoning_content": msg.ReasoningOutput,
		"finish_reason":     choice.FinishReason,
	}
	if requestOptions != nil {
		stepPayload["prompt_family"] = requestOptions.PromptFamily
		stepPayload["cache_enabled"] = requestOptions.PromptCacheEnabled
		if requestOptions.PromptCacheKeyHash != "" {
			stepPayload["cache_key_hash"] = requestOptions.PromptCacheKeyHash
		}
	}
	if len(stepUsage.Values) > 0 {
		stepPayload["usage"] = stepUsage.Values
		stepPayload["cost_usd"] = stepUsage.CostUSD
		if msg.Usage != nil {
			stepPayload["cache_hit"] = msg.Usage.CacheReadTokens > 0
		}
	}
	if len(msg.ToolCalls) > 0 {
		stepPayload["tool_calls"] = msg.ToolCalls
	}
	a.emitter.EmitStepFinish(iter, stepPayload)

	// Step phase: keep tool calling transcript within the step window only.
	sanitizeToolCallArguments(msg)
	a.stepHistory = append(a.stepHistory, msg)

	var (
		compactionResult *HistoryCompactionResult
		err              error
	)
	candidateHistory := NormalizeHistoryMsgInfos(a.history)
	if a.cfg.HistoryCompressor != nil {
		// Only compact the long-term skeleton history. Do NOT compact step-local tool transcript.
		compactionResult, err = a.cfg.HistoryCompressor.Compress(ctx, runClient, a.cfg.Instruction, candidateHistory)
		if err != nil {
			return nil, err
		}
	}
	if compactionResult == nil {
		compactionResult = &HistoryCompactionResult{
			History:     NormalizeHistoryMsgInfos(candidateHistory),
			State:       CompactionStateNormal,
			CanContinue: true,
		}
	}

	beforeHistory := candidateHistory
	afterHistory := NormalizeHistoryMsgInfos(compactionResult.History)
	if len(afterHistory) == 0 {
		afterHistory = NormalizeHistoryMsgInfos(candidateHistory)
	}
	if HistoryCompacted(beforeHistory, afterHistory) {
		a.history = afterHistory
		a.notifyHistoryReplace()
		if a.emitter != nil {
			a.emitter.EmitHistoryCompacted(map[string]any{
				"before_messages": len(beforeHistory),
				"after_messages":  len(afterHistory),
				"before_tokens":   estimateHistoryTokens(beforeHistory),
				"after_tokens":    estimateHistoryTokens(afterHistory),
				"history":         afterHistory,
				"state":           string(compactionResult.State),
				"still_overflow":  compactionResult.StillOverflow,
				"can_continue":    compactionResult.CanContinue,
				"attempt_count":   compactionResult.AttemptCount,
				"terminal_reason": compactionResult.TerminalReason,
			})
		}
	} else {
		// No change to long-term history.
	}

	// 将 assistant 输出记入 TimelineMemory，供 TimelineMemoryDiffer 生成 step window diff。
	a.AddMemoryAssistantOutput(content)

	return &aiCallProxyResult{
		ToolCalls:     msg.ToolCalls,
		AssistantText: content,
		FinishReason:  choice.FinishReason,
		Compaction:    compactionResult,
	}, nil
}

func mergeFunctionToolDeltas(existing []*ai.FunctionTool, incoming []*ai.FunctionTool) []*ai.FunctionTool {
	for _, tc := range incoming {
		if tc == nil {
			continue
		}
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}

		for len(existing) <= idx {
			existing = append(existing, &ai.FunctionTool{
				Function: &ai.FunctionDetail{},
			})
		}

		if tc.Id != "" {
			existing[idx].Id = tc.Id
		}
		if tc.Type != "" {
			existing[idx].Type = tc.Type
		}
		existing[idx].Index = tc.Index

		if tc.Function != nil {
			if existing[idx].Function == nil {
				existing[idx].Function = &ai.FunctionDetail{}
			}
			if tc.Function.Name != "" {
				existing[idx].Function.Name += tc.Function.Name
			}
			if args, ok := tc.Function.Arguments.(string); ok && args != "" {
				if existingArgs, ok := existing[idx].Function.Arguments.(string); ok {
					existing[idx].Function.Arguments = existingArgs + args
				} else {
					existing[idx].Function.Arguments = args
				}
			}
		}
	}
	return existing
}

func (a *Agent) AICallProxyWriteToolResult(callID, toolName, description string, args map[string]any, result, errText string, isAgent bool) {
	if a == nil {
		return
	}

	content := result
	if errText != "" {
		content = fmt.Sprintf("Error: %s", errText)
	}

	toolResultMsg := ai.NewToolCallResultMsgInfo(content, callID)
	// Step phase: tool results are step-local transcript and should not be persisted to long-term ai.history.
	a.stepHistory = append(a.stepHistory, toolResultMsg)

	if a.memory != nil {
		_ = a.memory.AddItem(
			generateRandomString(8),
			memory.NewToolCallItem(callID, toolName, description, args, result, errText),
		)
		a.memory.TryCompressAsync()
	}
}

// InjectAgentToolExtra 注入 Agent 工具额外信息
func (a *Agent) InjectAgentToolExtra(ctx context.Context, toolName string, args map[string]any) {
	if a == nil || args == nil {
		return
	}
	if handoffExtra := a.buildAgentHandoffExtra(ctx, toolName); handoffExtra != "" {
		args["__handoff_context__"] = handoffExtra
	}
}

// WithNextAgentCallInfo 注入下一个 Agent 调用信息到 context
func WithNextAgentCallInfo(ctx context.Context, parentAgentID, parentAgentName string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, ctxKeyParentAgentID, parentAgentID)
	ctx = context.WithValue(ctx, ctxKeyParentAgentName, parentAgentName)
	return ctx
}

type ctxKey string

const (
	ctxKeyParentAgentID   ctxKey = "parent_agent_id"
	ctxKeyParentAgentName ctxKey = "parent_agent_name"
)

// GetParentAgentInfo 从 context 获取父 Agent 信息
func GetParentAgentInfo(ctx context.Context) (agentID, agentName string) {
	if ctx == nil {
		return "", ""
	}
	if v := ctx.Value(ctxKeyParentAgentID); v != nil {
		if s, ok := v.(string); ok {
			agentID = s
		}
	}
	if v := ctx.Value(ctxKeyParentAgentName); v != nil {
		if s, ok := v.(string); ok {
			agentName = s
		}
	}
	return
}

func HistoryCompacted(before []*ai.MsgInfo, after []*ai.MsgInfo) bool {
	if len(after) == 0 {
		return false
	}
	if len(before) != len(after) {
		return true
	}
	if estimateHistoryTokens(before) != estimateHistoryTokens(after) {
		return true
	}
	for idx := range after {
		if !historyMsgComparable(before[idx], after[idx]) {
			return true
		}
	}
	return false
}

func shouldStopAfterCompaction(result *HistoryCompactionResult, snapshot builtin_tools.StateSnapshot) bool {
	if result == nil || result.CanContinue {
		return false
	}
	return !snapshot.Terminal()
}

func buildHistoryCompactionStopMessage(result *HistoryCompactionResult) string {
	if result == nil {
		return "history compaction stopped current run"
	}
	switch result.TerminalReason {
	case CompactionTerminalTimeout:
		return "history compaction timed out; current run stops after this step"
	case CompactionTerminalInterrupted:
		return "history compaction was interrupted; current run stops after this step"
	case CompactionTerminalEmptySummary:
		return "history compaction produced empty summary; current run stops after this step"
	case CompactionTerminalNoProgress:
		return "history compaction made no effective progress; current run stops after this step"
	case CompactionTerminalMaxAttempts:
		return "history compaction exceeded max attempts; current run stops after this step"
	case CompactionTerminalOverflow:
		return "history remains overflow after compaction; current run stops after this step"
	default:
		return "history compaction stopped current run"
	}
}

func historyMsgComparable(left *ai.MsgInfo, right *ai.MsgInfo) bool {
	if left == nil || right == nil {
		return left == right
	}
	if strings.TrimSpace(left.Role) != strings.TrimSpace(right.Role) {
		return false
	}
	if strings.TrimSpace(left.Type) != strings.TrimSpace(right.Type) {
		return false
	}
	if strings.TrimSpace(left.ToolCallID) != strings.TrimSpace(right.ToolCallID) {
		return false
	}
	if strings.TrimSpace(left.ReasoningOutput) != strings.TrimSpace(right.ReasoningOutput) {
		return false
	}
	if FormatMsgContent(left.Content) != FormatMsgContent(right.Content) {
		return false
	}
	return true
}

func sanitizeToolCallArguments(msg *ai.MsgInfo) {
	if msg == nil {
		return
	}
	for _, tc := range msg.ToolCalls {
		if tc == nil || tc.Function == nil {
			continue
		}
		args, ok := tc.Function.Arguments.(string)
		if !ok {
			continue
		}
		args = strings.TrimSpace(args)
		if args == "" || !json.Valid([]byte(args)) {
			tc.Function.Arguments = "{}"
		}
	}
}
