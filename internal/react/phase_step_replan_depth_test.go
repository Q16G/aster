package react

import (
	"context"
	"encoding/json"
	"testing"
)

// TestSubmitReplanTool_ReplanReasonRequired 校验 should_replan=true 时 replan_reason 必填。
func TestSubmitReplanTool_ReplanReasonRequired(t *testing.T) {
	args := map[string]any{
		"should_replan":    true,
		"replan_reason":    "",
		"incomplete_items": []any{"接口 B 从未覆盖"},
	}
	tool := newSubmitReplanTool()
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatalf("expected error when replan_reason is empty under should_replan=true")
	}
}

// TestSubmitReplanTool_NoReplanSucceeds 校验 should_replan=false 时可以无三轴内容。
func TestSubmitReplanTool_NoReplanSucceeds(t *testing.T) {
	args := map[string]any{
		"should_replan":      false,
		"replan_reason":      "",
		"current_phase_done": true,
	}
	tool := newSubmitReplanTool()
	out, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", out)
	}
	result := tool.getResult()
	if result == nil {
		t.Fatalf("result should be stored after successful Execute")
	}
	if result.ShouldReplan {
		t.Fatalf("expected should_replan=false")
	}
}

// TestSubmitReplanTool_ThreeAxesParsed 校验三轴字段正确解析。
func TestSubmitReplanTool_ThreeAxesParsed(t *testing.T) {
	args := map[string]any{
		"should_replan":    true,
		"replan_reason":    "发现多类缺口",
		"incomplete_items": []any{"接口 B 从未覆盖（evidence: 目录清单第 7 行）"},
		"depth_gaps":       []any{"auth 结论停在 JWT 层，未回溯到签发逻辑（evidence: digest 第 12 行）"},
		"new_surfaces":     []any{"同目录下 /api/user/delete 未审计（evidence: 文件扫描）"},
	}
	tool := newSubmitReplanTool()
	_, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	result := tool.getResult()
	if result == nil {
		t.Fatalf("result not stored")
	}
	if !result.ShouldReplan {
		t.Fatalf("expected should_replan=true")
	}
	if len(result.IncompleteItems) != 1 {
		t.Fatalf("expected 1 incomplete_item, got %d", len(result.IncompleteItems))
	}
	if len(result.DepthGaps) != 1 {
		t.Fatalf("expected 1 depth_gap, got %d", len(result.DepthGaps))
	}
	if len(result.NewSurfaces) != 1 {
		t.Fatalf("expected 1 new_surface, got %d", len(result.NewSurfaces))
	}
}

