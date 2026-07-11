package react

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/workspacefs"
)

const defaultMaxToolConcurrency = 5

// ConcurrencySafeTool marks a Tool as safe for concurrent execution with other safe tools.
// Read-only tools that don't modify shared state should implement this interface.
type ConcurrencySafeTool interface {
	Tool
	ConcurrencySafe() bool
}

var defaultConcurrencySafeTools = map[string]bool{
	builtin_tools.ListFilesToolName:       true,
	builtin_tools.ReadFileToolName:        true,
	builtin_tools.RgToolName:              true,
	builtin_tools.TaskStatusQueryToolName: true,
}

func isConcurrencySafe(t Tool) bool {
	if t == nil {
		return false
	}
	if cst, ok := t.(ConcurrencySafeTool); ok {
		return cst.ConcurrencySafe()
	}
	return defaultConcurrencySafeTools[t.Name()]
}

// neverConcurrentTools lists tools that must always run sequentially regardless
// of ConcurrencySafe declarations. These tools mutate agent state, trigger
// durable persistence, or have side effects that the concurrent path does not handle.
//
// **SkillToolName + EjectSkillToolName 必须成对出现**——两者都改 ActiveSkillNames
// （主路径走 a.state，peer 路径走 runCtx.LocalActiveSkillNames），对称纳入 sequential
// 名单防御未来谁给 eject 加 `ConcurrencySafe() bool { return true }` 触发 race。
var neverConcurrentTools = map[string]bool{
	builtin_tools.UpdateCurrentStepToolName: true,
	builtin_tools.HumanConfirmToolName:      true,
	builtin_tools.SubAgentToolName:          true,
	builtin_tools.SkillToolName:             true,
	builtin_tools.EjectSkillToolName:        true,
	builtin_tools.BashToolName:              true,
}

// partitionToolCalls splits tool calls into concurrent-safe and sequential groups.
// Both groups preserve their relative order from the original slice.
func partitionToolCalls(a *Agent, toolCalls []*ai.FunctionTool) (safe, unsafe []*ai.FunctionTool) {
	for _, tc := range toolCalls {
		if tc == nil || tc.Function == nil {
			continue
		}
		toolName := strings.TrimSpace(tc.Function.Name)
		if toolName == "" {
			unsafe = append(unsafe, tc)
			continue
		}
		if neverConcurrentTools[toolName] {
			unsafe = append(unsafe, tc)
			continue
		}
		tool, exists := a.GetTool(toolName)
		if !exists || tool == nil {
			unsafe = append(unsafe, tc)
			continue
		}
		if isConcurrencySafe(tool) {
			safe = append(safe, tc)
		} else {
			unsafe = append(unsafe, tc)
		}
	}
	return
}

// dispatchToolCalls executes tool calls with concurrency for safe tools.
// Returns the number of successfully dispatched tool calls.
//
// runCtx 用于 inline step 桶路由——tool_result 通过 AICallProxyWriteToolResult 走 runCtx 桶。
// 主路径调用方传 nil（行为不变）；commit 8b 接桶执行时传真正的 runCtx。
func (a *Agent) dispatchToolCalls(ctx context.Context, runCtx *InlineStepCtx, iter int, toolCalls []*ai.FunctionTool, allowedTools map[string]struct{}) (int, error) {
	safe, unsafe := partitionToolCalls(a, toolCalls)

	if len(safe) < 2 {
		return a.executeToolCallsSequentially(ctx, runCtx, iter, toolCalls, allowedTools)
	}

	executed := 0

	n, err := a.executeToolCallsConcurrently(ctx, runCtx, iter, safe, allowedTools)
	executed += n
	if err != nil {
		return executed, err
	}
	if a.state.Snapshot().Terminal() {
		return executed, nil
	}

	n2, err := a.executeToolCallsSequentially(ctx, runCtx, iter, unsafe, allowedTools)
	executed += n2
	return executed, err
}

// executeToolCallsSequentially runs tool calls one by one (the original behavior).
// runCtx 同 dispatchToolCalls 注释。
func (a *Agent) executeToolCallsSequentially(ctx context.Context, runCtx *InlineStepCtx, iter int, toolCalls []*ai.FunctionTool, allowedTools map[string]struct{}) (int, error) {
	executed := 0
	for _, tc := range toolCalls {
		if ctx.Err() != nil {
			break
		}
		if tc == nil || tc.Function == nil {
			continue
		}
		if err := a.executeToolCall(ctx, runCtx, iter, tc, allowedTools); err != nil {
			return executed, err
		}
		executed++
		if a.state.Snapshot().Terminal() {
			return executed, nil
		}
	}
	return executed, nil
}

