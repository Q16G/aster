package react

import (
	"fmt"
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

// TestPlanningSystemPromptContainsGranularityClauses 锁定 planning_system.prompt 的 Atomic Step Contract。
// 在 plan 相位渲染时含 N0-N4 五处核心条款，且不含被删/收缩的旧授权（防回流）。
// N0：FACTS-to-step 对账（来自 yaklang plan_from_document.txt:78-85 的 N-to-N hard quantification）
// N1：step = object × action × acceptance
// N2：§同手段子任务集落清单 收缩为规划层不预合并
// N3：清单是数据流，不是批量执行对象
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
		// 现已收窄到本回合 active phase 范围，断言更新为新措辞。
		"FACTS-to-step 对账（硬约束）",
		"只对账本回合 active phase 语义范围内的事实条目",
		"必须在至少一条 `plan[].step` 字面引用其稳定标识",
		"逐项事实覆盖",
		"active phase 范围内列出 N 项必须 N 项各自被 step 字面引用",
		// N1: 拆分维度收紧到 Atomic Step Contract
		"Atomic Step Contract（硬约束）",
		"object × action × acceptance",
		"一个可点名的执行对象",
		"唯一动作维度",
		"一个可独立验收的产出或结论",
		"Decomposition Algorithm（硬约束）",
		"只允许合并完全相同 object、action、acceptance 的重复项",
		// N2: 同手段子任务集 收缩为规划层不预合并
		"规划层不预合并同手段子任务集",
		"plan 不预合并",
		// N3: 清单是数据流，不允许作为批量执行对象
		"Inventory/Dataflow Rule（硬约束）",
		"清单是数据流，不是批处理许可",
		"生成清单可以是一个 atomic step",
		"消费清单时必须把清单内对象展开为多个 atomic work items",
		"展开后的 atomic work items 进入账本完整超集",
		"当前 plan 只释放本回合 active phase（frontier）范围内的 step",
		// Phase shape: phase 必须是单一主导切面，不得是矩阵或 skill checklist
		"主导切面约束",
		"组件×多维度矩阵",
		"skill checklist",
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
		"合法豁免",
		"清单消费塌缩",
		"coverage_checklist 矩阵塌缩",
		"SQLi",
		"XSS",
		"IDOR",
		"/api/login",
		"endpoint-ledger",
		"page-ledger",
		// N4 防回流：旧三动作并列示范
		"先编排功能枚举、入口提取、响应观测",
	}
	for _, banned := range forbidden {
		if strings.Contains(rendered, banned) {
			t.Fatalf("planning_system must not retain legacy authorization signal %q, got:\n%s", banned, rendered)
		}
	}
}

// TestStepReplanPromptCardCoarseGranularityCheck 锁定 step_replan_system.prompt 的 Atomic Contract Audit。
// 承接「已完成 step 实际越界 → 自标 verified 不豁免」的核验通路。
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
		// 顶层承接句：verified 不豁免 atomic 越界，按本卡维度①缺口计
		"Atomic Contract Audit",
		"object × action × acceptance",
		"自标 `verified` 不豁免",
		"拆为 atomic work items: (object, action, acceptance)",
		"清单文件只作为数据来源审计",
		"与维度③ §清单产出按数据流审计 同口径",
		"实际执行矩阵",
		"同 object/action 内部的同口径证据",
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

// TestThinkActPromptContainsAtomicExecutionBoundary 锁定执行期只消费 planner 已拆分 atomic step。
func TestThinkActPromptContainsAtomicExecutionBoundary(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{CanSpawnSubAgent: true})
	if err != nil {
		t.Fatalf("BuildThinkActPrompt failed: %v", err)
	}
	rendered := parts.SystemRules

	required := []string{
		"Atomic Execution Boundary（硬约束）",
		"object × action × acceptance",
		"当前 step 已由规划阶段拆分",
		"执行器不重新规划、不拆分、不因粒度问题拒绝 step",
		"不扩展本 step 的执行范围",
		"Inventory/Dataflow Use",
		"Atomic 子 Agent 辅助",
		"子 Agent 与并发分片只能辅助当前三元组内部取证",
		"否则不得派发",
		"所有分片归并后只产生当前 step 的单一验收产出",
	}
	for _, needle := range required {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("think_act must render atomic execution boundary (missing %q), got:\n%s", needle, rendered)
		}
	}
}

