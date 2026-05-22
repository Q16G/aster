package tui

import (
	"strings"
	"testing"
	"time"

	"aster/internal/react"
)

func TestHandleAgentEventLogDoesNotOverwriteStatus(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.statusText = "thinking..."

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeLog,
		Payload: map[string]any{
			"message": "simple reply path started",
		},
	})

	if m.statusText != "thinking..." {
		t.Fatalf("expected statusText to remain unchanged, got %q", m.statusText)
	}
}

func TestHandleAgentEventStateChangePrefersStatusSummary(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.statusText = "thinking..."

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeStateChange,
		Payload: map[string]any{
			"phase":          "step_summary",
			"status_summary": "正在整理结果",
		},
	})

	if m.statusText != "正在整理结果" {
		t.Fatalf("expected status summary, got %q", m.statusText)
	}
}

func TestHandleAgentEventStepReplanPhaseShowsPanelAndThinkContent(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeStateChange,
		Payload: map[string]any{
			"phase": "step_replan",
		},
	})

	if m.statusText != "evaluating plan..." {
		t.Fatalf("expected step_replan status text, got %q", m.statusText)
	}
	if !m.thinkingPanel.visible {
		t.Fatal("expected thinking panel to be visible during step_replan")
	}

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeThink,
		Payload: map[string]any{
			"think_content": "旧计划未覆盖新增验证缺口，需要补一轮验证",
		},
	})

	view := m.thinkingPanel.View()
	if !strings.Contains(view, "旧计划未") || !strings.Contains(view, "需要") || !strings.Contains(view, "补一轮验证") {
		t.Fatalf("expected replan think content in panel, got %q", view)
	}
}

func TestHandleAgentEventRetryUpdatesRetryState(t *testing.T) {
	m := NewModel(ModelDeps{})
	next := time.Now().Add(2 * time.Second)

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeRetry,
		Payload: map[string]any{
			"message":      "Too Many Requests",
			"attempt":      1,
			"max_attempts": 4,
			"next_unix_ms": next.UnixMilli(),
		},
	})

	if m.retryState == nil {
		t.Fatalf("expected retry state to be populated")
	}
	if m.retryState.Message != "Too Many Requests" || m.retryState.Attempt != 1 || m.retryState.MaxAttempts != 4 {
		t.Fatalf("unexpected retry state: %#v", m.retryState)
	}
}

func TestHandleAgentEventStateChangeDoesNotOverrideRetryLabel(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeRetry,
		Payload: map[string]any{
			"message":      "Too Many Requests",
			"attempt":      1,
			"max_attempts": 4,
			"next_unix_ms": time.Now().Add(2 * time.Second).UnixMilli(),
		},
	})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeStateChange,
		Payload: map[string]any{
			"phase":          "plan",
			"status_summary": "正在规划",
		},
	})

	if m.statusText != "正在规划" {
		t.Fatalf("expected latest status summary to be preserved, got %q", m.statusText)
	}
	label := m.loadingLabel(80)
	if label == "" || label == "正在规划" {
		t.Fatalf("expected retry label to stay visible, got %q", label)
	}
	if want := "Too Many Requests"; !strings.HasPrefix(label, want) {
		t.Fatalf("expected retry label to start with %q, got %q", want, label)
	}
}

func TestHandleAgentEventStateChangeTracksExternalInterrupt(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeStateChange,
		Payload: map[string]any{
			"status_summary": "正在收尾",
			"external_interrupt": map[string]any{
				"reason_code":       "provider_quota",
				"retryable":         false,
				"error":             "HTTP 429: insufficient_quota",
				"user_message":      "当前 provider 配额已耗尽，本次不会自动重试。",
				"suggested_actions": []any{"切换到仍有额度的 provider 或 model"},
			},
		},
	})

	if m.externalInterrupt == nil {
		t.Fatal("expected external interrupt to be captured")
	}
	if m.externalInterrupt.ReasonCode != "provider_quota" || m.externalInterrupt.Retryable {
		t.Fatalf("unexpected external interrupt: %#v", m.externalInterrupt)
	}
}

func TestHandleAgentEventToolUpdateAddsStepResultPart(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeToolUpdate,
		Payload: map[string]any{
			"tool_name":      "update_current_step",
			"presentation":   "step_result",
			"step_id":        "step-5",
			"step_name":      "输出结果",
			"step_status":    "completed",
			"display_result": "已输出 Markdown 标准报告",
		},
	})

	parts := m.chat.Parts()
	if len(parts) != 1 {
		t.Fatalf("expected 1 chat part, got %d", len(parts))
	}
	if parts[0].Type != PartTypeStepResult || parts[0].StepResult == nil {
		t.Fatalf("expected step result part, got %+v", parts[0])
	}
	if parts[0].StepResult.DisplayResult != "已输出 Markdown 标准报告" {
		t.Fatalf("unexpected step result content: %+v", parts[0].StepResult)
	}
}

func TestHandleAgentEventStepReplanResultAddsPart(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeStepReplanResult,
		Payload: map[string]any{
			"step_id":       "step-1",
			"step_name":     "检查上下文",
			"should_replan": true,
			"replan_reason": "旧计划未覆盖新增验证缺口",
			"next_goal":     "围绕新缺口补齐验证",
			"missing_items": []any{"missing-1"},
			"warnings":      []any{"warn-1"},
		},
	})

	parts := m.chat.Parts()
	if len(parts) != 1 {
		t.Fatalf("expected 1 chat part, got %d", len(parts))
	}
	if parts[0].Type != PartTypeStepReplan || parts[0].StepReplan == nil {
		t.Fatalf("expected step replan part, got %+v", parts[0])
	}
	if !parts[0].StepReplan.ShouldReplan {
		t.Fatalf("expected should_replan=true, got %+v", parts[0].StepReplan)
	}
	if parts[0].StepReplan.ReplanReason != "旧计划未覆盖新增验证缺口" {
		t.Fatalf("unexpected replan reason: %+v", parts[0].StepReplan)
	}
	if parts[0].StepReplan.NextGoal != "围绕新缺口补齐验证" {
		t.Fatalf("unexpected next goal: %+v", parts[0].StepReplan)
	}
}

