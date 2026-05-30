package tui

import (
	"strings"
	"testing"
	"time"

	"aster/internal/react"
)

func subAgentCardStatus(m *Model, callID string) (string, bool) {
	for _, p := range m.chat.Parts() {
		if p.Type == PartTypeSubAgent && p.SubAgent != nil && p.SubAgent.CallID == callID {
			return p.SubAgent.Status, true
		}
	}
	return "", false
}

// The card may receive both the child's own terminal event (from its goroutine
// defer) and the cancel-path fallback. Only the first running->terminal flip
// wins; later duplicate end events must be ignored.
func TestHandleAgentEventSubAgentBgEndIsIdempotent(t *testing.T) {
	m := NewModel(ModelDeps{})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:    react.EventTypeSubAgentBgStart,
		Payload: map[string]any{"agent_id": "sub-x", "instruction": "scan"},
	})
	if got, ok := subAgentCardStatus(&m, "sub-x"); !ok || got != "running" {
		t.Fatalf("expected running card, got %q (ok=%v)", got, ok)
	}

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:    react.EventTypeSubAgentBgEnd,
		Payload: map[string]any{"agent_id": "sub-x", "status": "completed", "summary": "done"},
	})
	if got, _ := subAgentCardStatus(&m, "sub-x"); got != "completed" {
		t.Fatalf("expected completed after first end, got %q", got)
	}

	// Late cancel-path fallback must not overwrite the settled status.
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:    react.EventTypeSubAgentBgEnd,
		Payload: map[string]any{"agent_id": "sub-x", "status": "cancelled"},
	})
	if got, _ := subAgentCardStatus(&m, "sub-x"); got != "completed" {
		t.Fatalf("expected status to stay completed, got %q", got)
	}
}

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

// TestSubAgentPhaseEventsDoNotLeakIntoMainArea verifies the fix for sub-agent
// step_replan / thinking / final_answer leaking into the main agent's runtime
// panel and status line. Sub-agent phase activity must only produce parts
// attributed to that sub-agent (collapsed behind its card); it must NOT touch
// the global thinkingPanel, runtimePhase, statusText, or hadFinalAnswerDuringRun.
func TestSubAgentPhaseEventsDoNotLeakIntoMainArea(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "code-audit"},
		},
	})
	const sub = "sub-call_99abc"
	m.statusText = "ready-sentinel"

	// 1. Sub-agent enters final_answer phase — must not drive the main panel.
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStateChange,
		AgentName: sub,
		Payload:   map[string]any{"phase": "final_answer"},
	})
	if m.runtimePhase != "" {
		t.Fatalf("sub-agent StateChange must not set runtimePhase, got %q", m.runtimePhase)
	}
	if m.thinkingPanel.visible {
		t.Fatal("sub-agent StateChange must not show the main thinking panel")
	}
	if m.statusText != "ready-sentinel" {
		t.Fatalf("sub-agent StateChange must not overwrite statusText, got %q", m.statusText)
	}

	// 2. Sub-agent thinking — must go to its own buffer, not the panel.
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeThink,
		AgentName: sub,
		GroupID:   "g-sub-1",
		Payload:   map[string]any{"think_content": "子agent内部推理片段"},
	})
	if len(m.thinkingPanel.entries) != 0 {
		t.Fatalf("sub-agent thinking must not push panel entries, got %d", len(m.thinkingPanel.entries))
	}
	if m.statusText != "ready-sentinel" {
		t.Fatalf("sub-agent thinking must not overwrite statusText, got %q", m.statusText)
	}

	// 3. Sub-agent step_replan result — part attributed, no panel entry.
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStepReplanResult,
		AgentName: sub,
		Payload: map[string]any{
			"step_id":       "s-1",
			"should_replan": true,
			"replan_reason": "子agent需要补一轮验证",
		},
	})
	if len(m.thinkingPanel.entries) != 0 {
		t.Fatalf("sub-agent step_replan must not push panel entries, got %d", len(m.thinkingPanel.entries))
	}

	// 4. Sub-agent final_answer result — must not set the root's run flag.
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeFinalAnswerResult,
		AgentName: sub,
		Payload:   map[string]any{"content": "子agent的最终答案", "source": "sub_final"},
	})
	if m.hadFinalAnswerDuringRun {
		t.Fatal("sub-agent final_answer must not set hadFinalAnswerDuringRun")
	}
	if m.thinkingPanel.visible || len(m.thinkingPanel.entries) != 0 {
		t.Fatalf("sub-agent final_answer must not touch the panel (visible=%v entries=%d)", m.thinkingPanel.visible, len(m.thinkingPanel.entries))
	}

	// The sub-agent's thinking, replan and final_answer must exist as parts
	// attributed to the sub-agent (so they render behind its card / drill-in),
	// and must NOT be attributed to the root agent.
	m.chat.FlushThinking()
	var sawSubThinking, sawSubReplan, sawSubFinal bool
	for _, p := range m.chat.Parts() {
		if name := partAgentName(p); name != "" && name != sub {
			t.Fatalf("unexpected part attributed to %q (expected only sub-agent or root): %+v", name, p)
		}
		switch p.Type {
		case PartTypeThinking:
			if p.Thinking != nil && p.Thinking.AgentName == sub && strings.Contains(p.Thinking.Content, "子agent内部推理片段") {
				sawSubThinking = true
			}
		case PartTypeStepReplan:
			if p.StepReplan != nil && p.StepReplan.AgentName == sub {
				sawSubReplan = true
			}
		case PartTypeFinalAnswer:
			if p.FinalAnswer != nil && p.FinalAnswer.AgentName == sub {
				sawSubFinal = true
			}
		}
	}
	if !sawSubThinking {
		t.Error("expected sub-agent thinking part attributed to the sub-agent")
	}
	if !sawSubReplan {
		t.Error("expected sub-agent step_replan part attributed to the sub-agent")
	}
	if !sawSubFinal {
		t.Error("expected sub-agent final_answer part attributed to the sub-agent")
	}
}

