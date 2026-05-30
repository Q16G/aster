package react

import (
	"context"
	"encoding/json"

	"aster/internal/builtin_tools"
)

// AwaitSubAgentsTool is a yield primitive: when called it does not block in the
// tool itself. Instead it sets awaitBackgroundRequested on the parent agent and
// returns immediately. The scheduler loop sees the flag and parks (zero model
// calls) until ALL background sub-agents finish (awaitAllBackgroundSubAgents),
// draining each completion into context as it arrives, then wakes.
//
// The tool runs synchronously on the scheduler goroutine (see executeToolCall),
// so the flag read/write is free of data races with the loop.
type AwaitSubAgentsTool struct {
	parentAgent *Agent
}

var _ ConcurrencySafeTool = (*AwaitSubAgentsTool)(nil)

func NewAwaitSubAgentsTool(parent *Agent) *AwaitSubAgentsTool {
	return &AwaitSubAgentsTool{parentAgent: parent}
}

func (t *AwaitSubAgentsTool) Name() string { return builtin_tools.AwaitSubAgentsToolName }

func (t *AwaitSubAgentsTool) ConcurrencySafe() bool { return true }

func (t *AwaitSubAgentsTool) Description() string {
	return "让出执行权，等待所有后台子 Agent 完成。在用 sub_agent (run_in_background=true) 启动后台子 Agent 后调用一次：系统会在不消耗模型调用的情况下挂起，直到全部后台子 Agent 完成，每个完成的结果都会自动推送进上下文。无需也不要用 sub_agent_status 紧密轮询。若当前没有运行中的后台子 Agent，会立即返回提示。"
}

func (t *AwaitSubAgentsTool) Parameters() any {
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
}

func (t *AwaitSubAgentsTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	if t == nil || t.parentAgent == nil {
		return marshalAwaitResult("no_running_subagents", 0), nil
	}

	registry := t.parentAgent.asyncRegistry
	if registry == nil || !registry.HasRunning() {
		return marshalAwaitResult("no_running_subagents", 0), nil
	}

	running := len(registry.RunningAgents())
	t.parentAgent.awaitBackgroundRequested = true
	return marshalAwaitResult("awaiting_background", running), nil
}

func marshalAwaitResult(status string, running int) string {
	payload := map[string]any{"status": status}
	if status == "awaiting_background" {
		payload["running"] = running
		payload["note"] = "已挂起等待所有后台子 Agent，全部完成后会自动唤醒并逐个推送结果，无需轮询。"
	} else {
		payload["note"] = "当前没有运行中的后台子 Agent，无需等待。"
	}
	out, _ := json.Marshal(payload)
	return string(out)
}
