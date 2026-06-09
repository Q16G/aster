package react

import (
	"context"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

func newIntentTestAgent(t *testing.T, client ai.ChatClient) *Agent {
	t.Helper()
	agent, err := NewReActAgent("test-intent", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	runtime, err := newLocalWorkspaceRuntime("intent-sess", t.TempDir(), "root")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	agent.workspaceRuntime = runtime
	agent.workspaceSessionID = "intent-sess"
	agent.state.Replace(builtin_tools.StateSnapshot{
		Phase:             builtin_tools.AgentPhaseIntentClassification,
		Status:            builtin_tools.TaskStatusCompleted,
		CurrentGoal:       "对目标系统做全量安全审计",
		GoalUnderstanding: "核心目标：全量安全审计；范围：整个仓库；约束：只读取证",
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "梳理攻击面", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "深入反编译三方依赖", Status: builtin_tools.PlanStepPending},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "攻击面梳理完成"},
		},
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "我系统里装了 jadx，请继续执行", CreatedAt: time.Now()},
		},
	})
	return agent
}

func submitIntentToolCall(id, args string) *ai.FunctionTool {
	return &ai.FunctionTool{
		Id:   id,
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:      submitIntentToolName,
			Arguments: args,
		},
	}
}

// submit_intent 一次命中 replan → 走 replan 分支：重生成目标、替换待办、相位回到 plan。
func TestRunIntentClassificationPhase_SubmitReplan(t *testing.T) {
	client := &intentTestClient{
		replies: []intentTestReply{
			{toolCalls: []*ai.FunctionTool{submitIntentToolCall("c1", `{"action":"replan","reason":"用户改变方向"}`)}},
		},
	}
	agent := newIntentTestAgent(t, client)

	if err := agent.runIntentClassificationPhase(context.Background(), 0, client); err != nil {
		t.Fatalf("runIntentClassificationPhase: %v", err)
	}
	snap := agent.state.Snapshot()
	if snap.Phase != builtin_tools.AgentPhasePlan {
		t.Fatalf("expected phase plan, got %q", snap.Phase)
	}
	if snap.ReplanContext == nil {
		t.Fatal("expected ReplanContext to be set for replan")
	}
	if !snap.ReplanContext.RegenerateGoal {
		t.Error("expected RegenerateGoal=true for replan")
	}
	if !snap.ReplanContext.ReplacePending {
		t.Error("expected ReplacePending=true for replan")
	}
}

// read_file 取证后再 submit carry：只读工具执行一次，相位回 plan，carry 不替换待办。
func TestRunIntentClassificationPhase_ReadThenCarry(t *testing.T) {
	calls := 0
	client := &intentTestClient{
		replies: []intentTestReply{
			{toolCalls: []*ai.FunctionTool{{
				Id:       "rf-1",
				Type:     "function",
				Function: &ai.FunctionDetail{Name: builtin_tools.ReadFileToolName, Arguments: `{"path":"/abs/task_context.md"}`},
			}}},
			{toolCalls: []*ai.FunctionTool{submitIntentToolCall("c2", `{"action":"carry","reason":"jadx 仅为手段，未偏离原审计意图"}`)}},
		},
	}
	agent := newIntentTestAgent(t, client)
	if err := agent.registerTool(&faStubTool{name: builtin_tools.ReadFileToolName, result: "# 事实板\n目标=审计", calls: &calls}); err != nil {
		t.Fatalf("registerTool: %v", err)
	}

	if err := agent.runIntentClassificationPhase(context.Background(), 0, client); err != nil {
		t.Fatalf("runIntentClassificationPhase: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected read_file executed once, got %d", calls)
	}
	snap := agent.state.Snapshot()
	if snap.Phase != builtin_tools.AgentPhasePlan {
		t.Fatalf("expected phase plan, got %q", snap.Phase)
	}
	if snap.ReplanContext == nil || snap.ReplanContext.ReplacePending {
		t.Fatalf("expected carry ReplanContext with ReplacePending=false, got %+v", snap.ReplanContext)
	}
}

// submit_intent 参数持续非法 → 超重试上限安全降级 carry，不返回错误，相位回 plan。
func TestRunIntentClassificationPhase_BadSubmitDegradesToCarry(t *testing.T) {
	bad := func() intentTestReply {
		return intentTestReply{toolCalls: []*ai.FunctionTool{submitIntentToolCall("c-bad", "{}")}}
	}
	client := &intentTestClient{
		replies: []intentTestReply{bad(), bad(), bad(), bad(), bad(), bad()},
	}
	agent := newIntentTestAgent(t, client)

	if err := agent.runIntentClassificationPhase(context.Background(), 0, client); err != nil {
		t.Fatalf("expected safe-degrade to carry without error, got %v", err)
	}
	snap := agent.state.Snapshot()
	if snap.Phase != builtin_tools.AgentPhasePlan {
		t.Fatalf("expected phase plan after degrade, got %q", snap.Phase)
	}
}

// 纯文本 JSON（cold_start）、不调 submit → 文本兜底解析生效，state.Reset 清空旧目标。
func TestRunIntentClassificationPhase_PlaintextColdStart(t *testing.T) {
	client := &intentTestClient{
		replies: []intentTestReply{
			{content: `{"action":"cold_start","reason":"与既有工作完全无关"}`},
		},
	}
	agent := newIntentTestAgent(t, client)

	if err := agent.runIntentClassificationPhase(context.Background(), 0, client); err != nil {
		t.Fatalf("runIntentClassificationPhase: %v", err)
	}
	snap := agent.state.Snapshot()
	if strings.Contains(snap.CurrentGoal, "全量安全审计") {
		t.Errorf("cold_start should clear old goal, got %q", snap.CurrentGoal)
	}
}

// BuildIntentClassificationPrompt 应注入核心目标理解 / 最新输入 / workspace 根目录。
func TestBuildIntentClassificationPrompt_Injection(t *testing.T) {
	agent := newIntentTestAgent(t, &intentTestClient{})
	snap := agent.state.Snapshot()
	input := buildIntentClassificationInput(snap)
	input.WorkspaceSharedDir = "/tmp/intent-workspace"

	prompt, err := agent.promptManager.BuildIntentClassificationPrompt(input)
	if err != nil {
		t.Fatalf("BuildIntentClassificationPrompt: %v", err)
	}
	for _, want := range []string{
		"全量安全审计",                              // GoalUnderstanding
		"我系统里装了 jadx",                         // LatestInput
		"/tmp/intent-workspace",                 // WorkspaceSharedDir
		"/tmp/intent-workspace/task_context.md", // 点名事实板
		"/tmp/intent-workspace/open_items.md",   // 点名未闭环账本
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected prompt to contain %q", want)
		}
	}
}
