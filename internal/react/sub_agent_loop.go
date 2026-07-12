package react

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/jsonextractor"
)

// SubAgentUsage 是子 Agent 单循环的用量遥测，随信封回传父 Agent。
type SubAgentUsage struct {
	ToolUses   int   `json:"tool_uses"`
	Tokens     int   `json:"tokens,omitempty"` // 经 aiCallProxyResult.Usage 逐轮累加（B4），不可用则 0
	DurationMs int64 `json:"duration_ms"`
}

// RunSubAgentLoop 是子 Agent 的单循环内核：一个 system prompt + 委派任务 + 工具调用循环，
// 由子 Agent 显式调用 submit_result 收尾（U3），或在既无工具调用又有文本时兜底当结果（B3）。
//
// 用户决策：
//   - U1 类型只差描述——工具/模型/上限统一，模型继承父（resolveAIClient 取父 client）。
//   - U2 无 MaxTurns / 无 Timeout——外层安全边界 = parent ctx（cancel / deadline）。
//   - U3 显式终止工具 submit_result——调用即收尾，不靠「无工具调用=自然收尾」。
//   - U4 无 AgentPhaseSubAgent——buildSubAgentFunctionTools 全量暴露子 a.tools（构造期已裁），
//     submit_result 仿 submit_final_answer 每轮 append schema + 循环拦截，不进 childDef、不 dispatch。
//
// B8：taskContext 由 buildChild 透传，拼进首条 user 消息（不再走 TaskContextEntry）。
//
// opts 携带子工作区运行时等环境（WithWorkspaceRuntime/WithParentWorkspace/WithSourceWorkingDir）；
// 环境装配走 Execute 同一段 prepareRunEnvironment（D3：workspace 解析 + client 绑定 + budget），
// 但**有意不建 v2Store、不做 state.Reset / resume**——子 loop 是无状态机的单循环（见 doc 10 D3）。
func (a *Agent) RunSubAgentLoop(ctx context.Context, task, taskContext string, spec SubAgentType, opts ...ExecuteOption) (*builtin_tools.RunResult, *SubAgentUsage, error) {
	start := time.Now()
	usage := &SubAgentUsage{}
	if a == nil || a.cfg == nil {
		return failedWith(usage, start, "sub_agent not initialized"), usage, nil
	}

	cfg := &ExecuteConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	preparedCtx, runClient, prepErr := a.prepareRunEnvironment(ctx, cfg) // 继承父模型 + 绑定 client + 预算（D3）
	if prepErr != nil {
		return failedWith(usage, start, "prepare run environment failed: "+prepErr.Error()), usage, nil
	}
	ctx = preparedCtx

	parts := a.BuildSubAgentPrompt(ctx, task, taskContext, spec)

	for turn := 1; ; turn++ {
		if ctx.Err() != nil { // parent cancel / deadline
			return cancelledWith(usage, start), usage, nil
		}

		fnTools, allowed := a.buildSubAgentFunctionTools()
		fnTools = append(fnTools, buildSubmitResultFunctionTool()) // 终止工具 schema（仿 final_answer）
		allowed[builtin_tools.SubmitResultToolName] = struct{}{}

		res, aerr := a.AICallProxy(ctx, nil, turn, runClient, parts, promptFamilySubAgent, fnTools...)
		if aerr != nil {
			if isFatalStepError(aerr) || isContextOverflowError(aerr) {
				return failedWith(usage, start, "recoverable: "+aerr.Error()), usage, nil
			}
			return failedWith(usage, start, aerr.Error()), usage, nil
		}
		if res.Compaction != nil && shouldStopAfterCompaction(res.Compaction, a.state.Snapshot()) {
			return failedWith(usage, start, "history compaction stopped sub_agent run"), usage, nil
		}
		if res.Usage != nil {
			usage.Tokens += totalTokens(res.Usage) // B4：累加本轮 usage，不裸调 LastTokenUsage
		}

		// 显式终止：本轮工具调用里若含 submit_result → 解析其 result/status 收尾（不再 dispatch 其余）。
		if tc := findToolCall(res.ToolCalls, builtin_tools.SubmitResultToolName); tc != nil {
			status, result := parseSubmitResult(tc)
			usage.DurationMs = time.Since(start).Milliseconds()
			success := status != "failed" && strings.TrimSpace(result) != ""
			return &builtin_tools.RunResult{
				Success:    success,
				Result:     result,
				Error:      failReason(status, result),
				TurnStatus: turnStatusOf(success),
			}, usage, nil
		}

		if len(res.ToolCalls) == 0 { // 既没干活也没 submit_result：宽松兜底
			text := strings.TrimSpace(res.AssistantText)
			usage.DurationMs = time.Since(start).Milliseconds()
			if text == "" { // B3：空文本不当 success
				return &builtin_tools.RunResult{
					Success:    false,
					Error:      "sub_agent produced no tool call and empty output",
					TurnStatus: "failed",
				}, usage, nil
			}
			return &builtin_tools.RunResult{
				Success:    true,
				Result:     text,
				TurnStatus: "succeeded",
			}, usage, nil
		}

		executed, derr := a.dispatchToolCalls(ctx, nil, turn, res.ToolCalls, allowed)
		usage.ToolUses += executed
		if derr != nil {
			return failedWith(usage, start, "dispatch tool calls failed: "+derr.Error()), usage, nil
		}
	}
}