// TestRootPhaseEventsStillDriveMainPanel is the control case for
// TestSubAgentPhaseEventsDoNotLeakIntoMainArea: the root agent's phase events
// must continue to drive the global panel, status line, and run flag.
func TestRootPhaseEventsStillDriveMainPanel(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "code-audit"},
		},
	})

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStateChange,
		AgentName: "code-audit",
		Payload:   map[string]any{"phase": "step_replan"},
	})
	if m.runtimePhase != "step_replan" {
		t.Fatalf("expected root runtimePhase=step_replan, got %q", m.runtimePhase)
	}
	if !m.thinkingPanel.visible {
		t.Fatal("expected root step_replan to show the thinking panel")
	}
	if m.statusText != "evaluating plan..." {
		t.Fatalf("expected root step_replan status text, got %q", m.statusText)
	}

	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeFinalAnswerResult,
		AgentName: "code-audit",
		Payload:   map[string]any{"content": "最终答案", "source": "final"},
	})
	if !m.hadFinalAnswerDuringRun {
		t.Fatal("expected root final_answer to set hadFinalAnswerDuringRun")
	}
}

func TestBuildSidebarSnapshotHierarchicalPlan(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "my-agent"},
		},
	})

	// Root plan
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

	// Set r2 as in_progress so activeStepByAgent tracks it
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskItem,
		AgentName: "my-agent",
		Payload:   map[string]any{"id": "r2", "status": "in_progress"},
	})

	// Simulate tool_start(is_agent=true) from parent
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeToolStart,
		AgentName: "my-agent",
		Payload: map[string]any{
			"tool_name": "sub_agent",
			"call_id":   "abc12345",
			"is_agent":  true,
		},
	})

	// Sub-agent plan arrives — should nest under r2
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

	// r1(depth=0), r2(depth=0), s1(depth=1, nested under r2)
	if len(snap.PlanItems) != 3 {
		t.Fatalf("expected 3 items (2 root + 1 sub nested), got %d", len(snap.PlanItems))
	}
	if snap.PlanItems[0].ID != "r1" || snap.PlanItems[0].Depth != 0 {
		t.Fatalf("expected r1 at depth 0, got %+v", snap.PlanItems[0])
	}
	if snap.PlanItems[1].ID != "r2" || snap.PlanItems[1].Depth != 0 {
		t.Fatalf("expected r2 at depth 0, got %+v", snap.PlanItems[1])
	}
	if snap.PlanItems[2].ID != "s1" || snap.PlanItems[2].Depth != 1 {
		t.Fatalf("expected s1 at depth 1, got %+v", snap.PlanItems[2])
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

	// root (old1 depth=0) + orphan sub (s1 depth=1)
	if len(snap.PlanItems) != 2 {
		t.Fatalf("expected 2 items (root + orphan sub), got %d", len(snap.PlanItems))
	}
	if snap.PlanItems[0].ID != "old1" || snap.PlanItems[0].Depth != 0 {
		t.Fatalf("expected legacy plan item at depth 0, got %+v", snap.PlanItems[0])
	}
	if snap.PlanItems[1].ID != "s1" || snap.PlanItems[1].Depth != 1 {
		t.Fatalf("expected orphan sub item at depth 1, got %+v", snap.PlanItems[1])
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

	// root (old1 depth=0) + orphan sub (s1 depth=1)
	if len(snap.PlanItems) != 2 {
		t.Fatalf("expected 2 items (root + orphan sub), got %d", len(snap.PlanItems))
	}
	if snap.PlanItems[0].ID != "old1" || snap.PlanItems[0].Depth != 0 {
		t.Fatalf("expected legacy plan item at depth 0, got %+v", snap.PlanItems[0])
	}
	if snap.PlanItems[1].ID != "s1" || snap.PlanItems[1].Depth != 1 {
		t.Fatalf("expected orphan sub at depth 1, got %+v", snap.PlanItems[1])
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

// TestBuildSidebarSnapshot_SessionFE413AAB simulates the plan structure from
// session fe413aab where root agent "code-audit" had 8 steps and sub-agents
// had their own plans. Verifies hierarchical nesting with proper depth.
func TestBuildSidebarSnapshot_SessionFE413AAB(t *testing.T) {
	t.Run("hierarchical_with_parent_step", func(t *testing.T) {
		m := NewModel(ModelDeps{
			AgentCtx: &AgentExecContext{
				Definition: react.AgentDefinition{Name: "code-audit"},
			},
		})

		rootPlanItems := []PlanItemView{
			{ID: "recon-1", Step: "全局侦察", Status: "completed"},
			{ID: "scan-vuln", Step: "SAST 漏洞扫描", Status: "completed"},
			{ID: "auth-review", Step: "认证授权审计", Status: "completed"},
			{ID: "config-sec", Step: "安全配置审计", Status: "completed"},
			{ID: "dep-audit", Step: "依赖审计", Status: "completed"},
			{ID: "biz-logic", Step: "业务逻辑审计", Status: "completed"},
			{ID: "dataflow-check", Step: "数据流验证", Status: "completed"},
			{ID: "analysis-report", Step: "综合报告", Status: "completed"},
		}
		m.chat.AddPart(DisplayPart{
			Type: PartTypePlan,
			Plan: &PlanPart{AgentName: "code-audit", Items: rootPlanItems},
		})

		// sub-call_02_ nested under biz-logic
		m.chat.AddPart(DisplayPart{
			Type: PartTypePlan,
			Plan: &PlanPart{AgentName: "sub-call_02_", ParentStepID: "biz-logic", Items: []PlanItemView{
				{ID: "s2-1", Step: "批量操作分析", Status: "completed"},
				{ID: "s2-2", Step: "竞态条件检查", Status: "in_progress"},
				{ID: "s2-3", Step: "工作流审计", Status: "pending"},
				{ID: "s2-4", Step: "汇总", Status: "pending"},
			}},
		})

		// sub-call_03_ also nested under biz-logic
		m.chat.AddPart(DisplayPart{
			Type: PartTypePlan,
			Plan: &PlanPart{AgentName: "sub-call_03_", ParentStepID: "biz-logic", Items: []PlanItemView{
				{ID: "s3-1", Step: "梳理安全问题发现点", Status: "completed"},
				{ID: "s3-2", Step: "深度分析风险1-3", Status: "completed"},
				{ID: "s3-3", Step: "深度分析风险4-6", Status: "pending"},
				{ID: "s3-4", Step: "综合分析WebSocket", Status: "pending"},
				{ID: "s3-5", Step: "汇总所有分析发现", Status: "pending"},
			}},
		})

		snap := m.buildSidebarSnapshot()

		// 8 root + 4 sub_02 + 5 sub_03 = 17 total
		if len(snap.PlanItems) != 17 {
			t.Fatalf("expected 17 items, got %d", len(snap.PlanItems))
		}

		// Verify structure: items before biz-logic are depth=0
		for i := 0; i < 5; i++ {
			if snap.PlanItems[i].Depth != 0 {
				t.Fatalf("item %d (%s) expected depth 0, got %d", i, snap.PlanItems[i].ID, snap.PlanItems[i].Depth)
			}
		}
		// biz-logic at index 5 depth=0
		if snap.PlanItems[5].ID != "biz-logic" || snap.PlanItems[5].Depth != 0 {
			t.Fatalf("expected biz-logic at depth 0, got %+v", snap.PlanItems[5])
		}
		// sub items at depth 1 (indices 6-14)
		for i := 6; i <= 14; i++ {
			if snap.PlanItems[i].Depth != 1 {
				t.Fatalf("item %d (%s) expected depth 1, got %d", i, snap.PlanItems[i].ID, snap.PlanItems[i].Depth)
			}
		}
		// remaining root items depth=0
		if snap.PlanItems[15].ID != "dataflow-check" || snap.PlanItems[15].Depth != 0 {
			t.Fatalf("expected dataflow-check at depth 0, got %+v", snap.PlanItems[15])
		}

		done := 0
		for _, item := range snap.PlanItems {
			if item.Status == "completed" {
				done++
			}
		}
		// 8 root + s2-1 + s3-1 + s3-2 = 11
		if done != 11 {
			t.Fatalf("expected 11 completed, got %d", done)
		}
	})

	t.Run("orphan_sub_plans_append_at_end", func(t *testing.T) {
		m := NewModel(ModelDeps{})

		rootPlanItems := []PlanItemView{
			{ID: "recon-1", Step: "全局侦察", Status: "completed"},
			{ID: "biz-logic", Step: "业务逻辑审计", Status: "completed"},
		}
		m.chat.AddPart(DisplayPart{
			Type: PartTypePlan,
			Plan: &PlanPart{Items: rootPlanItems},
		})

		// Sub plan without ParentStepID (orphan)
		m.chat.AddPart(DisplayPart{
			Type: PartTypePlan,
			Plan: &PlanPart{AgentName: "sub-call_03_", Items: []PlanItemView{
				{ID: "s3-1", Step: "分析", Status: "completed"},
			}},
		})

		snap := m.buildSidebarSnapshot()

		// 2 root + 1 orphan = 3
		if len(snap.PlanItems) != 3 {
			t.Fatalf("expected 3 items, got %d", len(snap.PlanItems))
		}
		if snap.PlanItems[2].ID != "s3-1" || snap.PlanItems[2].Depth != 1 {
			t.Fatalf("expected orphan s3-1 at depth 1, got %+v", snap.PlanItems[2])
		}
	})
}

func TestBuildSidebarSnapshot_RecursiveNesting(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "root"},
		},
	})

	// Root plan: 3 steps
	m.chat.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "root", Items: []PlanItemView{
			{ID: "r1", Step: "step-1", Status: "completed"},
			{ID: "r2", Step: "step-2", Status: "in_progress"},
			{ID: "r3", Step: "step-3", Status: "pending"},
		}},
	})

	// Sub-agent nested under r2
	m.chat.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "sub-xxx", ParentStepID: "r2", Items: []PlanItemView{
			{ID: "sub-1", Step: "sub step 1", Status: "completed"},
			{ID: "sub-2", Step: "sub step 2", Status: "in_progress"},
		}},
	})

	// Skill-fork nested under sub-2 (depth=2)
	m.chat.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "skill-scan-yyy", ParentStepID: "sub-2", Items: []PlanItemView{
			{ID: "sk-1", Step: "scan part A", Status: "completed"},
			{ID: "sk-2", Step: "scan part B", Status: "pending"},
		}},
	})

	snap := m.buildSidebarSnapshot()

	// r1(0), r2(0), sub-1(1), sub-2(1), sk-1(2), sk-2(2), r3(0)
	expected := []struct {
		ID    string
		Depth int
	}{
		{"r1", 0},
		{"r2", 0},
		{"sub-1", 1},
		{"sub-2", 1},
		{"sk-1", 2},
		{"sk-2", 2},
		{"r3", 0},
	}

	if len(snap.PlanItems) != len(expected) {
		t.Fatalf("expected %d items, got %d", len(expected), len(snap.PlanItems))
	}
	for i, want := range expected {
		got := snap.PlanItems[i]
		if got.ID != want.ID || got.Depth != want.Depth {
			t.Fatalf("item %d: expected {ID:%s Depth:%d}, got {ID:%s Depth:%d}", i, want.ID, want.Depth, got.ID, got.Depth)
		}
	}
}

