package react_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	. "aster/internal/react"
)

// TestFocusConstraint_PlannerLive 使用真实 LLM 验证用户聚焦约束是否生效。
// 构造一个带 SKILLS_INDEX 的审计场景，用户明确说"重点关注 RCE 和 SQL 注入"，
// 验证 planner 不会为 auth-authz、client-side-sec、config-sec 等非聚焦方向安排步骤。
//
// 启用方式：SASTPRO_REACT_LIVE_TEST=1 go test ./internal/react/tests/... -run TestFocusConstraint -v
func TestFocusConstraint_PlannerLive(t *testing.T) {
	if os.Getenv("SASTPRO_REACT_LIVE_TEST") != "1" {
		t.Skip("live test disabled; set SASTPRO_REACT_LIVE_TEST=1")
	}

	client := newOpenCodeGoClient(t)

	// 构造带聚焦方向的 snapshot
	now := time.Now()
	snapshot := builtin_tools.StateSnapshot{
		Phase:       builtin_tools.AgentPhasePlan,
		Status:      builtin_tools.TaskStatusRunning,
		CurrentGoal: "对项目进行安全审计，重点关注 RCE 和 SQL 注入",
		PlanVersion: 0,
		InputTimeline: []*builtin_tools.TimelineInput{
			{
				Content:   "请对 /repo/project 目录下的 Java Web 项目进行安全审计，着重关注 RCE 和 SQL 注入这两个方向",
				CreatedAt: now,
			},
		},
	}

	// 构造 skills index (模拟真实场景，包含多个维度的 skill)
	skillsCtx := &SkillsPromptContext{
		Table: `| name | description | when-to-use | status |
|------|-------------|-------------|--------|
| security-code-analysis | P0 总控路由 — 信号路由、覆盖维度编排 | 做系统性代码安全审计时首先加载 | available |
| sast-scan | 结构化漏洞扫描（RCE/SQLi/XXE/SSRF/XSS） | 需要静态分析扫描代码漏洞时 | available |
| dataflow-analysis | 跨函数数据流验证 | 验证 source-to-sink 可达性 | available |
| auth-authz | 认证授权路由 — 认证/授权/IDOR/CSRF | 项目有登录、会话管理、权限判断时 | available |
| client-side-sec | 客户端安全路由 — CSP/DOM XSS/postMessage | 项目有前端安全敏感 JS 逻辑时 | available |
| config-sec | 配置安全路由 — 密钥泄露/安全头/危险配置 | 项目有配置文件或安全响应头设置时 | available |
| dependency-audit | 依赖/供应链审计 | 存在 pom.xml/package.json 等依赖文件时 | available |
| stored-xss-detection | 存储型 XSS 检测 | 存在数据写入后读出并渲染的流程时 | available |`,
	}

	// Agent instruction (来自 code-audit profile，包含我们新增的"用户意图优先"条款)
	agentInstruction := `你是代码安全审计 Agent。

审计要求：
- 首先加载 security-code-analysis（P0 总控路由），它定义了信号路由表和覆盖维度，指导后续 skill 的加载和编排
- **用户意图优先**：当用户明确指定审计方向时，计划和执行必须聚焦在用户指定的方向内，不要主动扩展到用户未提及的维度。MUST 覆盖维度仅在全量审计（用户未指定方向）时才作为强制要求
- 全量审计时，分析手段和顺序根据项目实际情况和可用工具集灵活安排，必须满足 P0 Router 定义的 MUST 覆盖维度`

	planInput := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{
		UserInstruction: agentInstruction,
	})
	if planInput == "" {
		t.Fatal("PlannerInputFromSnapshot returned empty")
	}

	planner := NewDefaultTaskPlanner(client)
	prompt, err := planner.BuildPrompt(TaskPlannerPromptInput{
		Input:          planInput,
		SkillsContext:  skillsCtx,
		HasSkillsTable: true,
	})
	if err != nil {
		t.Fatalf("BuildPrompt failed: %v", err)
	}

	// 打印渲染后的完整 prompt
	t.Logf("=== RENDERED PLANNER PROMPT (%d bytes) ===", len(prompt))
	// 保存到文件方便查看
	dumpPath := "/tmp/focus_test_planner_prompt.txt"
	os.WriteFile(dumpPath, []byte(prompt), 0o644)
	t.Logf("Prompt saved to %s", dumpPath)

	// 发送到 LLM
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	t.Log("Sending prompt to LLM...")
	resp, err := client.Chat(ctx, ai.NewSystemMsgInfo(prompt))
	if err != nil {
		t.Fatalf("LLM call failed: %v", err)
	}

	t.Logf("=== LLM RESPONSE ===\n%s", resp)

	// 保存响应
	respPath := "/tmp/focus_test_planner_response.txt"
	os.WriteFile(respPath, []byte(resp), 0o644)
	t.Logf("Response saved to %s", respPath)

	// 基本验证：响应不应包含非聚焦方向的步骤关键词
	nonFocusKeywords := []string{
		"auth-authz", "认证授权",
		"client-side", "客户端安全", "CSP",
		"config-sec", "配置安全", "密钥泄露",
		"dependency-audit", "依赖审计", "供应链",
		"stored-xss", "存储型 XSS",
	}

	respLower := strings.ToLower(resp)
	var violations []string
	for _, kw := range nonFocusKeywords {
		if strings.Contains(respLower, strings.ToLower(kw)) {
			violations = append(violations, kw)
		}
	}

	if len(violations) > 0 {
		t.Errorf("LLM response contains non-focus keywords (聚焦约束可能未生效): %v", violations)
		t.Logf("Full response:\n%s", resp)
	} else {
		t.Log("PASS: No non-focus direction keywords found in plan")
	}

	// 尝试解析 JSON 响应
	var result map[string]any
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		// 可能是 markdown 包裹的 JSON，尝试提取
		if start := strings.Index(resp, "{"); start >= 0 {
			if end := strings.LastIndex(resp, "}"); end > start {
				json.Unmarshal([]byte(resp[start:end+1]), &result)
			}
		}
	}

	if result != nil {
		if plan, ok := result["plan"].([]any); ok {
			t.Logf("Plan has %d steps:", len(plan))
			for i, step := range plan {
				if m, ok := step.(map[string]any); ok {
					t.Logf("  step %d: %s [%s]", i+1, m["step"], m["status"])
				}
			}
		}
		if explanation, ok := result["explanation"].(string); ok {
			t.Logf("Explanation: %s", explanation)
		}
	}
}