type concurrentToolSlot struct {
	tc         *ai.FunctionTool
	toolName   string
	callID     string
	argsMap    map[string]any
	tool       Tool
	isAgent    bool
	stackDepth int

	validationErr string

	// res 是 Execute 窗口经工具洋葱链的产物（goroutine 内写、wg.Wait 后读）；
	// 截断已在链内完成（默认中间件），回填段直接消费。
	res *toolExecResult
}

// executeToolCallsConcurrently runs multiple concurrent-safe tools in parallel.
// Tool results are written to stepHistory in the original call order.
// runCtx 同 dispatchToolCalls 注释——所有 AICallProxyWriteToolResult 调用都路由到桶。
func (a *Agent) executeToolCallsConcurrently(ctx context.Context, runCtx *InlineStepCtx, iter int, toolCalls []*ai.FunctionTool, allowedTools map[string]struct{}) (int, error) {
	prevSnapshot := a.state.Snapshot()

	slots := make([]*concurrentToolSlot, len(toolCalls))
	for i, tc := range toolCalls {
		slot := &concurrentToolSlot{tc: tc}
		slots[i] = slot

		slot.callID = strings.TrimSpace(tc.Id)
		slot.toolName = strings.TrimSpace(tc.Function.Name)
		if slot.toolName == "" {
			continue
		}

		if len(allowedTools) > 0 {
			if _, ok := allowedTools[slot.toolName]; !ok {
				slot.validationErr = "tool not available in current phase"
				continue
			}
		}

		argsMap, argErr := ParseToolArguments(tc.Function.Arguments)
		if argsMap == nil {
			argsMap = map[string]any{}
		}
		slot.argsMap = argsMap
		if argErr != nil {
			rawArgs := ""
			if s, ok := tc.Function.Arguments.(string); ok {
				if len(s) > 500 {
					rawArgs = s[:500] + "..."
				} else {
					rawArgs = s
				}
			}
			slot.validationErr = fmt.Sprintf(
				"tool args parse failed: %v\n\nThe arguments JSON you provided is malformed. Raw arguments (truncated):\n%s\n\nPlease retry the tool call with valid JSON arguments.",
				argErr, rawArgs,
			)
			continue
		}

		tool, exists := a.GetTool(slot.toolName)
		if !exists || tool == nil {
			slot.validationErr = "tool not found"
			continue
		}
		slot.tool = tool
		slot.isAgent = IsAgentToolForCall(ctx, tool, slot.argsMap)
		if parentRuntime, ok := builtin_tools.GetToolRuntime(ctx); ok {
			slot.stackDepth = parentRuntime.StackDepth + 1
		}
	}

	// Emit ToolStart for all validated tools, then execute concurrently.
	var wg sync.WaitGroup
	sem := make(chan struct{}, defaultMaxToolConcurrency)

	for _, slot := range slots {
		if slot.toolName == "" || slot.validationErr != "" || slot.tool == nil {
			continue
		}

		a.emitter.EmitToolStart(iter, builtin_tools.ToolCall{
			ID:         slot.callID,
			Name:       slot.toolName,
			IsAgent:    slot.isAgent,
			StackDepth: slot.stackDepth,
			Arguments:  builtin_tools.CloneAnyMap(slot.argsMap),
		}, effectiveStepID(runCtx, prevSnapshot))

		wg.Add(1)
		sem <- struct{}{}
		go func(s *concurrentToolSlot) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					s.res = &toolExecResult{ErrText: fmt.Sprintf("tool panicked: %v", r)}
				}
			}()

			if ctx.Err() != nil {
				s.res = &toolExecResult{ErrText: fmt.Sprintf("context cancelled: %v", ctx.Err())}
				return
			}

			sharedDir := ""
			if a.workspaceRuntime != nil {
				sharedDir = a.workspaceRuntime.SharedDir()
			}
			callCtx := builtin_tools.WithToolRuntime(ctx, builtin_tools.ToolRuntimeInfo{
				Emitter:            a.emitter,
				RunID:              strings.TrimSpace(a.currentRunID),
				CallID:             s.callID,
				ToolName:           s.toolName,
				Iteration:          iter,
				IsAgent:            s.isAgent,
				StackDepth:         s.stackDepth,
				WorkspaceSessionID: strings.TrimSpace(a.workspaceSessionID),
				WorkspaceRootDir:   strings.TrimSpace(a.workspaceRootDir),
				WorkspaceNamespace: strings.TrimSpace(a.workspaceNamespace),
				WorkspaceSharedDir: sharedDir,
				SourceWorkingDir:   strings.TrimSpace(a.runtimeRepoContext.SourceWorkingDir),
				RepoRootDir:        strings.TrimSpace(a.runtimeRepoContext.RepoRootDir),
				IsGitRepo:          a.runtimeRepoContext.IsGitRepo,
				GitBranch:          strings.TrimSpace(a.runtimeRepoContext.Branch),
				GitRepoURL:         strings.TrimSpace(a.runtimeRepoContext.RemoteURL),
				IsGitWorktree:      a.runtimeRepoContext.IsWorktree,
				CurrentStepID:      effectiveStepID(runCtx, prevSnapshot),
			})

			// Execute 窗口统一穿工具洋葱链（与顺序路径同一条 a.toolExecChain）：
			// 超时/Execute/超时包装在 base，截断+截断日志与耗时为默认中间件——
			// 截断因此从 wg.Wait 后串行段移入 goroutine 内并行（落盘 IO 并行化，
			// tool_output_truncated 日志无顺序承诺，良性行为变化）。
			res, chainErr := a.toolExecChain(callCtx, &toolExecCall{
				CallID:       s.callID,
				ToolName:     s.toolName,
				Tool:         s.tool,
				Args:         s.argsMap,
				IsAgent:      s.isAgent,
				StackDepth:   s.stackDepth,
				Iter:         iter,
				StepID:       effectiveStepID(runCtx, prevSnapshot),
				Phase:        prevSnapshot.Phase,
				PrevSnapshot: prevSnapshot,
			})
			if chainErr != nil {
				// 链自身故障：并发批次无法像顺序路径那样中止回合，按 error
				// tool_result 回填给 LLM（工具业务错误同通道）。
				s.res = &toolExecResult{ErrText: fmt.Sprintf("tool exec chain failed: %v", chainErr)}
				return
			}
			s.res = res
		}(slot)
	}

	wg.Wait()

	// Write results to stepHistory in original order (single-writer invariant).
	executed := 0
	var wsl workspacefs.Layout
	if a.workspaceRuntime != nil {
		wsl = a.wsLayout()
	}

	for _, slot := range slots {
		if slot.toolName == "" {
			continue
		}

		if slot.validationErr != "" {
			a.AICallProxyWriteToolResult(runCtx, slot.callID, slot.toolName, "", slot.argsMap, "", slot.validationErr, false)
			executed++
			continue
		}

		if slot.tool == nil {
			continue
		}

		if slot.res == nil {
			// 防御：goroutine 被跳过（不应发生）。按空结果回填，避免悬空 tool_call_id。
			slot.res = &toolExecResult{}
		}
		out := slot.res.Out
		errText := slot.res.ErrText

		displayOut := out
		if strings.TrimSpace(displayOut) == "" && strings.TrimSpace(errText) != "" {
			displayOut = fmt.Sprintf("Error: %s", errText)
		}
		render := buildToolResultRender(slot.toolName, out)
		a.handleSkillToolStateSync(slot.toolName, slot.argsMap, out, errText, runCtx)
		a.AICallProxyWriteToolResult(runCtx, slot.callID, slot.toolName, slot.tool.Description(), slot.argsMap, render.Content, errText, slot.isAgent)

		if stepID := effectiveStepID(runCtx, prevSnapshot); wsl.SharedDir() != "" && stepID != "" {
			event := newToolCallTimelineEvent(slot.callID, slot.toolName, slot.argsMap, out, errText, slot.res.OutFullPath, slot.res.Duration)
			if len(render.Media) > 0 {
				event.Payload = map[string]any{"media": render.Media}
			}
			_ = appendStepTimeline(a.workspaceRuntime, stepID, event)
		}

		a.emitter.EmitToolEnd(iter, builtin_tools.ToolResult{
			ID:         slot.callID,
			Name:       slot.toolName,
			IsAgent:    slot.isAgent,
			StackDepth: slot.stackDepth,
			Result:     displayOut,
			Error:      errText,
			Media:      render.Media,
		}, effectiveStepID(runCtx, prevSnapshot))

		executed++
	}

	return executed, nil
}
