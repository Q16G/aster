package react

import (
	"context"
	"encoding/json"
	"testing"

	"aster/internal/builtin_tools"
)

// activeReplanTool：单 active phase-a（其下 step 全 completed，无 pending），
// 供单条 assessment 的用例——完整性校验只要求评估 phase-a。
func activeReplanTool(t *testing.T) *submitReplanTool {
	t.Helper()
	phases := []*builtin_tools.PlanPhase{
		{ID: "phase-a", Status: builtin_tools.PlanPhasePending},
	}
	plan := []*builtin_tools.PlanItem{
		{ID: "a1", Status: builtin_tools.PlanStepCompleted, PhaseID: "phase-a"},
	}
	return newSubmitReplanTool(phases, plan)
}

// pendingPhaseReplanTool：单 active phase-b，其下仍有 pending step，
// 供 completed-with-pending 守卫测试。
func pendingPhaseReplanTool(t *testing.T) *submitReplanTool {
	t.Helper()
	phases := []*builtin_tools.PlanPhase{
		{ID: "phase-b", Status: builtin_tools.PlanPhasePending},
	}
	plan := []*builtin_tools.PlanItem{
		{ID: "b1", Status: builtin_tools.PlanStepPending, PhaseID: "phase-b"},
	}
	return newSubmitReplanTool(phases, plan)
}

// TestSubmitReplanTool_ReplanReasonRequired 校验 should_replan=true 时 replan_reason 必填。
func TestSubmitReplanTool_ReplanReasonRequired(t *testing.T) {
	args := map[string]any{
		"should_replan": true,
		"replan_reason": "",
		"phase_assessments": []any{
			map[string]any{"phase_id": "phase-a", "status": "continue"},
		},
	}
	if _, err := activeReplanTool(t).Execute(context.Background(), args); err == nil {
		t.Fatalf("expected error when replan_reason is empty under should_replan=true")
	}
}

// TestSubmitReplanTool_NoReplanSucceeds 校验 should_replan=false + 全 completed/blocked 可通过。
func TestSubmitReplanTool_NoReplanSucceeds(t *testing.T) {
	args := map[string]any{
		"should_replan": false,
		"replan_reason": "",
		"phase_assessments": []any{
			map[string]any{"phase_id": "phase-a", "status": "completed"},
		},
	}
	out, err := activeReplanTool(t).Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", out)
	}
}

// TestSubmitReplanTool_PhaseAssessmentsParsed 校验 phase_assessments 与其三轴正确解析。
func TestSubmitReplanTool_PhaseAssessmentsParsed(t *testing.T) {
	args := map[string]any{
		"should_replan": true,
		"replan_reason": "phase-a 仍有深度缺口",
		"phase_assessments": []any{
			map[string]any{
				"phase_id":         "phase-a",
				"status":           "continue",
				"incomplete_items": []any{"接口 B 从未覆盖"},
				"depth_gaps":       []any{"auth 结论停在 JWT 层"},
				"new_surfaces":     []any{"/api/user/delete 未审计"},
			},
		},
	}
	tool := activeReplanTool(t)
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	result := tool.getResult()
	if result == nil || !result.ShouldReplan || len(result.PhaseAssessments) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	a := result.PhaseAssessments[0]
	if a.PhaseID != "phase-a" || a.Status != builtin_tools.PhaseAssessContinue {
		t.Fatalf("assessment mismatch: %+v", a)
	}
	if len(a.IncompleteItems) != 1 || len(a.DepthGaps) != 1 || len(a.NewSurfaces) != 1 {
		t.Fatalf("axes not parsed: %+v", a)
	}
}

// TestSubmitReplanTool_UnknownPhaseIDRejected 校验 phase_id 必须是本轮 active phase。
func TestSubmitReplanTool_UnknownPhaseIDRejected(t *testing.T) {
	args := map[string]any{
		"should_replan": true,
		"replan_reason": "x",
		"phase_assessments": []any{
			map[string]any{"phase_id": "phase-ghost", "status": "continue"},
		},
	}
	if _, err := activeReplanTool(t).Execute(context.Background(), args); err == nil {
		t.Fatal("expected error for phase_id not in active phases")
	}
}

// TestSubmitReplanTool_CompletedWithPendingRejected 校验 D7a①：completed 的 phase
// 若仍有 pending step 则拒绝。
func TestSubmitReplanTool_CompletedWithPendingRejected(t *testing.T) {
	args := map[string]any{
		"should_replan": false,
		"replan_reason": "",
		"phase_assessments": []any{
			// phase-b 仍有 pending step b1
			map[string]any{"phase_id": "phase-b", "status": "completed"},
		},
	}
	if _, err := pendingPhaseReplanTool(t).Execute(context.Background(), args); err == nil {
		t.Fatal("expected error: completed phase must not have pending steps")
	}
}

