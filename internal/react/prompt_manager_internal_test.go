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
	for _, needle := range []string{"5.1e", shared + "/task_context.md", "读取", "重写", "执行中补充"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act with shared dir must render 5.1e fact board (missing %q), got:\n%s", needle, with)
		}
	}
	for _, needle := range []string{"后续阶段判断", "coverage_checklist", "referenced_prior_coverage", "uncovered"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act should render updated replan ownership + checklist contract (missing %q), got:\n%s", needle, with)
		}
	}
	if strings.Contains(with, "由后续 `final_answer` phase 判断") {
		t.Fatalf("think_act should not assign replanning to final_answer anymore, got:\n%s", with)
	}

	without, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		AgentInstruction: "你是测试代理",
	})
	if err != nil {
		t.Fatalf("build think_act (no shared) failed: %v", err)
	}
	if strings.Contains(without, "5.1e") {
		t.Fatalf("think_act without shared dir must not render 5.1e block, got:\n%s", without)
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
	// 原则 0.8 重定位为"以三轴为主的多源综合"，旧 co-equal "交叉分析" 框架须移除。
	if !strings.Contains(with, "以三轴为主") {
		t.Fatalf("task_planner 原则0.8 must render 三轴为主 multi-source framing, got:\n%s", with)
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

	without, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input:         "测试输入",
		UserInputTurn: true,
	})
	if err != nil {
		t.Fatalf("build task_planner (no shared) failed: %v", err)
	}
	if strings.Contains(without, "贯穿事实校正") {
		t.Fatalf("task_planner without shared dir must not render correction guidance, got:\n%s", without)
	}
}

