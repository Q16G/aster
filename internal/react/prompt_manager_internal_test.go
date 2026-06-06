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
	for _, needle := range []string{"烘焙", shared + "/task_context.md", "只读", "内联"} {
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
