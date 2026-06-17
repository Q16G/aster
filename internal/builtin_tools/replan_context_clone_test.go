package builtin_tools

import (
	"encoding/json"
	"testing"
)

// TestCloneReplanContext_DepthGaps 校验轴②深度字段被深拷贝，且改动副本不影响原对象。
func TestCloneReplanContext_DepthGaps(t *testing.T) {
	in := &ReplanContext{
		SourceStepID:    "step-2",
		IncompleteItems: NewAxisItems([]string{"i1"}),
		DepthGaps:       NewAxisItems([]string{"d1", "d2"}),
		NewSurfaces:     NewAxisItems([]string{"s1"}),
	}
	out := CloneReplanContext(in)
	if out == nil {
		t.Fatal("clone returned nil")
	}
	if len(out.DepthGaps) != 2 || out.DepthGaps[0].Item != "d1" || out.DepthGaps[1].Item != "d2" {
		t.Fatalf("depth_gaps not cloned: %v", out.DepthGaps)
	}
	// 深拷贝：改动副本不应回写原对象。
	out.DepthGaps[0].Item = "mutated"
	if in.DepthGaps[0].Item != "d1" {
		t.Fatalf("expected deep copy, but mutation leaked to source: %v", in.DepthGaps)
	}
}

// TestCloneReplanContext_CurrentPhase 校验阶段焦点字段被克隆且 trim。
func TestCloneReplanContext_CurrentPhase(t *testing.T) {
	in := &ReplanContext{
		SourceStepID: "step-3",
		CurrentPhase: "  分析模块 A 的访问控制  ",
	}
	out := CloneReplanContext(in)
	if out == nil {
		t.Fatal("clone returned nil")
	}
	if out.CurrentPhase != "分析模块 A 的访问控制" {
		t.Fatalf("CurrentPhase not trimmed/cloned: %q", out.CurrentPhase)
	}
	// 改动副本不回写
	out.CurrentPhase = "mutated"
	if in.CurrentPhase != "  分析模块 A 的访问控制  " {
		t.Fatalf("mutation leaked: %q", in.CurrentPhase)
	}
}

// TestAxisItem_UnmarshalStringCompat 校验三轴条目兼容字符串形态（旧 prompt 输出 / 旧持久化）。
func TestAxisItem_UnmarshalStringCompat(t *testing.T) {
	var rc ReplanContext
	raw := `{"incomplete_items":["登出接口未测"],"depth_gaps":[{"item":"链路未追透","evidence":"timeline L12","ledger_id":"OI-003"}]}`
	if err := json.Unmarshal([]byte(raw), &rc); err != nil {
		t.Fatalf("unmarshal mixed forms: %v", err)
	}
	if len(rc.IncompleteItems) != 1 || rc.IncompleteItems[0].Item != "登出接口未测" {
		t.Fatalf("string form not upgraded: %+v", rc.IncompleteItems)
	}
	if len(rc.DepthGaps) != 1 || rc.DepthGaps[0].Evidence != "timeline L12" || rc.DepthGaps[0].LedgerID != "OI-003" {
		t.Fatalf("object form lost fields: %+v", rc.DepthGaps)
	}
}

// TestAxisItemStrings_EvidenceAnnotation 校验投影字符串带证据附注。
func TestAxisItemStrings_EvidenceAnnotation(t *testing.T) {
	got := AxisItemStrings([]*AxisItem{
		{Item: "模块 X 未深测", Evidence: "scan 输出仅 1 项"},
		{Item: "纯条目"},
		nil,
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 items, got %v", got)
	}
	if got[0] != "模块 X 未深测（证据: scan 输出仅 1 项）" || got[1] != "纯条目" {
		t.Fatalf("unexpected projection: %v", got)
	}
}
