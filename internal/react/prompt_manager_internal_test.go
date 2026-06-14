package react

import (
	"strings"
	"testing"
)

func TestPromptManager_BuildersDoNotRenderNonce(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	cases := []struct {
		name  string
		build func() (string, error)
	}{
		{
			name: "think_act",
			build: func() (string, error) {
				parts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{})
				return parts.Joined(), err
			},
		},
		{
			name: "history_compaction",
			build: func() (string, error) {
				return manager.BuildHistoryCompactionPrompt(HistoryCompactionPromptInput{
					Instruction: "总结对话",
					PrevSummary: "已有摘要",
				})
			},
		},
		{
			name: "agent_handoff",
			build: func() (string, error) {
				return manager.BuildAgentHandoffPrompt(AgentHandoffPromptInput{
					HandoffTo:        "sub_agent",
					AgentInstruction: "继续处理",
					PrevSummary:      "已有交接",
				})
			},
		},
	}

	for _, tc := range cases {
		rendered, err := tc.build()
		if err != nil {
			t.Fatalf("%s build failed: %v", tc.name, err)
		}
		if strings.Contains(strings.ToLower(rendered), "nonce") {
			t.Fatalf("%s prompt should not contain nonce markers, got:\n%s", tc.name, rendered)
		}
	}
}

func TestPromptManager_ThinkActTaskContextFileGate(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	parts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{})
	if err != nil {
		t.Fatalf("build think_act failed: %v", err)
	}
	with := parts.SystemRules
	// workspace 恒存在：事实板与账本无条件渲染；规则文本去参数化，
	// 绝对路径只在身份/env 块出现，规则用「共享工作区」泛称。
	// 事实板三类判据 + 入板闸门（高利用价值通用判据）。
	for _, needle := range []string{
		"task_context.md", "唯一全集", "执行中补充", "入板闸门",
		"关键结论与决策依据", "全局参数与环境事实", "产物索引与解索引指引", "什么情况下该读",
	} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act must render fact board contract (missing %q), got:\n%s", needle, with)
		}
	}
	for _, needle := range []string{"coverage_checklist", "uncovered"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act should render checklist contract (missing %q), got:\n%s", needle, with)
		}
	}
	// 边完成边归档（持续不变量，就地迁入本账本 `## 已闭环`）+ 共享区禁 emoji。
	for _, needle := range []string{"解决即归档", "就地迁入本账本", "## 已闭环", "禁止 emoji"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act must render incremental archive + emoji ban (missing %q), got:\n%s", needle, with)
		}
	}
	// 已废协议残留禁入。
	for _, banned := range []string{"open_item_ids", "next_id"} {
		if strings.Contains(with, banned) {
			t.Fatalf("think_act must not retain removed protocol %q, got:\n%s", banned, with)
		}
	}
	// step 过程文件契约：三节固定 + 每轮一致不变量。
	for _, needle := range []string{"step 过程文件", "## 未覆盖待补（实时维护）", "## 进展记录", "## 收尾产出", "每轮执行结束时文件与实际进度一致"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act should render step file template contract (missing %q), got:\n%s", needle, with)
		}
	}
	// 去参数化：system 两块不得出现任何运行时绝对路径占位遗留。
	if strings.Contains(with, "{{.WORKSPACE_SHARED_DIR}}") {
		t.Fatalf("think_act system must not retain WORKSPACE_SHARED_DIR placeholder, got:\n%s", with)
	}
}

