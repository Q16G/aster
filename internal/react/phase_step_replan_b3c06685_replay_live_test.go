package react

// 一次性 live 回放测试：用 session b3c06685 iter 209 的实际输入（GOAL_UNDERSTANDING、
// 账本未解决全集、全 completed 的 plan、末步 report 卡片）调用真实 LLM，验证修改后的
// step_replan prompt（维度①②③ 补「账本全局」子项 + 区间复核语义）能让模型输出
// should_replan=true。修改前模型输出 false（理由"未解决项正确标识为 Phase 2 工作"）。
//
// 仅在 SASTPRO_REACT_LIVE_TEST=1 且 SASTPRO_TEST_CHAT_URL/API_KEY/MODEL 三件套齐全时
// 启用；默认 skip。运行示例：
//
//	SASTPRO_REACT_LIVE_TEST=1 \
//	SASTPRO_TEST_CHAT_URL=http://10.95.15.250:3000/v1 \
//	SASTPRO_TEST_CHAT_API_KEY=<internal key> \
//	SASTPRO_TEST_CHAT_MODEL=internal/deepseek-v4-pro \
//	go test ./internal/react/ -run TestStepReplanB3c06685Replay -v -count=1 -timeout 5m

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/ai/openai"
	"aster/internal/builtin_tools"
)

const b3c06685GoalUnderstanding = `## 核心目标
对 http://10.43.34.50:18080/home 入口锚点出发的 RedHaze 集团全部 6 个在线子系统进行全面安全测试。

## 范围边界
全量/无边界。用户要求"全面测试"，入口锚点 ` + "`/home`" + ` 不限定 scope 宽度。从入口出发可到达的所有关联 artifact（6 个子系统、所有页面/API/认证端点/静态资源）均在范围内。

## 约束
- 黑盒测试，不依赖源码
- 所有子系统在 10.43.34.50 域名下，分布在 18080-18085 端口
- 已通过访客注册获得有效会话凭据 ` + "`sess_guest`" + `

## 交付物与验收标准
- 各子系统的完整端点/页面/API 覆盖账本
- 认证机制的安全性评估（SQLi/暴力破解/会话固定/注册绕过）
- 访问控制测试（未授权访问/IDOR）
- 跨服务安全测试（CORS/SSO/cookie 作用域）
- 按严重度分级的漏洞发现清单，含可复现 POC
- 覆盖四态（confirmed/safe/blocked/n/a）的端点级结论

## 显式聚焦
无显式聚焦，按全量处理。初始 Phase 1 聚焦侦察与初步测试，Phase 2 根据发现深化。

## 隐含需求与假设
- 假设所有子系统的认证统一通过 RedHaze ID（18080 端口）管理
- 假设访客/商城/工单等不同身份层级有不同的访问权限，需分别测试
- 假设跨子系统会话复用机制可能存在安全缺陷（SSO 伪造/会话固定）

## 未决歧义
无。通过初步调查已确认 6 个服务全部在线且有明确的端点结构。`

// b3c06685OpenItemsLedger 直接采用 session 当前快照（与 iter 209 的 `## 未解决` 区结构相同；
// `## 已闭环` 区即使更长也不影响判定——本测试的核心信号是「`## 未解决` 区含多条可行动 finding，
// pending 计划无任何承接步骤」）。
const b3c06685OpenItemsLedger = `## 未解决
- [OI-202] Mall库存逆向操纵: 负数量加购会逆向增加库存(stock_change=-cart_quantity)，已验证Product18 stock从35增至100,034。攻击者可任意修改任意商品库存。
- [OI-201] Mall订单API无需认证: GET /api/mall/orders/{id} 配合 X-Mall-Crawler-Bypass: 1 无需任何 Cookie 即可返回全部订单数据。
- [OI-203] Mall X-Mall-Premium-Override header未认证越权: 允许未认证用户列出全部Premium商品。
- [OI-204] Mall Premium产品单品详情无访问控制: GET /api/mall/products/{id} 不检查premium状态。
- [OI-205] Ads POST /api/ads/campaigns完全无认证: 无需会话即可创建广告活动。
- [OI-014] Desk SSRF任意文件读取 + 内网探测: POST /api/desk/webhooks 支持 file:// 和任意HTTP URL。
- [OI-015] Desk工单IDOR(3个API): GET tickets/{id}、POST comments、POST refund 均无所有权检查。
- [OI-018] Desk内部备注泄露(D-007): internal_only=true 的评论对客户可见。
- [OI-026] Chat私有频道认证绕过: staff-lounge/ops-watch 标记 public:false 但仅检查 cookie 存在性。
- [OI-027] Chat消息API无认证: 发送/读取消息均无需认证。
- [OI-029] Ads未授权访问全部Campaign数据: GET /api/ads/campaigns 和 /{id} 无认证。
- [OI-106] Mall/Desk/Ads CORS null Origin 也反射。
- [OI-107] 全系统无CSRF防护: 所有状态变更端点均无 CSRF Token。
- [OI-108] 跨端口Cookie共享+角色特权继承: Employee session 在 Mall 子系统解锁 premium。
- [OI-111] Employee SQL注入确认(6列双回显)。
- [OI-114] Desk模板SSTI confirmed(MEDIUM): body_template 被 Go html/template 解析。

## 不可解局限
- [OI-102] Mall无密码重置功能: /mall/forgot 和 /mall/reset 返回 404。
- [OI-105] Chat WebSocket深度测试: 缺少 wscat 等客户端工具。

## 已闭环
- [OI-001] Vendor SQL注入提取实际数据: 17张表全量数据，含81条明文密码（已闭环）。
- [OI-003] 访客注册存储型XSS exploit chain（已闭环）。
- [OI-007] Mall存储型XSS 利用链（已闭环）。
- [OI-017] Desk存储型XSS 利用链（已闭环）。
- [OI-022] CMS存储型XSS 利用链（已闭环）。
- [OI-025] Chat存储型XSS 利用链（已闭环）。
（其余已闭环条目从略——见 session workspace/shared/open_items.md）`

