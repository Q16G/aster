package react

import (
	"encoding/json"
	"testing"

	"aster/internal/builtin_tools"
)

// TestParseSubmitReplanArgs_DepthGaps 校验轴②深度字段能从 submit_plan 参数正确解析。
func TestParseSubmitReplanArgs_DepthGaps(t *testing.T) {
	args := map[string]any{
		"should_replan":    true,
		"replan_reason":    "存在链条断裂与整体遗漏模块",
		"next_goal":        "深挖分析链路并补齐剩余模块",
		"incomplete_items": []string{"认证模块的登出接口未测"},
		"depth_gaps":       []string{"终点 A 已定位但未追到触发点", "浅层判断停在 shallow_only"},
		"new_surfaces":     []string{"admin 分包整体未覆盖"},
		"warnings":         []string{},
	}
	decision, err := parseSubmitReplanArgs(args)
	if err != nil {
		t.Fatalf("parseSubmitReplanArgs failed: %v", err)
	}
	if !decision.ShouldReplan {
		t.Fatalf("expected should_replan=true")
	}
	if len(decision.DepthGaps) != 2 {
		t.Fatalf("expected 2 depth_gaps, got %d: %v", len(decision.DepthGaps), decision.DepthGaps)
	}
	if decision.DepthGaps[0].Item != "终点 A 已定位但未追到触发点" {
		t.Fatalf("unexpected first depth_gap: %q", decision.DepthGaps[0])
	}
}

// TestStepReplanModelOutput_DepthGapsJSONTag 校验结构体 json tag 为 depth_gaps。
func TestStepReplanModelOutput_DepthGapsJSONTag(t *testing.T) {
	out := stepReplanModelOutput{DepthGaps: builtin_tools.NewAxisItems([]string{"x"})}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := back["depth_gaps"]; !ok {
		t.Fatalf("expected json key depth_gaps, got %s", string(raw))
	}
}

// TestBuildSubmitReplanFunctionTool_DepthGapsSchema 校验工具 schema 含 depth_gaps 且列入 required。
func TestBuildSubmitReplanFunctionTool_DepthGapsSchema(t *testing.T) {
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
		if r == "depth_gaps" {
			foundRequired = true
			break
		}
	}
	if !foundRequired {
		t.Fatalf("depth_gaps not in required: %v", required)
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties is not a map: %T", params["properties"])
	}
	if _, ok := props["depth_gaps"]; !ok {
		t.Fatalf("depth_gaps missing from properties: %v", props)
	}
}