func TestPromptManager_TaskPlannerTaskContextWriteGate(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	// 顶层 planner（用户输入回合）：渲染共享区终态段 + 事实板快照。
	withParts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:            "测试输入",
		UserInputTurn:    true,
		IsSubAgent:       false,
		TaskContextBoard: "# 贯穿全程关键事实\n\n## 输入事实\n- 目标: x\n\n## 执行中补充\n",
	})
	if err != nil {
		t.Fatalf("build task_planner (user turn) failed: %v", err)
	}
	with := withParts.Joined()
	for _, needle := range []string{"共享区终态", "`submit_plan` 前须成立", "输入事实", "环境/参数事实", "禁止 emoji", "task_context.md", "<TASK_CONTEXT_BOARD>"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("task_planner top-level turn must render 共享区终态 section + board snapshot (missing %q), got:\n%s", needle, with)
		}
	}
	// 终态段措辞已下沉为陈述句，"唯一语义写者"角色化复述已按规范第 14 条删除。
	if strings.Contains(with, "唯一语义写者") {
		t.Fatalf("task_planner must drop the legacy role phrasing '唯一语义写者', got:\n%s", with)
	}
	// The removed structured array field must not reappear in the schema.
	if strings.Contains(with, `"task_context"`) {
		t.Fatalf("task_planner must not contain the removed task_context schema field, got:\n%s", with)
	}
	// 回流编排段归 plan 分支 + HAS_REPLAN_CONTEXT；非回流回合不渲染该段（用段标题断言以避开
	// 共享段对"复核重编排与回流编排"措辞的常规引用）。
	if strings.Contains(with, "# 回流编排（本回合消费") {
		t.Fatalf("task_planner non-replan turn must not render 回流编排 section, got:\n%s", with)
	}
	replanParts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:            "测试输入",
		UserInputTurn:    false,
		HasReplanContext: true,
	})
	if err != nil {
		t.Fatalf("build task_planner (replan turn) failed: %v", err)
	}
	replan := replanParts.Joined()
	for _, needle := range []string{"回流编排", "只编排不二次泛化", "产物-消费依赖", "最小扰动"} {
		if !strings.Contains(replan, needle) {
			t.Fatalf("task_planner replan turn must render 回流编排 branch (missing %q), got:\n%s", needle, replan)
		}
	}
	// step_replan 的内部判定段（核验依据 / 维度① / 账本复核与维护）不应渲染在 plan 模式下。
	for _, banned := range []string{"# 核验依据", "**维度①  存在性/完成度**", "# 账本复核与维护", "# 直接编排（账本维护完成后产出 pending 计划）"} {
		if strings.Contains(replan, banned) {
			t.Fatalf("task_planner replan turn must not render step_replan-only section %q, got:\n%s", banned, replan)
		}
	}
	if strings.Contains(with, "交叉分析") {
		t.Fatalf("task_planner must not retain the old co-equal 交叉分析 framing, got:\n%s", with)
	}
	if strings.Contains(with, "至少 3 步") {
		t.Fatalf("task_planner must not retain the hard minimum 3-step rule, got:\n%s", with)
	}
	if !strings.Contains(with, "4-8 步") {
		t.Fatalf("task_planner should align recon-first planning around 4-8 steps, got:\n%s", with)
	}

	// 顶层 planner 兜底回流回合（!UserInputTurn 但仍是顶层）：终态段仍渲染——守卫
	// 由 USER_INPUT_TURN 放宽为 !IS_SUB_AGENT，确保兜底回流场景也能校正事实板。
	fallbackParts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:            "测试输入",
		UserInputTurn:    false,
		IsSubAgent:       false,
		HasReplanContext: true,
	})
	if err != nil {
		t.Fatalf("build task_planner (fallback turn) failed: %v", err)
	}
	fallback := fallbackParts.Joined()
	for _, needle := range []string{"共享区终态", "`submit_plan` 前须成立"} {
		if !strings.Contains(fallback, needle) {
			t.Fatalf("task_planner fallback turn must still render 共享区终态 section (missing %q), got:\n%s", needle, fallback)
		}
	}

	// 子 Agent 回合：也渲染共享区终态段，因为子 Agent 在自己工作区下维护独立的
	// `task_context.md`；与父板的关系由"继承父事实板"段管控（只读父板）。
	subParts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:         "测试输入",
		UserInputTurn: true,
		IsSubAgent:    true,
	})
	if err != nil {
		t.Fatalf("build task_planner (sub-agent turn) failed: %v", err)
	}
	sub := subParts.Joined()
	if !strings.Contains(sub, "共享区终态") {
		t.Fatalf("sub-agent planner must render 共享区终态 section (own workspace board), got:\n%s", sub)
	}
}

// TestPromptManager_TaskPlannerSubAgentDelegation 校验顶层 planner 在 CAN_SPAWN_SUBAGENT=true
// 时渲染"规划期委派"条款，子 Agent / 未开放时该条款不渲染。
func TestPromptManager_TaskPlannerSubAgentDelegation(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	canParts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:            "测试输入",
		UserInputTurn:    true,
		IsSubAgent:       false,
		CanSpawnSubAgent: true,
	})
	if err != nil {
		t.Fatalf("build task_planner (can spawn) failed: %v", err)
	}
	canTxt := canParts.Joined()
	for _, needle := range []string{"规划期委派", "`sub_agent`"} {
		if !strings.Contains(canTxt, needle) {
			t.Fatalf("planner with CanSpawnSubAgent must render delegation clause (missing %q), got:\n%s", needle, canTxt)
		}
	}

	cannotParts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:            "测试输入",
		UserInputTurn:    true,
		IsSubAgent:       false,
		CanSpawnSubAgent: false,
	})
	if err != nil {
		t.Fatalf("build task_planner (cannot spawn) failed: %v", err)
	}
	if strings.Contains(cannotParts.Joined(), "规划期委派") {
		t.Fatalf("planner without CanSpawnSubAgent must not render delegation clause, got:\n%s", cannotParts.Joined())
	}
}