// TestBuildSidebarSnapshot_E2E_EventFlow simulates a full event stream:
// root agent creates plan → executes steps → spawns sub-agent → sub-agent
// creates plan → sub-agent spawns skill-fork → skill-fork creates plan.
// Prints the rendered sidebar output to verify visual hierarchy.
func TestBuildSidebarSnapshot_E2E_EventFlow(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "code-audit"},
		},
	})

	// 1. Root agent creates plan
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskPlan,
		AgentName: "code-audit",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "recon", "step": "全局侦察与代码结构分析", "status": "pending"},
				map[string]any{"id": "vuln-scan", "step": "SAST 漏洞扫描", "status": "pending"},
				map[string]any{"id": "auth-review", "step": "认证授权审计", "status": "pending"},
				map[string]any{"id": "biz-logic", "step": "业务逻辑深度审计", "status": "pending"},
				map[string]any{"id": "dep-audit", "step": "依赖与供应链审计", "status": "pending"},
				map[string]any{"id": "report", "step": "综合安全报告", "status": "pending"},
			},
		},
	})

	// 2. Root completes first 3 steps
	for _, id := range []string{"recon", "vuln-scan", "auth-review"} {
		m.handleAgentEvent(&react.AgentOutputEvent{
			Type: react.EventTypeTaskItem, AgentName: "code-audit",
			Payload: map[string]any{"id": id, "status": "in_progress"},
		})
		m.handleAgentEvent(&react.AgentOutputEvent{
			Type: react.EventTypeTaskItem, AgentName: "code-audit",
			Payload: map[string]any{"id": id, "status": "completed"},
		})
	}

	// 3. Root starts biz-logic step
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskItem, AgentName: "code-audit",
		Payload: map[string]any{"id": "biz-logic", "status": "in_progress"},
	})

	// 4. Root spawns sub-agent (tool_start is_agent=true)
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeToolStart, AgentName: "code-audit",
		Payload: map[string]any{
			"tool_name": "sub_agent", "call_id": "call_sub_01", "is_agent": true,
		},
	})

	// 5. Sub-agent creates its own plan
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "sub-call_su",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "batch-ops", "step": "批量操作与越权分析", "status": "pending"},
				map[string]any{"id": "race-cond", "step": "竞态条件检查", "status": "pending"},
				map[string]any{"id": "workflow", "step": "工作流状态机审计", "status": "pending"},
				map[string]any{"id": "summary", "step": "汇总子审计发现", "status": "pending"},
			},
		},
	})

	// 6. Sub-agent completes batch-ops, starts race-cond
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskItem, AgentName: "sub-call_su",
		Payload: map[string]any{"id": "batch-ops", "status": "in_progress"},
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskItem, AgentName: "sub-call_su",
		Payload: map[string]any{"id": "batch-ops", "status": "completed"},
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskItem, AgentName: "sub-call_su",
		Payload: map[string]any{"id": "race-cond", "status": "in_progress"},
	})

	// 7. Sub-agent spawns skill-fork for deep analysis
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeToolStart, AgentName: "sub-call_su",
		Payload: map[string]any{
			"tool_name": "skill_fork_deep_scan", "call_id": "call_skill_01", "is_agent": true,
		},
	})

	// 8. Skill-fork creates its own plan (depth=2)
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "skill-deep_scan-call_s",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "lock-analysis", "step": "锁竞争与死锁分析", "status": "pending"},
				map[string]any{"id": "atomic-check", "step": "原子操作正确性检查", "status": "pending"},
				map[string]any{"id": "toctou", "step": "TOCTOU 时序漏洞扫描", "status": "pending"},
			},
		},
	})

	// 9. Skill-fork completes first item, second in progress
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskItem, AgentName: "skill-deep_scan-call_s",
		Payload: map[string]any{"id": "lock-analysis", "status": "in_progress"},
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskItem, AgentName: "skill-deep_scan-call_s",
		Payload: map[string]any{"id": "lock-analysis", "status": "completed"},
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskItem, AgentName: "skill-deep_scan-call_s",
		Payload: map[string]any{"id": "atomic-check", "status": "in_progress"},
	})

	// --- Build snapshot and render ---
	snap := m.buildSidebarSnapshot()

	t.Logf("\n=== Sidebar PlanItems (total=%d) ===", len(snap.PlanItems))
	for i, item := range snap.PlanItems {
		icon := "○"
		switch item.Status {
		case "completed":
			icon = "✓"
		case "in_progress":
			icon = "▸"
		case "failed":
			icon = "✗"
		}
		indent := strings.Repeat("  ", item.Depth+1)
		t.Logf("%s%s %s (id=%s, depth=%d)", indent, icon, item.Step, item.ID, item.Depth)
		_ = i
	}

	// Render via sidebar model
	sidebar := NewSidebarModel()
	sidebar.SetSize(50, 40)
	sidebar.SetSnapshot(snap)
	var sb strings.Builder
	sidebar.renderTodoSection(&sb, 50)
	t.Logf("\n=== Rendered Sidebar (width=50) ===\n%s", sb.String())

	// --- Assertions ---
	// Total: 6 root + 4 sub + 3 skill = 13
	if len(snap.PlanItems) != 13 {
		t.Fatalf("expected 13 items, got %d", len(snap.PlanItems))
	}

	expected := []struct {
		ID    string
		Depth int
	}{
		{"recon", 0},
		{"vuln-scan", 0},
		{"auth-review", 0},
		{"biz-logic", 0},
		{"batch-ops", 1},
		{"race-cond", 1},
		{"lock-analysis", 2},
		{"atomic-check", 2},
		{"toctou", 2},
		{"workflow", 1},
		{"summary", 1},
		{"dep-audit", 0},
		{"report", 0},
	}
	for i, want := range expected {
		got := snap.PlanItems[i]
		if got.ID != want.ID || got.Depth != want.Depth {
			t.Errorf("item[%d]: want {%s depth=%d}, got {%s depth=%d}", i, want.ID, want.Depth, got.ID, got.Depth)
		}
	}
}

