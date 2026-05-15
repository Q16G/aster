package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/runtimelog"
)

type noopEmitter struct{}

func (noopEmitter) EmitThink(iteration int, content string, thinkContent string, reasoningContent string, toolCalls any, finishReason string) {
}
func (noopEmitter) EmitToolStart(iteration int, call ToolCall)        {}
func (noopEmitter) EmitToolEnd(iteration int, result ToolResult)      {}
func (noopEmitter) EmitStateChange(snapshot StateSnapshot)            {}
func (noopEmitter) EmitTaskPlan(plan []*PlanItem, explanation string) {}
func (noopEmitter) EmitHumanRequest(iteration int, requestID string, question string, context map[string]any) {
}
func (noopEmitter) EmitIteration(current int, max int, description string) {}
func (noopEmitter) EmitResult(result any, success bool)                    {}
func (noopEmitter) EmitToolUpdate(payload map[string]any)                  {}
func (noopEmitter) EmitLog(level string, message string)                   {}
func (noopEmitter) EmitInfo(message string)                                {}
func (noopEmitter) EmitWarning(message string)                             {}
func (noopEmitter) EmitError(message string)                               {}

type fakeToolContext struct {
	snapshot StateSnapshot
	emitter  Emitter
	planner  TaskPlanner
}

func newFakeToolContext() *fakeToolContext {
	return &fakeToolContext{
		emitter: noopEmitter{},
		snapshot: StateSnapshot{
			Phase:  AgentPhaseStep,
			Status: TaskStatusRunning,
		},
	}
}

func (f *fakeToolContext) Snapshot() StateSnapshot {
	return f.snapshot
}

func (f *fakeToolContext) UpdatePlan(plan []*PlanItem, explanation string, needsPlanning bool) StateSnapshot {
	_ = explanation
	f.snapshot.Plan = plan
	f.snapshot.NeedsPlanning = needsPlanning
	f.snapshot.PlanVersion++
	f.snapshot.CurrentStepID = currentStepIDForTest(plan, f.snapshot.CurrentStepID)
	return f.Snapshot()
}

func (f *fakeToolContext) UpdateCurrentStep(update CurrentStepUpdate) StateSnapshot {
	target := f.snapshot.CurrentStep()
	if target == nil {
		return f.Snapshot()
	}
	target.Status = update.Status
	f.snapshot.Phase = AgentPhaseStepReplan
	f.snapshot.StepOutcomes = append(f.snapshot.StepOutcomes, &StepOutcome{
		StepID:          target.ID,
		UpdatedAt:       time.Now(),
		Summary:         update.Summary,
		DisplayResult:   update.DisplayResult,
		Result:          update.Result,
		Error:           update.Error,
		References:      update.References,
		StatusSummary:   update.StatusSummary,
		ShortSummary:    update.ShortSummary,
		LongSummary:     update.LongSummary,
		KeyFacts:        update.KeyFacts,
		OpenQuestions:   update.OpenQuestions,
		ToolCallsDigest: update.ToolCallsDigest,
	})
	return f.Snapshot()
}

func (f *fakeToolContext) UpdateTaskStatus(update TaskStatusUpdate) StateSnapshot {
	if update.Status != "" {
		f.snapshot.Status = update.Status
	}
	if update.Message != "" {
		f.snapshot.StatusSummary = update.Message
	}
	if update.Progress >= 0 {
		f.snapshot.Progress = update.Progress
	}
	if update.Result != "" {
		f.snapshot.FinalAnswer = &FinalAnswer{Content: update.Result}
	}
	if update.Error != "" {
		f.snapshot.Error = update.Error
	}
	return f.Snapshot()
}

func (f *fakeToolContext) GetEmitter() Emitter {
	return f.emitter
}

func (f *fakeToolContext) ApplyPlanAndEmit(ctx context.Context, plan []*PlanItem, explanation string, needsPlanning bool) StateSnapshot {
	_ = ctx
	return f.UpdatePlan(plan, explanation, needsPlanning)
}

func (f *fakeToolContext) GetTaskPlanner() TaskPlanner {
	return f.planner
}

func (f *fakeToolContext) GetAIClient() ai.ChatClient {
	return nil
}

func currentStepIDForTest(plan []*PlanItem, currentStepID string) string {
	if current := (StateSnapshot{Plan: plan, CurrentStepID: currentStepID}).CurrentStep(); current != nil {
		return current.ID
	}
	return ""
}

func (f *fakeToolContext) GetHistory() []*ai.MsgInfo {
	return nil
}

func (f *fakeToolContext) GetOnHumanInput() OnHumanInputFunc {
	return nil
}

func decodeJSONMap(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode json failed: %v; raw=%s", err, raw)
	}
	if out == nil {
		t.Fatalf("decoded json is nil: %s", raw)
	}
	return out
}