// makeB3c06685Plan 合成 iter 209 时刻的 plan 全集：7 条 step 全部 completed，末步 report。
// 用于 PLAN_OVERVIEW（slim 全量卡片）。这里直接给出最小可序列化形态——内联 short_summary
// 简化，coverage / digest 走空（实际 session 内 plan_overview 字段也只承载小字段）。
func makeB3c06685Plan() []*builtin_tools.PlanItem {
	mk := func(id, step string) *builtin_tools.PlanItem {
		return &builtin_tools.PlanItem{
			ID:           id,
			Step:         step,
			Status:       builtin_tools.PlanStepCompleted,
			ShortSummary: "已完成（详见账本与 planner_journal）",
		}
	}
	return []*builtin_tools.PlanItem{
		mk("recon-portal", "对 RedHaze Portal (18080) 进行侦察，产出端点清单与认证机制评估。"),
		mk("recon-mall", "对 Mall (18083) 进行侦察，产出端点清单、CORS/认证/输入面初步评估。"),
		mk("recon-desk", "对 Desk (18082) 进行侦察，产出端点清单、SSRF/IDOR/SSTI 候选面。"),
		mk("recon-chat-cms-ads", "对 Chat/CMS/Ads (18084/18085) 进行侦察，产出 XSS/认证 候选。"),
		mk("cors-session-cross", "跨服务 CORS/Cookie/CSRF 复用测试，输出跨服务漏洞结论。"),
		mk("analysis", "汇总六子系统侦察结论并整理 Phase 1 findings。"),
		mk("report", "汇总输出 Phase 1 漏洞分级报告 redhaze-security-report.md。"),
	}
}

// makeB3c06685ReviewWindow 合成本回合复核区间——iter 209 触发条件为 plan_exhausted（末步
// report 完成后无下一可跑 step）。窗口里只放最后一卡 report（Latest=true），与 session
// 实际 review_window 形态对齐：模型基于 `report` 卡片的 coverage_checklist 自标 verified
// + deliverable=1587 行报告，结合账本未解决全集做判定。
func makeB3c06685ReviewWindow() *reviewWindow {
	card := &replanStepCard{
		ID:            "report",
		Step:          "汇总输出 Phase 1 漏洞分级报告 redhaze-security-report.md。",
		Status:        string(builtin_tools.PlanStepCompleted),
		StatusSummary: "Phase 1 报告已生成（1587 行），覆盖 6 子系统 finding 分级。",
		ShortSummary:  "redhaze-security-report.md 已落盘，含 finding 索引、严重度分级与 POC。",
		KeyFacts: []string{
			"产物=redhaze-security-report.md (1587 行)",
			"finding 索引含 OI-001 / OI-003 / OI-007 / OI-014 / OI-015 / OI-022 / OI-025 等",
			"Phase 2 工作项未在本 step 承接",
		},
		ToolCallsDigest: []string{
			"write_file: redhaze-security-report.md (1587 行)",
			"read_file: shared/open_items.md（核对已闭环 / 未解决全集）",
			"read_file: shared/task_context.md（核对入板事实）",
		},
		CoverageChecklist: []builtin_tools.CoverageChecklistItem{
			{Item: "Phase 1 报告产出 redhaze-security-report.md", Status: "verified", Evidence: "write_file ok"},
			{Item: "report 内含 finding 严重度分级", Status: "verified", Evidence: "report §3"},
			{Item: "report 含 POC 与可复现命令", Status: "verified", Evidence: "report §5"},
		},
		Latest: true,
	}
	return &reviewWindow{Cards: []*replanStepCard{card}, TotalCards: 1}
}

