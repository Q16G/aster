package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFlushThinking_AllowsAdjacentIdentical(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendThinking("thinking about X")
	if !m.FlushThinking() {
		t.Fatal("first FlushThinking should return true")
	}

	m.AppendThinking("thinking about X")
	if !m.FlushThinking() {
		t.Fatal("second FlushThinking with identical content should return true")
	}

	count := 0
	for _, p := range m.parts {
		if p.Type == PartTypeThinking {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 thinking parts, got %d", count)
	}
}

func TestFlushThinking_AllowsDifferentContent(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendThinking("first thought")
	if !m.FlushThinking() {
		t.Fatal("first FlushThinking should return true")
	}

	m.AppendThinking("second thought")
	if !m.FlushThinking() {
		t.Fatal("second FlushThinking with different content should return true")
	}

	count := 0
	for _, p := range m.parts {
		if p.Type == PartTypeThinking {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 thinking parts, got %d", count)
	}
}

func TestFlushThinking_EmptyBuffer(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	if m.FlushThinking() {
		t.Fatal("FlushThinking on empty buffer should return false")
	}
	if len(m.parts) != 0 {
		t.Fatalf("expected 0 parts, got %d", len(m.parts))
	}
}

func TestFlushThinking_MergesSameGroupID(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendThinkingWithGroupID("first ", "group-1")
	m.FlushThinking()
	m.AppendThinkingWithGroupID("second", "group-1")
	m.FlushThinking()

	count := 0
	for _, p := range m.parts {
		if p.Type == PartTypeThinking {
			count++
			if p.Thinking.Content != "first second" {
				t.Fatalf("expected merged content 'first second', got %q", p.Thinking.Content)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 merged thinking part, got %d", count)
	}
}

func TestFlushThinking_SeparatesDifferentGroupIDs(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendThinkingWithGroupID("thought A", "group-1")
	m.FlushThinking()
	m.AppendThinkingWithGroupID("thought B", "group-2")
	m.FlushThinking()

	count := 0
	for _, p := range m.parts {
		if p.Type == PartTypeThinking {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 separate thinking parts, got %d", count)
	}
}

func TestAppendThinkingWithGroupID_SessionSwitch(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendThinkingWithGroupID("old ", "group-1")
	m.AppendThinkingWithGroupID("new", "group-2")

	count := 0
	for _, p := range m.parts {
		if p.Type == PartTypeThinking {
			count++
			if p.Thinking.Content != "old " {
				t.Fatalf("expected flushed content 'old ', got %q", p.Thinking.Content)
			}
			if p.Thinking.GroupID != "group-1" {
				t.Fatalf("expected group ID 'group-1', got %q", p.Thinking.GroupID)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 flushed part from old session, got %d", count)
	}
	if !m.isThinking {
		t.Fatal("should still be thinking with new session")
	}
}

func TestAppendThinkingWithGroupID_ResumeAfterFlush(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendThinkingWithGroupID("part1 ", "group-1")
	m.FlushThinking()

	m.AppendThinkingWithGroupID("part2", "group-1")

	thinkingParts := 0
	for _, p := range m.parts {
		if p.Type == PartTypeThinking {
			thinkingParts++
			if p.Thinking.Content != "part1 part2" {
				t.Fatalf("expected 'part1 part2', got %q", p.Thinking.Content)
			}
		}
	}
	if thinkingParts != 1 {
		t.Fatalf("expected 1 thinking part (resumed via existing), got %d", thinkingParts)
	}
}

func TestFlushRender_DoesNotSnapBackAfterUserScrollsUp(t *testing.T) {
	m := newScrollableChatModel(t)
	if !m.viewport.AtBottom() {
		t.Fatal("expected seeded chat to start at bottom")
	}

	m = scrollChatUpUntilNotBottom(t, m)
	scrolledOffset := m.viewport.YOffset
	if m.autoFollowBottom {
		t.Fatal("expected auto-follow to be disabled after scrolling away from bottom")
	}

	m.AppendStream("more streaming output")
	if !m.FlushRender() {
		t.Fatal("expected FlushRender to render pending stream content")
	}

	if m.viewport.YOffset != scrolledOffset {
		t.Fatalf("expected viewport offset to stay at %d after render, got %d", scrolledOffset, m.viewport.YOffset)
	}
	if m.viewport.AtBottom() {
		t.Fatal("expected viewport to remain away from bottom after render")
	}
	if m.autoFollowBottom {
		t.Fatal("expected auto-follow to remain disabled after render")
	}
}

func TestFlushRender_ResumesAutoFollowAfterReturningToBottom(t *testing.T) {
	m := newScrollableChatModel(t)
	m = scrollChatUpUntilNotBottom(t, m)

	m.viewport.GotoBottom()
	m.syncAutoFollowFromViewport()
	if !m.viewport.AtBottom() {
		t.Fatal("expected viewport to be at bottom before follow-up render")
	}
	if !m.autoFollowBottom {
		t.Fatal("expected auto-follow to be re-enabled at bottom")
	}

	m.AppendThinking("fresh thought at bottom")
	if !m.FlushRender() {
		t.Fatal("expected FlushRender to render pending thinking content")
	}

	if !m.viewport.AtBottom() {
		t.Fatal("expected viewport to remain at bottom after render")
	}
	if !m.autoFollowBottom {
		t.Fatal("expected auto-follow to stay enabled after render")
	}
}

func TestRenderPlanPart_SubAgentShowsTag(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.rootAgentName = "my-agent"

	m.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "my-agent", Items: []PlanItemView{{Step: "root-step", Status: "pending"}}},
	})
	m.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "sub-abc", Items: []PlanItemView{{Step: "sub-step", Status: "pending"}}},
	})

	rootRender := m.renderPlanPart(0, m.parts[0], 80)
	subRender := m.renderPlanPart(1, m.parts[1], 80)

	if strings.Contains(rootRender, "(my-agent)") {
		t.Fatalf("root plan should NOT show agent tag, got %q", rootRender)
	}
	if !strings.Contains(subRender, "(sub-abc)") {
		t.Fatalf("sub-agent plan should show agent tag, got %q", subRender)
	}
}

func TestUpdateLastPlanForAgent_MatchesLegacyEmptyAgentName(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.rootAgentName = "code-audit"

	m.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{Items: []PlanItemView{{ID: "1", Step: "legacy-step", Status: "pending"}}},
	})

	m.UpdateLastPlanForAgent("code-audit", func(p *PlanPart) {
		p.Items[0].Status = "done"
	})

	if m.parts[0].Plan.Items[0].Status != "done" {
		t.Fatalf("expected legacy plan (empty AgentName) to be updated by root agent, got %q", m.parts[0].Plan.Items[0].Status)
	}
}

