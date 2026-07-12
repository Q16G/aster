package react

import (
	"strings"
	"testing"
)

// TestStepReplanPrompt_CapabilityHasGates 验证 step_replan 的 HAS_SKILLS_TABLE / HAS_AVAILABLE_TOOLS
// 门在填充时渲染对应段落、在空时不渲染。负例是关键：若 HasSkillsTable()/HasAvailableTools() 退化为
// 方法值（永真），空能力也会渲染 <SKILLS_INDEX> / 工具段——本测试锁死该回归。
func TestStepReplanPrompt_CapabilityHasGates(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager: %v", err)
	}

	// 填充：Skills 表 + 可用工具。
	full, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{
		CapabilityIndex: CapabilityIndex{
			SkillsContext:  &SkillsPromptContext{Table: "SENTINEL_SKILL_ROW_replan"},
			AvailableTools: []AvailableToolInfo{{Name: "sentinel_tool_replan", Description: "d"}},
		},
	})
	if err != nil {
		t.Fatalf("build full: %v", err)
	}
	// Skills 表已下沉 system（SystemRules）；可用工具仍在 user。
	if !strings.Contains(full.SystemRules, "<SKILLS_INDEX>") || !strings.Contains(full.SystemRules, "SENTINEL_SKILL_ROW_replan") {
		t.Error("HAS_SKILLS_TABLE 应在 system 渲染 <SKILLS_INDEX> 与表内容")
	}
	if !strings.Contains(full.User, "sentinel_tool_replan") {
		t.Error("HAS_AVAILABLE_TOOLS 应渲染可用工具段")
	}

	// 空：两门均 false，对应段落不出现。
	empty, err := manager.BuildStepReplanPrompt(StepReplanPromptInput{})
	if err != nil {
		t.Fatalf("build empty: %v", err)
	}
	if strings.Contains(empty.SystemRules, "<SKILLS_INDEX>") {
		t.Error("空 Skills 不应渲染 <SKILLS_INDEX>（HAS gate 应为 false，非永真）")
	}
	if strings.Contains(empty.User, "sentinel_tool_replan") {
		t.Error("空工具不应渲染工具段")
	}
}

// TestTaskPlannerPrompt_CapabilityHasGates 验证 task_planner 的 HAS_SKILLS_TABLE / HAS_MCP_TABLE /
// HAS_AVAILABLE_TOOLS 门渲染正确（填充出现、空缺席），锁死三门的方法-值回归。
func TestTaskPlannerPrompt_CapabilityHasGates(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager: %v", err)
	}

	full, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{
		Input: "任务",
		CapabilityIndex: CapabilityIndex{
			SkillsContext:  &SkillsPromptContext{Table: "SENTINEL_SKILL_ROW_planner"},
			MCPContext:     &MCPPromptContext{Table: "SENTINEL_MCP_ROW_planner"},
			AvailableTools: []AvailableToolInfo{{Name: "sentinel_tool_planner", Description: "d"}},
		},
	})
	if err != nil {
		t.Fatalf("build full: %v", err)
	}
	// Skills 表 / MCP 表已下沉 system（SystemRules）；可用工具仍在 user。
	for _, want := range []string{"<SKILLS_INDEX>", "SENTINEL_SKILL_ROW_planner", "<MCP_SERVERS>", "SENTINEL_MCP_ROW_planner"} {
		if !strings.Contains(full.SystemRules, want) {
			t.Errorf("填充能力应在 system 渲染 %q", want)
		}
	}
	if !strings.Contains(full.User, "sentinel_tool_planner") {
		t.Error("填充能力应在 user 渲染可用工具段 sentinel_tool_planner")
	}

	empty, err := manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{Input: "任务"})
	if err != nil {
		t.Fatalf("build empty: %v", err)
	}
	for _, notWant := range []string{"<SKILLS_INDEX>", "<MCP_SERVERS>"} {
		if strings.Contains(empty.SystemRules, notWant) {
			t.Errorf("空能力不应在 system 渲染 %q（HAS gate 应为 false，非永真）", notWant)
		}
	}
	if strings.Contains(empty.User, "sentinel_tool_planner") {
		t.Error("空工具不应渲染工具段")
	}
}

// TestAgentInstruction_PhaseGates 锁死 instruction 的任务级/执行级分层渲染契约：
// 任务级 phase（planner/step_replan/final_answer/intent_classification）非空时渲染
// Instruction 段、空时缺席；执行级 phase（think_act/sub_agent）任何时候不渲染；
// block2（agent_identity_env）为纯 env 块，不再含 AGENT_INSTRUCTION。
func TestAgentInstruction_PhaseGates(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager: %v", err)
	}
	const sentinel = "SENTINEL_INSTRUCTION_输出简洁的分析结论"
	profile := AgentProfile{AgentRole: "审计专家", AgentInstruction: sentinel}

	taskLevel := map[string]func(AgentProfile) (PromptParts, error){
		"planner": func(p AgentProfile) (PromptParts, error) {
			return manager.BuildTaskPlannerPrompt(TaskPlannerPromptInput{AgentProfile: p, Input: "任务"})
		},
		"step_replan": func(p AgentProfile) (PromptParts, error) {
			return manager.BuildStepReplanPrompt(StepReplanPromptInput{AgentProfile: p})
		},
		"final_answer": func(p AgentProfile) (PromptParts, error) {
			return manager.BuildFinalAnswerPrompt(FinalAnswerPromptInput{AgentProfile: p})
		},
		"intent_classification": func(p AgentProfile) (PromptParts, error) {
			return manager.BuildIntentClassificationPrompt(IntentClassificationPromptInput{AgentProfile: p, LatestInput: "输入"})
		},
	}
	for name, build := range taskLevel {
		full, err := build(profile)
		if err != nil {
			t.Fatalf("%s build full: %v", name, err)
		}
		if !strings.Contains(full.SystemRules, "# Instruction\n"+sentinel) {
			t.Errorf("%s 非空 instruction 应在 system 渲染 Instruction 段", name)
		}
		empty, err := build(AgentProfile{AgentRole: "审计专家"})
		if err != nil {
			t.Fatalf("%s build empty: %v", name, err)
		}
		// 断言锚定标题段（"# Instruction" 同时命中 "## Instruction"），避免正文出现
		// 英文单词 Instruction 时误报。
		if strings.Contains(empty.SystemRules, "# Instruction") {
			t.Errorf("%s 空 instruction 不应渲染 Instruction 段", name)
		}
	}

	execLevel := map[string]func() (PromptParts, error){
		"think_act": func() (PromptParts, error) {
			return manager.BuildThinkActPrompt(ThinkActPromptInput{AgentProfile: profile})
		},
		"sub_agent": func() (PromptParts, error) {
			return manager.BuildSubAgentPrompt(SubAgentPromptInput{AgentProfile: profile, Task: "委派任务"})
		},
	}
	for name, build := range execLevel {
		parts, err := build()
		if err != nil {
			t.Fatalf("%s build: %v", name, err)
		}
		if strings.Contains(parts.SystemRules, sentinel) {
			t.Errorf("%s 为执行级 phase，不应渲染 instruction", name)
		}
	}

	env, err := manager.BuildAgentIdentityEnvPrompt(AgentIdentityEnvPromptInput{
		WorkspaceRootDir:   "/tmp/ws",
		WorkspaceSharedDir: "/tmp/ws/shared",
	})
	if err != nil {
		t.Fatalf("identity env build: %v", err)
	}
	if strings.Contains(env, "AGENT_INSTRUCTION") {
		t.Error("block2 应为纯 env 块，不应再含 AGENT_INSTRUCTION")
	}
}
