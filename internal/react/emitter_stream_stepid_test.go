package react

import "testing"

// TestEmitStream_PeerStepIDInPayload 验证 peer 路径流式 token emit 时 stepID 进 payload。
// 修复前 EmitStream 签名无 stepID，peer 流和主路径流串在同一 stream buffer。
func TestEmitStream_PeerStepIDInPayload(t *testing.T) {
	var captured []*AgentOutputEvent
	em := NewEmitter("a", "agent", func(e *AgentOutputEvent) error {
		captured = append(captured, e)
		return nil
	})
	em.EmitStream(1, "peer token", "peer-p2")

	if len(captured) != 1 {
		t.Fatalf("expected 1 event, got %d", len(captured))
	}
	e := captured[0]
	if e.Type != EventTypeStream {
		t.Fatalf("Type=%q, want stream", e.Type)
	}
	if e.Content != "peer token" {
		t.Fatalf("Content=%q", e.Content)
	}
	stepID, _ := e.Payload["step_id"].(string)
	if stepID != "peer-p2" {
		t.Fatalf("payload step_id=%q, want peer-p2 (payload=%+v)", stepID, e.Payload)
	}
}

// TestEmitStream_MainPathOmitsStepIDFromPayload 主路径 stepID="" 时 payload 不含 step_id
// （与 EmitThink/EmitToolStart 行为对齐——空 stepID 不污染 payload）。
func TestEmitStream_MainPathOmitsStepIDFromPayload(t *testing.T) {
	var captured []*AgentOutputEvent
	em := NewEmitter("a", "agent", func(e *AgentOutputEvent) error {
		captured = append(captured, e)
		return nil
	})
	em.EmitStream(1, "main token", "")

	if len(captured) != 1 {
		t.Fatalf("expected 1 event, got %d", len(captured))
	}
	if _, exists := captured[0].Payload["step_id"]; exists {
		t.Fatalf("payload should not contain step_id for main path, got %+v", captured[0].Payload)
	}
}