func TestUpdateLastPlanForAgent_SubAgentDoesNotMatchLegacyPlan(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.rootAgentName = "code-audit"

	m.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{Items: []PlanItemView{{ID: "1", Step: "legacy-step", Status: "pending"}}},
	})

	called := false
	m.UpdateLastPlanForAgent("sub-abc", func(p *PlanPart) {
		called = true
	})

	if called {
		t.Fatal("sub-agent should NOT match legacy plan with empty AgentName")
	}
}

func TestUpdateLastPlanForAgent_OnlyMatchesSameAgent(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "root", Items: []PlanItemView{{ID: "1", Step: "step1", Status: "pending"}}},
	})
	m.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "sub-abc12345", Items: []PlanItemView{{ID: "2", Step: "sub-step", Status: "pending"}}},
	})

	m.UpdateLastPlanForAgent("root", func(p *PlanPart) {
		p.Items[0].Status = "done"
	})

	parts := m.Parts()
	for _, p := range parts {
		if p.Type == PartTypePlan && p.Plan.AgentName == "root" {
			if p.Plan.Items[0].Status != "done" {
				t.Fatalf("expected root plan item to be 'done', got %q", p.Plan.Items[0].Status)
			}
		}
		if p.Type == PartTypePlan && p.Plan.AgentName == "sub-abc12345" {
			if p.Plan.Items[0].Status != "pending" {
				t.Fatalf("expected sub-agent plan item to remain 'pending', got %q", p.Plan.Items[0].Status)
			}
		}
	}
}

func TestUpdateLastPlanForAgent_NoMatchDoesNothing(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AddPart(DisplayPart{
		Type: PartTypePlan,
		Plan: &PlanPart{AgentName: "root", Items: []PlanItemView{{ID: "1", Step: "step1", Status: "pending"}}},
	})

	called := false
	m.UpdateLastPlanForAgent("sub-nonexistent", func(p *PlanPart) {
		called = true
	})

	if called {
		t.Fatal("expected callback not to be called for non-matching agent")
	}
}

func newScrollableChatModel(t *testing.T) ChatModel {
	t.Helper()

	m := NewChatModel()
	m.SetSize(40, 6)
	for i := 0; i < 16; i++ {
		m.AddPart(DisplayPart{
			Type: PartTypeText,
			Text: &TextPart{
				Content: fmt.Sprintf("assistant line %02d with enough content to keep the viewport scrollable", i),
			},
		})
	}
	return m
}

func scrollChatUpUntilNotBottom(t *testing.T, m ChatModel) ChatModel {
	t.Helper()

	for i := 0; i < 32 && m.viewport.AtBottom(); i++ {
		var cmd tea.Cmd
		m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyUp})
		if cmd != nil {
			t.Fatal("expected no async command from chat update")
		}
	}
	if m.viewport.AtBottom() {
		t.Fatal("expected viewport to leave bottom after scrolling up")
	}
	return m
}