// TestStepReplanModelOutput_ThreeAxesJSONTags 校验三轴字段 json tag 正确，且无 plan / 已删字段。
func TestStepReplanModelOutput_ThreeAxesJSONTags(t *testing.T) {
	out := stepReplanModelOutput{
		ShouldReplan:    true,
		ReplanReason:    "test",
		IncompleteItems: []string{"item-a"},
		DepthGaps:       []string{"gap-b"},
		NewSurfaces:     []string{"surface-c"},
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	for _, key := range []string{"incomplete_items", "depth_gaps", "new_surfaces"} {
		if _, ok := back[key]; !ok {
			t.Fatalf("expected json key %q, got %s", key, string(raw))
		}
	}
	// 职责反转：step_replan 不再产出 next_phase / phase_shape_issue / next_goal。
	for _, removed := range []string{"plan", "next_phase", "phase_shape_issue", "next_goal"} {
		if _, ok := back[removed]; ok {
			t.Fatalf("removed field %q must not appear in stepReplanModelOutput json: %s", removed, string(raw))
		}
	}
}

// TestSubmitReplanTool_InPhaseDepthGap 校验 in-phase 保底场景的结构：
// 确认型发现未推深 → current_phase_done=false 且 depth_gaps 非空（planner 据此沿用当前 phase 深推）。
func TestSubmitReplanTool_InPhaseDepthGap(t *testing.T) {
	args := map[string]any{
		"should_replan":      true,
		"replan_reason":      "SQL 注入已检测坐实，但未按渗透角色职责链推进到利用/影响",
		"current_phase_done": false,
		"depth_gaps":         []any{"login.php?id 注入已确认，但未做利用与影响评估（evidence: result_file step-3 §2；确认型发现未按角色职责链推深）"},
	}
	tool := newSubmitReplanTool()
	_, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	result := tool.getResult()
	if result == nil {
		t.Fatalf("result not stored")
	}
	if result.CurrentPhaseDone {
		t.Fatalf("expected current_phase_done=false (confirmed finding not yet deepened)")
	}
	if len(result.DepthGaps) != 1 {
		t.Fatalf("expected 1 depth_gap, got %d", len(result.DepthGaps))
	}
}

// TestSubmitReplanTool_PhaseDoneWithQueuedBacklog 校验阶段闭环信号：
// current_phase_done=true 由 step_replan 报告（不附带选下一个 phase），planner 据信号决定切换。
func TestSubmitReplanTool_PhaseDoneWithQueuedBacklog(t *testing.T) {
	args := map[string]any{
		"should_replan":      true,
		"replan_reason":      "当前 phase 已从浅到深推到位，账本仍有同类对象排队",
		"current_phase_done": true,
		"incomplete_items":   []any{},
		"depth_gaps":         []any{},
		"new_surfaces":       []any{},
	}
	tool := newSubmitReplanTool()
	_, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	result := tool.getResult()
	if result == nil {
		t.Fatalf("result not stored")
	}
	if !result.CurrentPhaseDone {
		t.Fatalf("expected current_phase_done=true")
	}
	if !result.ShouldReplan {
		t.Fatalf("expected should_replan=true (backlog remains)")
	}
}

// TestStepReplanModelOutput_CurrentPhaseDoneJSONTag 校验 current_phase_done 的 json tag 正确，
// 且已删字段 next_phase / phase_shape_issue 不出现。
func TestStepReplanModelOutput_CurrentPhaseDoneJSONTag(t *testing.T) {
	out := stepReplanModelOutput{
		ShouldReplan:     true,
		ReplanReason:     "test",
		CurrentPhaseDone: true,
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := back["current_phase_done"]; !ok {
		t.Fatalf("expected json key current_phase_done present, got %s", string(raw))
	}
	for _, removed := range []string{"next_phase", "phase_shape_issue"} {
		if _, ok := back[removed]; ok {
			t.Fatalf("removed field %q must not appear: %s", removed, string(raw))
		}
	}
}

// TestSubmitReplanTool_ParametersSchema 校验 schema 含三轴 + current_phase_done，
// 不含 plan / 已删的 next_phase / phase_shape_issue / next_goal，且三轴非必填。
func TestSubmitReplanTool_ParametersSchema(t *testing.T) {
	tool := newSubmitReplanTool()
	params, ok := tool.Parameters().(map[string]any)
	if !ok {
		t.Fatalf("Parameters() is not map[string]any: %T", tool.Parameters())
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties is not a map: %T", params["properties"])
	}
	for _, field := range []string{"should_replan", "replan_reason", "current_phase_done", "incomplete_items", "depth_gaps", "new_surfaces"} {
		if _, ok := props[field]; !ok {
			t.Fatalf("field %q missing from properties", field)
		}
	}
	for _, removed := range []string{"plan", "next_phase", "phase_shape_issue", "next_goal"} {
		if _, ok := props[removed]; ok {
			t.Fatalf("removed field %q must not appear in submit_replan schema", removed)
		}
	}
	required, _ := params["required"].([]string)
	for _, axis := range []string{"incomplete_items", "depth_gaps", "new_surfaces"} {
		for _, r := range required {
			if r == axis {
				t.Fatalf("axis field %q must not be required (optional)", axis)
			}
		}
	}
}
