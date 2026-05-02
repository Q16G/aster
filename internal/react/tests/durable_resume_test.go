package react_test

import (
	. "aster/internal/react"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
)

type failChatClient struct {
	calls int
}

func (c *failChatClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	c.calls++
	return "", errors.New("model should not be called")
}

func (c *failChatClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.calls++
	return nil, errors.New("model should not be called")
}

func (c *failChatClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	c.calls++
	return "", errors.New("model should not be called")
}

func (c *failChatClient) ModelContextInfo() ai.ModelContextInfo {
	return ai.ModelContextInfo{}
}

func TestExecute_DurableResume_ReturnFinalWithoutModel(t *testing.T) {
	workspaceRoot := t.TempDir()
	sessionID := "33333333-3333-3333-3333-333333333333"

	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok",
						"display_result": "step ok",
						"result":         "step ok",
					}),
				},
			},
			{
				content: `{"status_summary":"完成","step_short_summary":"完成一步","step_long_summary":"完成该 step。","key_facts":["f1"],"open_questions":[]}`,
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"已完成并可交付。","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"final-answer-1","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"resume-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: false,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "执行用户请求", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "do task",
		WithWorkspaceSession(sessionID, workspaceRoot),
		WithSkipIntentPrelude(),
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "final-answer-1" {
		t.Fatalf("expected final answer, got %q", runResult.Result)
	}

	noModel := &failChatClient{}
	resumeAgent, err := NewReActAgent(
		"resume-agent",
		noModel,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(5),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{err: fmt.Errorf("planner should not be called")}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent(resume) failed: %v", err)
	}

	resumeResult, err := resumeAgent.Execute(context.Background(), "继续",
		WithWorkspaceSession(sessionID, workspaceRoot),
		WithSkipIntentPrelude(),
	)
	if err != nil {
		t.Fatalf("resume Execute failed: %v", err)
	}
	if resumeResult == nil || !resumeResult.Success {
		t.Fatalf("expected resume success, got %#v", resumeResult)
	}
	if strings.TrimSpace(resumeResult.Result) != "final-answer-1" {
		t.Fatalf("expected resume to reuse final answer, got %q", resumeResult.Result)
	}
	if noModel.calls != 0 {
		t.Fatalf("expected no model calls during return_final, got %d", noModel.calls)
	}
}