// TestRenderTodoSection_CollapseCompletedSubtree verifies that completed
// subtrees are auto-collapsed with a (done/total) suffix, while active
// subtrees remain expanded.
func TestRenderTodoSection_CollapseCompletedSubtree(t *testing.T) {
	items := []PlanItemView{
		{ID: "recon", Step: "全局侦察", Status: "completed", Depth: 0},
		{ID: "vuln-scan", Step: "SAST 漏洞扫描", Status: "completed", Depth: 0},
		// auth-review completed, with 3 completed children → should collapse
		{ID: "auth-review", Step: "认证授权审计", Status: "completed", Depth: 0},
		{ID: "a1", Step: "OAuth 流程审计", Status: "completed", Depth: 1},
		{ID: "a2", Step: "JWT 令牌审计", Status: "completed", Depth: 1},
		{ID: "a3", Step: "RBAC 权限审计", Status: "completed", Depth: 1},
		// biz-logic in_progress, with mixed children → should expand
		{ID: "biz-logic", Step: "业务逻辑审计", Status: "in_progress", Depth: 0},
		{ID: "b1", Step: "批量操作分析", Status: "completed", Depth: 1},
		{ID: "b2", Step: "竞态条件检查", Status: "in_progress", Depth: 1},
		// b2 has completed sub-children → should collapse
		{ID: "b2-1", Step: "锁竞争分析", Status: "completed", Depth: 2},
		{ID: "b2-2", Step: "原子操作检查", Status: "completed", Depth: 2},
		{ID: "b3", Step: "工作流审计", Status: "pending", Depth: 1},
		// dep-audit completed, with 2 completed grandchildren → should collapse whole tree
		{ID: "dep-audit", Step: "依赖审计", Status: "completed", Depth: 0},
		{ID: "d1", Step: "CVE 扫描", Status: "completed", Depth: 1},
		{ID: "d1-1", Step: "直接依赖 CVE", Status: "completed", Depth: 2},
		{ID: "d1-2", Step: "间接依赖 CVE", Status: "completed", Depth: 2},
		{ID: "d2", Step: "许可证审计", Status: "completed", Depth: 1},
		{ID: "report", Step: "综合报告", Status: "pending", Depth: 0},
	}

	snap := SidebarSnapshot{PlanItems: items}
	sidebar := NewSidebarModel()
	sidebar.SetSize(50, 40)
	sidebar.SetSnapshot(snap)

	var sb strings.Builder
	sidebar.renderTodoSection(&sb, 50)
	rendered := sb.String()

	t.Logf("\n=== Collapsed Rendering (width=50) ===\n%s", rendered)

	// auth-review should show (3/3) and NOT show its children
	if !strings.Contains(rendered, "认证授权审计 (3/3)") {
		t.Error("expected auth-review collapsed with (3/3)")
	}
	if strings.Contains(rendered, "OAuth") || strings.Contains(rendered, "JWT") || strings.Contains(rendered, "RBAC") {
		t.Error("expected auth-review children to be hidden")
	}

	// biz-logic should be expanded — children visible
	if !strings.Contains(rendered, "批量操作分析") {
		t.Error("expected biz-logic children to be visible")
	}
	if !strings.Contains(rendered, "竞态条件检查") {
		t.Error("expected 竞态条件检查 to be visible")
	}

	// b2 (竞态条件检查) has all-completed children → collapsed with (2/2)
	if !strings.Contains(rendered, "竞态条件检查 (2/2)") {
		t.Error("expected 竞态条件检查 collapsed with (2/2)")
	}
	if strings.Contains(rendered, "锁竞争分析") || strings.Contains(rendered, "原子操作检查") {
		t.Error("expected b2 children to be hidden")
	}

	// dep-audit should show (4/4) — whole subtree collapsed
	if !strings.Contains(rendered, "依赖审计 (4/4)") {
		t.Error("expected dep-audit collapsed with (4/4)")
	}
	if strings.Contains(rendered, "CVE 扫描") || strings.Contains(rendered, "许可证审计") {
		t.Error("expected dep-audit children to be hidden")
	}

	// 工作流审计 and 综合报告 should be visible (pending)
	if !strings.Contains(rendered, "工作流审计") {
		t.Error("expected 工作流审计 to be visible")
	}
	if !strings.Contains(rendered, "综合报告") {
		t.Error("expected 综合报告 to be visible")
	}
}

