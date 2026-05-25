package react

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

const defaultMaxToolConcurrency = 5

// ConcurrencySafeTool marks a Tool as safe for concurrent execution with other safe tools.
// Read-only tools that don't modify shared state should implement this interface.
type ConcurrencySafeTool interface {
	Tool
	ConcurrencySafe() bool
}

var defaultConcurrencySafeTools = map[string]bool{
	builtin_tools.ListFilesToolName:    true,
	builtin_tools.ReadFileToolName:     true,
	builtin_tools.RgToolName:           true,
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
var neverConcurrentTools = map[string]bool{
	builtin_tools.UpdateCurrentStepToolName: true,
	builtin_tools.HumanConfirmToolName:      true,
	builtin_tools.SubAgentToolName:          true,
	builtin_tools.SkillToolName:             true,
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
func (a *Agent) dispatchToolCalls(ctx context.Context, iter int, toolCalls []*ai.FunctionTool, allowedTools map[string]struct{}) (int, error) {
	safe, unsafe := partitionToolCalls(a, toolCalls)

	if len(safe) < 2 {
		return a.executeToolCallsSequentially(ctx, iter, toolCalls, allowedTools)
	}

	executed := 0

	n, err := a.executeToolCallsConcurrently(ctx, iter, safe, allowedTools)
	executed += n
	if err != nil {
		return executed, err
	}
	if a.state.Snapshot().Terminal() {
		return executed, nil
	}

	n2, err := a.executeToolCallsSequentially(ctx, iter, unsafe, allowedTools)
	executed += n2
	return executed, err
}

// executeToolCallsSequentially runs tool calls one by one (the original behavior).
func (a *Agent) executeToolCallsSequentially(ctx context.Context, iter int, toolCalls []*ai.FunctionTool, allowedTools map[string]struct{}) (int, error) {
	executed := 0
	for _, tc := range toolCalls {
		if ctx.Err() != nil {
			break
		}
		if tc == nil || tc.Function == nil {
			continue
		}
		if err := a.executeToolCall(ctx, iter, tc, allowedTools); err != nil {
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
	tc        *ai.FunctionTool
	toolName  string
	callID    string
	argsMap   map[string]any
	tool      Tool
	isAgent   bool
	stackDepth int

	validationErr string

	rawOut  string
	rawErr  string

	out      string
	errText  string
	outTrunc bool
	errTrunc bool
}

// executeToolCallsConcurrently runs multiple concurrent-safe tools in parallel.
// Tool results are written to stepHistory in the original call order.
func (a *Agent) executeToolCallsConcurrently(ctx context.Context, iter int, toolCalls []*ai.FunctionTool, allowedTools map[string]struct{}) (int, error) {
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
		slot.isAgent = IsAgentTool(tool)
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
		})

		wg.Add(1)
		sem <- struct{}{}
		go func(s *concurrentToolSlot) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					s.rawErr = fmt.Sprintf("tool panicked: %v", r)
				}
			}()

			if ctx.Err() != nil {
				s.rawErr = fmt.Sprintf("context cancelled: %v", ctx.Err())
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
				CurrentStepID:      strings.TrimSpace(prevSnapshot.CurrentStepID),
			})

			toolTimeout := a.cfg.resolveToolTimeout(s.argsMap)
			execCtx, cancelTimeout := context.WithTimeout(callCtx, toolTimeout)
			defer cancelTimeout()

			out, err := s.tool.Execute(execCtx, s.argsMap)
			if err != nil && execCtx.Err() == context.DeadlineExceeded {
				err = fmt.Errorf("tool %q timed out after %s: %w", s.toolName, toolTimeout, err)
			}
			if err != nil {
				s.rawErr = err.Error()
			}
			s.rawOut = out
		}(slot)
	}

	wg.Wait()

	// Write results to stepHistory in original order (single-writer invariant).
	executed := 0
	sharedDir := ""
	if a.workspaceRuntime != nil {
		sharedDir = a.workspaceRuntime.SharedDir()
	}

	for _, slot := range slots {
		if slot.toolName == "" {
			continue
		}

		if slot.validationErr != "" {
			a.AICallProxyWriteToolResult(slot.callID, slot.toolName, "", slot.argsMap, "", slot.validationErr, false)
			executed++
			continue
		}

		if slot.tool == nil {
			continue
		}

		slot.out, slot.outTrunc = TruncateToolOutput(slot.toolName, slot.rawOut, a.workspaceRootDir)
		slot.errText, slot.errTrunc = TruncateToolOutput(slot.toolName+"-error", slot.rawErr, a.workspaceRootDir)

		if slot.outTrunc || slot.errTrunc {
			a.emitRuntimeLog("info", "tool output truncated", prevSnapshot, map[string]any{
				"event":         "tool_output_truncated",
				"tool":          slot.toolName,
				"out_truncated": slot.outTrunc,
				"err_truncated": slot.errTrunc,
			})
		}

		displayOut := slot.out
		if strings.TrimSpace(displayOut) == "" && strings.TrimSpace(slot.errText) != "" {
			displayOut = fmt.Sprintf("Error: %s", slot.errText)
		}
		a.handleSkillToolStateSync(slot.toolName, slot.argsMap, slot.out, slot.errText)
		a.AICallProxyWriteToolResult(slot.callID, slot.toolName, slot.tool.Description(), slot.argsMap, displayOut, slot.errText, slot.isAgent)

		if stepID := strings.TrimSpace(prevSnapshot.CurrentStepID); sharedDir != "" && stepID != "" {
			_ = appendStepTimeline(sharedDir, stepID, &TimelineEvent{
				TS:   time.Now().UTC(),
				Type: "tool_call",
				Key:  slot.callID,
				Payload: map[string]any{
					"tool":   slot.toolName,
					"args":   slot.argsMap,
					"result": slot.out,
					"error":  slot.errText,
				},
			})
		}

		a.emitter.EmitToolEnd(iter, builtin_tools.ToolResult{
			ID:         slot.callID,
			Name:       slot.toolName,
			IsAgent:    slot.isAgent,
			StackDepth: slot.stackDepth,
			Result:     displayOut,
			Error:      slot.errText,
		})

		executed++
	}

	return executed, nil
}