// 仓库上下文与身份段统一收敛到公共身份/env 块（system block2），各阶段模板不再各自渲染。
func TestPromptManager_AgentIdentityEnvBlock(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	block, err := manager.BuildAgentIdentityEnvPrompt(AgentIdentityEnvPromptInput{
		AgentInstruction:   "你是测试代理",
		WorkspaceRootDir:   "/ws/root",
		WorkspaceNamespace: "ns-1",
		WorkspaceSharedDir: "/ws/shared",
		RuntimeRepoContext: RuntimeRepoContext{
			SourceWorkingDir: "/repo/worktree",
			RepoRootDir:      "/repo/worktree",
			IsGitRepo:        true,
			Branch:           "feature/demo",
			IsWorktree:       true,
		},
		TaskContext: &TaskContextData{Entries: []TaskContextEntry{
			{Label: "项目路径", Value: "/repo/project", Description: "目标项目"},
		}},
	})
	if err != nil {
		t.Fatalf("build agent identity env failed: %v", err)
	}
	for _, needle := range []string{
		"<AGENT_INSTRUCTION>",
		"你是测试代理",
		"<env>",
		"workspace 路径: /ws/root",
		"workspace namespace: ns-1",
		"共享工作区: /ws/shared",
		"source working dir: /repo/worktree",
		"repo root: /repo/worktree",
		"is git repo: true",
		"current branch: feature/demo",
		"项目路径: /repo/project",
	} {
		if !strings.Contains(block, needle) {
			t.Fatalf("identity env block missing %q, got:\n%s", needle, block)
		}
	}
	// AGENT_ROLE / AGENT_BACKGROUND 已下沉至各 phase prompt 顶部，identity_env 不再渲染。
	for _, banned := range []string{"<AGENT_ROLE>", "</AGENT_ROLE>", "<AGENT_BACKGROUND>", "</AGENT_BACKGROUND>"} {
		if strings.Contains(block, banned) {
			t.Fatalf("identity env block must not render %q (moved to phase prompt # Role/# Background), got:\n%s", banned, block)
		}
	}
	if strings.Contains(block, "github.com") || strings.Contains(block, "gitRepoUrl") {
		t.Fatalf("identity env block must not expose remote url by default, got:\n%s", block)
	}

	// 多条任务上下文逐条渲染。
	multi, err := manager.BuildAgentIdentityEnvPrompt(AgentIdentityEnvPromptInput{
		WorkspaceRootDir:   "/ws/root",
		WorkspaceSharedDir: "/ws/shared",
		TaskContext: &TaskContextData{Entries: []TaskContextEntry{
			{Label: "项目路径", Value: "/repo/project"},
			{Label: "编译状态", Value: "ready"},
			{Label: "结构化输入", Value: "{\"ticket\":\"TASK-1\"}"},
		}},
	})
	if err != nil {
		t.Fatalf("build identity env (multi entries) failed: %v", err)
	}
	for _, needle := range []string{"项目路径: /repo/project", "编译状态: ready", "结构化输入: {\"ticket\":\"TASK-1\"}"} {
		if !strings.Contains(multi, needle) {
			t.Fatalf("identity env block missing task context entry %q, got:\n%s", needle, multi)
		}
	}

	// 空任务上下文不渲染该节。
	empty, err := manager.BuildAgentIdentityEnvPrompt(AgentIdentityEnvPromptInput{
		WorkspaceRootDir:   "/ws/root",
		WorkspaceSharedDir: "/ws/shared",
		TaskContext:        &TaskContextData{},
	})
	if err != nil {
		t.Fatalf("build identity env (empty task context) failed: %v", err)
	}
	if strings.Contains(empty, "任务上下文") {
		t.Fatalf("identity env block must omit task context section for empty data, got:\n%s", empty)
	}
}

// 各阶段模板不再渲染身份段与仓库上下文（由 block2 承担），且 system/user 双部分非空。
func TestPromptManager_PhasePromptsExcludeIdentityAndRepoContext(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	builds := map[string]func() (PromptParts, error){
		"think_act": func() (PromptParts, error) {
			return manager.BuildThinkActPrompt(ThinkActPromptInput{})
		},
		"task_planner": func() (PromptParts, error) {
			return manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{Input: "测试输入"})
		},
		"step_replan": func() (PromptParts, error) {
			return manager.BuildStepReplanPrompt(StepReplanPromptInput{CurrentGoal: "目标"})
		},
		"final_answer": func() (PromptParts, error) {
			return manager.BuildFinalAnswerPrompt(FinalAnswerPromptInput{Status: "completed"})
		},
		"intent_classification": func() (PromptParts, error) {
			return manager.BuildIntentClassificationPrompt(IntentClassificationPromptInput{LatestInput: "最新输入"})
		},
	}
	for name, build := range builds {
		parts, err := build()
		if err != nil {
			t.Fatalf("%s build failed: %v", name, err)
		}
		if strings.TrimSpace(parts.SystemRules) == "" || strings.TrimSpace(parts.User) == "" {
			t.Fatalf("%s must produce non-empty system and user parts", name)
		}
		joined := parts.Joined()
		// 原则文本可引用 `<AGENT_ROLE>` 等标签名；只有真正渲染身份块才会出现闭合标签。
		for _, banned := range []string{"</AGENT_ROLE>", "</AGENT_BACKGROUND>", "</AGENT_INSTRUCTION>", "source working dir", "代码仓库上下文"} {
			if strings.Contains(joined, banned) {
				t.Fatalf("%s must not render identity/repo context (%q moved to identity env block), got:\n%s", name, banned, joined)
			}
		}
	}
}

