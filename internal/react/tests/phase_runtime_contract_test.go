package react_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"aster/internal/builtin_tools"
	. "aster/internal/react"
)

func TestBuildStepReferences_UsesExplicitAndArtifactsOnly(t *testing.T) {
	t.Skip("V2 step attempt results no longer generate legacy step reference lists")
}

func TestBuildStepReplanPrompt_UsesSemanticBlocks(t *testing.T) {
	agent, err := NewReActAgent("prompt-test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	prompt, err := agent.BuildStepReplanPrompt(map[string]any{
		"current_goal":  "继续推进",
		"current_step":  map[string]any{"id": "step-1", "step": "执行"},
		"step_outcome":  `{"summary":"done","status":"completed"}`,
		"task_plan":     []map[string]any{{"id": "step-1", "step": "执行", "status": "completed"}},
		"step_outcomes": []map[string]any{{"step_id": "step-1", "status": "completed"}},
		"warnings":      []string{"warn-1"},
		"unresolved":    []string{"missing-1"},
	})
	if err != nil {
		t.Fatalf("buildStepReplanPrompt failed: %v", err)
	}

	for _, marker := range []string{"CURRENT_GOAL", "CURRENT_STEP", "TASK_PLAN", "STEP_OUTCOME"} {
		if !strings.Contains(prompt, marker) {
			t.Fatalf("expected marker %s in prompt, got:\n%s", marker, prompt)
		}
	}
}

func TestNewReActAgent_NoDomainToolsByDefault(t *testing.T) {
	agent, err := NewReActAgent("tool-test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	for _, name := range []string{"list_files", "read_file", "rg", "grep", "ast_grep"} {
		if _, ok := agent.GetTool(name); ok {
			t.Fatalf("expected %s to not be registered by default (domain tools should be opt-in)", name)
		}
	}
	// Platform-level tools should always be present.
	for _, name := range []string{"update_current_step", "task_status", "human_confirm"} {
		if _, ok := agent.GetTool(name); !ok {
			t.Fatalf("expected platform tool %s to be registered by default", name)
		}
	}
}

func TestBuildThinkActPrompt_CurrentStatusConditionalRendering(t *testing.T) {
	// HAS_CURRENT_STATUS=false → no CURRENT_STATUS block
	agent, err := NewReActAgent("status-test", &stubChatClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}
	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:  builtin_tools.AgentPhaseStep,
		Status: builtin_tools.TaskStatusRunning,
	})
	prompt := agent.BuildThinkActPrompt(context.Background(), "", nil)
	if strings.Contains(prompt, "<CURRENT_STATUS>") {
		t.Fatalf("expected no CURRENT_STATUS block when statusContext is empty, got %s", prompt)
	}
}

func TestBuildThinkActPrompt_UsesExpandedSections(t *testing.T) {
	agent, err := NewReActAgent(
		"think-act-test",
		&stubChatClient{},
		WithEmitter(NewDummyEmitter()),
		WithInstruction("你是代码审查代理"),
	)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	agent.ReplaceState(builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhaseStep,
		Status:        builtin_tools.TaskStatusRunning,
		CurrentGoal:   "完成代码审查",
		CurrentStepID: "step-2",
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "请审查代码", CreatedAt: time.Unix(0, 0)},
		},
		Plan: []*builtin_tools.PlanItem{
			{ID: "step-1", Step: "收集证据", Status: builtin_tools.PlanStepCompleted},
			{ID: "step-2", Step: "检查结果", Status: builtin_tools.PlanStepInProgress, DependsOn: []string{"step-1"}},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{
				StepID:        "step-1",
				Status:        builtin_tools.StepOutcomeCompleted,
				ShortSummary:  "已收集证据",
				KeyFacts:      []string{"fact-1"},
				References:    []string{"shared/step_artifacts/00000000-0000-0000-0000-000000000000_step-1.result.json"},
				StatusSummary: "第一步已完成",
				UpdatedAt:     time.Unix(1, 0),
			},
			{
				StepID:       "step-x",
				Status:       builtin_tools.StepOutcomeCompleted,
				ShortSummary: "无关步骤",
				UpdatedAt:    time.Unix(2, 0),
			},
		},
	})

	prompt := agent.BuildThinkActPrompt(context.Background(), "", nil)
	for _, marker := range []string{"<CURRENT_STEP>", "<LATEST_INPUT>", "<INPUT_TIMELINE>", "<DEPENDENCY_STEP_SUMMARIES>"} {
		if !strings.Contains(prompt, marker) {
			t.Fatalf("expected marker %s in prompt, got %s", marker, prompt)
		}
	}
	if strings.Contains(prompt, "<EXECUTION_CONTEXTS>") {
		t.Fatalf("did not expect execution contexts without frozen lineage, got %s", prompt)
	}
	if strings.Contains(prompt, "<LAST_OUTCOME>") || strings.Contains(prompt, "<SELECTED_STEP_SUMMARIES>") {
		t.Fatalf("expected legacy summary markers removed, got %s", prompt)
	}
	if !strings.Contains(prompt, "step-1") {
		t.Fatalf("expected dependency summary for step-1 in prompt, got %s", prompt)
	}
	if strings.Contains(prompt, "step-x") {
		t.Fatalf("did not expect unrelated summary in prompt, got %s", prompt)
	}
	if strings.Contains(prompt, "```json") {
		t.Fatalf("expected semantic blocks instead of raw runtime json block")
	}
}

