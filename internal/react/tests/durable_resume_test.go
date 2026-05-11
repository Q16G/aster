package react_test

import (
	. "aster/internal/react"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

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
				NeedsPlanning: true,
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
		WithResumeExecutionIntent(),
		WithResumeOnly(),
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
