package react

import (
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

// faStubTool 是一个可控桩工具，用于驱动 final_answer agentic 循环里的 allowed-tool 分支。
type faStubTool struct {
	name   string
	result string
	calls  *int
}

func (s *faStubTool) Name() string        { return s.name }
func (s *faStubTool) Description() string { return "stub tool for final_answer agentic test" }
func (s *faStubTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (s *faStubTool) Execute(_ context.Context, _ map[string]any) (string, error) {
	if s.calls != nil {
		*s.calls++
	}
	return s.result, nil
}

func newFinalAnswerTestAgent(t *testing.T, client ai.ChatClient) *Agent {
	t.Helper()
	agent, err := NewReActAgent("test-final-answer", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	runtime, err := newLocalWorkspaceRuntime("fa-sess", t.TempDir(), "root")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	agent.workspaceRuntime = runtime
	agent.workspaceSessionID = "fa-sess"
	// NeedsPlanning=true 绕过 fast-close，强制走 agentic 模型路径。
	agent.state.Replace(builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhaseFinalAnswer,
		Status:        builtin_tools.TaskStatusRunning,
		NeedsPlanning: true,
		CurrentGoal:   "审计目标系统",
		Plan: []*builtin_tools.PlanItem{
			{ID: "s1", Step: "step one", Status: builtin_tools.PlanStepCompleted},
			{ID: "s2", Step: "step two", Status: builtin_tools.PlanStepCompleted},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, ShortSummary: "done one"},
		},
	})
	return agent
}

func submitFinalAnswerToolCall(id, args string) *ai.FunctionTool {
	return &ai.FunctionTool{
		Id:   id,
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:      submitFinalAnswerToolName,
			Arguments: args,
		},
	}
}

func validSubmitFinalAnswerArgs(userMessage string) string {
	return `{"is_complete":true,"status":"completed","reason":"all done","should_replan":false,` +
		`"next_goal":"","incomplete_items":[],"depth_gaps":[],"new_surfaces":[],"warnings":[],` +
		`"user_message":"` + userMessage + `","references":["/abs/evidence.md"]}`
}

// 提交工具直接命中 → 进入终态、终报内容与引用按提交参数落库。
func TestRunFinalAnswerPhase_SubmitOnFirstRound(t *testing.T) {
	client := &intentTestClient{
		replies: []intentTestReply{
			{toolCalls: []*ai.FunctionTool{submitFinalAnswerToolCall("call-1", validSubmitFinalAnswerArgs("最终答复正文"))}},
		},
	}
	agent := newFinalAnswerTestAgent(t, client)

	snap, err := agent.runFinalAnswerPhase(context.Background(), 0, client)
	if err != nil {
		t.Fatalf("runFinalAnswerPhase: %v", err)
	}
	if snap.Status != builtin_tools.TaskStatusCompleted {
		t.Fatalf("expected completed status, got %q", snap.Status)
	}
	if snap.FinalAnswer == nil {
		t.Fatal("expected FinalAnswer to be set")
	}
	if snap.FinalAnswer.Content != "最终答复正文" {
		t.Fatalf("expected final answer content %q, got %q", "最终答复正文", snap.FinalAnswer.Content)
	}
	if snap.FinalAnswer.Source != "final_assessment" {
		t.Errorf("expected source final_assessment, got %q", snap.FinalAnswer.Source)
	}
	found := false
	for _, ref := range snap.FinalAnswer.References {
		if ref == "/abs/evidence.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reference /abs/evidence.md, got %v", snap.FinalAnswer.References)
	}
}

// read_file 取证后再 submit：allowed-tool 分支执行一次，随后提交进入终态。
func TestRunFinalAnswerPhase_ReadThenSubmit(t *testing.T) {
	calls := 0
	client := &intentTestClient{
		replies: []intentTestReply{
			{toolCalls: []*ai.FunctionTool{{
				Id:       "rf-1",
				Type:     "function",
				Function: &ai.FunctionDetail{Name: builtin_tools.ReadFileToolName, Arguments: `{"path":"/abs/task_context.md"}`},
			}}},
			{toolCalls: []*ai.FunctionTool{submitFinalAnswerToolCall("call-2", validSubmitFinalAnswerArgs("读取事实板后的终报"))}},
		},
	}
	agent := newFinalAnswerTestAgent(t, client)
	if err := agent.registerTool(&faStubTool{name: builtin_tools.ReadFileToolName, result: "# 贯穿全程关键事实\n端点=https://x", calls: &calls}); err != nil {
		t.Fatalf("registerTool: %v", err)
	}

	snap, err := agent.runFinalAnswerPhase(context.Background(), 0, client)
	if err != nil {
		t.Fatalf("runFinalAnswerPhase: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected read_file executed once, got %d", calls)
	}
	if snap.Status != builtin_tools.TaskStatusCompleted {
		t.Fatalf("expected completed status, got %q", snap.Status)
	}
	if snap.FinalAnswer == nil || snap.FinalAnswer.Content != "读取事实板后的终报" {
		t.Fatalf("expected final answer from submit, got %+v", snap.FinalAnswer)
	}
}

// 只产出文本、不调 submit → plaintext 兜底产出可交付终报。
func TestRunFinalAnswerPhase_PlaintextFallback(t *testing.T) {
	client := &intentTestClient{
		replies: []intentTestReply{
			{content: "直接交付的纯文本答复"},
		},
	}
	agent := newFinalAnswerTestAgent(t, client)

	snap, err := agent.runFinalAnswerPhase(context.Background(), 0, client)
	if err != nil {
		t.Fatalf("runFinalAnswerPhase: %v", err)
	}
	if snap.Status != builtin_tools.TaskStatusCompleted {
		t.Fatalf("expected completed status, got %q", snap.Status)
	}
	if snap.FinalAnswer == nil || snap.FinalAnswer.Content != "直接交付的纯文本答复" {
		t.Fatalf("expected plaintext fallback content, got %+v", snap.FinalAnswer)
	}
}

// submit 参数非法且持续失败 → 超过重试上限返回错误。
func TestRunFinalAnswerPhase_BadSubmitRetryExhausted(t *testing.T) {
	bad := func() intentTestReply {
		return intentTestReply{toolCalls: []*ai.FunctionTool{submitFinalAnswerToolCall("call-bad", "{}")}}
	}
	client := &intentTestClient{
		replies: []intentTestReply{bad(), bad(), bad(), bad(), bad()},
	}
	agent := newFinalAnswerTestAgent(t, client)

	_, err := agent.runFinalAnswerPhase(context.Background(), 0, client)
	if err == nil {
		t.Fatal("expected error after submit retry exhaustion")
	}
	if !strings.Contains(err.Error(), "submit_final_answer failed after 3 retries") {
		t.Fatalf("expected retry-exhaustion error, got %v", err)
	}
}
