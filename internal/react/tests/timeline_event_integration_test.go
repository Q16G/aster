package react_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	. "aster/internal/react"
)

func TestExecute_WritesTimelineEventsForToolCalls(t *testing.T) {
	wsRoot := t.TempDir()
	wsRuntime := &realFileWorkspaceRuntime{rootDir: wsRoot}

	targetFile := filepath.Join(wsRoot, "hello.txt")
	if err := os.WriteFile(targetFile, []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	client := &executeModelTestClient{
		replies: []executeModelReply{
			// step phase iter 1: model calls read_file
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-read-1", builtin_tools.ReadFileToolName, map[string]any{
						"path": targetFile,
					}),
				},
			},
			// step phase iter 2: model marks step completed
			{
				toolCalls: []*ai.FunctionTool{
					mustBuildToolCall(t, "call-step-done", builtin_tools.UpdateCurrentStepToolName, map[string]any{
						"status":        "completed",
						"summary":       "read done",
						"result":        "read ok",
						"short_summary": "read the file",
					}),
				},
			},
			// step_replan
			{content: `{"should_replan":false,"replan_reason":"","next_goal":"","missing_items":[],"warnings":[]}`},
			// final_answer
			{content: `{"is_complete":true,"status":"completed","reason":"done","should_replan":false,"next_goal":"","missing_items":[],"warnings":[],"user_message":"timeline-test-done","references":[]}`},
		},
	}

	agent, err := NewReActAgent(
		"timeline-agent",
		client,
		WithEmitter(NewDummyEmitter()),
		WithMaxIterations(10),
		WithHistoryCompressor(&noopHistoryCompressor{}),
		WithTools(builtin_tools.NewReadFileTool()),
		WithTaskPlanner(&executeModelStaticPlanner{
			result: &builtin_tools.TaskPlannerResult{
				NeedsPlanning: true,
				Plan: []*builtin_tools.PlanItem{
					{ID: "step-1", Step: "读取文件", Status: builtin_tools.PlanStepPending},
				},
			},
		}),
	)
	if err != nil {
		t.Fatalf("NewReActAgent failed: %v", err)
	}

	runResult, err := agent.Execute(context.Background(), "read the file",
		WithSkipIntentPrelude(),
		WithWorkspaceRuntime(wsRuntime),
	)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("expected success, got %#v", runResult)
	}

	// --- Verify timeline.jsonl exists under shared/step-1/ ---
	timelinePath := filepath.Join(wsRoot, "shared", "step-1", "timeline.jsonl")
	f, err := os.Open(timelinePath)
	if err != nil {
		t.Fatalf("open timeline.jsonl: %v", err)
	}
	defer f.Close()

	type timelineEvent struct {
		Type    string         `json:"type"`
		Key     string         `json:"key"`
		Payload map[string]any `json:"payload"`
	}

	var events []timelineEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev timelineEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("unmarshal timeline event: %v", err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 timeline events (read_file + update_current_step), got %d", len(events))
	}

	// First event should be the read_file tool call
	if events[0].Type != "tool_call" {
		t.Errorf("event[0].Type = %q, want tool_call", events[0].Type)
	}
	if events[0].Key != "call-read-1" {
		t.Errorf("event[0].Key = %q, want call-read-1", events[0].Key)
	}
	if tool, _ := events[0].Payload["tool"].(string); tool != builtin_tools.ReadFileToolName {
		t.Errorf("event[0].Payload.tool = %q, want %s", tool, builtin_tools.ReadFileToolName)
	}

	// Second event should be the update_current_step tool call
	if events[1].Type != "tool_call" {
		t.Errorf("event[1].Type = %q, want tool_call", events[1].Type)
	}
	if events[1].Key != "call-step-done" {
		t.Errorf("event[1].Key = %q, want call-step-done", events[1].Key)
	}

	// --- Verify StepContextRecord.TimelineFile is set ---
	records, loadErr := builtin_tools.LoadWorkspaceStepContextRecords(wsRoot, 0)
	if loadErr != nil {
		t.Fatalf("LoadWorkspaceStepContextRecords: %v", loadErr)
	}

	var stepRec *builtin_tools.StepContextRecord
	for _, r := range records {
		if r.StepID == "step-1" {
			stepRec = r
			break
		}
	}
	if stepRec == nil {
		t.Fatalf("no StepContextRecord found for step-1")
	}
	wantTimelineFile := filepath.ToSlash(filepath.Join(wsRoot, "shared/step-1/timeline.jsonl"))
	if stepRec.TimelineFile != wantTimelineFile {
		t.Errorf("StepContextRecord.TimelineFile = %q, want %q", stepRec.TimelineFile, wantTimelineFile)
	}
}