func TestBuildThinkActPrompt_RendersGenericTaskContextEntries(t *testing.T) {
	agent, err := NewReActAgent(
		"generic-ctx-test",
		&stubChatClient{},
		WithEmitter(NewDummyEmitter()),
		WithInstruction("你是代码审查代理"),
	)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	prompt := agent.BuildThinkActPrompt(context.Background(), "", &TaskContextData{
		Entries: []TaskContextEntry{
			{Label: "项目路径", Value: "/repo/project", Description: "待分析的项目根目录"},
			{Label: "共享路径", Value: "/tmp/workspace/shared/step-1"},
		},
	})
	if !strings.Contains(prompt, "项目路径: /repo/project") {
		t.Fatalf("expected generic entry in prompt, got %s", prompt)
	}
	if !strings.Contains(prompt, "待分析的项目根目录") {
		t.Fatalf("expected description in prompt, got %s", prompt)
	}
	if !strings.Contains(prompt, "共享路径: /tmp/workspace/shared/step-1") {
		t.Fatalf("expected generic entry in prompt, got %s", prompt)
	}
}

func TestBuildThinkActPrompt_OmitsTaskContextWhenEmpty(t *testing.T) {
	agent, err := NewReActAgent(
		"empty-ctx-test",
		&stubChatClient{},
		WithEmitter(NewDummyEmitter()),
		WithInstruction("你是代码审查代理"),
	)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	prompt := agent.BuildThinkActPrompt(context.Background(), "", &TaskContextData{})
	if strings.Contains(prompt, "任务上下文") {
		t.Fatalf("did not expect task context section for empty data, got %s", prompt)
	}
}

func TestBuildThinkActPrompt_RendersMultipleTaskContextEntries(t *testing.T) {
	agent, err := NewReActAgent(
		"multi-entry-test",
		&stubChatClient{},
		WithEmitter(NewDummyEmitter()),
		WithInstruction("你是代码审查代理"),
	)
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	prompt := agent.BuildThinkActPrompt(context.Background(), "", &TaskContextData{
		Entries: []TaskContextEntry{
			{Label: "项目路径", Value: "/repo/project"},
			{Label: "当前路径", Value: "/repo/project/internal/react"},
			{Label: "编译状态", Value: "ready"},
			{Label: "阶段说明", Value: "等待当前 step 继续推进。"},
			{Label: "结构化输入", Value: "{\"ticket\":\"TASK-1\"}"},
		},
	})

	for _, expected := range []string{
		"项目路径: /repo/project",
		"当前路径: /repo/project/internal/react",
		"编译状态: ready",
		"阶段说明: 等待当前 step 继续推进。",
		"结构化输入: {\"ticket\":\"TASK-1\"}",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("expected prompt to contain %q, got %s", expected, prompt)
		}
	}
}

func TestSelectDependencyStepSummaryCards_OnlyReturnsDirectDependencies(t *testing.T) {
	current := &builtin_tools.PlanItem{
		ID:        "step-3",
		Step:      "实现改动",
		Status:    builtin_tools.PlanStepPending,
		DependsOn: []string{"step-1", "step-2", "step-1"},
	}
	snapshot := builtin_tools.StateSnapshot{
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "step-1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "依赖 1", UpdatedAt: time.Unix(1, 0)},
			{StepID: "step-2", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "依赖 2", UpdatedAt: time.Unix(2, 0)},
			{StepID: "step-x", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "无关", UpdatedAt: time.Unix(3, 0)},
		},
	}

	cards := SelectDependencyStepSummaryCards(snapshot, current)
	if len(cards) != 2 {
		t.Fatalf("expected 2 dependency summary cards, got %d: %+v", len(cards), cards)
	}
	if cards[0].StepID != "step-1" || cards[1].StepID != "step-2" {
		t.Fatalf("expected cards ordered by depends_on, got %+v", cards)
	}
}