// TestBuildSubmitPlanFunctionTool_StepDescriptionUsesAtomicContract 锁定模型可见 schema 描述。
func TestBuildSubmitPlanFunctionTool_StepDescriptionUsesAtomicContract(t *testing.T) {
	tool := buildSubmitPlanFunctionTool()
	if tool == nil || tool.Function == nil {
		t.Fatalf("submit_plan tool is nil")
	}
	params, ok := tool.Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("submit_plan parameters must be map[string]any, got %T", tool.Function.Parameters)
	}
	desc, ok := nestedString(params, "properties", "plan", "items", "properties", "step", "description")
	if !ok {
		t.Fatalf("submit_plan step description not found in schema: %#v", params)
	}

	required := []string{
		"object × action × acceptance",
		"任一维度不同就拆成不同 step",
		"清单是数据流",
		"清单文件名本身不是批量执行对象",
	}
	for _, needle := range required {
		if !strings.Contains(desc, needle) {
			t.Fatalf("submit_plan step description missing %q: %s", needle, desc)
		}
	}

	forbidden := []string{"合法豁免", "单步内联完整清单", "指向承载完整清单", "塌缩反模式", "SQLi", "XSS", "IDOR"}
	for _, banned := range forbidden {
		if strings.Contains(desc, banned) {
			t.Fatalf("submit_plan step description must not retain legacy wording %q: %s", banned, desc)
		}
	}
}

// TestCorePromptPhaseChainContracts 锁定阶段链纪律的关键短语 + negative 断言。
// 验证以下纪律已落进 prompt 且未回退到旧版本：
//   - planning_system：phase=最小从浅到深单元 / 初始 frontier 起手 / 同类范围 / lane 推进次序 / lane 承接 + 自决收束 / phase 动态增删改 / 深度优先纪律
//   - step_replan_system：三视角（A/B/C）/ in-phase 保底 / phase_assessments status 判据（无可测未测、无可深未深）/ 视角 A 新对象登账本
//   - negative：planner 不再原样回填、step_replan 不再产出 next_phase 选择优先级 / 决策真值表 / Phase Shape Audit / 双视角 / 阶段内待跑深度
func TestCorePromptPhaseChainContracts(t *testing.T) {
	planningRequired := []string{
		"最小从浅到深单元",
		"初始 frontier 起手",
		"同类范围识别",
		"递进 lane 独立成 lane",
		"lane / step 边界",
		"lane 依赖形态",
		"phase 动态增/改/删",
		"lane 承接 + 自决收束",
		"同类范围只来自事实板",
		"Skill 索引参与深度层暗示",
		"主导切面约束",
		"组件×多维度矩阵",
		"skill checklist",
		"深度优先纪律",
		"lane 依赖表达",
		"能力 vs 默认",
	}
	for _, needle := range planningRequired {
		if !strings.Contains(planningSystemPrompt, needle) {
			t.Fatalf("planning_system must contain %q", needle)
		}
	}

	planningForbidden := []string{
		"报告步骤恒为末步",
		"新页面",
		"新 API",
		"对等面",
		"对等覆盖面",
		"字段必填条件按 schema description",
		// 职责反转：planner 不再原样回填，step_replan 不再产出 next_phase / phase_shape_issue。
		"原样回填",
		"next_phase",
		"phase_shape_issue",
	}
	for _, banned := range planningForbidden {
		if strings.Contains(planningSystemPrompt, banned) {
			t.Fatalf("planning_system must not retain legacy %q", banned)
		}
	}

	stepReplanRequired := []string{
		"三视角",
		"视角 B",
		"视角 C",
		"in-phase 保底",
		"无可测未测",
		"无可深未深",
		"视角 A 新对象登账本",
		"skill 仅作为职责维度候选的弹性参考",
		"phase_assessments 提交契约",
	}
	for _, needle := range stepReplanRequired {
		if !strings.Contains(stepReplanSystemPrompt, needle) {
			t.Fatalf("step_replan_system must contain %q", needle)
		}
	}

	stepReplanForbidden := []string{
		"身份 a workbench",
		"sysop 控制台",
		"纯静态展示组件",
		"未能登录 sysop",
		"对等面",
		"对等覆盖面",
		"路由型 skill 按下级清单逐项对账",
		"阶段内待跑深度: <X>",
		// 职责反转后这些 step_replan 概念已删除（phase 决策归 planner）。
		"决策真值表",
		"Phase Shape Audit",
		"next_phase 选择优先级",
		"双视角",
	}
	for _, banned := range stepReplanForbidden {
		if strings.Contains(stepReplanSystemPrompt, banned) {
			t.Fatalf("step_replan_system must not retain legacy %q", banned)
		}
	}
}

// TestCorePromptAtomicBudget 防止这组规则重新膨胀成黑名单堆叠。
func TestCorePromptAtomicBudget(t *testing.T) {
	budgets := map[string]struct {
		text     string
		maxLines int
	}{
		"planning_system":    {text: planningSystemPrompt, maxLines: 140},
		"step_replan_system": {text: stepReplanSystemPrompt, maxLines: 225},
		"think_act_system":   {text: thinkActSystemPrompt, maxLines: 95},
	}
	totalLines := 0
	var combined strings.Builder
	for name, budget := range budgets {
		lines := countLines(budget.text)
		totalLines += lines
		combined.WriteString(budget.text)
		combined.WriteByte('\n')
		if lines > budget.maxLines {
			t.Fatalf("%s prompt line budget exceeded: got %d, want <= %d", name, lines, budget.maxLines)
		}
	}
	if totalLines > 465 {
		t.Fatalf("core prompt total line budget exceeded: got %d, want <= 465", totalLines)
	}
	if count := strings.Count(combined.String(), "塌缩"); count > 1 {
		t.Fatalf("core prompts should not re-grow collapse blacklist wording: got %d occurrences", count)
	}
}

