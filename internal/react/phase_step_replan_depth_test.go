package react

import (
	"context"
	"encoding/json"
	"testing"
)

// TestSubmitReplanTool_NextGoalRequired 校验 should_replan=true 时 next_goal 必填。
func TestSubmitReplanTool_NextGoalRequired(t *testing.T) {
	args := map[string]any{
		"should_replan": true,
		"replan_reason": "发现存在性缺口",
		"next_goal":     "",
		"incomplete_items": []any{"接口 B 从未覆盖"},
	}
	tool := newSubmitReplanTool()
	_, err := tool.Execute(context.Background(), args)
	if err == nil {
		t.Fatalf("expected error when next_goal is empty under should_replan=true")
	}
}

// TestSubmitReplanTool_NoReplanSucceeds 校验 should_replan=false 时可以无三轴内容。
func TestSubmitReplanTool_NoReplanSucceeds(t *testing.T) {
	args := map[string]any{
		"should_replan": false,
		"replan_reason": "",
		"next_goal":     "",
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
		"should_replan": true,
		"replan_reason": "发现多类缺口",
		"next_goal":     "补齐接口 B 并深挖 auth",
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

// TestStepReplanModelOutput_ThreeAxesJSONTags 校验三轴字段 json tag 正确。
func TestStepReplanModelOutput_ThreeAxesJSONTags(t *testing.T) {
	out := stepReplanModelOutput{
		ShouldReplan:    true,
		ReplanReason:    "test",
		NextGoal:        "test",
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
	if _, ok := back["plan"]; ok {
		t.Fatalf("plan must not appear in stepReplanModelOutput json: %s", string(raw))
	}
}

// TestSubmitReplanTool_ParametersSchema 校验 schema 含三轴且无 plan 字段。
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
	for _, axis := range []string{"incomplete_items", "depth_gaps", "new_surfaces"} {
		if _, ok := props[axis]; !ok {
			t.Fatalf("axis field %q missing from properties", axis)
		}
	}
	if _, ok := props["plan"]; ok {
		t.Fatalf("plan must not appear in submit_replan schema")
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