func TestToolRegistry_RegisterAndResolve(t *testing.T) {
	registry := NewDefaultToolRegistry()
	if !registry.Has("list_files") {
		t.Fatal("expected list_files to be registered in default registry")
	}
	if !registry.Has("read_file") {
		t.Fatal("expected read_file to be registered in default registry")
	}
	if !registry.Has("rg") {
		t.Fatal("expected rg to be registered in default registry")
	}

	tool, err := registry.Resolve("list_files", nil)
	if err != nil {
		t.Fatalf("resolve list_files: %v", err)
	}
	if tool.Name() != "list_files" {
		t.Fatalf("expected tool name list_files, got %s", tool.Name())
	}

	_, err = registry.Resolve("nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent tool")
	}
}

func TestToolRegistry_CustomToolRegistration(t *testing.T) {
	registry := NewToolRegistry()
	registry.Register("my_custom_tool", func(_ builtin_tools.ToolContext) Tool {
		return &stubTool{name: "my_custom_tool"}
	})
	if !registry.Has("my_custom_tool") {
		t.Fatal("expected custom tool to be registered")
	}
	names := registry.Names()
	if len(names) != 1 || names[0] != "my_custom_tool" {
		t.Fatalf("expected [my_custom_tool], got %v", names)
	}
}

func TestAgentFactory_BuildFromDefinition(t *testing.T) {
	registry := NewDefaultToolRegistry()
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryToolRegistry(registry),
		WithFactoryEmitter(NewDummyEmitter()),
	)

	def := AgentDefinition{
		Name:        "test-analysis",
		Role:        "你是分析 Agent",
		Instruction: "分析代码",
		ToolNames:   []string{"list_files", "read_file", "rg"},
		Policies: AgentPolicies{
			MaxIterations: 5,
		},
	}

	agent, err := factory.Build(def)
	if err != nil {
		t.Fatalf("build agent: %v", err)
	}
	if agent.Name() != "test-analysis" {
		t.Fatalf("expected name test-analysis, got %s", agent.Name())
	}
	for _, toolName := range []string{"list_files", "read_file", "rg"} {
		if _, ok := agent.GetTool(toolName); !ok {
			t.Fatalf("expected tool %s to be registered via factory", toolName)
		}
	}
	// Platform tools should also be present.
	for _, toolName := range []string{"update_current_step", "task_status", "human_confirm"} {
		if _, ok := agent.GetTool(toolName); !ok {
			t.Fatalf("expected platform tool %s to be registered", toolName)
		}
	}
}

func TestAgentFactory_BuildMinimalAgent(t *testing.T) {
	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(&stubChatClient{}),
		WithFactoryEmitter(NewDummyEmitter()),
	)

	def := AgentDefinition{
		Name:        "minimal-agent",
		Instruction: "最小 Agent",
	}

	agent, err := factory.Build(def)
	if err != nil {
		t.Fatalf("build minimal agent: %v", err)
	}
	if agent.Name() != "minimal-agent" {
		t.Fatalf("expected name minimal-agent, got %s", agent.Name())
	}
	// Should only have platform tools
	if _, ok := agent.GetTool("list_files"); ok {
		t.Fatal("minimal agent should not have list_files")
	}
}

func TestAgentDefinition_BuildInstruction(t *testing.T) {
	def := AgentDefinition{
		Role:        "安全分析师",
		Background:  "正在检查 SQL 注入漏洞",
		Instruction: "逐文件分析入口参数",
	}
	instruction := def.BuildInstruction()
	if !strings.Contains(instruction, "安全分析师") {
		t.Fatal("expected role in instruction")
	}
	if !strings.Contains(instruction, "SQL 注入漏洞") {
		t.Fatal("expected background in instruction")
	}
	if !strings.Contains(instruction, "逐文件分析入口参数") {
		t.Fatal("expected instruction in instruction")
	}
}

func TestAgentDefinition_BuildTaskContext(t *testing.T) {
	def := AgentDefinition{
		Context: []TaskContextEntry{
			{Label: "项目路径", Value: "/repo/project", Description: "待分析的项目根目录"},
			{Label: "编译状态", Value: "ready"},
		},
	}
	ctx := def.BuildTaskContext()
	if ctx == nil {
		t.Fatal("expected non-nil task context")
	}
	if !ctx.HasVisibleData() {
		t.Fatal("expected visible data in task context")
	}
	entries := ctx.VisibleEntries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Description != "待分析的项目根目录" {
		t.Fatalf("expected description on first entry, got %q", entries[0].Description)
	}
}

type stubTool struct {
	name string
}

func (s *stubTool) Name() string        { return s.name }
func (s *stubTool) Description() string { return "stub tool" }
func (s *stubTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (s *stubTool) Execute(_ context.Context, _ map[string]any) (string, error) { return "", nil }