// TestBuildStepReplanPrompt_OmitsJournalPathWhenNotTruncated 校验 PlannerJournalPath 传空串
// （表示 journal 未触发超限截断）时，渲染产物里不应出现"计划全量历史 journal 文件指针"行。
func TestBuildStepReplanPrompt_OmitsJournalPathWhenNotTruncated(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		CurrentGoal:        "测试目标",
		PlannerJournal:     `{"plan_version":1,"kind":"plan"}`,
		PlannerJournalPath: "", // 调用方判定未截断 → 传空串
	})
	if err != nil {
		t.Fatalf("BuildStepReplanPrompt failed: %v", err)
	}
	rendered := parts.Joined()
	if strings.Contains(rendered, "计划全量历史 journal 文件指针") {
		t.Fatalf("journal pointer line must be omitted when not truncated, got:\n%s", rendered)
	}
}

// TestBuildStepReplanPrompt_KeepsJournalPathWhenTruncated 校验 PlannerJournalPath 非空
// （表示 journal 触发超限截断）时，渲染产物含路径指针行供模型按需回读。
func TestBuildStepReplanPrompt_KeepsJournalPathWhenTruncated(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		CurrentGoal:        "测试目标",
		PlannerJournal:     `{"plan_version":1}` + "\n\n（[截断] 仅显示前 1.0 KB。完整内容见文件：/tmp/planner.jsonl）",
		PlannerJournalPath: "/tmp/planner.jsonl",
	})
	if err != nil {
		t.Fatalf("BuildStepReplanPrompt failed: %v", err)
	}
	rendered := parts.Joined()
	if !strings.Contains(rendered, "计划全量历史 journal 文件指针") {
		t.Fatalf("journal pointer line must appear when truncated, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "/tmp/planner.jsonl") {
		t.Fatalf("rendered prompt must contain the journal path, got:\n%s", rendered)
	}
}

func TestPromptManager_StepReplanLedgerAndFactBoardContract(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	parts, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		CurrentGoal:      "测试目标",
		OpenItemsLedger:  "# 未闭环账本\n\n## 未解决\n- [OI-001] x\n\n## 不可解局限\n",
		TaskContextBoard: "# 贯穿全程关键事实\n\n## 输入事实\n- 地址: 10.0.0.1\n\n## 执行中补充\n",
	})
	if err != nil {
		t.Fatalf("build step_replan failed: %v", err)
	}
	with := parts.Joined()
	// 事实烘焙（L4）：具体值内联进条目；账本与事实板全文注入。
	for _, needle := range []string{"烘焙", "<TASK_CONTEXT_BOARD>", "内联", "<OPEN_ITEMS_LEDGER>", "[OI-001] x", "地址: 10.0.0.1"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must inject ledger + fact board with bake contract (missing %q), got:\n%s", needle, with)
		}
	}
	// AI 直接维护共享区：边裁定边落盘 + 提交前落盘终态 + 禁 emoji；指令机制残留禁入。
	for _, needle := range []string{"直接补正", "边裁定边落盘", "落盘终态", "禁止静默消失", "禁止 emoji"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must render direct shared-area maintenance (missing %q), got:\n%s", needle, with)
		}
	}
	for _, banned := range []string{"maintenance_directives", "ledger_add", "archive_item", "context_bake", "merge_staging", "next_id"} {
		if strings.Contains(with, banned) {
			t.Fatalf("step_replan must not retain directive mechanism %q, got:\n%s", banned, with)
		}
	}
	// 旧注入点与 heredoc 写盘指令残留禁入。
	for _, banned := range []string{"CARRIED_INCOMPLETE_ITEMS", "CARRIED_DEPTH_GAPS", "CARRIED_NEW_SURFACES", "<STEP_OUTCOME>", "<TASK_PLAN>", "<STEP_OUTCOMES>", "heredoc"} {
		if strings.Contains(with, banned) {
			t.Fatalf("step_replan must not retain removed injection/instruction %q, got:\n%s", banned, with)
		}
	}
	// evidence-grounded：账本条目要求观测事实锚点。OI-id 对账作为约定但不再以 "ledger_id" 字段名出现。
	for _, needle := range []string{"evidence", "观测事实", "OI-"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must require evidence-grounded ledger entries (missing %q), got:\n%s", needle, with)
		}
	}
	// 内化三轴 + 直接编排：判定纪律保留，但输出字段已删，改为直接产出 pending 计划。
	for _, needle := range []string{"# 判定维度", "# 直接编排（账本维护完成后产出 pending 计划）", "并输出重编排后的"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must render internalized triaxes + direct re-plan section (missing %q), got:\n%s", needle, with)
		}
	}
	for _, banned := range []string{"`incomplete_items`", "`depth_gaps`", "`new_surfaces`"} {
		if strings.Contains(with, banned) {
			t.Fatalf("step_replan must not retain triaxes as output field names %q, got:\n%s", banned, with)
		}
	}
}

