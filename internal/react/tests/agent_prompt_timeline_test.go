package react_test

import (
	. "aster/internal/react"
	"strings"
	"testing"
	"time"

	"aster/internal/builtin_tools"
)

func TestFormatRuntimeStateJSON_IncludesInputTimeline(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{
		CurrentGoal: "latest goal",
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "first input", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
			{Content: "second input", CreatedAt: time.Date(2026, 4, 3, 10, 1, 0, 0, time.UTC)},
		},
	}

	raw := FormatRuntimeStateJSON(snapshot, "ses-test")
	if !strings.Contains(raw, "\"input_timeline\"") {
		t.Fatalf("expected input_timeline in runtime state json, got %s", raw)
	}
	if !strings.Contains(raw, "first input") || !strings.Contains(raw, "second input") {
		t.Fatalf("expected both timeline inputs in runtime state json, got %s", raw)
	}
}

func TestPlannerInputFromSnapshot_UsesTimeline(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "first input", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
			{Content: "second input", CreatedAt: time.Date(2026, 4, 3, 10, 1, 0, 0, time.UTC)},
		},
	}

	got := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{})
	if !strings.Contains(got, "用户输入时间线") {
		t.Fatalf("expected planner input header, got %s", got)
	}
	if !strings.Contains(got, "first input") || !strings.Contains(got, "second input") {
		t.Fatalf("expected planner input to include full timeline, got %s", got)
	}
}

func TestPlannerInputFromSnapshot_EmptyWithoutTimeline(t *testing.T) {
	got := PlannerInputFromSnapshot(builtin_tools.StateSnapshot{
		CurrentGoal: "latest goal",
	}, PlannerInputOptions{})
	if got != "" {
		t.Fatalf("expected empty planner input without timeline, got %s", got)
	}
}

// 身份三段已上移至公共身份/env 块（system block2），planner_input 只承载交接上下文等动态输入。
func TestPlannerInputFromSnapshot_IncludesHandoffContextWithoutIdentity(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "hello", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
		},
	}
	opts := PlannerInputOptions{
		HandoffContext: "[SESSION_CONTEXT]\nproject_path: /tmp/repo",
	}

	got := PlannerInputFromSnapshot(snapshot, opts)
	for _, marker := range []string{
		"<HANDOFF_CONTEXT>",
		"project_path: /tmp/repo",
		"</HANDOFF_CONTEXT>",
	} {
		if !strings.Contains(got, marker) {
			t.Fatalf("expected marker %q in planner input, got %s", marker, got)
		}
	}
	if strings.Contains(got, "<AGENT_ROLE>") || strings.Contains(got, "<AGENT_INSTRUCTION>") {
		t.Fatalf("planner input must not render identity blocks (moved to identity env block), got %s", got)
	}
}

func TestPlannerInputFromSnapshot_TaskItemsCarryBakedOutputs(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhasePlan,
		Status:        builtin_tools.TaskStatusRunning,
		PlanVersion:   2,
		CurrentGoal:   "继续承接已有执行线推进",
		CurrentStepID: "step-2",
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "please continue", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
		},
		Plan: []*builtin_tools.PlanItem{
			{
				ID:           "step-1",
				Step:         "收集证据",
				Status:       builtin_tools.PlanStepCompleted,
				ShortSummary: "已完成证据收集",
				KeyFacts:     []string{"fact-1", "fact-2"},
				References:   []string{"ref-000001"},
				TimelineFile: "shared/step-1/timeline.jsonl",
			},
			{ID: "step-2", Step: "验证调用链", Status: builtin_tools.PlanStepInProgress, DependsOn: []string{"step-1"}},
		},
	}

	got := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{WorkspaceRootDir: "/ws/root"})
	for _, marker := range []string{
		"<TASK_ITEMS>",
		"\"id\":\"step-1\"",
		"\"short_summary\":\"已完成证据收集\"",
		"\"timeline_file\":\"/ws/root/shared/step-1/timeline.jsonl\"",
		"</TASK_ITEMS>",
	} {
		if !strings.Contains(got, marker) {
			t.Fatalf("expected marker %q in planner input, got %s", marker, got)
		}
	}
	// 旧的 EXECUTION_LINE / WORKSPACE_STEP_CONTEXTS 全量注入已取消（copy→pointer）。
	for _, banned := range []string{"<EXECUTION_LINE>", "<WORKSPACE_STEP_CONTEXTS>"} {
		if strings.Contains(got, banned) {
			t.Fatalf("planner input must not contain removed section %q, got %s", banned, got)
		}
	}
}

func TestPlannerInputFromSnapshot_IncludesReplanContext(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "please continue", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
		},
		Plan: []*builtin_tools.PlanItem{
			{ID: "step-1", Step: "收集证据", Status: builtin_tools.PlanStepCompleted},
			{ID: "step-2", Step: "旧步骤", Status: builtin_tools.PlanStepPending},
		},
		ReplanContext: &builtin_tools.ReplanContext{
			SourceStepID:    "step-1",
			Reason:          "旧计划未覆盖新增缺口",
			NextGoal:        "围绕新缺口重排计划",
			IncompleteItems: builtin_tools.NewAxisItems([]string{"missing-1"}),
			Warnings:        []string{"warn-1"},
			ReplacePending:  true,
		},
	}

	got := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{})
	for _, marker := range []string{
		"<REPLAN_CONTEXT>",
		"\"source_step_id\":\"step-1\"",
		"\"reason\":\"旧计划未覆盖新增缺口\"",
		"\"next_goal\":\"围绕新缺口重排计划\"",
		"\"replace_pending\":true",
		"</REPLAN_CONTEXT>",
	} {
		if !strings.Contains(got, marker) {
			t.Fatalf("expected marker %q in planner input, got %s", marker, got)
		}
	}
}
