package tui

import (
	"encoding/json"
	"testing"

	"aster/internal/react"
)

func TestSubAgentPart_KindRoundTripsThroughJSON(t *testing.T) {
	// Kind=remote_step：序列化 + 反序列化后字段不丢
	original := SubAgentPart{
		AgentName:   "step-b",
		CallID:      "step-b",
		Kind:        subAgentPartKindRemoteStep,
		Status:      "running",
		Description: "task",
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var restored SubAgentPart
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if restored.Kind != subAgentPartKindRemoteStep {
		t.Fatalf("Kind not round-tripped: got %q", restored.Kind)
	}

	// Kind="" 零值：omitempty 应让序列化不带字段，反序列化保持空
	zero := SubAgentPart{AgentName: "sub-x", CallID: "sub-x", Status: "running"}
	rawZero, _ := json.Marshal(zero)
	if got := string(rawZero); got == "" {
		t.Fatal("expected non-empty JSON")
	}
	// 默认 sub_agent 路径不写 Kind 字段（omitempty）
	if contains := string(rawZero); jsonHasKey(contains, "kind") {
		t.Fatalf("Kind zero value should be omitted in JSON, got %s", contains)
	}
	var restoredZero SubAgentPart
	if err := json.Unmarshal(rawZero, &restoredZero); err != nil {
		t.Fatalf("unmarshal zero: %v", err)
	}
	if restoredZero.Kind != "" {
		t.Fatalf("expected Kind=\"\", got %q", restoredZero.Kind)
	}
}

// jsonHasKey 浅检测 JSON 字符串里是否存在 "key" 字段名（不解析转义内容，够本测试用）。
func jsonHasKey(s, key string) bool {
	needle := "\"" + key + "\":"
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// inlineStepCardStatus 返回 InlineStepPart 卡片的当前状态——commit 12 把
// inline_step 卡片从 SubAgentPart 复用切到独立 InlineStepPart 类型。
func inlineStepCardStatus(m *Model, stepID string) (status string, ok bool) {
	for _, p := range m.chat.Parts() {
		if p.Type == PartTypeInlineStep && p.InlineStep != nil && p.InlineStep.StepID == stepID {
			return p.InlineStep.Status, true
		}
	}
	return "", false
}

func TestHandleAgentEvent_RemoteStepBgStart_AddsCard(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeInlineStepStart,
		Payload: map[string]any{
			"agent_id":  "step-b",
			"step_text": "分析模块 X 的认证逻辑",
			"workspace": "/tmp/ws",
		},
	})

	status, ok := inlineStepCardStatus(&m, "step-b")
	if !ok {
		t.Fatal("expected InlineStepPart card for step-b after Start event")
	}
	if status != "running" {
		t.Fatalf("expected status=running, got %q", status)
	}
	// Description 应填入 step_text
	for _, p := range m.chat.Parts() {
		if p.Type == PartTypeInlineStep && p.InlineStep != nil && p.InlineStep.StepID == "step-b" {
			if p.InlineStep.Description != "分析模块 X 的认证逻辑" {
				t.Fatalf("expected Description=step_text, got %q", p.InlineStep.Description)
			}
			return
		}
	}
}

func TestHandleAgentEvent_RemoteStepBgEnd_UpdatesStatus(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:    react.EventTypeInlineStepStart,
		Payload: map[string]any{"agent_id": "step-c", "step_text": "do c"},
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeInlineStepEnd,
		Payload: map[string]any{
			"agent_id": "step-c",
			"status":   "completed",
			"summary":  "c done",
		},
	})

	status, ok := inlineStepCardStatus(&m, "step-c")
	if !ok {
		t.Fatal("expected card for step-c")
	}
	if status != "completed" {
		t.Fatalf("expected status=completed after BgEnd, got %q", status)
	}
}

func TestHandleAgentEvent_RemoteStepCancelledNotOverwritingTerminal(t *testing.T) {
	// 复用 isTerminalSubAgentStatus 守卫：BgEnd(cancelled) 在 BgEnd(completed) 之后到达不应覆盖
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:    react.EventTypeInlineStepStart,
		Payload: map[string]any{"agent_id": "step-d", "step_text": "do d"},
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:    react.EventTypeInlineStepEnd,
		Payload: map[string]any{"agent_id": "step-d", "status": "completed"},
	})
	// 延迟到达的 cancelled 不应覆盖
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:    react.EventTypeInlineStepEnd,
		Payload: map[string]any{"agent_id": "step-d", "status": "cancelled"},
	})

	status, _ := inlineStepCardStatus(&m, "step-d")
	if status != "completed" {
		t.Fatalf("expected status to stay completed (terminal guard), got %q", status)
	}
}