func TestPromptManager_ThinkActConcurrentCoverageViaTaskContext(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	parts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		CanSpawnSubAgent: true,
	})
	if err != nil {
		t.Fatalf("build think_act (spawn) failed: %v", err)
	}
	with := parts.SystemRules
	// P11：并发子 Agent 分区下发 + 跨波去重 + 父对账门禁。
	for _, needle := range []string{
		"分区下发",
		"覆盖对账",
		"按全集逐项对账",
	} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act P11 must render concurrent partition + reconciliation (missing %q), got:\n%s", needle, with)
		}
	}
	// 子 Agent 产出归并由 think_act 主动消费子工作区按主视角重判归类，并维护父事实板汇总表。
	for _, needle := range []string{"产出归并", "子 Agent 汇总表", "关键内容索引", "读取路径"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act must render sub-agent merge contract (missing %q), got:\n%s", needle, with)
		}
	}
	// 暂存区机制已废除：三区结构和"待复核"区不再出现。
	for _, banned := range []string{"待复核（子agent）", "## 待复核"} {
		if strings.Contains(with, banned) {
			t.Fatalf("think_act must not retain runtime staging area %q, got:\n%s", banned, with)
		}
	}
	// 账本契约：单文件三区结构 + 唯一写者纪律（路径去参数化为「共享工作区」泛称；
	// 维护职责按终态不变量表述：闭环历史不丢失、非本 step 条目原样保留）。
	for _, needle := range []string{
		"open_items.md",
		"## 未解决",
		"## 不可解局限",
		"## 已闭环",
		"OI-",
		"闭环历史不丢失",
		"原样保留",
		"唯一写者",
	} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act must render open_items ledger contract (missing %q), got:\n%s", needle, with)
		}
	}
	// 收窄：step 不自判不可解局限（裁决交 step_replan）。
	for _, needle := range []string{"不自行判定", "待裁决"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act must keep limitation adjudication out of step entry (missing %q), got:\n%s", needle, with)
		}
	}
	// 旧注入点残留禁入。
	for _, banned := range []string{"DEPENDENCY_STEP_SUMMARIES", "EXECUTION_CONTEXTS"} {
		if strings.Contains(parts.Joined(), banned) {
			t.Fatalf("think_act must not reference removed injection %q, got:\n%s", banned, parts.Joined())
		}
	}
}

// TestBuildSubmitPlanFunctionTool_DescriptionStaysToolScoped 锁定 submit_plan 工具描述只描述
// 工具职责本身，不携带回合相关约束（事实板维护等）——这些约束属于 task_planner system prompt 的
// USER_INPUT_TURN 段，分层后避免约束跨 system/user/tool 三层耦合。
func TestBuildSubmitPlanFunctionTool_DescriptionStaysToolScoped(t *testing.T) {
	tool := buildSubmitPlanFunctionTool()
	desc := tool.Function.Description
	for _, banned := range []string{"task_context.md", "## 输入事实", "维护到与当前输入一致", "用户输入回合", "USER_INPUT_TURN"} {
		if strings.Contains(desc, banned) {
			t.Fatalf("submit_plan description must not embed turn-conditional constraint %q, got: %s", banned, desc)
		}
	}
	if !strings.Contains(desc, "submit_plan"[:5]) && !strings.Contains(desc, "提交") {
		t.Fatalf("submit_plan description should still describe its purpose, got: %s", desc)
	}
}

func TestBuildSubmitReplanFunctionTool_DropsWarningsField(t *testing.T) {
	tool := buildSubmitReplanFunctionTool()
	params, ok := tool.Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("submit_plan parameters not a map: %T", tool.Function.Parameters)
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("submit_plan properties not a map: %T", params["properties"])
	}
	if _, exists := props["warnings"]; exists {
		t.Fatalf("submit_plan must not define a warnings property anymore")
	}
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("submit_plan required not a []string: %T", params["required"])
	}
	for _, r := range required {
		if r == "warnings" {
			t.Fatalf("submit_plan required must not list warnings")
		}
	}
}

