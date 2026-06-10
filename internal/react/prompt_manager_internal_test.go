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
				return manager.BuildThinkActPrompt(ThinkActPromptInput{
					AgentInstruction: "你是测试代理",
				})
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

	const shared = "/ws/shared"
	with, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		AgentInstruction:   "你是测试代理",
		WorkspaceSharedDir: shared,
	})
	if err != nil {
		t.Fatalf("build think_act (has shared) failed: %v", err)
	}
	// workspace 恒存在：事实板（6.2）与账本（6.3）无条件渲染，不再有无-workspace 分支。
	for _, needle := range []string{"6.2", shared + "/task_context.md", "read_file", "整段覆盖重写", "执行中补充"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act must render 6.2 fact board (missing %q), got:\n%s", needle, with)
		}
	}
	for _, needle := range []string{"coverage_checklist", "uncovered", "open_item_ids"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act should render checklist + ledger id contract (missing %q), got:\n%s", needle, with)
		}
	}
	// step 文件模板（6.4）：progress + 流程与产出 两节。
	for _, needle := range []string{"step.md", "## progress", "流程与产出"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act should render step.md template contract (missing %q), got:\n%s", needle, with)
		}
	}
}

func TestPromptManager_TaskPlannerTaskContextWriteGate(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	const shared = "/ws/shared"
	// 用户输入回合（cold_start / replan / carry）+ shared dir：渲染校正引导。
	with, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:              "测试输入",
		WorkspaceSharedDir: shared,
		UserInputTurn:      true,
	})
	if err != nil {
		t.Fatalf("build task_planner (user turn) failed: %v", err)
	}
	if !strings.Contains(with, "贯穿事实校正") || !strings.Contains(with, "输入事实") || !strings.Contains(with, shared+"/task_context.md") {
		t.Fatalf("task_planner user-input turn must render correction guidance + 输入事实 + path, got:\n%s", with)
	}
	// The removed structured array field must not reappear in the schema.
	if strings.Contains(with, `"task_context"`) {
		t.Fatalf("task_planner must not contain the removed task_context schema field, got:\n%s", with)
	}
	// 事实板综合（R6 三轴为主轴）归重规划分支：非重规划回合不渲染。
	if strings.Contains(with, "三轴为主轴") {
		t.Fatalf("task_planner non-replan turn must not render replan branch, got:\n%s", with)
	}
	replan, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:              "测试输入",
		WorkspaceSharedDir: shared,
		UserInputTurn:      false,
		HasReplanContext:   true,
	})
	if err != nil {
		t.Fatalf("build task_planner (replan turn) failed: %v", err)
	}
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

	// 运行过程中回合（step_replan 内部重规划 / 子 Agent 等待）：即便有 shared dir 也不渲染。
	inRun, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:              "测试输入",
		WorkspaceSharedDir: shared,
		UserInputTurn:      false,
	})
	if err != nil {
		t.Fatalf("build task_planner (in-run turn) failed: %v", err)
	}
	if strings.Contains(inRun, "贯穿事实校正") {
		t.Fatalf("task_planner in-run turn must not render correction guidance, got:\n%s", inRun)
	}

}

func TestPromptManager_ThinkActRepoContextSection(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	prompt, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		AgentInstruction: "你是测试代理",
		RuntimeRepoContext: RuntimeRepoContext{
			SourceWorkingDir: "/repo/worktree",
			RepoRootDir:      "/repo/worktree",
			IsGitRepo:        true,
			Branch:           "feature/demo",
			IsWorktree:       true,
		},
	})
	if err != nil {
		t.Fatalf("build think_act failed: %v", err)
	}
	for _, needle := range []string{
		"source working dir: /repo/worktree",
		"repo root: /repo/worktree",
		"is git repo: true",
		"current branch: feature/demo",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected think_act repo section to contain %q, got:\n%s", needle, prompt)
		}
	}
}

func TestPromptManager_TaskPlannerRepoContextSection(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	prompt, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input: "测试输入",
		RuntimeRepoContext: RuntimeRepoContext{
			SourceWorkingDir: "/repo/worktree",
			RepoRootDir:      "/repo/worktree",
			IsGitRepo:        true,
			Branch:           "feature/demo",
			IsWorktree:       true,
		},
	})
	if err != nil {
		t.Fatalf("build task_planner failed: %v", err)
	}
	for _, needle := range []string{
		"### 6.1 代码仓库上下文",
		"source working dir: `/repo/worktree`",
		"repo root: `/repo/worktree`",
		"is git repo: `true`",
		"current branch: `feature/demo`",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected task_planner repo section to contain %q, got:\n%s", needle, prompt)
		}
	}
	if strings.Contains(prompt, "github.com") || strings.Contains(prompt, "gitRepoUrl") {
		t.Fatalf("task_planner prompt must not expose remote url by default, got:\n%s", prompt)
	}
}

