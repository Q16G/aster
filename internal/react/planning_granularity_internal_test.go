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
		// N0: 逐项事实对账（事实板 N 条 → plan 文案逐条字面引用）——重构后概念措辞
		"逐项对账",
		"都须在某条 step 字面引用",
		// N1: 原子拆分 = 一对象一动作一验收
		"一条 step 只做一个对象、一个动作、一个可独立验收的产出",
		"先列对象、再列每个对象要做的动作，逐一成 step",
		"只合并对象、动作、产出完全相同的重复项",
		// N1b: step 命令式 + 三层可判定验收（借鉴 yaklang）
		"命令式动词开头",
		"如何验证",
		"何时完成",
		// N3: 清单是数据流
		"清单是数据流",
		"消费清单时须把清单内对象逐项展开成 step",
		// 主题形状：单一主导切面，不得是矩阵
		"单一主导切面",
		"对象×多维度矩阵",
		// P2a 探索递进（session 8a247641 复盘补救，回补概念句）
		"未观测对象或维度不得由已观测的外推",
		"某维度只部分覆盖时禁止据已观测部分推断未观测部分",
		"新暴露的对象/维度必须新增独立观测 step",
		"编排补观测 step 换路径重试",
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
		// 证据地面 + 塌缩审计（session 65448d5d/6e50312b 复盘补救，回补概念句）：
		// 自标已完成不豁免核验；一步覆盖多对象/动作/验收 → 按缺失登记拆回逐项核验
		"证据地面",
		"自标已完成不豁免核验",
		"覆盖了多个对象/动作/验收",
		"按缺失登记、拆回逐项核验",
		// pending 项实际已闭环裁定（就地归档，解决冗余待办）
		"已被证据实证闭环的待跑步骤就地裁定归档",
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
	parts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{RunFlags: RunFlags{CanSpawnSubAgent: true}})
	if err != nil {
		t.Fatalf("BuildThinkActPrompt failed: %v", err)
	}
	rendered := parts.SystemRules

	required := []string{
		// 双重范围边界（当前 step ∩ 注入角色与指令职责半径）+ 越界分流
		"双重范围边界",
		"当前 step 的对象×动作×验收",
		"注入的角色与指令界定的职责半径",
		"不改 plan、不判任务整体完成、不出最终答案",
		"越界线索分流",
		// 子 Agent 委派边界（轻量化：委派独立可验收子任务、只服务当前 step 单一验收物）
		"独立可并行、可独立验收的调研/取证/执行子任务委派给子 Agent",
		"只服务当前 step 的单一验收物",
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
		"单一主导切面",
		"对象×多维度矩阵",
		"主题级承接",
		"全局收束承接",
		"意图恢复承接",
		"据复核自决",
		"真依赖才连边",
		"深度优先仅两义",
		"就绪面并发释放",
		"不预铺未观测面",
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
		// frontier 默认对齐飞书文档「释放所有 ready、多 lane 并发」：禁「默认1条/逐面最少释放」回流，
		// 且 planner prompt 不泄漏运行时旋钮 max_parallel_steps。
		"逐面最少释放",
		"默认单条",
		"只起手同类中的 1 条",
		"max_parallel_steps",
	}
	for _, banned := range planningForbidden {
		if strings.Contains(planningSystemPrompt, banned) {
			t.Fatalf("planning_system must not retain legacy %q", banned)
		}
	}

	stepReplanRequired := []string{
		"三个范围锚",
		"证据地面",
		"主题焦点",
		"全职责半径",
		"缺口的归宿",
		"无可测未测",
		"无可深未深",
		"自标已完成不豁免核验",
		// 两态 hoist（本 commit 新增核心逻辑，与 runtime 局部/全局 review 路由强耦合）守卫
		"有活跃主题",
		"无活跃主题",
		"全职责半径这一遍每轮收束都要做",
		// 深度气味注入块守卫（{{.DEPTH_SMELLS}} 承载 7 类深度判据）
		"深度气味",
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
