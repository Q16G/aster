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
	// 边完成边归档（持续不变量）+ 共享区禁 emoji。
	for _, needle := range []string{"解决即归档", "立即迁入归档", "禁止 emoji"} {
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
	for _, needle := range []string{"step 过程文件", "## 子步骤清单", "## 进展记录", "## 收尾产出", "每轮执行结束时文件与实际进度一致"} {
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

	// 用户输入回合（cold_start / replan / carry）：渲染共享区终态段 + 事实板快照。
	withParts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:            "测试输入",
		UserInputTurn:    true,
		TaskContextBoard: "# 贯穿全程关键事实\n\n## 输入事实\n- 目标: x\n\n## 执行中补充\n",
	})
	if err != nil {
		t.Fatalf("build task_planner (user turn) failed: %v", err)
	}
	with := withParts.Joined()
	for _, needle := range []string{"共享区终态", "唯一语义写者", "提交执行计划前", "输入事实", "环境/参数事实", "禁止 emoji", "task_context.md", "<TASK_CONTEXT_BOARD>"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("task_planner user-input turn must render 共享区终态 section + board snapshot (missing %q), got:\n%s", needle, with)
		}
	}
	// The removed structured array field must not reappear in the schema.
	if strings.Contains(with, `"task_context"`) {
		t.Fatalf("task_planner must not contain the removed task_context schema field, got:\n%s", with)
	}
	// 事实板综合（R6 三轴为主轴）归重规划分支：非重规划回合不渲染。
	if strings.Contains(with, "三轴为主轴") {
		t.Fatalf("task_planner non-replan turn must not render replan branch, got:\n%s", with)
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
	for _, needle := range []string{"三轴为主轴", "只编排不二次泛化", "产物-消费依赖", "最小扰动"} {
		if !strings.Contains(replan, needle) {
			t.Fatalf("task_planner replan turn must render replan branch (missing %q), got:\n%s", needle, replan)
		}
	}
	if strings.Contains(with, "交叉分析") {
		t.Fatalf("task_planner must not retain the old co-equal 交叉分析 framing, got:\n%s", with)
	}
	if strings.Contains(with, "至少 3 步") {
		t.Fatalf("task_planner must not retain the hard minimum 3-step rule, got:\n%s", with)
	}
	if !strings.Contains(with, "2-4 步") {
		t.Fatalf("task_planner should align recon-first planning around 2-4 steps, got:\n%s", with)
	}

	// 运行过程中回合（step_replan 内部重规划 / 子 Agent 等待）：不渲染共享区终态段与事实板块。
	inRunParts, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:         "测试输入",
		UserInputTurn: false,
	})
	if err != nil {
		t.Fatalf("build task_planner (in-run turn) failed: %v", err)
	}
	inRun := inRunParts.Joined()
	for _, needle := range []string{"共享区终态", "<TASK_CONTEXT_BOARD>"} {
		if strings.Contains(inRun, needle) {
			t.Fatalf("task_planner in-run turn must not render %q, got:\n%s", needle, inRun)
		}
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

func TestPromptManager_StepReplanLedgerAndFactBoardContract(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	parts, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		CurrentGoal:      "测试目标",
		OpenItemsLedger:  "# 未闭环账本\n\n## 未解决\n- [OI-001] x\n\n## 不可解局限\n\n## 待复核（子agent）\n",
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
	// evidence-grounded：三轴条目要求 evidence 锚点。
	for _, needle := range []string{"evidence", "观测事实", "ledger_id"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must require evidence-grounded axis items (missing %q), got:\n%s", needle, with)
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
	// 子 Agent 产出经 runtime 机械回流暂存区，think_act 收尾归并并维护汇总表。
	for _, needle := range []string{"待复核（子agent）", "子 Agent 汇总表", "关键内容索引", "读取路径", "归并"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act must render staging merge + summary index (missing %q), got:\n%s", needle, with)
		}
	}
	// 账本契约：三区结构 + 唯一写者纪律（路径去参数化为「共享工作区」泛称；
	// 维护职责按终态不变量表述：归档历史不丢失、非本 step 条目原样保留）。
	for _, needle := range []string{
		"open_items.md",
		"## 未解决",
		"## 不可解局限",
		"OI-",
		"归档历史不丢失",
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
		OpenItemsLedger: "## 未解决\n- [OI-003] 模块 Y 未深测\n\n## 不可解局限\n\n## 待复核（子agent）\n",
	})
	if err != nil {
		t.Fatalf("build step_replan failed: %v", err)
	}
	with := parts.Joined()
	// 账本=完整超集，多源共判先账本后三轴；每条轴项以 ledger_id 对账。
	for _, needle := range []string{
		"完整超集",
		"多源共判",
		"投影",
		"[OI-003] 模块 Y 未深测",
		"ledger_id",
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
	// digest 默认 + timeline 按需 + 探索预算降级（B3 协议入 prompt）。
	for _, needle := range []string{"tool_calls_digest", "timeline", "探索预算", "digest-only"} {
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
			stageTag: "step_replan（交付复核）",
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