// BuildSubAgentPrompt 组装子 Agent 单循环的 PromptParts：block1（sub_agent_system）经
// promptManager 渲染，block2（身份 + env）复用 identityEnvBlock()。渲染失败退化到最小可用 prompt。
func (a *Agent) BuildSubAgentPrompt(ctx context.Context, task, taskContext string, spec SubAgentType) PromptParts {
	if a == nil || a.promptManager == nil {
		return PromptParts{}
	}
	snap := a.state.Snapshot()
	skillsContext := a.buildSkillsPromptContext(ctx, snap)
	mcpContext := a.buildMCPPromptContext()
	supportsVision := ModelSupportsVision(a.getCurrentRunClient())

	parts, err := a.promptManager.BuildSubAgentPrompt(SubAgentPromptInput{
		AgentProfile:    AgentProfile{AgentRole: strings.TrimSpace(a.cfg.Role), AgentBackground: strings.TrimSpace(a.cfg.Background)},
		CapabilityIndex: CapabilityIndex{SkillsContext: skillsContext, MCPContext: mcpContext},
		RunFlags:        RunFlags{SupportsVision: supportsVision},
		TypeExtra:       strings.TrimSpace(spec.SystemPromptExtra),
		Task:            strings.TrimSpace(task),
		Context:         strings.TrimSpace(taskContext),
	})
	if err == nil {
		parts.SystemAgent = a.identityEnvBlock()
		return parts
	}

	return PromptParts{
		SystemRules: firstNonEmpty(strings.TrimSpace(a.cfg.Instruction), "你是受托单任务执行的子 Agent，只解决被委派的这一件事，达成即调用提交结论的工具收尾。"),
		SystemAgent: a.identityEnvBlock(),
		User:        strings.TrimSpace(task),
	}
}

// buildSubAgentFunctionTools 是 phase-less 的工具装配：遍历子 a.tools 全量构造
// []*ai.FunctionTool + allowed map。子 a.tools 在构造期（factory.Build + resolveChildToolNames）
// 已是正确子集（域工具 + 基线，去递归/编排/状态机），无需 toolEnabledInPhase 过滤；子 loop
// runCtx=nil，无 peer 桶挡板。仿 BuildFunctionTools 内层循环（U4）。
func (a *Agent) buildSubAgentFunctionTools() ([]*ai.FunctionTool, map[string]struct{}) {
	if a == nil || a.tools == nil || a.tools.Len() == 0 {
		return nil, make(map[string]struct{}, 1)
	}
	tools := make([]*ai.FunctionTool, 0, a.tools.Len())
	allowed := make(map[string]struct{}, a.tools.Len()+1)
	a.tools.ForEach(func(_ string, tool Tool) {
		if tool == nil {
			return
		}
		name := strings.TrimSpace(tool.Name())
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

// buildSubmitResultFunctionTool 构造子 Agent 终止工具的 FunctionTool（仅 name + schema）。
// 仿 buildSubmitFinalAnswerFunctionTool：内核每轮 append，检测到其 tool_call 即读参数收尾，
// 不进 dispatch、不需 Execute、不进 childDef.ToolNames。
func buildSubmitResultFunctionTool() *ai.FunctionTool {
	tool := builtin_tools.NewSubmitResultTool()
	return &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
		},
	}
}

// findToolCall 返回工具调用列表里首个匹配 name 的调用（无则 nil）。
func findToolCall(toolCalls []*ai.FunctionTool, name string) *ai.FunctionTool {
	for _, tc := range toolCalls {
		if tc == nil || tc.Function == nil {
			continue
		}
		if strings.TrimSpace(tc.Function.Name) == name {
			return tc
		}
	}
	return nil
}

// parseSubmitResult 从 submit_result 的 tool_call 参数里解析 (status, result)。
// 参数形态兼容 string / []byte / 结构化 map（AICallProxy 归一后通常是 string）。
func parseSubmitResult(tc *ai.FunctionTool) (status, result string) {
	if tc == nil || tc.Function == nil {
		return "", ""
	}
	var raw string
	switch v := tc.Function.Arguments.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	default:
		raw = ""
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	objects := jsonextractor.ExtractObjectsOnly(raw)
	if len(objects) == 0 {
		return "", ""
	}
	var parsed struct {
		Status string `json:"status"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(objects[0]), &parsed); err != nil {
		return "", ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Status)), strings.TrimSpace(parsed.Result)
}

// failedWith / cancelledWith 构造失败 / 取消态信封（DurationMs 就地补齐）。
func failedWith(usage *SubAgentUsage, start time.Time, reason string) *builtin_tools.RunResult {
	if usage != nil {
		usage.DurationMs = time.Since(start).Milliseconds()
	}
	return &builtin_tools.RunResult{Success: false, Error: strings.TrimSpace(reason), TurnStatus: "failed"}
}

func cancelledWith(usage *SubAgentUsage, start time.Time) *builtin_tools.RunResult {
	if usage != nil {
		usage.DurationMs = time.Since(start).Milliseconds()
	}
	return &builtin_tools.RunResult{Success: false, Error: "sub_agent cancelled by parent context", TurnStatus: "cancelled"}
}

// turnStatusOf 把成功布尔映射为 RunResult.TurnStatus。
func turnStatusOf(success bool) string {
	if success {
		return "succeeded"
	}
	return "failed"
}

// failReason 在提交为失败态（或空结论）时给出 Error 文案，成功态返回空。
func failReason(status, result string) string {
	if status == "failed" {
		if strings.TrimSpace(result) == "" {
			return "sub_agent reported failure with empty result"
		}
		return "sub_agent reported failure"
	}
	if strings.TrimSpace(result) == "" {
		return "sub_agent submitted empty result"
	}
	return ""
}

// totalTokens 从一轮 usage 里取总 token（TotalTokens 优先，否则 input+output）。
func totalTokens(u *ai.TokenUsage) int {
	if u == nil {
		return 0
	}
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	sum := u.InputTokens + u.OutputTokens
	if sum < 0 {
		return 0
	}
	return sum
}
