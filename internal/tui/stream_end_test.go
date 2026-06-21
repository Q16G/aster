package tui

import (
	"testing"

	"aster/internal/react"
)

// TestStreamEnd_FlushesCorrectBucket 红线 FR1：stream_end 显式信号 + 正确的 stepID
// 决定 flush 哪个桶。peer 流和主流互不影响。
func TestStreamEnd_FlushesCorrectBucket(t *testing.T) {
	m := NewModel(ModelDeps{})

	// 灌入两路流：peer p1 和主路径。
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStream,
		AgentName: "pentest",
		Content:   "peer-p1 token",
		Payload:   map[string]any{"step_id": "p1"},
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStream,
		AgentName: "pentest",
		Content:   "main token",
	})

	// 只 stream_end main 路径：peer p1 buffer 保持。
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStreamEnd,
		AgentName: "pentest",
	})

	// 检查：main 路径 buffer 已被 flush（应该产生 TextPart, StepID=""）
	// peer p1 路径 buffer 仍在
	if got := m.chat.StreamContent("pentest", ""); got != "" {
		t.Errorf("main buffer should be empty after stream_end, got %q", got)
	}
	if got := m.chat.StreamContent("pentest", "p1"); got != "peer-p1 token" {
		t.Errorf("peer p1 buffer should be intact, got %q", got)
	}
	var foundMain bool
	for _, p := range m.chat.store.parts {
		if p.Type == PartTypeText && p.Text != nil && p.Text.Content == "main token" && p.Text.StepID == "" {
			foundMain = true
		}
	}
	if !foundMain {
		t.Errorf("expected TextPart for main path; got parts=%v", m.chat.store.parts)
	}
}

// TestStreamEnd_PeerEndsDoesNotTouchMain peer 的 stream_end 不能 flush 主路径流。
func TestStreamEnd_PeerEndsDoesNotTouchMain(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStream,
		AgentName: "pentest",
		Content:   "main token",
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStream,
		AgentName: "pentest",
		Content:   "peer-p1 token",
		Payload:   map[string]any{"step_id": "p1"},
	})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStreamEnd,
		AgentName: "pentest",
		Payload:   map[string]any{"step_id": "p1"},
	})

	if got := m.chat.StreamContent("pentest", ""); got != "main token" {
		t.Errorf("main buffer must NOT be touched by peer stream_end, got %q", got)
	}
	if got := m.chat.StreamContent("pentest", "p1"); got != "" {
		t.Errorf("peer p1 buffer should be flushed, got %q", got)
	}
}

// TestPhaseChange_DoesNotShredPeerStream 红线 FR2：phase 切换（结构事件）不再强 flush
// 仍在跑的 peer 流，避免一段流被切成 N 段碎片 TextPart。
func TestPhaseChange_DoesNotShredPeerStream(t *testing.T) {
	m := NewModel(ModelDeps{})

	// peer 输出一段 token
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStream,
		AgentName: "pentest",
		Content:   "peer chunk-1 ",
		Payload:   map[string]any{"step_id": "p1"},
	})

	// 主路径触发 phase 切换
	m.chat.rootAgentName = "pentest"
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStateChange,
		AgentName: "pentest",
		Payload:   map[string]any{"phase": "step_replan"},
	})

	// peer 再来一段
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStream,
		AgentName: "pentest",
		Content:   "peer chunk-2",
		Payload:   map[string]any{"step_id": "p1"},
	})

	// peer 显式 stream_end
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStreamEnd,
		AgentName: "pentest",
		Payload:   map[string]any{"step_id": "p1"},
	})

	// 期望：peer 流被合成 **一条** 完整 TextPart，而不是因 phase 切换被切成两段
	peerTextCount := 0
	var peerContent string
	for _, p := range m.chat.store.parts {
		if p.Type == PartTypeText && p.Text != nil && p.Text.StepID == "p1" {
			peerTextCount++
			peerContent = p.Text.Content
		}
	}
	if peerTextCount != 1 {
		t.Fatalf("expected exactly 1 TextPart for peer p1 (not shredded by phase change), got %d", peerTextCount)
	}
	if peerContent != "peer chunk-1 peer chunk-2" {
		t.Errorf("peer content should be intact, got %q", peerContent)
	}
}