func TestStepReplanB3c06685Replay_LiveFromENV(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode enabled")
	}
	if v := strings.TrimSpace(os.Getenv("SASTPRO_REACT_LIVE_TEST")); !truthy(v) {
		t.Skip("live test disabled; set SASTPRO_REACT_LIVE_TEST=1 to enable")
	}
	baseURL := firstEnv("SASTPRO_TEST_CHAT_URL", "SAST_AGENT_TEST_BASE_URL")
	apiKey := firstEnv("SASTPRO_TEST_CHAT_API_KEY", "SAST_AGENT_TEST_API_KEY")
	model := firstEnv("SASTPRO_TEST_CHAT_MODEL", "SAST_AGENT_TEST_MODEL")
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("missing live config; set SASTPRO_TEST_CHAT_URL / API_KEY / MODEL")
	}

	timeout := 4 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	plan := makeB3c06685Plan()
	reviewWin := makeB3c06685ReviewWindow()

	parts, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		CurrentGoal:       "对网站全面测试：http://10.43.34.50:18080/home",
		GoalUnderstanding: b3c06685GoalUnderstanding,
		ReviewWindow:      reviewWin,
		PlanOverview:      plan,
		OpenItemsLedger:   b3c06685OpenItemsLedger,
		TaskContextBoard:  "## 输入事实\n- target=http://10.43.34.50:18080/home\n- scope=full\n\n## 执行中补充\n- 6 个子系统已枚举完毕；已闭环 finding 含 XSS/SQLi/IDOR 全链。\n",
		PriorBoundaryStepID: "cors-session-cross",
		OpenItemsPath:       "/tmp/replay/shared/open_items.md",
		TaskContextPath:     "/tmp/replay/shared/task_context.md",
		StepFilePath:        "/tmp/replay/shared/step_report.md",
	})
	if err != nil {
		t.Fatalf("BuildStepReplanPrompt failed: %v", err)
	}
	if strings.TrimSpace(parts.SystemRules) == "" || strings.TrimSpace(parts.User) == "" {
		t.Fatalf("rendered prompt parts empty: system=%d user=%d", len(parts.SystemRules), len(parts.User))
	}

	t.Logf("[replay] system bytes=%d user bytes=%d", len(parts.SystemRules), len(parts.User))

	client := openai.NewClient(
		openai.WithURL(baseURL),
		openai.WithURLAutoComplete(true),
		openai.WithAPIKey(apiKey),
		openai.WithModel(model),
		openai.WithTimeout(timeout),
		openai.WithMaxRetries(1),
		openai.WithStream(false),
	)

	msgs := []*ai.MsgInfo{
		ai.NewSystemMsgInfo(parts.SystemRules),
		ai.NewUserMsgInfo(parts.User),
	}
	submitToolInst := newSubmitReplanTool()
	fnTool := &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        submitToolInst.Name(),
			Description: submitToolInst.Description(),
			Parameters:  submitToolInst.Parameters(),
		},
	}

	choices, err := client.ChatEx(ctx, msgs, fnTool)
	if err != nil {
		t.Fatalf("live ChatEx failed: %v", err)
	}
	if len(choices) == 0 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("live ChatEx returned empty choices")
	}
	msg := choices[0].Message

	var submitArgs map[string]any
	for _, tc := range msg.ToolCalls {
		if tc == nil || tc.Function == nil {
			continue
		}
		if strings.TrimSpace(tc.Function.Name) == submitReplanToolName {
			if m, ok := tc.Function.Arguments.(map[string]any); ok {
				submitArgs = m
			}
			break
		}
	}
	if submitArgs == nil {
		t.Fatalf("model did not call submit_replan; content=%q reasoning=%q tool_calls=%d",
			truncate(asString(msg.Content), 800), truncate(msg.ReasoningOutput, 800), len(msg.ToolCalls))
	}

	if _, err := submitToolInst.Execute(context.Background(), submitArgs); err != nil {
		raw, _ := json.Marshal(submitArgs)
		t.Fatalf("Execute failed: %v\nraw args=%s", err, string(raw))
	}
	decision := submitToolInst.getResult()
	if decision == nil {
		t.Fatalf("no result stored after Execute")
	}

	t.Logf("[replay] should_replan=%v", decision.ShouldReplan)
	t.Logf("[replay] replan_reason=%s", decision.ReplanReason)
	t.Logf("[replay] current_phase_done=%v", decision.CurrentPhaseDone)
	t.Logf("[replay] incomplete_items=%d depth_gaps=%d new_surfaces=%d",
		len(decision.IncompleteItems), len(decision.DepthGaps), len(decision.NewSurfaces))
	if msg.ReasoningOutput != "" {
		t.Logf("[replay] reasoning=%s", truncate(msg.ReasoningOutput, 2000))
	}

	if !decision.ShouldReplan {
		t.Fatalf("expected should_replan=true after prompt hardening, got false; reason=%q",
			decision.ReplanReason)
	}
}

// ---- helpers (本文件 scoped；避免与 react_test 包 helpers 串名) ----

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}
