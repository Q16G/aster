package react

import (
	"encoding/json"
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

// TestSubmitResultToolSchema 锁 submit_result 的 schema：required=[result]、status 枚举。
func TestSubmitResultToolSchema(t *testing.T) {
	tool := builtin_tools.NewSubmitResultTool()
	if tool.Name() != builtin_tools.SubmitResultToolName {
		t.Fatalf("name = %q", tool.Name())
	}
	raw, err := json.Marshal(tool.Parameters())
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required, _ := schema["required"].([]any)
	if len(required) != 1 || required[0] != "result" {
		t.Fatalf("required must be [result], got %v", required)
	}
	props, _ := schema["properties"].(map[string]any)
	status, _ := props["status"].(map[string]any)
	enum, _ := status["enum"].([]any)
	if len(enum) != 2 {
		t.Fatalf("status enum must have 2 values, got %v", enum)
	}
}

func TestBuildSubAgentEnvelope_ShortResultInline(t *testing.T) {
	result := &builtin_tools.RunResult{
		Success:    true,
		Result:     "结论：端口=8080",
		TurnStatus: "succeeded",
		Usage:      &builtin_tools.SubAgentUsage{ToolUses: 2, Tokens: 100, DurationMs: 5},
	}
	out := buildSubAgentEnvelope("sub-x", t.TempDir(), result)
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env["status"] != "completed" {
		t.Fatalf("status = %v", env["status"])
	}
	if !strings.Contains(env["result"].(string), "8080") {
		t.Fatalf("short result should be inline, got %v", env["result"])
	}
	if _, ok := env["result_file"]; ok {
		t.Fatalf("short result must not spill to result_file")
	}
	if _, ok := env["usage"]; !ok {
		t.Fatalf("envelope must carry usage")
	}
	// 删除的旧字段不得回归。
	for _, banned := range []string{"ok", "agent_name", "plan_summary"} {
		if _, ok := env[banned]; ok {
			t.Fatalf("banned legacy field %q present in envelope", banned)
		}
	}
}

func TestBuildSubAgentEnvelope_LongResultSpills(t *testing.T) {
	long := strings.Repeat("配置项 ", 2000) // 远超 maxAsyncNotificationRunes
	result := &builtin_tools.RunResult{Success: true, Result: long, TurnStatus: "succeeded"}
	out := buildSubAgentEnvelope("sub-y", t.TempDir(), result)
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if _, ok := env["result_file"]; !ok {
		t.Fatalf("long result must spill to result_file, env=%v", env)
	}
	if _, ok := env["result"]; ok {
		t.Fatalf("long result must not inline full text (only result_file pointer)")
	}
	summary, _ := env["summary"].(string)
	if !strings.Contains(summary, "truncated") || len([]rune(summary)) >= len([]rune(long)) {
		t.Fatalf("summary must be truncated (marker + shorter than full), got %d runes", len([]rune(summary)))
	}
}

func TestBuildSubAgentEnvelope_FailedNoResult(t *testing.T) {
	out := buildSubAgentEnvelope("sub-z", t.TempDir(), &builtin_tools.RunResult{Success: false, Error: "boom"})
	var env map[string]any
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env["status"] != "failed" {
		t.Fatalf("status = %v, want failed", env["status"])
	}
	if env["error"] != "boom" {
		t.Fatalf("error = %v", env["error"])
	}
}

// TestBuildSubAgentPromptRender 锁 P4 prompt 渲染：类型描述注入 system、任务/上下文进 user、全文禁 emoji。
func TestBuildSubAgentPromptRender(t *testing.T) {
	pm, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("prompt manager: %v", err)
	}
	spec := resolveSubAgentType("explore")
	parts, err := pm.BuildSubAgentPrompt(SubAgentPromptInput{
		AgentProfile: AgentProfile{AgentRole: "资深审计员"},
		TypeExtra:    spec.SystemPromptExtra,
		Task:         "查找入口函数",
		Context:      "委派上下文：仓库在 /repo",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(parts.SystemRules, "sub_agent") {
		t.Errorf("system 应含 sub_agent 阶段说明")
	}
	if !strings.Contains(parts.SystemRules, "本次委派类型") || !strings.Contains(parts.SystemRules, "检索取证") {
		t.Errorf("system 应注入类型描述")
	}
	if !strings.Contains(parts.SystemRules, "资深审计员") {
		t.Errorf("system 应含注入的 Role")
	}
	if !strings.Contains(parts.User, "查找入口函数") {
		t.Errorf("user 应含委派任务")
	}
	if !strings.Contains(parts.User, "/repo") {
		t.Errorf("user 应含随附上下文")
	}
	assertNoEmojiSubAgent(t, "system", parts.SystemRules)
	assertNoEmojiSubAgent(t, "user", parts.User)
}

func assertNoEmojiSubAgent(t *testing.T, label, s string) {
	t.Helper()
	for _, r := range s {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) {
			t.Errorf("%s 含 emoji/符号 U+%04X", label, r)
			return
		}
	}
}
