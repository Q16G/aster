package tui

import (
	"strings"
	"testing"
	"time"
)

// SetSnapshot stores items in given order and clamps the cursor when the list
// shrinks so a finished sub-agent never leaves a dangling selection.
func TestSubAgentPanelSnapshotAndCursorClamp(t *testing.T) {
	p := NewSubAgentPanel()
	p.SetSnapshot([]subAgentPanelItem{
		{CallID: "call_a", Title: "agent A", Status: "running", Running: true},
		{CallID: "call_b", Title: "agent B", Status: "done"},
		{CallID: "call_c", Title: "agent C", Status: "done"},
	})
	if p.Count() != 3 {
		t.Fatalf("Count = %d, want 3", p.Count())
	}

	p.MoveDown()
	p.MoveDown()
	if it, ok := p.Selected(); !ok || it.CallID != "call_c" {
		t.Fatalf("Selected after two MoveDown = %+v ok=%v, want call_c", it, ok)
	}

	// Shrink the list: cursor (2) must clamp to the new last index (1).
	p.SetSnapshot([]subAgentPanelItem{
		{CallID: "call_a", Title: "agent A", Status: "done"},
		{CallID: "call_b", Title: "agent B", Status: "done"},
	})
	if it, ok := p.Selected(); !ok || it.CallID != "call_b" {
		t.Fatalf("Selected after shrink = %+v ok=%v, want call_b", it, ok)
	}
}

// The panel scope is the current turn, not transient "running": once a turn
// spawns a sub-agent the panel stays visible after it settles (matching the Todo
// nesting), and the cards list includes terminal ones. A new user turn drops the
// previous turn's cards.
func TestSubAgentPanelScopedToTurn(t *testing.T) {
	m := NewChatModel()
	m.AddPart(DisplayPart{Type: PartTypeUser, User: &UserPart{Content: "turn 1"}})
	m.AddPart(DisplayPart{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_a", Status: "running"}})

	if !m.HasSubAgentsThisTurn() {
		t.Fatal("running sub-agent should make the panel visible")
	}
	if got := len(m.SubAgentCardsThisTurn()); got != 1 {
		t.Fatalf("cards this turn = %d, want 1", got)
	}

	// The sub-agent finishes: panel must persist (unlike the old running-only gate).
	m.UpdateSubAgentByCallID("call_a", func(sa *SubAgentPart) { sa.Status = "completed" })
	if !m.HasSubAgentsThisTurn() {
		t.Fatal("finished sub-agent must keep the panel visible for the turn")
	}
	cards := m.SubAgentCardsThisTurn()
	if len(cards) != 1 || cards[0].Status != "completed" {
		t.Fatalf("terminal card not retained: %+v", cards)
	}

	// A new user turn drops the previous turn's card.
	m.AddPart(DisplayPart{Type: PartTypeUser, User: &UserPart{Content: "turn 2"}})
	if m.HasSubAgentsThisTurn() {
		t.Fatal("new turn must clear the previous turn's sub-agent cards")
	}
	if got := len(m.SubAgentCardsThisTurn()); got != 0 {
		t.Fatalf("cards after new turn = %d, want 0", got)
	}
}

// MoveUp/MoveDown must stay within bounds and never wrap.
func TestSubAgentPanelMoveBounds(t *testing.T) {
	p := NewSubAgentPanel()
	p.SetSnapshot([]subAgentPanelItem{
		{CallID: "call_a", Title: "A"},
		{CallID: "call_b", Title: "B"},
	})

	p.MoveUp() // already at top, no-op
	if it, _ := p.Selected(); it.CallID != "call_a" {
		t.Fatalf("MoveUp at top moved selection to %q", it.CallID)
	}

	p.MoveDown()
	p.MoveDown() // past end, clamps at last
	if it, _ := p.Selected(); it.CallID != "call_b" {
		t.Fatalf("MoveDown past end = %q, want call_b", it.CallID)
	}
}

// Selected returns ok=false for an empty panel; View renders without panicking
// for both empty and populated states.
func TestSubAgentPanelSelectedEmptyAndView(t *testing.T) {
	p := NewSubAgentPanel()
	p.SetSize(subAgentPanelWidth, 20)
	if _, ok := p.Selected(); ok {
		t.Fatal("Selected on empty panel should return ok=false")
	}
	if got := p.View(); !strings.Contains(got, "子 Agent") {
		t.Fatalf("empty View missing header: %q", got)
	}

	p.SetSnapshot([]subAgentPanelItem{
		{CallID: "call_a", Title: "agent A", Description: "scan repo", Status: "running", Elapsed: 3 * time.Second, Running: true},
	})
	p.SetFocused(true)
	out := p.View()
	if !strings.Contains(out, "agent A") {
		t.Fatalf("View missing title: %q", out)
	}
}
