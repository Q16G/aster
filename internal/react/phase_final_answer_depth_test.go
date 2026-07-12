package react

import (
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

// TestParseFinalAnswerOutput_NoThreeAxes 校验 final_answer 退回纯裁决闸门后，仅凭
// is_complete/status/reason/should_replan/user_message 等字段即可解析（不再要求三轴）。
func TestParseFinalAnswerOutput_NoThreeAxes(t *testing.T) {
	raw := `{"is_complete":false,"status":"running","reason":"仍有链条断裂","should_replan":true,"warnings":[],"user_message":"总结","references":[]}`
	out, err := parseFinalAnswerOutput(raw)
	if err != nil {
		t.Fatalf("parseFinalAnswerOutput failed: %v", err)
	}
	if out.Reason != "仍有链条断裂" || !out.ShouldReplan {
		t.Fatalf("unexpected decision fields: %+v", out)
	}
	decision := normalizeFinalAnswerDecision(out)
	if decision.isTerminal {
		t.Fatalf("expected non-terminal decision for running status")
	}
}

// TestSubmitFinalAnswerSchema_DropsThreeAxes 校验 submit_final_answer schema 不再暴露三轴/next_goal
// 字段（三轴唯一来源=step_replan，final_answer 只产是否完成 + 未完成原因）。
func TestSubmitFinalAnswerSchema_DropsThreeAxes(t *testing.T) {
	schema := builtin_tools.NewSubmitFinalAnswerTool().Parameters()
	blob := prettyJSON(schema)
	for _, banned := range []string{"incomplete_items", "depth_gaps", "new_surfaces", "next_goal"} {
		if strings.Contains(blob, banned) {
			t.Fatalf("submit_final_answer schema should not contain %q, got:\n%s", banned, blob)
		}
	}
}