// TestSubmitReplanTool_ContinueRequiresShouldReplan 校验 D7a②：存在 continue ⇒ should_replan=true。
func TestSubmitReplanTool_ContinueRequiresShouldReplan(t *testing.T) {
	args := map[string]any{
		"should_replan": false,
		"replan_reason": "",
		"phase_assessments": []any{
			map[string]any{"phase_id": "phase-a", "status": "continue"},
		},
	}
	if _, err := activeReplanTool(t).Execute(context.Background(), args); err == nil {
		t.Fatal("expected error: continue assessment requires should_replan=true")
	}
}

// TestSubmitReplanTool_BlockedAccepted 校验 blocked 状态被接受（其下 pending 由 runtime 收敛）。
func TestSubmitReplanTool_BlockedAccepted(t *testing.T) {
	args := map[string]any{
		"should_replan": false,
		"replan_reason": "",
		"phase_assessments": []any{
			map[string]any{"phase_id": "phase-b", "status": "blocked", "reason": "外部依赖不可用"},
		},
	}
	tool := pendingPhaseReplanTool(t)
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("blocked assessment should be accepted: %v", err)
	}
	if tool.getResult().PhaseAssessments[0].Status != builtin_tools.PhaseAssessBlocked {
		t.Fatal("expected blocked status stored")
	}
}

// TestSubmitReplanTool_IncompleteCoverageRejected 校验完整性守卫：本轮多个 active phase
// 但只评估其一 → 拒绝（漏评的 lane 会被静默跳过）。ACTIVE_PHASES 为空时豁免。
func TestSubmitReplanTool_IncompleteCoverageRejected(t *testing.T) {
	phases := []*builtin_tools.PlanPhase{
		{ID: "phase-a", Status: builtin_tools.PlanPhasePending},
		{ID: "phase-b", Status: builtin_tools.PlanPhasePending},
	}
	plan := []*builtin_tools.PlanItem{
		{ID: "a1", Status: builtin_tools.PlanStepCompleted, PhaseID: "phase-a"},
		{ID: "b1", Status: builtin_tools.PlanStepPending, PhaseID: "phase-b"},
	}
	tool := newSubmitReplanTool(phases, plan)
	// 只评估 phase-a，漏 phase-b
	args := map[string]any{
		"should_replan": false,
		"replan_reason": "",
		"phase_assessments": []any{
			map[string]any{"phase_id": "phase-a", "status": "completed"},
		},
	}
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("expected error: not all active phases assessed")
	}

	// 空 active phase（simple/历史 session）→ 空 assessments 豁免
	emptyTool := newSubmitReplanTool(nil, nil)
	if _, err := emptyTool.Execute(context.Background(), map[string]any{
		"should_replan": false, "replan_reason": "", "phase_assessments": []any{},
	}); err != nil {
		t.Fatalf("empty active phases should exempt completeness: %v", err)
	}
}

// TestSubmitReplanTool_DuplicateAssessmentRejected 校验同一 phase 重复评估被拒。
func TestSubmitReplanTool_DuplicateAssessmentRejected(t *testing.T) {
	tool := activeReplanTool(t)
	args := map[string]any{
		"should_replan": true,
		"replan_reason": "x",
		"phase_assessments": []any{
			map[string]any{"phase_id": "phase-a", "status": "continue"},
			map[string]any{"phase_id": "phase-a", "status": "completed"},
		},
	}
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("expected error for duplicate phase assessment")
	}
}

// TestStepReplanModelOutput_JSONTags 校验输出 json tag：含 phase_assessments，
// 不含已删的 current_phase_done / 顶层三轴 / next_phase。
func TestStepReplanModelOutput_JSONTags(t *testing.T) {
	out := stepReplanModelOutput{
		ShouldReplan: true,
		ReplanReason: "test",
		PhaseAssessments: []*builtin_tools.PhaseAssessment{
			{PhaseID: "phase-a", Status: builtin_tools.PhaseAssessContinue},
		},
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := back["phase_assessments"]; !ok {
		t.Fatalf("expected json key phase_assessments, got %s", string(raw))
	}
	for _, removed := range []string{"current_phase_done", "incomplete_items", "depth_gaps", "new_surfaces", "next_phase", "plan"} {
		if _, ok := back[removed]; ok {
			t.Fatalf("removed field %q must not appear: %s", removed, string(raw))
		}
	}
}

// TestSubmitReplanTool_ParametersSchema 校验 schema 含 phase_assessments，required 正确，
// 不含已删字段。
func TestSubmitReplanTool_ParametersSchema(t *testing.T) {
	params := activeReplanTool(t).Parameters().(map[string]any)
	props := params["properties"].(map[string]any)
	for _, field := range []string{"should_replan", "replan_reason", "phase_assessments"} {
		if _, ok := props[field]; !ok {
			t.Fatalf("field %q missing from properties", field)
		}
	}
	for _, removed := range []string{"current_phase_done", "incomplete_items", "depth_gaps", "new_surfaces", "next_phase", "plan"} {
		if _, ok := props[removed]; ok {
			t.Fatalf("removed field %q must not appear in submit_replan schema", removed)
		}
	}
	required, _ := params["required"].([]string)
	hasPA := false
	for _, r := range required {
		if r == "phase_assessments" {
			hasPA = true
		}
	}
	if !hasPA {
		t.Fatal("phase_assessments must be required")
	}
}
