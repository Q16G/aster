package react

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aster/internal/builtin_tools"
)

// SubAgentStatusTool lets the model query the status of running background sub-agents.
// Only returns agents with status "running"; completed/failed results are
// auto-delivered to stepHistory via drainAsyncAgentNotifications.
type SubAgentStatusTool struct {
	parentAgent *Agent
}

var _ ConcurrencySafeTool = (*SubAgentStatusTool)(nil)

func NewSubAgentStatusTool(parent *Agent) *SubAgentStatusTool {
	return &SubAgentStatusTool{parentAgent: parent}
}

func (t *SubAgentStatusTool) Name() string { return builtin_tools.SubAgentStatusToolName }

func (t *SubAgentStatusTool) ConcurrencySafe() bool { return true }

func (t *SubAgentStatusTool) Description() string {
	return "查询正在运行中的后台子 Agent 状态。仅用于偶发性确认“是否仍在运行”；completed/failed 结果会自动推送到上下文，不要把它当作高频轮询工具。等待后台子 Agent 完成时使用 await_subagents 让出执行权，由系统在完成时自动唤醒，而不是用本工具紧密轮询。"
}

func (t *SubAgentStatusTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"agent_id": map[string]any{
				"type":        "string",
				"description": "可选：查询特定 running agent。为空时返回所有正在运行的后台 agent。",
			},
		},
		"additionalProperties": false,
	}
}

func (t *SubAgentStatusTool) Execute(_ context.Context, args map[string]any) (string, error) {
	if t == nil || t.parentAgent == nil {
		return "", fmt.Errorf("sub_agent_status tool not initialized")
	}

	registry := t.parentAgent.asyncRegistry
	if registry == nil {
		return formatStatusResult(nil), nil
	}

	agentID := strings.TrimSpace(fmt.Sprintf("%v", args["agent_id"]))
	if agentID != "" && agentID != "<nil>" {
		entry := registry.Get(agentID)
		if entry == nil {
			return formatStatusResult(nil), nil
		}
		if entry.Status != "running" {
			return formatStatusResult(nil), nil
		}
		return formatStatusResult([]*AsyncAgentEntry{entry}), nil
	}

	return formatStatusResult(registry.RunningAgents()), nil
}

func formatStatusResult(entries []*AsyncAgentEntry) string {
	agents := make([]map[string]any, 0, len(entries))
	now := time.Now()
	for _, e := range entries {
		agents = append(agents, map[string]any{
			"agent_id":    e.AgentID,
			"status":      e.Status,
			"instruction": e.Instruction,
			"workspace":   e.WorkspaceDir,
			"elapsed":     now.Sub(e.StartedAt).Truncate(time.Second).String(),
		})
	}
	out, _ := json.Marshal(map[string]any{
		"agents": agents,
	})
	return string(out)
}
