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

// captureEmitThinkGroupIDForStep 同 captureNextEmitThinkGroupID，但指定 stepID 作用域。
func captureEmitThinkGroupIDForStep(t *testing.T, em *Emitter, stepID string) string {
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
	em.EmitThink(1, "", "delta", "delta", nil, "", stepID)
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

// 红线（并发 step 分区）：thinking 分组键改为 per-stepID 作用域后，一个 step 的边界
// 事件不得切碎另一个并发 step 的 in-flight reasoning（spawnInlinePeer 共享同一 Emitter）。

// TestEmitThink_PerStepIDGroupsAreIndependent 红线：不同 stepID 拿到不同 groupID，
// 避免两个并发 reasoning 流在 TUI idxByThinkingGroup 上碰撞合并。
func TestEmitThink_PerStepIDGroupsAreIndependent(t *testing.T) {
	em := newGroupingTestEmitter()
	g1 := captureEmitThinkGroupIDForStep(t, em, "p1")
	g2 := captureEmitThinkGroupIDForStep(t, em, "p2")

	if g1 == "" || g2 == "" {
		t.Fatalf("captured empty groupID: g1=%q g2=%q", g1, g2)
	}
	if g1 == g2 {
		t.Errorf("different stepIDs MUST get different groupIDs; both=%s", g1)
	}
}

// TestEmitToolStart_PeerDoesNotResetOtherStep 核心 bug 红线：peer p2 的 tool 事件
// 不得清掉 peer p1 正在累积的 reasoning 分组。
func TestEmitToolStart_PeerDoesNotResetOtherStep(t *testing.T) {
	em := newGroupingTestEmitter()
	p1First := captureEmitThinkGroupIDForStep(t, em, "p1")
	em.EmitToolStart(1, builtin_tools.ToolCall{ID: "c1", Name: "bash"}, "p2")
	p1Second := captureEmitThinkGroupIDForStep(t, em, "p1")

	if p1First == "" || p1Second == "" {
		t.Fatalf("captured empty groupID: first=%q second=%q", p1First, p1Second)
	}
	if p1First != p1Second {
		t.Errorf("EmitToolStart(stepID=p2) MUST NOT reset p1's group; first=%s second=%s", p1First, p1Second)
	}
}

// TestEmitToolStart_ResetsOwnStep 保留行为：tool 事件仍应终结**自己** step 的 reasoning 段。
func TestEmitToolStart_ResetsOwnStep(t *testing.T) {
	em := newGroupingTestEmitter()
	first := captureEmitThinkGroupIDForStep(t, em, "p1")
	em.EmitToolStart(1, builtin_tools.ToolCall{ID: "c1", Name: "bash"}, "p1")
	second := captureEmitThinkGroupIDForStep(t, em, "p1")

	if first == second {
		t.Errorf("EmitToolStart(stepID=p1) should reset p1's own group; both=%s", first)
	}
}

// TestEmitStateChange_DoesNotResetAnyStep 红线：全局 observer 状态翻转不得切断任何
// step 的 reasoning 段（并发下高频，曾全局 reset 切碎别的 step）。
func TestEmitStateChange_DoesNotResetAnyStep(t *testing.T) {
	em := newGroupingTestEmitter()
	first := captureEmitThinkGroupIDForStep(t, em, "p1")
	em.EmitStateChange(builtin_tools.StateSnapshot{Iteration: 1, CurrentStepID: "p1"})
	second := captureEmitThinkGroupIDForStep(t, em, "p1")

	if first != second {
		t.Errorf("EmitStateChange MUST NOT reset any step's group; first=%s second=%s", first, second)
	}
}

// TestEmitHumanRequest_DoesNotResetAnyStep 红线：人工请求不是 reasoning 段终结边界。
func TestEmitHumanRequest_DoesNotResetAnyStep(t *testing.T) {
	em := newGroupingTestEmitter()
	first := captureEmitThinkGroupIDForStep(t, em, "p1")
	em.EmitHumanRequest(1, "req1", "continue?", nil)
	second := captureEmitThinkGroupIDForStep(t, em, "p1")

	if first != second {
		t.Errorf("EmitHumanRequest MUST NOT reset any step's group; first=%s second=%s", first, second)
	}
}

// TestEmitFinalAnswerResult_ResetsAllScopes 保留行为：turn 终结清空所有作用域。
func TestEmitFinalAnswerResult_ResetsAllScopes(t *testing.T) {
	em := newGroupingTestEmitter()
	main1 := captureNextEmitThinkGroupID(t, em)
	p1A := captureEmitThinkGroupIDForStep(t, em, "p1")
	p2A := captureEmitThinkGroupIDForStep(t, em, "p2")

	em.EmitFinalAnswerResult(&builtin_tools.FinalAnswer{Content: "done"})

	main2 := captureNextEmitThinkGroupID(t, em)
	p1B := captureEmitThinkGroupIDForStep(t, em, "p1")
	p2B := captureEmitThinkGroupIDForStep(t, em, "p2")

	if main1 == main2 || p1A == p1B || p2A == p2B {
		t.Errorf("EmitFinalAnswerResult should reset ALL scopes; main %s->%s p1 %s->%s p2 %s->%s",
			main1, main2, p1A, p1B, p2A, p2B)
	}
}
