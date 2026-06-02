package builtin_tools

import "testing"

// TestCloneReplanContext_DepthGaps 校验轴②深度字段被深拷贝，且改动副本不影响原对象。
func TestCloneReplanContext_DepthGaps(t *testing.T) {
	in := &ReplanContext{
		SourceStepID:    "step-2",
		IncompleteItems: []string{"i1"},
		DepthGaps:       []string{"d1", "d2"},
		NewSurfaces:     []string{"s1"},
	}
	out := CloneReplanContext(in)
	if out == nil {
		t.Fatal("clone returned nil")
	}
	if len(out.DepthGaps) != 2 || out.DepthGaps[0] != "d1" || out.DepthGaps[1] != "d2" {
		t.Fatalf("depth_gaps not cloned: %v", out.DepthGaps)
	}
	// 深拷贝：改动副本不应回写原对象。
	out.DepthGaps[0] = "mutated"
	if in.DepthGaps[0] != "d1" {
		t.Fatalf("expected deep copy, but mutation leaked to source: %v", in.DepthGaps)
	}
}
