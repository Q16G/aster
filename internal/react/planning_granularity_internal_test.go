package react

import (
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

// TestPlanningSystemPromptContainsGranularityClauses 锁定 planning_system.prompt 的粒度纪律
// 在 plan 相位渲染时含 N0-N4 五处核心条款，且不含被本轮删/收缩的旧豁免授权（防回流）。
// N0：FACTS-to-step 对账（来自 yaklang plan_from_document.txt:78-85 的 N-to-N hard quantification）
// N1：删 §粒度纪律 维度①「按操作阶段」，只按操作对象拆
// N2：§同手段子任务集落清单 收缩为规划层不预合并
// N3：删 §塌缩反模式 ②合法豁免「内联清单」分支
// N4：§未观测面禁止预占位 示例从三动作并列改为单动作
func TestPlanningSystemPromptContainsGranularityClauses(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{})
	if err != nil {
		t.Fatalf("BuildTaskPlannerPrompt failed: %v", err)
	}
	rendered := parts.SystemRules

	required := []string{
		// N0: 机械对账硬约束（最强杠杆）——事实板 N 条 → plan 文案逐条字面引用
		"FACTS-to-step 对账（硬约束）",
		"必须在至少一条 `plan[].step` 文案里字面引用其稳定标识",
		"禁止代表抽样",
		"事实板列出 N 项即必须 N 项各自被 step 字面引用",
		// N1: 拆分维度收紧到"只按操作对象拆"
		"只按操作对象拆",
		`"探索 / 分析 / 实施 / 验证" 不是合法拆分粒度`,
		// N2: 同手段子任务集 收缩为规划层不预合并
		"规划层不预合并同手段子任务集",
		"plan 不预合并",
		// N3: 合法豁免改为"前序 step 落盘产物"且禁内联清单
		"该产物文件须来自前序 step 的实际落盘",
		"不允许本 step 内联生成清单作豁免依据",
		// N4: 未观测面示例改为单动作
		"按观测对象逐条编排",
		"每条 step 单一观测动作产出单一清单文件",
		// P2a: 探索递进三条硬约束（session 8a247641 复盘补救）
		"同维度未完成探索禁止跨维度推断",
		"探索阻塞必补观测",
		"新发现面必拆 step",
		// P2a 正反例骨架（确保示例没被删）
		"以偏概全",
		"阻塞补观测",
		"按发现拆",
	}
	for _, needle := range required {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("planning_system must render granularity clause (missing %q), got:\n%s", needle, rendered)
		}
	}

	forbidden := []string{
		// N1 防回流：旧"按阶段拆"维度
		"按操作阶段（探索 / 分析 / 实施 / 验证）",
		// N2 防回流：旧"同手段子任务集落清单"作为规划侧豁免
		"同手段子任务集落清单",
		// N3 防回流：旧"内联清单+对账"豁免分支
		`单步内联完整清单并附"对账该清单全部 N 项"`,
		// N4 防回流：旧三动作并列示范
		"先编排功能枚举、入口提取、响应观测",
	}
	for _, banned := range forbidden {
		if strings.Contains(rendered, banned) {
			t.Fatalf("planning_system must not retain legacy authorization signal %q, got:\n%s", banned, rendered)
		}
	}
}

// TestStepReplanPromptCardCoarseGranularityCheck 锁定 step_replan_system.prompt 维度①视角A
// 承接「卡片所属 step 文案命中粒度塌缩 → 自标 verified 不豁免」的核验通路，
// 含 N5 客观结构指标：coverage_checklist 项数 ≥ 3 且各项动作动词互异 → 实证文案塌缩。
// session 65448d5d/6e50312b 复盘补救：planner 漏放的塌缩 step 在 step_replan 侧必须有兜底。
func TestStepReplanPromptCardCoarseGranularityCheck(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{CurrentGoal: "粒度承接"})
	if err != nil {
		t.Fatalf("BuildStepReplanPrompt failed: %v", err)
	}
	rendered := parts.SystemRules

	required := []string{
		// 顶层承接句：verified 不豁免文案塌缩，按本卡维度①缺口计
		"卡片所属 step 文案命中粒度塌缩",
		`自标 ` + "`verified`" + ` 不豁免`,
		"应按工件/产出拆为 N 条独立可验收子项",
		// 三判据骨架（self-contained 不跨文件引用）
		"多动作壳词",
		"双产物明文",
		"类目无锚点收口",
		// 关键语义判据
		"壳词不抵消语义多产",
		"可独立落盘、可被下游独立消费的中间产物形态",
		// 豁免口径：清单+对账只豁免「类目无锚点收口」，不抵消「双产物明文」
		"该豁免不抵消「双产物明文」判定",
		// N5: 客观结构指标——绕开文案修辞直接看落地形态
		"coverage_checklist 项数 ≥ 3 且各项动作动词互异",
		// P1: pending 项实际已闭环裁定（解决断点 B 冗余规划塌缩）
		"pending 项实际已闭环裁定",
		"语义重叠 ≥80% 关键词覆盖",
		// P2b: 维度① 视角 A 加探索断点承接判据
		"以偏概全识别",
		"探索阻塞未补",
		"跨对象推断措辞",
		"阻塞被静默丢弃",
	}
	for _, needle := range required {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("step_replan must render card-level granularity hook (missing %q), got:\n%s", needle, rendered)
		}
	}
}

// TestValidatePlanItemsGranularity 验证 P3 机械兜底——submit_plan handler 的粒度校验。
// 与 prompt 侧 §粒度纪律 / §塌缩反模式 同口径，runtime 强制点：
// (1) 单条 step 文案长度 ≤ planItemStepMaxRunes（120 字符），超长几乎必然塞多事
// (2) 单条 step 文案不含中文分号 `；`（多句堆叠的强信号）
// 失败时由 submit_plan 重试通道把 error 回写给 LLM 让其拆条重试（不直接报错）。
func TestValidatePlanItemsGranularity(t *testing.T) {
	cases := []struct {
		name    string
		items   []*builtin_tools.PlanItem
		wantErr bool
	}{
		{
			name: "single concrete artifact passes",
			items: []*builtin_tools.PlanItem{
				{ID: "s1", Step: "枚举端点 /api/portal/auth/enterprise/login 产出请求/响应快照"},
			},
			wantErr: false,
		},
		{
			name: "chinese semicolon rejected (multi-clause stuffing)",
			items: []*builtin_tools.PlanItem{
				{ID: "s2", Step: "检测安全头；分析 Cookie 安全属性；测试 CORS 跨域配置"},
			},
			wantErr: true,
		},
		{
			name: "overlength rejected",
			items: []*builtin_tools.PlanItem{
				{ID: "s3", Step: strings.Repeat("对", 130) + "象"},
			},
			wantErr: true,
		},
		{
			name: "nil items skipped without crash",
			items: []*builtin_tools.PlanItem{
				nil,
				{ID: "s4", Step: "读取产物文件 endpoint-ledger.jsonl 产出对账清单"},
			},
			wantErr: false,
		},
		{
			name: "empty step skipped",
			items: []*builtin_tools.PlanItem{
				{ID: "s5", Step: "   "},
			},
			wantErr: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePlanItemsGranularity(tc.items)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
