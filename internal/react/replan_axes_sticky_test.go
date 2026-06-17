package react

import (
	"encoding/json"
	"testing"

	"aster/internal/builtin_tools"
)

// TestApplyStepReplan_NoLongerWritesStickyAxes 校验合并后的 step_replan 不再写 sticky 三轴：
// step_replan 三轴输出已删除（缺口经 AI 用文件工具直接补到 open_items.md），UnresolvedAxes
// 仅由 final_answer 自身评估写入。本测试断言 step_replan 路径下 sticky 字段保持为 nil。
func TestApplyStepReplan_NoLongerWritesStickyAxes(t *testing.T) {
	tracker := NewStateTracker()

	// step_replan 完成（无论是否直达 Step）都不应写入 UnresolvedAxes。
	snap := tracker.ApplyStepReplan("step-2", stepReplanUpdate{
		NextPhase: builtin_tools.AgentPhaseStep,
	})
	if snap.UnresolvedAxes != nil {
		t.Fatalf("expected step_replan to not write UnresolvedAxes, got %+v", snap.UnresolvedAxes)
	}

	// final_answer 评估未终态时仍可写入 sticky 三轴，作为后续兜底回流的依据。
	snap = tracker.ApplyFinalAnswerPhaseUpdate(finalAnswerPhaseUpdate{
		Status: builtin_tools.TaskStatusRunning,
		UnresolvedAxes: &builtin_tools.ReplanAxes{
			DepthGaps: builtin_tools.NewAxisItems([]string{"终点 A 已定位但未追到触发点"}),
		},
	})
	if snap.UnresolvedAxes == nil || len(snap.UnresolvedAxes.DepthGaps) != 1 {
		t.Fatalf("expected final_answer-written sticky UnresolvedAxes, got %+v", snap.UnresolvedAxes)
	}

	// 接下来一轮 step_replan 不应抹掉 final_answer 写入的 sticky 三轴。
	snap = tracker.ApplyStepReplan("step-3", stepReplanUpdate{
		NextPhase: builtin_tools.AgentPhaseStep,
	})
	if snap.UnresolvedAxes == nil || len(snap.UnresolvedAxes.DepthGaps) != 1 {
		t.Fatalf("expected sticky UnresolvedAxes preserved through step_replan, got %+v", snap.UnresolvedAxes)
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