func TestPromptManager_StepReplanRepoContextSection(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	prompt, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentInstruction: "你是测试代理",
		RuntimeRepoContext: RuntimeRepoContext{
			SourceWorkingDir: "/repo/worktree",
			RepoRootDir:      "/repo/worktree",
			IsGitRepo:        true,
			Branch:           "feature/demo",
			IsWorktree:       true,
		},
	})
	if err != nil {
		t.Fatalf("build step_replan failed: %v", err)
	}
	for _, needle := range []string{
		"### 6.3 代码仓库上下文",
		"source working dir: `/repo/worktree`",
		"repo root: `/repo/worktree`",
		"is git repo: `true`",
		"current branch: `feature/demo`",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected step_replan repo section to contain %q, got:\n%s", needle, prompt)
		}
	}
}

func TestPromptManager_FinalAnswerRepoContextSection(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	prompt, err := manager.BuildFinalAnswerPrompt(FinalAnswerPromptInput{
		AgentInstruction: "你是测试代理",
		RuntimeRepoContext: RuntimeRepoContext{
			SourceWorkingDir: "/repo/worktree",
			RepoRootDir:      "/repo/worktree",
			IsGitRepo:        true,
			Branch:           "feature/demo",
			IsWorktree:       true,
		},
	})
	if err != nil {
		t.Fatalf("build final_answer failed: %v", err)
	}
	for _, needle := range []string{
		"### 6.2 代码仓库上下文",
		"source working dir: `/repo/worktree`",
		"repo root: `/repo/worktree`",
		"is git repo: `true`",
		"current branch: `feature/demo`",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected final_answer repo section to contain %q, got:\n%s", needle, prompt)
		}
	}
	if strings.Contains(prompt, "git@github.com") || strings.Contains(prompt, "gitRepoUrl") {
		t.Fatalf("final_answer prompt must not expose remote url by default, got:\n%s", prompt)
	}
}

func TestPromptManager_StepReplanLedgerAndFactBoardContract(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	with, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentInstruction: "你是测试代理",
		OpenItemsLedger:  "# 未闭环账本\nnext_id: 2\n\n## 未解决\n- [OI-001] x\n\n## 不可解局限\n\n## 待复核（子agent）\n",
		TaskContextBoard: "# 贯穿全程关键事实\n\n## 输入事实\n- 地址: 10.0.0.1\n\n## 执行中补充\n",
	})
	if err != nil {
		t.Fatalf("build step_replan failed: %v", err)
	}
	// 事实烘焙（L4）：只读 + 具体值内联进条目；账本与事实板全文注入。
	for _, needle := range []string{"烘焙", "<TASK_CONTEXT_BOARD>", "只读", "内联", "<OPEN_ITEMS_LEDGER>", "[OI-001] x", "地址: 10.0.0.1"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must inject ledger + fact board with bake contract (missing %q), got:\n%s", needle, with)
		}
	}
	// 不直接写文件：维护经 maintenance_directives；收敛显式可审计。
	for _, needle := range []string{"maintenance_directives", "不直接写", "ledger_add", "archive_item", "context_bake", "禁止静默消失"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must route maintenance via directives (missing %q), got:\n%s", needle, with)
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

	const shared = "/ws/shared"
	with, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		AgentInstruction:   "你是测试代理",
		WorkspaceSharedDir: shared,
		CanSpawnSubAgent:   true,
	})
	if err != nil {
		t.Fatalf("build think_act (shared + spawn) failed: %v", err)
	}
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
	// 账本 6.3：OI-id 取号 + 三区结构 + 唯一语义写者纪律。
	for _, needle := range []string{
		"6.3",
		shared + "/open_items.md",
		"## 未解决",
		"## 不可解局限",
		"next_id",
		"OI-",
		"整段覆盖重写",
		"禁止直接删",
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
		if strings.Contains(with, banned) {
			t.Fatalf("think_act must not reference removed injection %q, got:\n%s", banned, with)
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

	with, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentInstruction: "你是测试代理",
		OpenItemsLedger:  "## 未解决\n- [OI-003] 模块 Y 未深测\n\n## 不可解局限\n\n## 待复核（子agent）\n",
	})
	if err != nil {
		t.Fatalf("build step_replan failed: %v", err)
	}
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
	withSubAgent, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		AgentInstruction: "你是测试代理",
		CanSpawnSubAgent: true,
	})
	if err != nil {
		t.Fatalf("build think_act prompt (can spawn) failed: %v", err)
	}
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
	withoutSubAgent, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		AgentInstruction: "你是测试代理",
		CanSpawnSubAgent: false,
	})
	if err != nil {
		t.Fatalf("build think_act prompt (cannot spawn) failed: %v", err)
	}
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
