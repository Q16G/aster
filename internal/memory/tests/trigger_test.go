package memory_test

import (
	. "aster/internal/memory"
	"strings"
	"testing"
)

func TestRenderTriggerPrompt_EncouragesEmptyGraphForLowDensityInput(t *testing.T) {
	prompt, err := RenderTriggerPrompt("source=step_window", nil, "StepWindow\nstep_id: auto-1\nwindow_summary: 当前窗口只有低密度会话信息。")
	if err != nil {
		t.Fatalf("RenderTriggerPrompt failed: %v", err)
	}

	mustContain := []string{
		"先判断：输入中是否存在值得长期保留的高密度、稳定知识增量",
		`{"nodes":[],"internal_triplets":[],"external_triplets":[]}`,
		"空图是合法结果，表示“当前无高密度信息”",
		"脱离当前轮次、当前调度过程和当前对话瞬时上下文后仍然成立的稳定知识",
		"仅服务于推进当前对话的瞬时信息，而不能作为后续检索、复用或上下文恢复的长期知识，则默认不建图",
	}
	for _, needle := range mustContain {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", needle, prompt)
		}
	}
}
