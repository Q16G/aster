package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// 红线（Commit 2）：peerScopedSnapshot 把 snapshot 投影到 runCtx.StepID。
// 防 step_inline.go peer 路径 emitRuntimeLog 日志显示主路径 stepID 的 debug 障碍回归。

func TestPeerScopedSnapshot_NilRunCtxNoop(t *testing.T) {
	snap := builtin_tools.StateSnapshot{CurrentStepID: "s1"}
	out := peerScopedSnapshot(snap, nil)
	if out.CurrentStepID != "s1" {
		t.Errorf("nil runCtx should be no-op; got CurrentStepID=%q want s1", out.CurrentStepID)
	}
}

func TestPeerScopedSnapshot_PeerRunCtxProjects(t *testing.T) {
	snap := builtin_tools.StateSnapshot{CurrentStepID: "s1"} // 主路径 step
	runCtx := &InlineStepCtx{StepID: "s2"}                   // peer 路径 step
	out := peerScopedSnapshot(snap, runCtx)
	if out.CurrentStepID != "s2" {
		t.Errorf("peer runCtx should project CurrentStepID to s2; got %q", out.CurrentStepID)
	}
}

func TestPeerScopedSnapshot_EmptyRunCtxStepIDNoop(t *testing.T) {
	snap := builtin_tools.StateSnapshot{CurrentStepID: "s1"}
	runCtx := &InlineStepCtx{StepID: ""} // 异常：runCtx 存在但 StepID 空
	out := peerScopedSnapshot(snap, runCtx)
	if out.CurrentStepID != "s1" {
		t.Errorf("empty runCtx.StepID should be no-op (defensive); got %q", out.CurrentStepID)
	}
}

func TestPeerScopedSnapshot_DoesNotMutateInput(t *testing.T) {
	snap := builtin_tools.StateSnapshot{CurrentStepID: "s1"}
	runCtx := &InlineStepCtx{StepID: "s2"}
	_ = peerScopedSnapshot(snap, runCtx)
	if snap.CurrentStepID != "s1" {
		t.Errorf("input snapshot should not be mutated by peerScopedSnapshot; got %q", snap.CurrentStepID)
	}
}