func TestPromptManager_StepReplanOpenItemsLedgerGate(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	parts, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		CurrentGoal:     "测试目标",
		OpenItemsLedger: "## 未解决\n- [OI-003] 模块 Y 未深测\n\n## 不可解局限\n",
	})
	if err != nil {
		t.Fatalf("build step_replan failed: %v", err)
	}
	with := parts.Joined()
	// 账本=完整超集，plan items=可行动投影；账本条目沿用 OI-xxx 编号惯例。
	for _, needle := range []string{
		"完整超集",
		"多源共判",
		"投影",
		"[OI-003] 模块 Y 未深测",
		"OI-",
	} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must render superset+projection contract (missing %q), got:\n%s", needle, with)
		}
	}
	// 账本条目逐条复核与归置：受阻影响后续必入轴、高优先级不沉账本、防死循环开闸。
	for _, needle := range []string{
		"受阻且影响后续",
		"高优先级项天然可行动",
		"事实开闸",
		"方法开闸",
	} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must render ledger adjudication rules (missing %q), got:\n%s", needle, with)
		}
	}
	// coverage_checklist 第一手信号消费（原则 A3）。
	for _, needle := range []string{"coverage_checklist", "referenced_prior_coverage", "justified_skip", "uncovered"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must consume coverage_checklist (missing %q), got:\n%s", needle, with)
		}
	}
	// digest 默认 + timeline 按需核验（思考预算上限已删，改为"按需充分核验"自律口径）。
	for _, needle := range []string{"tool_calls_digest", "timeline", "按需充分核验", "digest-only"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must render bounded verification contract (missing %q), got:\n%s", needle, with)
		}
	}
	// 无 workspace 退化分支已删（workspace 恒存在）。
	for _, banned := range []string{"无 workspace 退化", "<WARNINGS>", "完整回抛"} {
		if strings.Contains(with, banned) {
			t.Fatalf("step_replan must not retain workspace-less fallback %q, got:\n%s", banned, with)
		}
	}
}

func TestPromptManager_ThinkActPromptSubAgentGuidanceGate(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	// When the agent can spawn sub-agents, the delegation + await guidance renders.
	withSubAgentParts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		CanSpawnSubAgent: true,
	})
	if err != nil {
		t.Fatalf("build think_act prompt (can spawn) failed: %v", err)
	}
	withSubAgent := withSubAgentParts.SystemRules
	for _, needle := range []string{
		"委派即首选",
		"await_subagents",
		"禁止",
		"完成性与等待",
	} {
		if !strings.Contains(withSubAgent, needle) {
			t.Fatalf("think_act prompt (can spawn) missing guidance %q\nprompt:\n%s", needle, withSubAgent)
		}
	}

	// When the agent is itself a sub-agent (cannot spawn), the delegation section
	// must be hidden entirely, but unrelated principles (3-Strike) must remain.
	withoutSubAgentParts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		CanSpawnSubAgent: false,
	})
	if err != nil {
		t.Fatalf("build think_act prompt (cannot spawn) failed: %v", err)
	}
	withoutSubAgent := withoutSubAgentParts.SystemRules
	for _, absent := range []string{
		"委派即首选",
		"await_subagents",
		"完成性与等待",
	} {
		if strings.Contains(withoutSubAgent, absent) {
			t.Fatalf("think_act prompt (cannot spawn) should not contain %q\nprompt:\n%s", absent, withoutSubAgent)
		}
	}
	if !strings.Contains(withoutSubAgent, "3-Strike") {
		t.Fatalf("think_act prompt (cannot spawn) should still contain 3-Strike principle\nprompt:\n%s", withoutSubAgent)
	}
}

// 5 个 phase prompt 顶部必须按「# 当前阶段 + # Role + # Background」三段结构渲染
// AGENT_ROLE / AGENT_BACKGROUND 占位；空值时 # Role / # Background 不渲染（条件分支）。
func TestPromptManager_PhasePromptsRenderRoleAndBackground(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	const role = "资深安全审计专家"
	const background = "10 年 Web 渗透与代码审计经验"

	cases := []struct {
		name      string
		stageTag  string
		buildWith func(role, bg string) (PromptParts, error)
	}{
		{
			name:     "think_act",
			stageTag: "step（单步执行）",
			buildWith: func(r, b string) (PromptParts, error) {
				return manager.BuildThinkActPrompt(ThinkActPromptInput{AgentRole: r, AgentBackground: b})
			},
		},
		{
			name:     "task_planner",
			stageTag: "planner（任务规划）",
			buildWith: func(r, b string) (PromptParts, error) {
				return manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{Input: "x", AgentRole: r, AgentBackground: b})
			},
		},
		{
			name:     "step_replan",
			stageTag: "step_replan（交付复核与重编排）",
			buildWith: func(r, b string) (PromptParts, error) {
				return manager.BuildStepReplanPrompt(StepReplanPromptInput{CurrentGoal: "目标", AgentRole: r, AgentBackground: b})
			},
		},
		{
			name:     "final_answer",
			stageTag: "final_answer（任务验收与最终交付）",
			buildWith: func(r, b string) (PromptParts, error) {
				return manager.BuildFinalAnswerPrompt(FinalAnswerPromptInput{Status: "completed", AgentRole: r, AgentBackground: b})
			},
		},
		{
			name:     "intent_classification",
			stageTag: "intent_classification（执行意图分析）",
			buildWith: func(r, b string) (PromptParts, error) {
				return manager.BuildIntentClassificationPrompt(IntentClassificationPromptInput{LatestInput: "最新输入", AgentRole: r, AgentBackground: b})
			},
		},
	}

	for _, tc := range cases {
		// 注入 role + background：三段都必须出现。
		parts, err := tc.buildWith(role, background)
		if err != nil {
			t.Fatalf("%s build (with role/bg) failed: %v", tc.name, err)
		}
		sys := parts.SystemRules
		for _, needle := range []string{"# 当前阶段", tc.stageTag, "# Role\n" + role, "# Background\n" + background} {
			if !strings.Contains(sys, needle) {
				t.Fatalf("%s system must render %q, got:\n%s", tc.name, needle, sys)
			}
		}

		// 空 role/bg：「# 当前阶段」恒在，「# Role」/「# Background」段不渲染。
		emptyParts, err := tc.buildWith("", "")
		if err != nil {
			t.Fatalf("%s build (empty role/bg) failed: %v", tc.name, err)
		}
		emptySys := emptyParts.SystemRules
		if !strings.Contains(emptySys, "# 当前阶段") || !strings.Contains(emptySys, tc.stageTag) {
			t.Fatalf("%s system must always render # 当前阶段 + stage tag, got:\n%s", tc.name, emptySys)
		}
		if strings.Contains(emptySys, "# Role\n") || strings.Contains(emptySys, "# Background\n") {
			t.Fatalf("%s system must not render empty # Role / # Background blocks, got:\n%s", tc.name, emptySys)
		}
	}
}