// TestBuildSidebarSnapshot_ReplanDuplication reproduces the sidebar duplication
// bug: when the root agent replans and incorporates sub-agent's completed items
// into its own plan, those items appear TWICE — once nested under the parent
// step (via sub-agent plan) and once at depth 0 (in root plan).
func TestBuildSidebarSnapshot_ReplanDuplication(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "code-audit"},
		},
	})

	// 1. Root agent creates initial plan
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "code-audit",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "sast-scan", "step": "SAST 漏洞扫描", "status": "completed"},
				map[string]any{"id": "dataflow", "step": "对 SAST 高价值候选漏洞进行数据流分析", "status": "pending"},
				map[string]any{"id": "report", "step": "综合安全报告", "status": "pending"},
			},
		},
	})

	// 2. Root starts dataflow step
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskItem, AgentName: "code-audit",
		Payload: map[string]any{"id": "dataflow", "status": "in_progress"},
	})

	// 3. Root spawns sub-agent for dataflow analysis
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeToolStart, AgentName: "code-audit",
		Payload: map[string]any{
			"tool_name": "sub_agent", "call_id": "call_00_abc", "is_agent": true,
		},
	})

	// 4. Sub-agent creates its own plan (nested under "dataflow")
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "sub-call_00_abc",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "id-1", "step": "读取全部 10 个 Controller", "status": "pending"},
				map[string]any{"id": "id-2", "step": "逐文件追踪 Controller 数据流", "status": "pending"},
				map[string]any{"id": "id-3", "step": "汇总全部 10 个 Controller 分析结果", "status": "pending"},
			},
		},
	})

	// 5. Sub-agent completes all items
	for _, id := range []string{"id-1", "id-2", "id-3"} {
		m.handleAgentEvent(&react.AgentOutputEvent{
			Type: react.EventTypeTaskItem, AgentName: "sub-call_00_abc",
			Payload: map[string]any{"id": id, "status": "in_progress"},
		})
		m.handleAgentEvent(&react.AgentOutputEvent{
			Type: react.EventTypeTaskItem, AgentName: "sub-call_00_abc",
			Payload: map[string]any{"id": id, "status": "completed"},
		})
	}

	// 6. Root agent REPLANS — LLM incorporates sub-agent's completed items
	//    into the root plan (this is the scenario that causes duplication)
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "code-audit",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "sast-scan", "step": "SAST 漏洞扫描", "status": "completed"},
				map[string]any{"id": "dataflow", "step": "对 SAST 高价值候选漏洞进行数据流分析", "status": "completed"},
				// LLM copies sub-agent items into root plan:
				map[string]any{"id": "read-ctrl", "step": "读取全部 10 个 Controller", "status": "completed"},
				map[string]any{"id": "trace-ctrl", "step": "逐文件追踪 Controller 数据流", "status": "completed"},
				map[string]any{"id": "summary-ctrl", "step": "汇总全部 10 个 Controller 分析结果", "status": "completed"},
				map[string]any{"id": "final-report", "step": "综合安全报告", "status": "pending"},
			},
		},
	})

	snap := m.buildSidebarSnapshot()

	// Log what we see
	t.Logf("\n=== Sidebar PlanItems (total=%d) ===", len(snap.PlanItems))
	stepCounts := map[string]int{}
	for i, item := range snap.PlanItems {
		indent := strings.Repeat("  ", item.Depth+1)
		icon := "○"
		switch item.Status {
		case "completed":
			icon = "✓"
		case "in_progress":
			icon = "▸"
		}
		t.Logf("%s%s %s (id=%s, depth=%d)", indent, icon, item.Step, item.ID, item.Depth)
		stepCounts[item.Step]++
		_ = i
	}

	// Check for duplicated step text
	hasDuplicates := false
	for step, count := range stepCounts {
		if count > 1 {
			t.Errorf("DUPLICATE: step %q appears %d times", step, count)
			hasDuplicates = true
		}
	}
	if hasDuplicates {
		t.Logf("\n>>> BUG REPRODUCED: sub-agent items appear both nested (depth=1) and in root plan (depth=0)")
	}

	// Render the sidebar for visual inspection
	sidebar := NewSidebarModel()
	sidebar.SetSize(60, 40)
	sidebar.SetSnapshot(snap)
	var sb strings.Builder
	sidebar.renderTodoSection(&sb, 60)
	t.Logf("\n=== Rendered Sidebar ===\n%s", sb.String())
}

