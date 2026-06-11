package react

import (
	"encoding/json"
	"testing"

	"aster/internal/builtin_tools"
)

// TestParseSubmitReplanArgs_DuplicateIdRejected 校验 plan 内重复 id（最常见的结构错误）
// 在 parse 阶段被识别，让主循环走 retry 通道，而不是 phase 级 bail 到 final_answer。
// 注意：「merge 后 depends_on 引用不存在 id」属于 merge 后才能发现的错误，归 runStepReplanPhase
// 主循环里的 merge+normalize；这里只校验单 plan 内的本地结构错误能被 parse 识别。
func TestParseSubmitReplanArgs_DuplicateIdRejected(t *testing.T) {
	args := map[string]any{
		"should_replan": true,
		"replan_reason": "测试重复 id 触发 retry",
		"next_goal":     "x",
		"plan": []any{
			map[string]any{"id": "s2", "step": "a", "status": "pending", "depends_on": []any{}},
			map[string]any{"id": "s2", "step": "b", "status": "pending", "depends_on": []any{}},
		},
	}
	if _, err := parseSubmitReplanArgs(args); err == nil {
		t.Fatalf("expected error for duplicate id, got nil")
	}
}

// TestParseSubmitReplanArgs_PlanRequired 校验 should_replan=true 时 plan 必填非空，
// 否则进入 retry 循环（不丢缺口；缺口已经在 AI 用文件工具补到 open_items.md）。
func TestParseSubmitReplanArgs_PlanRequired(t *testing.T) {
	args := map[string]any{
		"should_replan": true,
		"replan_reason": "复核发现需要新增取证步骤",
		"next_goal":     "对补齐项做深度核验",
		"plan":          []any{},
	}
	if _, err := parseSubmitReplanArgs(args); err == nil {
		t.Fatalf("expected error when plan is empty under should_replan=true")
	}
}

// TestParseSubmitReplanArgs_PlanParsed 校验完整 plan 项能正确解析为 PlanItem。
func TestParseSubmitReplanArgs_PlanParsed(t *testing.T) {
	args := map[string]any{
		"should_replan": true,
		"replan_reason": "新增深挖步骤",
		"next_goal":     "深挖 alpha 模块",
		"plan": []any{
			map[string]any{
				"id":         "s2",
				"step":       "深挖 alpha 模块的触发点回溯（OI-001）",
				"status":     "pending",
				"depends_on": []any{"s1"},
			},
		},
	}
	decision, err := parseSubmitReplanArgs(args)
	if err != nil {
		t.Fatalf("parseSubmitReplanArgs failed: %v", err)
	}
	if !decision.ShouldReplan {
		t.Fatalf("expected should_replan=true")
	}
	if len(decision.Plan) != 1 {
		t.Fatalf("expected 1 plan item, got %d", len(decision.Plan))
	}
	item := decision.Plan[0]
	if item.ID != "s2" || string(item.Status) != "pending" {
		t.Fatalf("unexpected plan item: %+v", item)
	}
	if len(item.DependsOn) != 1 || item.DependsOn[0] != "s1" {
		t.Fatalf("unexpected depends_on: %v", item.DependsOn)
	}
}

// TestStepReplanModelOutput_PlanJSONTag 校验结构体 json tag 为 plan。
func TestStepReplanModelOutput_PlanJSONTag(t *testing.T) {
	out := stepReplanModelOutput{
		ShouldReplan: true,
		Plan: []*builtin_tools.PlanItem{{
			ID: "s1", Step: "x", Status: builtin_tools.PlanStepPending,
		}},
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := back["plan"]; !ok {
		t.Fatalf("expected json key plan, got %s", string(raw))
	}
}

// TestBuildSubmitReplanFunctionTool_PlanSchema 校验工具 schema 含 plan 且列入 required；
// 三轴字段（incomplete_items / depth_gaps / new_surfaces）作为 step_replan 输出已删除。
func TestBuildSubmitReplanFunctionTool_PlanSchema(t *testing.T) {
	tool := buildSubmitReplanFunctionTool()
	params, ok := tool.Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("Parameters is not a map[string]any: %T", tool.Function.Parameters)
	}

	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("required is not []string: %T", params["required"])
	}
	foundRequired := false
	for _, r := range required {
		if r == "plan" {
			foundRequired = true
		}
		if r == "incomplete_items" || r == "depth_gaps" || r == "new_surfaces" {
			t.Fatalf("axis field %q must not appear in required: %v", r, required)
		}
	}
	if !foundRequired {
		t.Fatalf("plan not in required: %v", required)
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties is not a map: %T", params["properties"])
	}
	if _, ok := props["plan"]; !ok {
		t.Fatalf("plan missing from properties: %v", props)
	}
	for _, axis := range []string{"incomplete_items", "depth_gaps", "new_surfaces"} {
		if _, ok := props[axis]; ok {
			t.Fatalf("axis field %q must not appear in properties: %v", axis, props)
		}
	}
}