// TestBuildStepReplanPrompt_InjectsMaintainedFilePaths 校验直接维护文件三路径注入：
// 路径齐备时 system 模板渲染「共享区直接维护文件」段（已从 user 模板下沉至 planning_system）；
// 全空时整节不渲染。
func TestBuildStepReplanPrompt_InjectsMaintainedFilePaths(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		CurrentGoal:     "测试目标",
		OpenItemsPath:   "/ws/shared/open_items.md",
		TaskContextPath: "/ws/shared/task_context.md",
		StepFilePath:    "/ws/shared/step_s1.md",
	})
	if err != nil {
		t.Fatalf("BuildStepReplanPrompt failed: %v", err)
	}
	rendered := parts.SystemRules
	for _, needle := range []string{
		"# 共享区文件路径",
		"`open_items.md`（未闭环账本，单文件三区 `## 未解决` / `## 不可解局限` / `## 已闭环`）：`/ws/shared/open_items.md`",
		"`task_context.md`（事实板，`## 输入事实` / `## 执行中补充`）：`/ws/shared/task_context.md`",
		"`step_<step id>.md`（当前 step 过程文件，人类可读摘要）：`/ws/shared/step_s1.md`",
	} {
		if !strings.Contains(rendered, needle) {
			t.Fatalf("replan system prompt missing %q, got:\n%s", needle, rendered)
		}
	}
	if strings.Contains(parts.User, "# 共享区文件路径") {
		t.Fatalf("shared-file-paths section must live in system prompt only, found in user:\n%s", parts.User)
	}

	without, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{CurrentGoal: "测试目标"})
	if err != nil {
		t.Fatalf("BuildStepReplanPrompt (no paths) failed: %v", err)
	}
	if strings.Contains(without.Joined(), "# 共享区文件路径") {
		t.Fatalf("shared-file-paths section must be omitted when paths absent, got:\n%s", without.Joined())
	}
}

// TestBuildThinkActPrompt_InjectsTaskContextPath 校验 think_act user 模板渲染事实板路径条目。
func TestBuildThinkActPrompt_InjectsTaskContextPath(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		HasCurrentStep:      true,
		CurrentStep:         map[string]any{"id": "s1"},
		CurrentStepFilePath: "/ws/shared/step_s1.md",
		TaskContextPath:     "/ws/shared/task_context.md",
		OpenItemsLedgerPath: "/ws/shared/open_items.md",
	})
	if err != nil {
		t.Fatalf("BuildThinkActPrompt failed: %v", err)
	}
	if !strings.Contains(parts.User, "事实板（已预置骨架，按系统 prompt 共享区维护契约即时维护 `## 执行中补充`）：`/ws/shared/task_context.md`") {
		t.Fatalf("think_act user prompt missing task_context path entry, got:\n%s", parts.User)
	}

	without, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		HasCurrentStep: true,
		CurrentStep:    map[string]any{"id": "s1"},
	})
	if err != nil {
		t.Fatalf("BuildThinkActPrompt (no paths) failed: %v", err)
	}
	if strings.Contains(without.User, "事实板（已预置骨架") {
		t.Fatalf("task_context path entry must be omitted when path absent, got:\n%s", without.User)
	}
}

// TestBuildTaskPlannerPrompt_InjectsSharedFilePaths 校验 plan 相位 system 模板也渲染
// 「共享区直接维护文件」段（含事实板/账本绝对路径）。
func TestBuildTaskPlannerPrompt_InjectsSharedFilePaths(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:               "做点事",
		TaskContextPath:     "/ws/shared/task_context.md",
		OpenItemsLedgerPath: "/ws/shared/open_items.md",
	})
	if err != nil {
		t.Fatalf("BuildTaskPlannerPrompt failed: %v", err)
	}
	for _, needle := range []string{
		"# 共享区文件路径",
		"`task_context.md`（事实板，`## 输入事实` / `## 执行中补充`）：`/ws/shared/task_context.md`",
		"`open_items.md`（未闭环账本，单文件三区 `## 未解决` / `## 不可解局限` / `## 已闭环`）：`/ws/shared/open_items.md`",
	} {
		if !strings.Contains(parts.SystemRules, needle) {
			t.Fatalf("planner system prompt missing %q, got:\n%s", needle, parts.SystemRules)
		}
	}
	// plan 相位无 current step，过程文件条目不应渲染。
	if strings.Contains(parts.SystemRules, "`step_<step id>.md`") {
		t.Fatalf("planner system prompt must omit step_file entry, got:\n%s", parts.SystemRules)
	}
}