// TestBuildSidebarSnapshot_ReplanDuplication_Rephrased verifies that dedup
// works even when the root plan copies sub-agent items with whitespace or
// casing differences.
func TestBuildSidebarSnapshot_ReplanDuplication_Rephrased(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "code-audit"},
		},
	})

	// 1. Root agent creates initial plan
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "code-audit",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "scan", "step": "SAST 漏洞扫描", "status": "completed"},
				map[string]any{"id": "dataflow", "step": "数据流分析", "status": "pending"},
				map[string]any{"id": "report", "step": "安全报告", "status": "pending"},
			},
		},
	})

	// 2. Spawn sub-agent for dataflow
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskItem, AgentName: "code-audit",
		Payload: map[string]any{"id": "dataflow", "status": "in_progress"},
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeToolStart, AgentName: "code-audit",
		Payload: map[string]any{
			"tool_name": "sub_agent", "call_id": "call_01_xyz", "is_agent": true,
		},
	})

	// 3. Sub-agent creates plan with exact text
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "sub-call_01_xyz",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "s1", "step": "读取 Controller 源码", "status": "pending"},
				map[string]any{"id": "s2", "step": "追踪数据流路径", "status": "pending"},
			},
		},
	})

	// 4. Sub-agent completes
	for _, id := range []string{"s1", "s2"} {
		m.handleAgentEvent(&react.AgentOutputEvent{
			Type: react.EventTypeTaskItem, AgentName: "sub-call_01_xyz",
			Payload: map[string]any{"id": id, "status": "completed"},
		})
	}

	// 5. Root replans with REPHRASED text (extra spaces, different casing)
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "code-audit",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "scan", "step": "SAST 漏洞扫描", "status": "completed"},
				map[string]any{"id": "dataflow", "step": "数据流分析", "status": "completed"},
				// Rephrased copies: extra spaces and different casing
				map[string]any{"id": "r1", "step": "读取  Controller  源码", "status": "completed"},
				map[string]any{"id": "r2", "step": "追踪数据流路径", "status": "completed"},
				map[string]any{"id": "report", "step": "安全报告", "status": "pending"},
			},
		},
	})

	snap := m.buildSidebarSnapshot()

	stepCounts := map[string]int{}
	for _, item := range snap.PlanItems {
		norm := strings.ToLower(strings.Join(strings.Fields(item.Step), " "))
		stepCounts[norm]++
	}

	for step, count := range stepCounts {
		if count > 1 {
			t.Errorf("DUPLICATE: normalized step %q appears %d times", step, count)
		}
	}

	// Verify root-only items are still present
	found := map[string]bool{}
	for _, item := range snap.PlanItems {
		if item.Depth == 0 {
			found[item.Step] = true
		}
	}
	if !found["SAST 漏洞扫描"] {
		t.Error("expected root item 'SAST 漏洞扫描' to be present")
	}
	if !found["安全报告"] {
		t.Error("expected root item '安全报告' to be present")
	}
}

