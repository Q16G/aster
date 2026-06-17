package tui

import (
	"testing"
	"time"

	tuicontext "aster/internal/tui/context"
)

// TestSubAgentPanelLazyRefresh_ViewDoesNotMutate is the F3 regression.
// Model.View() is a value-receiver: the d6e1ff15 attempt at "View 内惰性"
// (compare PartsStore.SubAgentVersion() vs m.subAgentPanelVer, refresh and
// write the new value back) silently lost the write because it landed on a
// stack-frame copy of m. Result: every frame saw a mismatch, every frame
// refreshed the panel, every frame wasted work — the opposite of the intent.
//
// Fix: pull the lazy refresh entirely out of View and keep it in Update
// (renderTickMsg / updateLayout). View's job is now read-only.
//
// This test exercises the contract directly: with the store's
// SubAgentVersion already ahead of subAgentPanelVer, repeated View() calls
// must NOT trigger refreshSubAgentPanel. Only an Update path may.
func TestSubAgentPanelLazyRefresh_ViewDoesNotMutate(t *testing.T) {
	m := NewModel(ModelDeps{SyncStore: tuicontext.NewSyncStore()})
	m.width = 120
	m.height = 30

	// Seed a UserPart + a SubAgent to make the panel visible. AddPart bumps
	// SubAgentVersion twice (UserPart resets-this-turn; SubAgent adds one).
	m.chat.AddPart(DisplayPart{
		Type: PartTypeUser,
		Time: time.Now(),
		User: &UserPart{Content: "do it"},
	})
	m.chat.AddPart(DisplayPart{
		Type: PartTypeSubAgent,
		Time: time.Now(),
		SubAgent: &SubAgentPart{
			AgentName:   "sub_agent",
			CallID:      "child-X",
			Status:      "running",
			Description: "child task",
			StartedAt:   time.Now(),
		},
	})
	m.updateLayout()

	if !m.subAgentPanelVisible() {
		t.Fatal("panel should be visible after seeding a sub-agent this turn")
	}

	// updateLayout calls refreshSubAgentPanel once. Reset the counter so we
	// only measure View()-driven refreshes from here on.
	base := m.subAgentPanelRefreshes

	// Force the version-skew condition that V老 was supposed to catch in View.
	// Set subAgentPanelVer back to 0 so cur != m.subAgentPanelVer is true on
	// every following View() call.
	m.subAgentPanelVer = 0

	// Render N times with no intervening Update. View() must stay read-only.
	const N = 10
	for i := 0; i < N; i++ {
		_ = m.View()
	}

	if got := m.subAgentPanelRefreshes - base; got != 0 {
		t.Fatalf("View() N=%d times triggered %d refreshSubAgentPanel calls; want 0 (View must be read-only)", N, got)
	}
	if m.subAgentPanelVer != 0 {
		t.Fatalf("subAgentPanelVer must not be mutated by View(); got %d, want 0 (set by test)", m.subAgentPanelVer)
	}
}