// TestPlanningSystemPromptContainsGranularityClauses 锁定 planning_system.prompt 的
// step 粒度纪律——对齐 yaklang `generate_plan.go` 的「约 3 步 / >5 必拆 / 禁并列连接 / 按对象拆」
// 四条硬约束；同时禁止旧的「同一动作跨多个对象 → 一条内联完整清单」豁免条款回流。
// 两相位（plan 与 step_replan）共用同一 system prompt，须同时渲染出粒度条款。
func TestPlanningSystemPromptContainsGranularityClauses(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	cases := []struct {
		name  string
		build func() (string, error)
	}{
		{
			name: "plan_phase",
			build: func() (string, error) {
				parts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{})
				return parts.SystemRules, err
			},
		},
		{
			name: "step_replan_phase",
			build: func() (string, error) {
				parts, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
					CurrentGoal: "粒度自检",
				})
				return parts.SystemRules, err
			},
		},
	}

	required := []string{
		"约 3 次工具调用",
		">5 必拆",
		"并且 / 同时 / + / 以及",
		"按操作对象",
		"不在一条内列清单",
		"粒度自检",
	}
	forbidden := []string{
		// 旧豁免条款：同一动作跨多个对象（逐一处理 A/B/C）→ 一条内联完整清单
		"逐一处理 A/B/C",
		"一条内联完整清单",
	}

	for _, tc := range cases {
		rendered, err := tc.build()
		if err != nil {
			t.Fatalf("%s build failed: %v", tc.name, err)
		}
		for _, needle := range required {
			if !strings.Contains(rendered, needle) {
				t.Fatalf("%s prompt missing granularity clause %q, got:\n%s", tc.name, needle, rendered)
			}
		}
		for _, banned := range forbidden {
			if strings.Contains(rendered, banned) {
				t.Fatalf("%s prompt must drop legacy exemption %q, got:\n%s", tc.name, banned, rendered)
			}
		}
	}
}

// TestBuildSubmitPlanFunctionTool_StepDescriptionContainsGranularityConstraints 锁定
// submit_plan / submit_replan 两入口的 step 字段 description 都含粒度上限与按对象拆条款，
// 同时禁止旧豁免条款（"同一种处理作用于多个对象算一件事可在一条内把对象列成清单"）回流。
func TestBuildSubmitPlanFunctionTool_StepDescriptionContainsGranularityConstraints(t *testing.T) {
	required := []string{
		"约 3 次工具调用",
		">5 必拆",
		"并且/同时",
		"按对象逐条拆",
	}
	forbidden := "算一件事可在一条内把对象列成清单"

	for _, tc := range []struct {
		name    string
		stepKey string
		desc    func(t *testing.T) string
	}{
		{
			name: "submit_plan",
			desc: func(t *testing.T) string {
				tool := buildSubmitPlanFunctionTool()
				params, ok := tool.Function.Parameters.(map[string]any)
				if !ok {
					t.Fatalf("submit_plan parameters not a map: %T", tool.Function.Parameters)
				}
				return digStepDescription(t, params, []string{"properties", "plan", "items", "properties", "step", "description"})
			},
		},
		{
			name: "submit_replan",
			desc: func(t *testing.T) string {
				tool := buildSubmitReplanFunctionTool()
				params, ok := tool.Function.Parameters.(map[string]any)
				if !ok {
					t.Fatalf("submit_replan parameters not a map: %T", tool.Function.Parameters)
				}
				return digStepDescription(t, params, []string{"properties", "plan", "items", "properties", "step", "description"})
			},
		},
	} {
		desc := tc.desc(t)
		for _, needle := range required {
			if !strings.Contains(desc, needle) {
				t.Fatalf("%s step description missing %q, got: %s", tc.name, needle, desc)
			}
		}
		if strings.Contains(desc, forbidden) {
			t.Fatalf("%s step description must drop legacy exemption %q, got: %s", tc.name, forbidden, desc)
		}
	}
}

func digStepDescription(t *testing.T, root map[string]any, path []string) string {
	t.Helper()
	cur := any(root)
	for i, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path[%d] %q: parent not a map (%T)", i, key, cur)
		}
		next, exists := m[key]
		if !exists {
			t.Fatalf("path[%d] %q missing in: %+v", i, key, m)
		}
		cur = next
	}
	desc, ok := cur.(string)
	if !ok {
		t.Fatalf("description not a string: %T", cur)
	}
	return desc
}
