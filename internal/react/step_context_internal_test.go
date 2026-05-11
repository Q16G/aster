package react

import (
	"context"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type noopStepContextClient struct{}

func (noopStepContextClient) Chat(context.Context, *ai.MsgInfo, ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (noopStepContextClient) ChatEx(context.Context, []*ai.MsgInfo, ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (noopStepContextClient) ChatText(context.Context, string, ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func TestCollectStepContextViews_OrderAndFilter(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	plan := []*builtin_tools.PlanItem{
		{ID: "step-1", Step: "准备环境"},
		{ID: "step-2", Step: "执行扫描"},
	}
	outcomes := []*builtin_tools.StepOutcome{
		{
			StepID:        "step-1",
			Status:        builtin_tools.StepOutcomeCompleted,
			StatusSummary: "环境已准备",
			ShortSummary:  "完成初始化",
			LongSummary:   "完成所有初始化工作",
			KeyFacts:      []string{"fact-1"},
			Result:        "初始化结果-旧",
			UpdatedAt:     now.Add(-2 * time.Minute),
		},
		{
			StepID:        "step-2",
			Status:        builtin_tools.StepOutcomeFailed,
			StatusSummary: "扫描失败",
			ShortSummary:  "命令返回错误",
			UpdatedAt:     now.Add(-1 * time.Minute),
		},
		{
			StepID:        "step-1",
			Status:        builtin_tools.StepOutcomeCompleted,
			StatusSummary: "环境最终已准备",
			ShortSummary:  "初始化成功",
			Result:        "初始化结果-新",
			UpdatedAt:     now,
		},
		{
			StepID:        "step-x",
			Status:        builtin_tools.StepOutcomeCompleted,
			StatusSummary: "外部步骤完成",
			ShortSummary:  "额外上下文",
			UpdatedAt:     now.Add(time.Minute),
		},
	}

	completed := collectCompletedStepContextViews(plan, outcomes)
	if len(completed) != 2 {
		t.Fatalf("expected 2 completed views, got %d", len(completed))
	}
	if completed[0].StepID != "step-1" {
		t.Fatalf("expected step-1 first, got %q", completed[0].StepID)
	}
	if completed[0].StatusSummary != "环境最终已准备" {
		t.Fatalf("expected latest summary for step-1, got %q", completed[0].StatusSummary)
	}
	if completed[1].StepID != "step-x" {
		t.Fatalf("expected extra completed step last, got %q", completed[1].StepID)
	}
	if completed[1].Step != "step-x" {
		t.Fatalf("expected fallback step label to use step id, got %q", completed[1].Step)
	}

	allViews := collectAllStepContextViews(plan, outcomes)
	if len(allViews) != 3 {
		t.Fatalf("expected 3 total views, got %d", len(allViews))
	}
	if allViews[0].StepID != "step-1" || allViews[1].StepID != "step-2" || allViews[2].StepID != "step-x" {
		t.Fatalf("unexpected ordering: %#v", []string{allViews[0].StepID, allViews[1].StepID, allViews[2].StepID})
	}
	if allViews[0].Result != "初始化结果-新" {
		t.Fatalf("expected latest result to be preserved, got %q", allViews[0].Result)
	}
}

func TestDefaultOnHandoff_UsesCompletedStepContext(t *testing.T) {
	agent, err := NewReActAgent(
		"handoff-agent",
		noopStepContextClient{},
		WithEmitter(NewDummyEmitter()),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	agent.state.Replace(builtin_tools.StateSnapshot{
		Phase:  builtin_tools.AgentPhaseStep,
		Status: builtin_tools.TaskStatusRunning,
		Plan: []*builtin_tools.PlanItem{
			{ID: "step-1", Step: "准备环境"},
			{ID: "step-2", Step: "执行扫描"},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{
				StepID:        "step-1",
				Status:        builtin_tools.StepOutcomeCompleted,
				StatusSummary: "环境最终已准备",
				ShortSummary:  "初始化成功",
				Result:        "不应出现在 handoff 中的结构化结果",
				UpdatedAt:     time.Now().Add(-2 * time.Minute),
			},
			{
				StepID:        "step-2",
				Status:        builtin_tools.StepOutcomeFailed,
				StatusSummary: "扫描失败",
				ShortSummary:  "命令返回错误",
				UpdatedAt:     time.Now().Add(-time.Minute),
			},
		},
	})
	agent.handoff.summary = "旧摘要"

	got := DefaultOnHandoffFunc(context.Background(), agent, "sub_agent")
	if got == "" {
		t.Fatal("expected handoff context, got empty string")
	}
	if !strings.Contains(got, "准备环境") {
		t.Fatalf("expected completed step to appear in handoff context, got:\n%s", got)
	}
	if !strings.Contains(got, "status_summary: 环境最终已准备") {
		t.Fatalf("expected status summary to appear in handoff context, got:\n%s", got)
	}
	if !strings.Contains(got, "short_summary: 初始化成功") {
		t.Fatalf("expected short summary to appear in handoff context, got:\n%s", got)
	}
	if strings.Contains(got, "扫描失败") {
		t.Fatalf("failed step should not appear in completed-step handoff context, got:\n%s", got)
	}
	if strings.Contains(got, "不应出现在 handoff 中的结构化结果") {
		t.Fatalf("handoff context should not inline result payload, got:\n%s", got)
	}
	if agent.handoff.summary != got {
		t.Fatalf("expected handoff summary cache to be updated")
	}
}

func TestBuildSubAgentContextEntries_MergesExplicitAndHandoff(t *testing.T) {
	entries := buildSubAgentContextEntries("显式上下文", "交接上下文")
	if len(entries) != 2 {
		t.Fatalf("expected 2 context entries, got %d", len(entries))
	}
	if entries[0].Label != "委派上下文" || entries[1].Label != "交接上下文" {
		t.Fatalf("unexpected entry order: %#v", entries)
	}
	if !strings.Contains(entries[0].Description, "若与交接上下文冲突，以此为准") {
		t.Fatalf("expected explicit context precedence note, got %#v", entries[0])
	}
	if !strings.Contains(entries[1].Description, "仅作补充") || !strings.Contains(entries[1].Description, "以显式上下文为准") {
		t.Fatalf("expected handoff context precedence note, got %#v", entries[1])
	}

	entries = buildSubAgentContextEntries("显式上下文", "显式上下文")
	if len(entries) != 1 {
		t.Fatalf("expected deduplicated context entries, got %d", len(entries))
	}
	if entries[0].Label != "委派上下文" {
		t.Fatalf("expected explicit context to remain first, got %#v", entries)
	}
}
