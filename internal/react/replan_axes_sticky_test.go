package react

import (
	"encoding/json"
	"testing"

	"aster/internal/builtin_tools"
)

// TestApplyStepReplan_UnresolvedAxesStickyCarryForward 校验三轴未决盘点的 sticky 跨步骤语义：
// step-2 replan 产出三轴后，step-3 未触发 replan（不下发 UnresolvedAxes）时应原样保留 step-2 的三轴；
// 终态下发空三轴（非 nil）则复位清空。
func TestApplyStepReplan_UnresolvedAxesStickyCarryForward(t *testing.T) {
	tracker := NewStateTracker()

	// step-2 完成并 replan，产出三轴未决盘点。
	snap2 := tracker.ApplyStepReplan("step-2", stepReplanUpdate{
		UnresolvedAxes: &builtin_tools.ReplanAxes{
			IncompleteItems: builtin_tools.NewAxisItems([]string{"登出接口未测"}),
			DepthGaps:       builtin_tools.NewAxisItems([]string{"终点 A 已定位但未追到触发点"}),
			NewSurfaces:     builtin_tools.NewAxisItems([]string{"admin 分包整体未覆盖"}),
		},
		NextPhase: builtin_tools.AgentPhaseStep,
	})
	if snap2.UnresolvedAxes == nil || len(snap2.UnresolvedAxes.DepthGaps) != 1 {
		t.Fatalf("expected step-2 three-axis written, got %+v", snap2.UnresolvedAxes)
	}

	// step-3 未触发 replan（快路径）：UnresolvedAxes 不下发（nil），应保留 step-2 的三轴（sticky）。
	snap3 := tracker.ApplyStepReplan("step-3", stepReplanUpdate{
		NextPhase: builtin_tools.AgentPhaseStep,
	})
	if snap3.UnresolvedAxes == nil {
		t.Fatalf("expected sticky UnresolvedAxes preserved after non-replan step, got nil")
	}
	if len(snap3.UnresolvedAxes.IncompleteItems) != 1 || snap3.UnresolvedAxes.IncompleteItems[0].Item != "登出接口未测" {
		t.Fatalf("expected step-2 incomplete_items preserved, got %+v", snap3.UnresolvedAxes.IncompleteItems)
	}
	if len(snap3.UnresolvedAxes.DepthGaps) != 1 || snap3.UnresolvedAxes.DepthGaps[0].Item != "终点 A 已定位但未追到触发点" {
		t.Fatalf("expected step-2 depth_gaps preserved, got %+v", snap3.UnresolvedAxes.DepthGaps)
	}
	if len(snap3.UnresolvedAxes.NewSurfaces) != 1 || snap3.UnresolvedAxes.NewSurfaces[0].Item != "admin 分包整体未覆盖" {
		t.Fatalf("expected step-2 new_surfaces preserved, got %+v", snap3.UnresolvedAxes.NewSurfaces)
	}

	// 终态复位：下发空三轴（非 nil），sticky 状态应被清空。
	snapReset := tracker.ApplyFinalAnswerPhaseUpdate(finalAnswerPhaseUpdate{
		Status:         builtin_tools.TaskStatusCompleted,
		UnresolvedAxes: &builtin_tools.ReplanAxes{},
	})
	if !builtin_tools.ReplanAxesEmpty(snapReset.UnresolvedAxes) {
		t.Fatalf("expected terminal reset to clear UnresolvedAxes, got %+v", snapReset.UnresolvedAxes)
	}
}

// TestSynthesizeResumeSnapshot_RestoresUnresolvedAxesFromAssessedState 校验续跑时
// assessed_state.unresolved_axes 三轴能完整重建到 snapshot。
func TestSynthesizeResumeSnapshot_RestoresUnresolvedAxesFromAssessedState(t *testing.T) {
	plan := []*builtin_tools.PlanItem{
		{ID: "step-1", Step: "分析", Status: builtin_tools.PlanStepCompleted},
	}
	raw, err := json.Marshal(map[string]any{
		"session_id":   "resume-axes",
		"plan_version": 1,
		"assessed_state": map[string]any{
			"status":        builtin_tools.TaskStatusRunning,
			"plan":          plan,
			"plan_version":  1,
			"step_outcomes": collectAllStepContextViews(plan, nil),
			"unresolved_axes": map[string]any{
				"incomplete_items": []string{"登出接口未测"},
				"depth_gaps":       []string{"终点 A 已定位但未追到触发点"},
				"new_surfaces":     []string{"admin 分包整体未覆盖"},
			},
		},
		"assessment": map[string]any{
			"is_complete": false,
			"status":      string(builtin_tools.TaskStatusRunning),
		},
	})
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	var artifact FinalAssessmentArtifact
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}

	snapshot, _ := synthesizeResumeSnapshot(nil, nil, nil, &artifact, 0)
	if snapshot.UnresolvedAxes == nil {
		t.Fatal("expected UnresolvedAxes restored from assessed_state, got nil")
	}
	if len(snapshot.UnresolvedAxes.IncompleteItems) != 1 || snapshot.UnresolvedAxes.IncompleteItems[0].Item != "登出接口未测" {
		t.Fatalf("incomplete_items not restored: %+v", snapshot.UnresolvedAxes.IncompleteItems)
	}
	if len(snapshot.UnresolvedAxes.DepthGaps) != 1 || snapshot.UnresolvedAxes.DepthGaps[0].Item != "终点 A 已定位但未追到触发点" {
		t.Fatalf("depth_gaps not restored: %+v", snapshot.UnresolvedAxes.DepthGaps)
	}
	if len(snapshot.UnresolvedAxes.NewSurfaces) != 1 || snapshot.UnresolvedAxes.NewSurfaces[0].Item != "admin 分包整体未覆盖" {
		t.Fatalf("new_surfaces not restored: %+v", snapshot.UnresolvedAxes.NewSurfaces)
	}
}

// TestSynthesizeResumeSnapshot_StickyFallbackFromWorkspaceState 校验当 assessed_state 缺三轴时，
// 回退到 workspace/state.json 的 sticky 三轴（保持续跑前的跨步骤累积）。
func TestSynthesizeResumeSnapshot_StickyFallbackFromWorkspaceState(t *testing.T) {
	ws := &builtin_tools.WorkspaceState{
		Status: builtin_tools.TaskStatusRunning,
		UnresolvedAxes: &builtin_tools.ReplanAxes{
			DepthGaps: builtin_tools.NewAxisItems([]string{"浅层判断停在 shallow_only"}),
		},
	}

	snapshot, _ := synthesizeResumeSnapshot(nil, nil, ws, nil, 0)
	if snapshot.UnresolvedAxes == nil || len(snapshot.UnresolvedAxes.DepthGaps) != 1 {
		t.Fatalf("expected sticky UnresolvedAxes from workspace state, got %+v", snapshot.UnresolvedAxes)
	}
	if snapshot.UnresolvedAxes.DepthGaps[0].Item != "浅层判断停在 shallow_only" {
		t.Fatalf("unexpected depth_gap: %q", snapshot.UnresolvedAxes.DepthGaps[0].Item)
	}
}