// TestValidatePlanItemsGranularity 验证 P3 机械兜底——submit_plan handler 的粒度校验。
// 与 prompt 侧 Atomic Step Contract 同口径，runtime 强制点：
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

// TestValidatePlanItemsGranularity_AggregatesAllViolations 锁定聚合行为：
// 多条 step 违例时单次 retry 必须一次性反馈所有违例，避免「修对一条又留另一条」死循环。
func TestValidatePlanItemsGranularity_AggregatesAllViolations(t *testing.T) {
	items := []*builtin_tools.PlanItem{
		{ID: "long-a", Step: strings.Repeat("对", 130) + "象"},
		{ID: "semicolon-b", Step: "检测安全头；分析 Cookie 安全属性；测试 CORS"},
		{ID: "long-c", Step: strings.Repeat("枚", 125)},
		{ID: "ok-d", Step: "枚举 /api/auth/login 产出请求快照"},
	}
	err := validatePlanItemsGranularity(items)
	if err == nil {
		t.Fatalf("expected error from mixed violations, got nil")
	}
	text := err.Error()
	for _, id := range []string{"long-a", "semicolon-b", "long-c"} {
		if !strings.Contains(text, id) {
			t.Errorf("aggregated error must reference violation id %q, got:\n%s", id, text)
		}
	}
	if strings.Contains(text, "ok-d") {
		t.Errorf("non-violating step ok-d must not appear in error, got:\n%s", text)
	}
	if !strings.Contains(text, "共 3 条违例") {
		t.Errorf("aggregated error must announce violation count, got:\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("≤%d 字符", planItemStepMaxRunes)) {
		t.Errorf("aggregated error trailer must remind of %d-char ceiling, got:\n%s", planItemStepMaxRunes, text)
	}
}

// TestValidatePlanItemsGranularity_ExcerptTruncated 锁定违例 step 文案摘要 ≤60 rune + 省略号。
// 避免反馈面把整段超长 step 全文粘贴回去（既冗余又可能再次触发上下文上限）。
func TestValidatePlanItemsGranularity_ExcerptTruncated(t *testing.T) {
	longStep := strings.Repeat("甲", 200)
	err := validatePlanItemsGranularity([]*builtin_tools.PlanItem{
		{ID: "x1", Step: longStep},
	})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	text := err.Error()
	if !strings.Contains(text, "…") {
		t.Fatalf("error must include truncation marker `…`, got:\n%s", text)
	}
	// 摘要 ≤60 rune；整段 200 rune 不允许全部出现
	if strings.Contains(text, strings.Repeat("甲", 100)) {
		t.Fatalf("error must not contain the full untruncated step text")
	}
	full := strings.Repeat("甲", planItemStepExcerptRunes+1)
	if strings.Contains(text, full) {
		t.Fatalf("excerpt must be capped at %d runes, got more in:\n%s", planItemStepExcerptRunes, text)
	}
}

// TestCollectGranularityViolationIDs 锁定 ID 收集助手——降级放行分支用它写 warning + 标记。
func TestCollectGranularityViolationIDs(t *testing.T) {
	ids := collectGranularityViolationIDs([]*builtin_tools.PlanItem{
		nil,
		{ID: "long-1", Step: strings.Repeat("对", 121)},
		{ID: "semi-2", Step: "A；B"},
		{ID: "ok-3", Step: "短步"},
		{ID: "  ", Step: strings.Repeat("乙", 130)},
	})
	if want, got := []string{"long-1", "semi-2", "<unnamed>"}, ids; !equalStringSlice(want, got) {
		t.Fatalf("violation IDs mismatch: want %v, got %v", want, got)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestStepTextExcerpt 锁定摘要工具的边界。
func TestStepTextExcerpt(t *testing.T) {
	t.Run("under limit returns as-is", func(t *testing.T) {
		if got := stepTextExcerpt("短", 10); got != "短" {
			t.Fatalf("want 短, got %q", got)
		}
	})
	t.Run("over limit truncates with ellipsis", func(t *testing.T) {
		got := stepTextExcerpt(strings.Repeat("甲", 100), 5)
		if got != strings.Repeat("甲", 5)+"…" {
			t.Fatalf("unexpected excerpt: %q", got)
		}
	})
	t.Run("zero max yields empty", func(t *testing.T) {
		if got := stepTextExcerpt("任意", 0); got != "" {
			t.Fatalf("want empty, got %q", got)
		}
	})
}

func nestedString(root map[string]any, path ...string) (string, bool) {
	var cur any = root
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		cur, ok = m[key]
		if !ok {
			return "", false
		}
	}
	s, ok := cur.(string)
	return s, ok
}

func countLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}