// TestFocusConstraint_FinalAnswerLive 验证 FinalAnswer 在聚焦场景下不会因为非聚焦维度未覆盖而触发 replan
func TestFocusConstraint_FinalAnswerLive(t *testing.T) {
	if os.Getenv("SASTPRO_REACT_LIVE_TEST") != "1" {
		t.Skip("live test disabled; set SASTPRO_REACT_LIVE_TEST=1")
	}

	client := newOpenCodeGoClient(t)

	now := time.Now()
	agent, err := NewReActAgent(
		"focus-final-answer-test",
		client,
		WithEmitter(NewDummyEmitter()),
		WithInstruction("你是代码安全审计 Agent"),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	// 构造一个聚焦 RCE/SQL 的完成状态
	snapshot := builtin_tools.StateSnapshot{
		Phase:       builtin_tools.AgentPhaseFinalAnswer,
		Status:      builtin_tools.TaskStatusRunning,
		CurrentGoal: "",
		PlanVersion: 1,
		InputTimeline: []*builtin_tools.TimelineInput{
			{
				Content:   "请对项目进行安全审计，着重关注 RCE 和 SQL 注入",
				CreatedAt: now.Add(-15 * time.Minute),
			},
		},
		Plan: []*builtin_tools.PlanItem{
			{ID: "step-1", Step: "聚焦 RCE/SQL 方向 — 收集项目结构和入口点", Status: builtin_tools.PlanStepCompleted},
			{ID: "step-2", Step: "聚焦 RCE/SQL 方向 — SAST 扫描 RCE 和 SQL 注入模式", Status: builtin_tools.PlanStepCompleted, DependsOn: []string{"step-1"}},
			{ID: "step-3", Step: "聚焦 RCE/SQL 方向 — 数据流验证发现的候选漏洞", Status: builtin_tools.PlanStepCompleted, DependsOn: []string{"step-2"}},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{
				StepID:       "step-1",
				Status:       builtin_tools.StepOutcomeCompleted,
				ShortSummary: "已收集项目结构，发现 15 个 controller 和 8 个 mapper 文件",
				KeyFacts:     []string{"Java Spring Boot 项目", "MyBatis 做 SQL 映射", "存在 ProcessBuilder 调用"},
				UpdatedAt:    now.Add(-10 * time.Minute),
			},
			{
				StepID:       "step-2",
				Status:       builtin_tools.StepOutcomeCompleted,
				ShortSummary: "SAST 扫描完成，发现 4 处 SQL 注入和 2 处 RCE",
				KeyFacts:     []string{"SQL注入: UserMapper.xml:45, OrderMapper.xml:82, SearchMapper.xml:31, ReportMapper.xml:67", "RCE: FileController.java:120 (ProcessBuilder), AdminController.java:85 (Runtime.exec)"},
				UpdatedAt:    now.Add(-7 * time.Minute),
			},
			{
				StepID:       "step-3",
				Status:       builtin_tools.StepOutcomeCompleted,
				ShortSummary: "数据流验证确认 3 处 SQL 注入和 2 处 RCE 为真阳性",
				LongSummary:  "经数据流验证，4 处 SQL 注入中 3 处确认 source-to-sink 可达（ReportMapper 的参数来自内部常量，排除）。2 处 RCE 均确认用户输入可达。",
				KeyFacts:     []string{"confirmed SQL注入: 3处", "confirmed RCE: 2处", "false positive: 1处 (ReportMapper)"},
				UpdatedAt:    now.Add(-3 * time.Minute),
			},
		},
		Warnings:   []string{},
		Unresolved: []string{},
	}

	prompt, err := agent.BuildFinalAnswerPrompt(map[string]any{
		"status":         builtin_tools.TaskStatusRunning,
		"state_error":    "",
		"input_timeline": snapshot.InputTimeline,
		"show_plan":      true,
		"plan":           snapshot.Plan,
		"plan_version":   snapshot.PlanVersion,
		"step_outcomes":  snapshot.StepOutcomes,
		"warnings":       snapshot.Warnings,
		"unresolved":     snapshot.Unresolved,
	})
	if err != nil {
		t.Fatalf("BuildFinalAnswerPrompt failed: %v", err)
	}
	if prompt == "" {
		t.Fatal("BuildFinalAnswerPrompt returned empty")
	}

	// 打印渲染后的完整 prompt
	t.Logf("=== RENDERED FINAL ANSWER PROMPT (%d bytes) ===", len(prompt))
	dumpPath := "/tmp/focus_test_final_answer_prompt.txt"
	os.WriteFile(dumpPath, []byte(prompt), 0o644)
	t.Logf("Prompt saved to %s", dumpPath)

	// 发送到 LLM
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	t.Log("Sending final answer prompt to LLM...")
	resp, err := client.Chat(ctx, ai.NewSystemMsgInfo(prompt))
	if err != nil {
		t.Fatalf("LLM call failed: %v", err)
	}

	t.Logf("=== LLM RESPONSE ===\n%s", resp)

	respPath := "/tmp/focus_test_final_answer_response.txt"
	os.WriteFile(respPath, []byte(resp), 0o644)
	t.Logf("Response saved to %s", respPath)

	// 解析 JSON 响应
	cleanResp := resp
	if start := strings.Index(resp, "{"); start >= 0 {
		if end := strings.LastIndex(resp, "}"); end > start {
			cleanResp = resp[start : end+1]
		}
	}

	var result struct {
		IsComplete   bool     `json:"is_complete"`
		Status       string   `json:"status"`
		ShouldReplan bool     `json:"should_replan"`
		MissingItems []string `json:"missing_items"`
		Reason       string   `json:"reason"`
		UserMessage  string   `json:"user_message"`
	}
	if err := json.Unmarshal([]byte(cleanResp), &result); err != nil {
		t.Logf("Failed to parse response as JSON: %v", err)
		t.Logf("Raw response: %s", resp)
	} else {
		t.Logf("is_complete=%v status=%s should_replan=%v", result.IsComplete, result.Status, result.ShouldReplan)
		t.Logf("reason=%s", result.Reason)
		t.Logf("missing_items=%v", result.MissingItems)
		t.Logf("user_message length=%d", len(result.UserMessage))

		if result.ShouldReplan {
			t.Error("FAIL: should_replan=true — 聚焦约束未生效，FinalAnswer 想要扩展到其他维度")
		}
		if !result.IsComplete {
			t.Error("FAIL: is_complete=false — 聚焦方向的步骤已全部完成，应判定为 complete")
		}
		if result.Status != "completed" {
			t.Errorf("FAIL: status=%s, expected completed", result.Status)
		}
		// 检查 missing_items 不应包含非聚焦维度
		for _, item := range result.MissingItems {
			itemLower := strings.ToLower(item)
			for _, kw := range []string{"auth", "认证", "授权", "client", "客户端", "csp", "config", "配置", "密钥", "依赖", "xss"} {
				if strings.Contains(itemLower, kw) {
					t.Errorf("FAIL: missing_items contains non-focus keyword %q: %s", kw, item)
				}
			}
		}

		fmt.Println("\n=== USER MESSAGE PREVIEW ===")
		if len(result.UserMessage) > 500 {
			fmt.Println(result.UserMessage[:500] + "...")
		} else {
			fmt.Println(result.UserMessage)
		}
	}
}
