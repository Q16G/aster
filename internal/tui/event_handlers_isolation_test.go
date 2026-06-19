package tui

import (
	"strings"
	"testing"

	"aster/internal/react"
)

// TestHandleAgentEvent_ToolStart_NonRootDoesNotChangeStatusText：面 7.D 漏洞 1。
// 非 root agent 的 tool_start 不应改 m.statusText（避免 X2 远程 step 工具调用污染状态栏）。
func TestHandleAgentEvent_ToolStart_NonRootDoesNotChangeStatusText(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "root"
	m.statusText = "initial-status"

	// child agent（非 root）调工具
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeToolStart,
		AgentName: "remote-step-step-b", // 非 root
		Payload: map[string]any{
			"tool_name": "rg",
			"arguments": "main",
		},
	})

	if m.statusText != "initial-status" {
		t.Fatalf("non-root tool_start should not change statusText, got %q", m.statusText)
	}
}

// TestHandleAgentEvent_ToolStart_RootDoesChangeStatusText：回归。
// root agent 的 tool_start 仍应正常改 statusText。
func TestHandleAgentEvent_ToolStart_RootDoesChangeStatusText(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "root"
	m.statusText = "initial-status"

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeToolStart,
		AgentName: "root", // root
		Payload: map[string]any{
			"tool_name": "rg",
			"arguments": "main",
		},
	})

	if m.statusText != "calling rg..." {
		t.Fatalf("root tool_start should change statusText to 'calling rg...', got %q", m.statusText)
	}
}

// TestHandleAgentEvent_ToolStart_NonRootStillAddsToolPartWithAgentName：面 7.D 漏洞 1 回归。
// isRoot 守卫只覆盖 statusText 改写，ToolPart 仍应被添加且 AgentName 非空（chat 区按 AgentName 隔离）。
func TestHandleAgentEvent_ToolStart_NonRootStillAddsToolPartWithAgentName(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "root"

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeToolStart,
		AgentName: "remote-step-step-b",
		Payload: map[string]any{
			"tool_name": "rg",
			"arguments": "main",
		},
	})

	// ToolPart 应被添加（filterMainParts 后续按 AgentName 过滤，但 part 本身要存在）
	var foundToolPart bool
	for _, p := range m.chat.Parts() {
		if p.Type == PartTypeTool && p.Tool != nil && p.Tool.Name == "rg" {
			foundToolPart = true
			if p.Tool.AgentName != "remote-step-step-b" {
				t.Fatalf("ToolPart should carry AgentName=remote-step-step-b, got %q", p.Tool.AgentName)
			}
		}
	}
	if !foundToolPart {
		t.Fatal("expected ToolPart in chat.Parts() even for non-root agent")
	}
}

// TestHandleAgentEvent_RemoteStepBgStart_DoesNotChangeStatusText：面 7 review #3 修复。
// 高并发 spawn 时 BgStart 不应抢占主状态栏（root agent 主路径状态保持）。
func TestHandleAgentEvent_RemoteStepBgStart_DoesNotChangeStatusText(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "root"
	m.statusText = "thinking..."

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeInlineStepStart,
		AgentName: "root",
		Payload: map[string]any{
			"agent_id":  "step-b",
			"step_text": "task",
		},
	})

	if m.statusText != "thinking..." {
		t.Fatalf("RemoteStepBgStart should not change statusText (X2 后台任务不抢主状态栏), got %q", m.statusText)
	}
}

// TestHandleAgentEvent_ResultAddsPartWithAgentName：面 7.D 漏洞 2 的运行时保障。
// child agent 的 result event 添加到 chat 时必须带 AgentName，否则 filterMainParts 误判为 root part。
// 持久化路径（persistPartWithAgent）写文件，没暴露 hook，留运行时层验证；
// session 恢复路径（partAgentName 读取）由现有 session 测试覆盖。
func TestHandleAgentEvent_ResultAddsPartWithAgentName(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "root"

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeResult,
		AgentName: "remote-step-step-b",
		Payload: map[string]any{
			"result": "child agent done",
		},
	})

	var foundResultPart bool
	for _, p := range m.chat.Parts() {
		if p.Type == PartTypeText && p.Text != nil && strings.Contains(p.Text.Content, "child agent done") {
			foundResultPart = true
			if p.Text.AgentName != "remote-step-step-b" {
				t.Fatalf("result TextPart should carry AgentName=remote-step-step-b, got %q", p.Text.AgentName)
			}
		}
	}
	if !foundResultPart {
		t.Fatal("expected result TextPart in chat.Parts()")
	}
}
