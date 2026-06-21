package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// 红线（Bug A 修复后）：ResetThinkGroupID 边界 = 真正 reasoning 段终结 / 真正阶段切换。
//
// 旧实现 EmitStream 和 EmitTaskItem 都调 reset，导致：
//   - 主路径 thinking 流被切碎为 200 条 1-3 token 的 ThinkingPart（症状 A）
//   - observer 期间 task_item 事件随机切断主路径 thinking
//
// 修复后：删 EmitStream + EmitTaskItem 的 reset，保留终结类 + 切换类。

func newGroupingTestEmitter() *Emitter {
	return NewEmitter("a", "agent", func(e *AgentOutputEvent) error { return nil })
}

// thinkGroupID 调 EmitThink 并返回它实际入 event 的 GroupID（通过 base emitter 截获）。
func captureNextEmitThinkGroupID(t *testing.T, em *Emitter) string {
	t.Helper()
	var captured string
	original := em.baseEmitter
	em.baseEmitter = func(e *AgentOutputEvent) error {
		if e.Type == EventTypeThink {
			captured = e.GroupID
		}
		if original != nil {
			return original(e)
		}
		return nil
	}
	em.EmitThink(1, "", "delta", "delta", nil, "", "")
	em.baseEmitter = original
	return captured
}

// TestEmitStream_DoesNotResetThinkGroupID 红线：EmitStream 不应清掉 think groupID。
// 旧实现每次 content chunk 都 reset，让下一段 reasoning 起新 group → 碎片化。
func TestEmitStream_DoesNotResetThinkGroupID(t *testing.T) {
	em := newGroupingTestEmitter()
	first := captureNextEmitThinkGroupID(t, em)
	em.EmitStream(1, "content chunk", "")
	second := captureNextEmitThinkGroupID(t, em)

	if first == "" || second == "" {
		t.Fatalf("captured empty groupID: first=%q second=%q", first, second)
	}
	if first != second {
		t.Errorf("EmitStream MUST NOT reset think groupID; first=%s second=%s (Bug A regression)", first, second)
	}
}

// TestEmitTaskItem_DoesNotResetThinkGroupID 红线：observer 细粒度 task_item 事件
// 不应切断主路径 thinking 段。
func TestEmitTaskItem_DoesNotResetThinkGroupID(t *testing.T) {
	em := newGroupingTestEmitter()
	first := captureNextEmitThinkGroupID(t, em)
	em.EmitTaskItem(&builtin_tools.PlanItem{ID: "s1", Step: "x", Status: builtin_tools.PlanStepInProgress},
		builtin_tools.PlanStepPending, 0, "")
	second := captureNextEmitThinkGroupID(t, em)

	if first != second {
		t.Errorf("EmitTaskItem MUST NOT reset think groupID; first=%s second=%s", first, second)
	}
}

// TestEmitThink_FinishReasonStillResets 保留行为：finishReason 非空时 reset。
func TestEmitThink_FinishReasonStillResets(t *testing.T) {
	em := newGroupingTestEmitter()
	first := captureNextEmitThinkGroupID(t, em)
	em.EmitThink(1, "", "x", "x", nil, "stop", "")
	second := captureNextEmitThinkGroupID(t, em)

	if first == second {
		t.Errorf("EmitThink(finishReason=stop) should reset groupID; both=%s", first)
	}
}

// TestEmitToolStart_StillResets 保留行为：reasoning → tool 切换应 reset。
func TestEmitToolStart_StillResets(t *testing.T) {
	em := newGroupingTestEmitter()
	first := captureNextEmitThinkGroupID(t, em)
	em.EmitToolStart(1, builtin_tools.ToolCall{ID: "c1", Name: "bash"}, "")
	second := captureNextEmitThinkGroupID(t, em)

	if first == second {
		t.Errorf("EmitToolStart should reset groupID; both=%s", first)
	}
}

// TestEmitToolEnd_StillResets 保留行为：tool → reasoning 切换也 reset
// （每轮 ReAct 的 think 段独立，明确这是 intended）。
func TestEmitToolEnd_StillResets(t *testing.T) {
	em := newGroupingTestEmitter()
	first := captureNextEmitThinkGroupID(t, em)
	em.EmitToolEnd(1, builtin_tools.ToolResult{ID: "c1", Name: "bash"}, "")
	second := captureNextEmitThinkGroupID(t, em)

	if first == second {
		t.Errorf("EmitToolEnd should reset groupID (ReAct each round new think segment); both=%s", first)
	}
}