func TestHandleAgentEventSubAgentPlanDoesNotOverrideRootPlan(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskPlan,
		AgentName: "my-agent",
		Payload: map[string]any{
			"explanation": "root plan",
			"plan": []any{
				map[string]any{"id": "r1", "step": "root-step-1", "status": "pending"},
			},
		},
	})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskPlan,
		AgentName: "sub-abc12345",
		Payload: map[string]any{
			"explanation": "sub plan",
			"plan": []any{
				map[string]any{"id": "s1", "step": "sub-step-1", "status": "pending"},
			},
		},
	})

	var rootPlan, subPlan *PlanPart
	for _, p := range m.chat.Parts() {
		if p.Type == PartTypePlan && p.Plan != nil {
			if p.Plan.AgentName == "my-agent" {
				rootPlan = p.Plan
			}
			if p.Plan.AgentName == "sub-abc12345" {
				subPlan = p.Plan
			}
		}
	}

	if rootPlan == nil {
		t.Fatal("expected root plan to be present")
	}
	if rootPlan.Explanation != "root plan" {
		t.Fatalf("expected root plan explanation, got %q", rootPlan.Explanation)
	}
	if subPlan == nil {
		t.Fatal("expected sub-agent plan to be present separately")
	}
	if subPlan.Explanation != "sub plan" {
		t.Fatalf("expected sub plan explanation, got %q", subPlan.Explanation)
	}
}

func TestHandleAgentEventTaskItemUpdatesCorrectAgentPlan(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskPlan,
		AgentName: "my-agent",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "r1", "step": "root-step", "status": "pending"},
			},
		},
	})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskPlan,
		AgentName: "sub-xyz",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "s1", "step": "sub-step", "status": "pending"},
			},
		},
	})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskItem,
		AgentName: "my-agent",
		Payload: map[string]any{
			"id":     "r1",
			"status": "done",
		},
	})

	for _, p := range m.chat.Parts() {
		if p.Type == PartTypePlan && p.Plan != nil {
			if p.Plan.AgentName == "my-agent" {
				if p.Plan.Items[0].Status != "done" {
					t.Fatalf("expected root plan item to be 'done', got %q", p.Plan.Items[0].Status)
				}
			}
			if p.Plan.AgentName == "sub-xyz" {
				if p.Plan.Items[0].Status != "pending" {
					t.Fatalf("expected sub plan item to remain 'pending', got %q", p.Plan.Items[0].Status)
				}
			}
		}
	}
}

func TestBuildSidebarSnapshotOnlyShowsRootAgentPlan(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "my-agent"},
		},
	})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskPlan,
		AgentName: "my-agent",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "r1", "step": "root-step", "status": "pending"},
				map[string]any{"id": "r2", "step": "root-step-2", "status": "completed"},
			},
		},
	})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskPlan,
		AgentName: "sub-abc12345",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "s1", "step": "sub-step", "status": "in_progress"},
			},
		},
	})

	snap := m.buildSidebarSnapshot()

	if len(snap.PlanItems) != 2 {
		t.Fatalf("expected sidebar to show 2 root plan items, got %d", len(snap.PlanItems))
	}
	if snap.PlanItems[0].ID != "r1" || snap.PlanItems[1].ID != "r2" {
		t.Fatalf("expected root plan items r1 and r2, got %+v", snap.PlanItems)
	}
}

func TestBuildSidebarSnapshotShowsLegacyUntaggedPlans(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "code-audit"},
		},
	})

	m.chat.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{Items: []PlanItemView{{ID: "old1", Step: "legacy-step", Status: "pending"}}},
	})
	m.chat.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "sub-xyz", Items: []PlanItemView{{ID: "s1", Step: "sub-step", Status: "pending"}}},
	})

	snap := m.buildSidebarSnapshot()

	if len(snap.PlanItems) != 1 {
		t.Fatalf("expected sidebar to show 1 legacy plan item, got %d", len(snap.PlanItems))
	}
	if snap.PlanItems[0].ID != "old1" {
		t.Fatalf("expected legacy plan item, got %+v", snap.PlanItems)
	}
}

func TestBuildSidebarSnapshotNoAgentCtxShowsUntaggedPlans(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.chat.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{Items: []PlanItemView{{ID: "old1", Step: "legacy-step", Status: "pending"}}},
	})

	m.chat.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "sub-xyz", Items: []PlanItemView{{ID: "s1", Step: "sub-step", Status: "pending"}}},
	})

	snap := m.buildSidebarSnapshot()

	if len(snap.PlanItems) != 1 {
		t.Fatalf("expected sidebar to show 1 untagged plan item, got %d", len(snap.PlanItems))
	}
	if snap.PlanItems[0].ID != "old1" {
		t.Fatalf("expected legacy plan item, got %+v", snap.PlanItems)
	}
}

func TestHandleAgentEventFinalAnswerShowsFullContentByDefault(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.SetSize(100, 20)
	content := strings.Repeat("A", 70) + "TAIL-XYZ"

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeFinalAnswerResult,
		Payload: map[string]any{
			"content": content,
			"source":  "final_assessment",
		},
	})

	view := m.chat.View()
	if !strings.Contains(view, "TAIL-XYZ") {
		t.Fatalf("expected final answer full content in default view, got %q", view)
	}
}
