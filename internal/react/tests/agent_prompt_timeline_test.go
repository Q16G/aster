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

func TestPlannerInputFromSnapshot_IncludesUserInstructionAndHandoffContext(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "hello", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
		},
	}
	opts := PlannerInputOptions{
		UserInstruction: "你是 data_flow_analysis_agent，需要做取证与路径验证，不要直接输出修复方案。",
		ExtraContext:    "[SESSION_CONTEXT]\nproject_path: /tmp/repo",
	}

	got := PlannerInputFromSnapshot(snapshot, opts)
	for _, marker := range []string{
		"<USER_INSTRUCTION>",
		"data_flow_analysis_agent",
		"</USER_INSTRUCTION>",
		"<HANDOFF_CONTEXT>",
		"project_path: /tmp/repo",
		"</HANDOFF_CONTEXT>",
	} {
		if !strings.Contains(got, marker) {
			t.Fatalf("expected marker %q in planner input, got %s", marker, got)
		}
	}
}

func TestPlannerInputFromSnapshot_IncludesTaskItemsAndExecutionLine(t *testing.T) {
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
			{ID: "step-1", Step: "收集证据", Status: builtin_tools.PlanStepCompleted},
			{ID: "step-2", Step: "验证调用链", Status: builtin_tools.PlanStepInProgress, DependsOn: []string{"step-1"}},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{
				StepID:       "step-1",
				Status:       builtin_tools.StepOutcomeCompleted,
				UpdatedAt:    time.Date(2026, 4, 3, 10, 2, 0, 0, time.UTC),
				ShortSummary: "已完成证据收集",
				KeyFacts:     []string{"fact-1", "fact-2"},
				References:   []string{"ref-000001"},
			},
		},
	}

	got := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{})
	for _, marker := range []string{
		"<TASK_ITEMS>",
		"\"id\":\"step-1\"",
		"</TASK_ITEMS>",
		"<EXECUTION_LINE>",
		"\"step_id\":\"step-1\"",
		"\"short_summary\":\"已完成证据收集\"",
		"</EXECUTION_LINE>",
	} {
		if !strings.Contains(got, marker) {
			t.Fatalf("expected marker %q in planner input, got %s", marker, got)
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
			SourceStepID:   "step-1",
			Reason:         "旧计划未覆盖新增缺口",
			NextGoal:       "围绕新缺口重排计划",
			MissingItems:   []string{"missing-1"},
			Warnings:       []string{"warn-1"},
			ReplacePending: true,
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

func TestPlannerInputFromSnapshot_IncludesWorkspaceStepContexts(t *testing.T) {
	workspaceRoot := t.TempDir()
	if err := builtin_tools.AppendWorkspaceStepContextRecords(workspaceRoot, []*builtin_tools.StepContextRecord{
		{
			ContextKey:   "ctx-1",
			Namespace:    "agents/dfa",
			StepID:       "step-1",
			StepKey:      "step-1",
			PlanVersion:  2,
			AgentProfile: "dfa",
			SummaryFile:  "shared/step_artifacts/dfa.summary.md",
			ResultFile:   "shared/step_artifacts/dfa.result.json",
			ResultKeys:   []string{"flow_evidence"},
			ShortSummary: "child summary",
			KeyFacts:     []string{"k1"},
			References:   []string{"ref-000002"},
			CreatedAt:    time.Now(),
		},
	}); err != nil {
		t.Fatalf("append workspace step contexts failed: %v", err)
	}

	snapshot := builtin_tools.StateSnapshot{
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "continue", CreatedAt: time.Date(2026, 4, 3, 10, 0, 0, 0, time.UTC)},
		},
		Plan: []*builtin_tools.PlanItem{
			{ID: "step-1", Step: "seed", Status: builtin_tools.PlanStepCompleted},
		},
	}

	got := PlannerInputFromSnapshot(snapshot, PlannerInputOptions{
		WorkspaceRootDir:   workspaceRoot,
		WorkspaceNamespace: "agents/dfa",
	})
	for _, marker := range []string{
		"<WORKSPACE_STEP_CONTEXTS>",
		"\"workspace_namespace\":\"agents/dfa\"",
		"\"context_key\":\"ctx-1\"",
		"\"namespace\":\"agents/dfa\"",
		"flow_evidence",
		"</WORKSPACE_STEP_CONTEXTS>",
	} {
		if !strings.Contains(got, marker) {
			t.Fatalf("expected marker %q in planner input, got %s", marker, got)
		}
	}
}
