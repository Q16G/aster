package react

import (
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type noopChatClientForScheduler struct{}

func (s *noopChatClientForScheduler) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (s *noopChatClientForScheduler) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (s *noopChatClientForScheduler) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestScheduler_FallbackDoesNotSwallowFinalAnswerError(t *testing.T) {
	agent, err := NewReActAgent("test", &noopChatClientForScheduler{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}
	// Intentionally do NOT configure workspaceRuntime: runFinalAnswerPhase should fail
	// while handling a plan-phase error.
	agent.workspaceRuntime = nil

	res, runErr := agent.runSchedulerLoop(context.Background(), nil, "", nil, 1)
	if runErr == nil {
		t.Fatalf("expected error, got result=%#v", res)
	}
	if res != nil {
		t.Fatalf("expected nil result on error, got %#v", res)
	}
	msg := runErr.Error()
	if !strings.Contains(msg, "input timeline is empty") {
		t.Fatalf("expected original phase error to be present, got: %s", msg)
	}
	if !strings.Contains(msg, "final_answer error") {
		t.Fatalf("expected final_answer error context, got: %s", msg)
	}
	if !strings.Contains(msg, "workspace runtime is nil") {
		t.Fatalf("expected final answer root cause, got: %s", msg)
	}
}

func TestMergeReplannedPlan_StepTextDedup(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "step-1", Step: "加载项目结构", Status: builtin_tools.PlanStepCompleted},
		{ID: "step-2", Step: "分析 SQL 注入风险", Status: builtin_tools.PlanStepCompleted},
		{ID: "step-3", Step: "验证数据流路径", Status: builtin_tools.PlanStepInProgress},
	}
	next := []*builtin_tools.PlanItem{
		{ID: "step-1", Step: "加载项目结构", Status: builtin_tools.PlanStepCompleted},
		{ID: "step-2", Step: "分析 SQL 注入风险", Status: builtin_tools.PlanStepCompleted},
		{ID: "step-3", Step: "验证数据流路径", Status: builtin_tools.PlanStepInProgress},
		{ID: "step-new-1", Step: "分析  SQL 注入风险", Status: builtin_tools.PlanStepPending},
		{ID: "step-new-2", Step: "输出审计报告", Status: builtin_tools.PlanStepPending},
	}

	merged := mergeReplannedPlan(prev, next)

	var ids []string
	for _, item := range merged {
		ids = append(ids, item.ID)
	}
	expected := []string{"step-1", "step-2", "step-3", "step-new-2"}
	if len(ids) != len(expected) {
		t.Fatalf("expected %d items %v, got %d items %v", len(expected), expected, len(ids), ids)
	}
	for i, id := range expected {
		if ids[i] != id {
			t.Fatalf("item[%d]: expected %q, got %q", i, id, ids[i])
		}
	}
}

func TestNormalizeStepText(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"分析  SQL 注入风险", "分析 sql 注入风险"},
		{"分析 SQL 注入风险", "分析 sql 注入风险"},
		{"加载项目结构（P0）", "加载项目结构(p0)"},
		{"  ", ""},
	}
	for _, tc := range cases {
		got := normalizeStepText(tc.in)
		if got != tc.want {
			t.Errorf("normalizeStepText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
