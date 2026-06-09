package react

import "testing"

// buildFinalAnswerRawJSON 构造一个含三轴字段的合法 final_answer 输出，便于按需删字段做缺字段校验。
func buildFinalAnswerRawJSON(omit string) string {
	fields := map[string]string{
		"is_complete":      `false`,
		"status":           `"running"`,
		"reason":           `"仍有链条断裂与整体遗漏模块"`,
		"should_replan":    `true`,
		"next_goal":        `"深挖分析链路并补齐剩余模块"`,
		"incomplete_items": `["登出接口未测"]`,
		"depth_gaps":       `["终点 A 已定位但未追到触发点","浅层判断停在 shallow_only"]`,
		"new_surfaces":     `["admin 分包整体未覆盖"]`,
		"warnings":         `[]`,
		"user_message":     `"总结"`,
		"references":       `[]`,
	}
	delete(fields, omit)
	var b []byte
	b = append(b, '{')
	first := true
	// 固定顺序无关紧要，JSON 对象键序不影响解析。
	for k, v := range fields {
		if !first {
			b = append(b, ',')
		}
		first = false
		b = append(b, '"')
		b = append(b, k...)
		b = append(b, '"', ':')
		b = append(b, v...)
	}
	b = append(b, '}')
	return string(b)
}

// TestParseFinalAnswerOutput_DepthGaps 校验轴②深度字段能从 final_answer 输出正确解析。
func TestParseFinalAnswerOutput_DepthGaps(t *testing.T) {
	out, err := parseFinalAnswerOutput(buildFinalAnswerRawJSON(""))
	if err != nil {
		t.Fatalf("parseFinalAnswerOutput failed: %v", err)
	}
	if len(out.DepthGaps) != 2 {
		t.Fatalf("expected 2 depth_gaps, got %d: %v", len(out.DepthGaps), out.DepthGaps)
	}
	if out.DepthGaps[0] != "终点 A 已定位但未追到触发点" {
		t.Fatalf("unexpected first depth_gap: %q", out.DepthGaps[0])
	}
}

// TestParseFinalAnswerOutput_DepthGapsRequired 校验缺失 depth_gaps 字段时解析报错（depth_gaps 已列入 required）。
func TestParseFinalAnswerOutput_DepthGapsRequired(t *testing.T) {
	if _, err := parseFinalAnswerOutput(buildFinalAnswerRawJSON("depth_gaps")); err == nil {
		t.Fatalf("expected error when depth_gaps missing, got nil")
	}
}

// TestNormalizeFinalAnswerDecision_DepthGapsPreserved 校验非终态决策的轴②深度项被保留，
// 后续会原样写入 sticky UnresolvedAxes.DepthGaps。
func TestNormalizeFinalAnswerDecision_DepthGapsPreserved(t *testing.T) {
	out, err := parseFinalAnswerOutput(buildFinalAnswerRawJSON(""))
	if err != nil {
		t.Fatalf("parseFinalAnswerOutput failed: %v", err)
	}
	decision := normalizeFinalAnswerDecision(out)
	if decision.isTerminal {
		t.Fatalf("expected non-terminal decision for running status")
	}
	foundDepth := false
	for _, m := range decision.model.DepthGaps {
		if m == "终点 A 已定位但未追到触发点" {
			foundDepth = true
			break
		}
	}
	if !foundDepth {
		t.Fatalf("expected depth gap preserved in decision.model.DepthGaps: %v", decision.model.DepthGaps)
	}
}