func TestUpdateTaskStatusCompletedWithUnfinishedPlanReturnsSoftFailure(t *testing.T) {
	ctx := newFakeToolContext()
	ctx.snapshot.Plan = []*PlanItem{
		{Step: "source", Status: PlanStepInProgress},
		{Step: "flow", Status: PlanStepPending},
	}

	tool := NewUpdateTaskStatusTool(ctx)
	out, err := tool.Execute(context.Background(), map[string]any{
		"status": "completed",
		"result": "done",
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	payload := decodeJSONMap(t, out)
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got %v", payload["ok"])
	}
	if payload["reason"] != "unfinished_plan" {
		t.Fatalf("expected reason unfinished_plan, got %#v", payload["reason"])
	}
	if ctx.snapshot.Status == TaskStatusCompleted {
		t.Fatalf("task status should not be updated to completed")
	}
}

func TestUpdateTaskStatusSupportsStatusAlias(t *testing.T) {
	ctx := newFakeToolContext()
	ctx.snapshot.Plan = []*PlanItem{{Step: "done", Status: PlanStepCompleted}}

	tool := NewUpdateTaskStatusTool(ctx)
	out, err := tool.Execute(context.Background(), map[string]any{
		"status": "done",
		"result": "final result",
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	payload := decodeJSONMap(t, out)
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %v", payload["ok"])
	}
	if payload["status"] != string(TaskStatusCompleted) {
		t.Fatalf("expected status=%s, got %#v", TaskStatusCompleted, payload["status"])
	}
	if ctx.snapshot.Status != TaskStatusCompleted {
		t.Fatalf("expected state status completed, got %s", ctx.snapshot.Status)
	}
}

func TestUpdateTaskStatusMissingResultReturnsSoftFailure(t *testing.T) {
	ctx := newFakeToolContext()
	ctx.snapshot.Plan = []*PlanItem{{Step: "done", Status: PlanStepCompleted}}

	tool := NewUpdateTaskStatusTool(ctx)
	out, err := tool.Execute(context.Background(), map[string]any{
		"status": "completed",
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	payload := decodeJSONMap(t, out)
	if ok, _ := payload["ok"].(bool); ok {
		t.Fatalf("expected ok=false, got %v", payload["ok"])
	}
	if payload["reason"] != "missing_result" {
		t.Fatalf("expected reason missing_result, got %#v", payload["reason"])
	}
	if ctx.snapshot.Status == TaskStatusCompleted {
		t.Fatalf("task status should remain non-terminal when result missing")
	}
}

func TestUpdateCurrentStepIgnoresUnexpectedArgs(t *testing.T) {
	ctx := newFakeToolContext()
	ctx.snapshot.Plan = []*PlanItem{
		{Step: "step-1", Status: PlanStepInProgress},
		{Step: "step-2", Status: PlanStepPending},
	}

	tool := NewUpdateCurrentStepTool(ctx)
	_, err := tool.Execute(context.Background(), map[string]any{
		"status": "completed",
		"task":   "ignored-task",
		"step":   "ignored-step",
	})
	if err != nil {
		t.Fatalf("unexpected error for ignored arguments: %v", err)
	}
}

func TestUpdateCurrentStepWritesTerminalLog(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

	ctx := newFakeToolContext()
	ctx.snapshot.Plan = []*PlanItem{
		{ID: "inspect", Step: "梳理链路", Status: PlanStepInProgress},
	}
	ctx.snapshot.CurrentStepID = "inspect"

	tool := NewUpdateCurrentStepTool(ctx)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"status":  "failed",
		"summary": "定位到依赖缺失",
		"error":   "dependency missing",
	}); err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	out := buf.String()
	if out == "" {
		t.Fatalf("expected terminal log output")
	}
	if !strings.Contains(out, "\"event\":\"step_updated\"") {
		t.Fatalf("expected step_updated log, got %s", out)
	}
	if !strings.Contains(out, "\"step_id\":\"inspect\"") {
		t.Fatalf("expected inspect step log, got %s", out)
	}
	if strings.Contains(out, "\"need_replan\"") {
		t.Fatalf("expected terminal log without need_replan, got %s", out)
	}
}

func TestUpdateCurrentStepSchemaDoesNotExposeNeedReplan(t *testing.T) {
	ctx := newFakeToolContext()
	tool := NewUpdateCurrentStepTool(ctx)

	params, ok := tool.Parameters().(map[string]any)
	if !ok {
		t.Fatalf("expected object schema, got %#v", tool.Parameters())
	}
	properties, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties in schema, got %#v", params)
	}
	if _, exists := properties["need_replan"]; exists {
		t.Fatalf("need_replan should not appear in update_current_step schema")
	}
}