// TestSubAgentPendingItemsPersistAfterRootCompletes verifies the hypothesis:
// when root agent's plan is fully completed but a sub-agent terminated early
// with pending steps, those pending steps still appear in the sidebar.
// This is the most likely cause of the "6 pending steps after session ends" bug.
func TestSubAgentPendingItemsPersistAfterRootCompletes(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{Name: "pentest-agent"},
		},
	})

	// 1. Root plan — all 7 steps completed (matches the real session)
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "pentest-agent",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "recon", "step": "全局侦察与信息收集", "status": "completed"},
				map[string]any{"id": "vuln-scan", "step": "SAST 漏洞扫描", "status": "completed"},
				map[string]any{"id": "auth-review", "step": "认证授权审计", "status": "completed"},
				map[string]any{"id": "biz-logic", "step": "业务逻辑审计", "status": "completed"},
				map[string]any{"id": "dep-audit", "step": "依赖安全审计", "status": "completed"},
				map[string]any{"id": "dataflow", "step": "数据流分析", "status": "completed"},
				map[string]any{"id": "summary", "step": "汇总所有发现与证据链", "status": "completed"},
			},
		},
	})

	// 2. Set auth-review as in_progress (to parent the sub-agent)
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskItem,
		AgentName: "pentest-agent",
		Payload:   map[string]any{"id": "auth-review", "status": "in_progress"},
	})

	// 3. Tool start: sub-agent spawned for credential acquisition
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeToolStart, AgentName: "pentest-agent",
		Payload: map[string]any{
			"tool_name": "sub_agent", "call_id": "call_cred", "is_agent": true,
		},
	})

	// 4. Sub-agent creates its own plan with 6 credential steps
	//    First step completed, rest pending — sub-agent terminated early
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeTaskPlan, AgentName: "sub-call_cred",
		Payload: map[string]any{
			"plan": []any{
				map[string]any{"id": "install-deps", "step": "安装环境依赖", "status": "pending"},
				map[string]any{"id": "captcha-script", "step": "编写验证码识别脚本", "status": "pending"},
				map[string]any{"id": "try-passwords", "step": "尝试弱密码登录", "status": "pending"},
				map[string]any{"id": "hashcat", "step": "hashcat 破解密码哈希", "status": "pending"},
				map[string]any{"id": "get-jwt", "step": "获取 JWT token", "status": "pending"},
				map[string]any{"id": "save-token", "step": "保存 token 到工作区", "status": "pending"},
			},
		},
	})

	// 5. Sub-agent tool call completes (sub-agent returned early without
	//    finishing all steps — model decided complete or errored out)
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type: react.EventTypeToolEnd, AgentName: "pentest-agent",
		Payload: map[string]any{
			"tool_name": "sub_agent", "call_id": "call_cred",
			"result": "认证测试完成，未获取到有效凭证",
		},
	})

	// 6. Root continues and marks auth-review as completed
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeTaskItem,
		AgentName: "pentest-agent",
		Payload:   map[string]any{"id": "auth-review", "status": "completed"},
	})

	// --- Build snapshot ---
	snap := m.buildSidebarSnapshot()

	t.Logf("\n=== Sidebar PlanItems (total=%d) ===", len(snap.PlanItems))
	for i, item := range snap.PlanItems {
		icon := "○"
		switch item.Status {
		case "completed":
			icon = "✓"
		case "in_progress":
			icon = "▸"
		}
		indent := strings.Repeat("  ", item.Depth+1)
		t.Logf("%s%s %s (id=%s, depth=%d, status=%s)", indent, icon, item.Step, item.ID, item.Depth, item.Status)
		_ = i
	}

	// --- Key assertions ---
	// Root plan has 7 completed steps at depth 0
	rootCompleted := 0
	for _, item := range snap.PlanItems {
		if item.Depth == 0 && item.Status == "completed" {
			rootCompleted++
		}
	}
	if rootCompleted != 7 {
		t.Fatalf("expected 7 completed root steps, got %d", rootCompleted)
	}

	// Sub-agent's pending steps appear at depth 1
	subPending := 0
	subPendingIDs := []string{}
	for _, item := range snap.PlanItems {
		if item.Depth == 1 && item.Status == "pending" {
			subPending++
			subPendingIDs = append(subPendingIDs, item.ID)
		}
	}

	// THIS is the diagnosis: if subPending > 0, the sidebar shows sub-agent
	// pending items even though the root plan is fully completed.
	// This explains the user's screenshot.
	t.Logf("sub-agent pending items visible in sidebar: %d — IDs: %v", subPending, subPendingIDs)
	if subPending == 0 {
		t.Fatal("expected sub-agent pending items to persist in sidebar — hypothesis invalidated")
	}

	expectedPendingIDs := []string{"install-deps", "captcha-script", "try-passwords", "hashcat", "get-jwt", "save-token"}
	if len(subPendingIDs) != len(expectedPendingIDs) {
		t.Fatalf("expected %d sub-agent pending items, got %d: %v", len(expectedPendingIDs), len(subPendingIDs), subPendingIDs)
	}

	t.Log("CONFIRMED: sub-agent pending items persist in sidebar after root plan completes.")
	t.Log("Root scheduler only checks its own plan → enters FinalAnswer.")
	t.Log("Sidebar flattenPlan merges root + sub-agent plans → pending items visible to user.")
}