func TestExecute_DurableResume_ContinuesNextStepWithoutRepeatingCompleted(t *testing.T) {
	workspaceRoot := t.TempDir()
	sessionID := "44444444-4444-4444-4444-444444444444"

	writer, err := NewArtifactWriter(workspaceRoot)
	if err != nil {
		t.Fatalf("newArtifactWriter failed: %v", err)
	}

	seedAt := time.Now().Add(-2 * time.Minute)
	seed := builtin_tools.StateSnapshot{
		Phase:         builtin_tools.AgentPhaseStep,
		Status:        builtin_tools.TaskStatusRunning,
		CurrentGoal:   "old-goal",
		CurrentStepID: "step-1",
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "old-goal", CreatedAt: seedAt},
		},
		PlanVersion: 1,
		Plan: []*builtin_tools.PlanItem{
			{ID: "step-1", Step: "S1", Status: builtin_tools.PlanStepPending},
			{ID: "step-2", Step: "S2", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
		},
	}
	builtin_tools.HydratePlanRelations(seed.Plan)
	if err := writer.PersistPlanArtifacts(seed, sessionID, "seed"); err != nil {
		t.Fatalf("PersistPlanArtifacts(seed) failed: %v", err)
	}

	step1Plan := &builtin_tools.PlanItem{ID: "step-1", Step: "S1", Status: builtin_tools.PlanStepCompleted}
	step1Outcome := &builtin_tools.StepOutcome{
		StepID:        "step-1",
		Status:        builtin_tools.StepOutcomeCompleted,
		UpdatedAt:     time.Now().Add(-90 * time.Second),
		Summary:       "s1",
		DisplayResult: "s1",
		Result:        `{"ok":true}`,
		StatusSummary: "s1 done",
		ShortSummary:  "s1 done",
		LongSummary:   "s1 done long",
		ContextKey:    "ctx-step-1",
	}
	artifactPlan, err := writer.PlanStepArtifactRel(seed.PlanVersion, "step-1", "step-1")
	if err != nil {
		t.Fatalf("planStepArtifactRel failed: %v", err)
	}
	if _, err := writer.PersistStepArtifacts(seed, sessionID, "seed-agent", step1Plan, step1Outcome, &StepWindow{StepID: "step-1"}, artifactPlan); err != nil {
		t.Fatalf("PersistStepArtifacts(seed step-1) failed: %v", err)
	}

	seedState, err := writer.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("loadWorkspaceState failed: %v", err)
	}
	if seedState == nil || seedState.LatestStepOutcomes == nil || seedState.LatestStepOutcomes["step-1"] == nil {
		t.Fatalf("expected workspace state to contain step-1 pointer")
	}
	step1ArtifactID := strings.TrimSpace(seedState.LatestStepOutcomes["step-1"].ArtifactID)
	if step1ArtifactID == "" {
		t.Fatalf("expected non-empty step-1 artifact_id")
	}

	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok2",
						"display_result": "step2 ok",
						"result":         "step2 ok",
					}),
				},
			},
			{
				content: `{"status_summary":"完成","step_short_summary":"完成 step2","step_long_summary":"完成 step2。","key_facts":["f2"],"open_questions":[]}`,
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"final-answer-2","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"resume-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(&executeModelStaticPlanner{err: fmt.Errorf("planner should not be called")}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "继续",
		WithWorkspaceSession(sessionID, workspaceRoot),
		WithSkipIntentPrelude(),
	)
	if err != nil {
		t.Fatalf("Execute(resume) failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}

	afterState, err := writer.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("loadWorkspaceState(after) failed: %v", err)
	}
	if afterState == nil || afterState.LatestStepOutcomes == nil {
		t.Fatalf("expected workspace state after resume")
	}
	if afterState.LatestStepOutcomes["step-2"] == nil {
		t.Fatalf("expected step-2 pointer after resume")
	}
	if got := strings.TrimSpace(afterState.LatestStepOutcomes["step-1"].ArtifactID); got != step1ArtifactID {
		t.Fatalf("expected step-1 artifact_id unchanged (no rerun), want=%q got=%q", step1ArtifactID, got)
	}

	// "继续" should not overwrite the old goal.
	if snap := agent.State(); strings.TrimSpace(snap.CurrentGoal) != "old-goal" {
		t.Fatalf("expected current_goal preserved on continuation, got %q", snap.CurrentGoal)
	}
}

func TestExecute_DurableResume_ReplansBeforeRunningOldPendingStep(t *testing.T) {
	workspaceRoot := t.TempDir()
	sessionID := "55555555-5555-5555-5555-555555555555"

	writer, err := NewArtifactWriter(workspaceRoot)
	if err != nil {
		t.Fatalf("newArtifactWriter failed: %v", err)
	}

	seedAt := time.Now().Add(-2 * time.Minute)
	seed := builtin_tools.StateSnapshot{
		Phase:       builtin_tools.AgentPhasePlan,
		Status:      builtin_tools.TaskStatusRunning,
		CurrentGoal: "围绕新缺口补齐验证",
		InputTimeline: []*builtin_tools.TimelineInput{
			{Content: "old-goal", CreatedAt: seedAt},
		},
		PlanVersion: 1,
		Plan: []*builtin_tools.PlanItem{
			{ID: "step-1", Step: "已完成旧步骤", Status: builtin_tools.PlanStepCompleted},
			{ID: "legacy-step", Step: "过时旧待办", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{
				StepID:        "step-1",
				Status:        builtin_tools.StepOutcomeCompleted,
				UpdatedAt:     time.Now().Add(-90 * time.Second),
				StatusSummary: "旧步骤已完成",
				ShortSummary:  "旧步骤已完成",
				LongSummary:   "旧步骤已完成",
			},
		},
		Warnings:   []string{"warn-1"},
		Unresolved: []string{"missing-1"},
		ReplanContext: &builtin_tools.ReplanContext{
			SourceStepID:   "step-1",
			Reason:         "旧计划未覆盖新增缺口",
			NextGoal:       "围绕新缺口补齐验证",
			MissingItems:   []string{"missing-1"},
			Warnings:       []string{"warn-1"},
			ReplacePending: true,
		},
	}
	builtin_tools.HydratePlanRelations(seed.Plan)
	if err := writer.PersistPlanArtifacts(seed, sessionID, "seed"); err != nil {
		t.Fatalf("PersistPlanArtifacts(seed) failed: %v", err)
	}
	if err := writer.PersistRuntimeCheckpoint(seed, sessionID, "step_summary"); err != nil {
		t.Fatalf("PersistRuntimeCheckpoint(seed) failed: %v", err)
	}

	planner := &executeModelSequencePlanner{
		results: []*builtin_tools.TaskPlannerResult{
			{
				NeedsPlanning: true,
				Explanation:   "重规划继续执行",
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-2", Step: "围绕新缺口补齐验证", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-1"}},
				},
			},
		},
	}
	client := &executeModelTestClient{
		replies: []executeModelReply{
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-2-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":         "completed",
						"summary":        "ok2",
						"display_result": "step2 ok",
						"result":         "step2 ok",
					}),
				},
			},
			{
				content: `{"status_summary":"完成","step_short_summary":"完成补齐步骤","step_long_summary":"恢复后先重规划，再完成新步骤。","key_facts":["f2"],"open_questions":[]}`,
			},
			{
				content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"resumed-replanned-done","references":[]}`,
			},
		},
	}

	agent, err := NewReActAgent(
		"resume-replan-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(6),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTaskPlanner(planner),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "继续",
		WithWorkspaceSession(sessionID, workspaceRoot),
		WithSkipIntentPrelude(),
	)
	if err != nil {
		t.Fatalf("Execute(resume) failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success run result, got %#v", runResult)
	}
	if strings.TrimSpace(runResult.Result) != "resumed-replanned-done" {
		t.Fatalf("expected resumed replanned final answer, got %q", runResult.Result)
	}
	if planner.calls != 1 {
		t.Fatalf("expected resume to trigger planner once, got %d", planner.calls)
	}
	if len(planner.inputs) != 1 || !strings.Contains(planner.inputs[0], "<REPLAN_CONTEXT>") {
		t.Fatalf("expected resume planner input to include replan context, got %+v", planner.inputs)
	}
	if !strings.Contains(planner.inputs[0], "\"reason\": \"旧计划未覆盖新增缺口\"") {
		t.Fatalf("expected resume planner input to include replan reason, got %q", planner.inputs[0])
	}

	snapshot := agent.State()
	if snapshot.PlanVersion != 2 {
		t.Fatalf("expected replanned snapshot plan version 2, got %d", snapshot.PlanVersion)
	}
	statusByID := make(map[string]builtin_tools.PlanStepStatus, len(snapshot.Plan))
	for _, item := range snapshot.Plan {
		if item == nil {
			continue
		}
		statusByID[item.ID] = item.Status
	}
	if _, ok := statusByID["legacy-step"]; ok {
		t.Fatalf("expected legacy pending step removed after durable resume replan, got %+v", statusByID)
	}
	if statusByID["step-1"] != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected completed step-1 preserved, got %+v", statusByID)
	}
	if statusByID["step-2"] != builtin_tools.PlanStepCompleted {
		t.Fatalf("expected replanned step-2 completed, got %+v", statusByID)
	}
}

func TestLoadLatestFinalAssessment_PrefersHighestAvailableSeq(t *testing.T) {
	workspaceRoot := t.TempDir()
	writer, err := NewArtifactWriter(workspaceRoot)
	if err != nil {
		t.Fatalf("newArtifactWriter failed: %v", err)
	}

	snapshot := builtin_tools.StateSnapshot{
		Status:      builtin_tools.TaskStatusCompleted,
		PlanVersion: 1,
	}
	record1, err := writer.PersistFinalArtifacts(snapshot, "ses-stale-final", FinalAssessmentArtifact{
		SessionID:   "ses-stale-final",
		PlanVersion: 1,
		Assessment: FinalAnswerModelOutput{
			IsComplete:  true,
			Status:      string(builtin_tools.TaskStatusCompleted),
			UserMessage: "final-1",
		},
	}, "final-1")
	if err != nil {
		t.Fatalf("PersistFinalArtifacts(seq1) failed: %v", err)
	}
	record2, err := writer.PersistFinalArtifacts(snapshot, "ses-stale-final", FinalAssessmentArtifact{
		SessionID:   "ses-stale-final",
		PlanVersion: 1,
		Assessment: FinalAnswerModelOutput{
			IsComplete:  true,
			Status:      string(builtin_tools.TaskStatusCompleted),
			UserMessage: "final-2",
		},
	}, "final-2")
	if err != nil {
		t.Fatalf("PersistFinalArtifacts(seq2) failed: %v", err)
	}
	if record1.FinalSeq != 1 || record2.FinalSeq != 2 {
		t.Fatalf("expected final seq 1 then 2, got %d and %d", record1.FinalSeq, record2.FinalSeq)
	}

	state, err := writer.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("loadWorkspaceState failed: %v", err)
	}
	state.LatestFinalSeq = 1
	if err := writer.WriteWorkspaceState(state); err != nil {
		t.Fatalf("writeWorkspaceState failed: %v", err)
	}

	checkpoint, err := writer.LoadPlanCurrentCheckpoint()
	if err != nil {
		t.Fatalf("loadPlanCurrentCheckpoint failed: %v", err)
	}
	checkpoint.LatestFinalSeq = 1
	if err := writer.WritePlanCurrentCheckpoint(*checkpoint); err != nil {
		t.Fatalf("writePlanCurrentCheckpoint failed: %v", err)
	}

	artifact, seq, err := LoadLatestFinalAssessment(writer, state, checkpoint)
	if err != nil {
		t.Fatalf("loadLatestFinalAssessment failed: %v", err)
	}
	if seq != 2 {
		t.Fatalf("expected latest final seq 2, got %d", seq)
	}
	if artifact == nil || strings.TrimSpace(artifact.Assessment.UserMessage) != "final-2" {
		t.Fatalf("expected latest final assessment to be seq2, got %#v", artifact)
	}
}

func TestPersistFinalArtifacts_UsesNamespacedFinalSequence(t *testing.T) {
	workspaceRoot := t.TempDir()
	writer, err := NewArtifactWriter(workspaceRoot, WithArtifactNamespace("agents/msg/call/agent"))
	if err != nil {
		t.Fatalf("newArtifactWriter failed: %v", err)
	}

	snapshot := builtin_tools.StateSnapshot{
		Status:      builtin_tools.TaskStatusCompleted,
		PlanVersion: 1,
	}
	record1, err := writer.PersistFinalArtifacts(snapshot, "ses-ns-final", FinalAssessmentArtifact{
		SessionID:   "ses-ns-final",
		PlanVersion: 1,
		Assessment: FinalAnswerModelOutput{
			IsComplete:  true,
			Status:      string(builtin_tools.TaskStatusCompleted),
			UserMessage: "child-final-1",
		},
	}, "child-final-1")
	if err != nil {
		t.Fatalf("PersistFinalArtifacts(seq1) failed: %v", err)
	}
	record2, err := writer.PersistFinalArtifacts(snapshot, "ses-ns-final", FinalAssessmentArtifact{
		SessionID:   "ses-ns-final",
		PlanVersion: 1,
		Assessment: FinalAnswerModelOutput{
			IsComplete:  true,
			Status:      string(builtin_tools.TaskStatusCompleted),
			UserMessage: "child-final-2",
		},
	}, "child-final-2")
	if err != nil {
		t.Fatalf("PersistFinalArtifacts(seq2) failed: %v", err)
	}

	if record1.FinalSeq != 1 || record2.FinalSeq != 2 {
		t.Fatalf("expected namespaced final seq 1 then 2, got %d and %d", record1.FinalSeq, record2.FinalSeq)
	}
	if !strings.Contains(record1.FinalAssessmentFile, "artifacts/agents/msg/call/agent/final/1/") {
		t.Fatalf("expected namespaced final path for seq1, got %q", record1.FinalAssessmentFile)
	}
	if !strings.Contains(record2.FinalAssessmentFile, "artifacts/agents/msg/call/agent/final/2/") {
		t.Fatalf("expected namespaced final path for seq2, got %q", record2.FinalAssessmentFile)
	}

	raw1, err := writer.ReadFileRel(record1.FinalAnswerFile)
	if err != nil {
		t.Fatalf("readFileRel(seq1) failed: %v", err)
	}
	raw2, err := writer.ReadFileRel(record2.FinalAnswerFile)
	if err != nil {
		t.Fatalf("readFileRel(seq2) failed: %v", err)
	}
	if got := strings.TrimSpace(string(raw1)); got != "child-final-1" {
		t.Fatalf("expected seq1 final answer preserved, got %q", got)
	}
	if got := strings.TrimSpace(string(raw2)); got != "child-final-2" {
		t.Fatalf("expected seq2 final answer preserved, got %q", got)
	}
}