func TestPromptManager_StepReplanTaskContextParticipateGate(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}

	const shared = "/ws/shared"
	with, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentInstruction:   "你是测试代理",
		WorkspaceSharedDir: shared,
	})
	if err != nil {
		t.Fatalf("build step_replan (has shared) failed: %v", err)
	}
	for _, needle := range []string{"烘焙", shared + "/task_context.md", "只读", "内联", "不是要机械原样回抛", "当前仍成立"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan with shared dir must bake facts into the three axes (missing %q), got:\n%s", needle, with)
		}
	}
	// Read-only: must not instruct writing back to the fact board.
	if strings.Contains(with, "回写贯穿事实") {
		t.Fatalf("step_replan bake block must be read-only (no writeback), got:\n%s", with)
	}

	without, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentInstruction: "你是测试代理",
	})
	if err != nil {
		t.Fatalf("build step_replan (no shared) failed: %v", err)
	}
	if strings.Contains(without, "烘焙") {
		t.Fatalf("step_replan without shared dir must not render bake block, got:\n%s", without)
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
	// Fix6: 原则 12b now drives concurrent coverage via task_context, no ledger.
	for _, needle := range []string{
		"按全集分区下发",
		"覆盖对账",
		"父汇合兜底对账",
	} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act 原则12b must route concurrent coverage via task_context (missing %q), got:\n%s", needle, with)
		}
	}
	// Fix5: child open_items.md route pointer in上卷.
	for _, needle := range []string{"open_items.md", "路由指针"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("think_act 上卷 must leave child open_items pointer (missing %q), got:\n%s", needle, with)
		}
	}
	// Fix4: result multi-key coexistence.
	if !strings.Contains(with, "多 key 共存") {
		t.Fatalf("think_act must render result multi-key coexistence contract, got:\n%s", with)
	}
	// Fix6: no coverage-ledger residue.
	for _, banned := range []string{"coverage-ledger", "coverage_ledger"} {
		if strings.Contains(with, banned) {
			t.Fatalf("think_act must not reference removed %q, got:\n%s", banned, with)
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

	const shared = "/ws/shared"
	with, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentInstruction:   "你是测试代理",
		WorkspaceSharedDir: shared,
	})
	if err != nil {
		t.Fatalf("build step_replan (has shared) failed: %v", err)
	}
	// Fix1: persistent ledger open_items.md with three sections + no-direct-delete discipline.
	for _, needle := range []string{
		shared + "/open_items.md",
		"## 未解决",
		"## 不可解局限",
		"## 已闭环",
		"禁止直接删",
		"整段覆盖重写",
		"原则 7.1",
	} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan with shared must render open_items ledger (missing %q), got:\n%s", needle, with)
		}
	}
	// Fix5: sub-agent open_items merge回流.
	for _, needle := range []string{"子 agent", "合并进父账本"} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must render sub-agent open_items 回流 (missing %q), got:\n%s", needle, with)
		}
	}
	// Fix2 (ungated): convergence limited to three axes; blocked-affects-future→three axes.
	for _, needle := range []string{
		"适用于三组承载项 / 三轴输出",
		"受阻但影响后续执行的项必入三轴",
	} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must scope convergence rules (missing %q), got:\n%s", needle, with)
		}
	}
	// Fix7/Fix9: limitations routed to ledger (no meta-info about removed field).
	if !strings.Contains(with, "不可解局限的唯一载体是账本") {
		t.Fatalf("step_replan must route limitations to ledger (missing %q), got:\n%s", "不可解局限的唯一载体是账本", with)
	}
	// Fix7: the rendered submit_plan JSON schema must no longer require/define warnings.
	if strings.Contains(with, "\"new_surfaces\", \"warnings\"]") {
		t.Fatalf("step_replan submit_plan schema must drop warnings from required, got:\n%s", with)
	}
	if strings.Contains(with, "\"warnings\": {") {
		t.Fatalf("step_replan submit_plan schema must drop the warnings property, got:\n%s", with)
	}
	// coverage_checklist consumption (原则 0.2, ungated): first-hand per-item signal feeds axes/warnings.
	for _, needle := range []string{
		"原则 0.2",
		"coverage_checklist",
		"referenced_prior_coverage",
		"justified_skip",
		"uncovered",
	} {
		if !strings.Contains(with, needle) {
			t.Fatalf("step_replan must consume think_act coverage_checklist (missing %q), got:\n%s", needle, with)
		}
	}
	// Fix6: no coverage-ledger residue.
	for _, banned := range []string{"coverage-ledger", "coverage_ledger"} {
		if strings.Contains(with, banned) {
			t.Fatalf("step_replan must not reference removed %q, got:\n%s", banned, with)
		}
	}
	// Fix7 follow-up: <WARNINGS> injection removed from step_replan entirely.
	if strings.Contains(with, "<WARNINGS>") {
		t.Fatalf("step_replan must not inject <WARNINGS> after dropping warnings field, got:\n%s", with)
	}

	without, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		AgentInstruction: "你是测试代理",
	})
	if err != nil {
		t.Fatalf("build step_replan (no shared) failed: %v", err)
	}
	if strings.Contains(without, "open_items.md") {
		t.Fatalf("step_replan without shared dir must not render open_items ledger, got:\n%s", without)
	}
	// Non-workspace fallback: in-memory carried items are the only truth, must完整回抛.
	for _, needle := range []string{"无 workspace 退化", "完整回抛"} {
		if !strings.Contains(without, needle) {
			t.Fatalf("step_replan no-shared fallback must render completeness wording (missing %q), got:\n%s", needle, without)
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
		"子 Agent 完成性",
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
		"子 Agent 完成性",
	} {
		if strings.Contains(withoutSubAgent, absent) {
			t.Fatalf("think_act prompt (cannot spawn) should not contain %q\nprompt:\n%s", absent, withoutSubAgent)
		}
	}
	if !strings.Contains(withoutSubAgent, "3-Strike") {
		t.Fatalf("think_act prompt (cannot spawn) should still contain 3-Strike principle\nprompt:\n%s", withoutSubAgent)
	}
}
