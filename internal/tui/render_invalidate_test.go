package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestExpandToggle_InvalidatesFragment is the F2 regression: flipping
// toolExpanded must MarkDirty the part so the Renderer's fragment cache
// (keyed by id, version, width) gets evicted on the next refreshContent.
// Before the fix, refreshContent saw the same (id, version, width) and
// returned the cached fragment, so Enter/Space on StepResult/Plan/etc.
// produced no visual change.
func TestExpandToggle_InvalidatesFragment(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	// StepResult is auto-expanded by AddPart. Put it at cursor 0.
	m.AddPart(DisplayPart{
		Type: PartTypeStepResult,
		StepResult: &StepResultPart{
			StepName:      "discover endpoints",
			Status:        "success",
			DisplayResult: "found /api/login /api/logout and three internal admin routes",
		},
	})
	if m.cursor != 0 {
		t.Fatalf("cursor: want 0, got %d", m.cursor)
	}
	if !m.toolExpanded[0] {
		t.Fatal("StepResult should be auto-expanded after AddPart")
	}

	expanded := m.fullContent
	if expanded == "" {
		t.Fatal("expanded fullContent must not be empty")
	}

	// Collapse via the same key path the user takes: Enter on the cursor.
	// This exercises the toggle through chat.Update so any missing dirty
	// mark in the production path would surface here, not only in unit
	// tests of the helper.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	collapsed := m.fullContent

	if expanded == collapsed {
		t.Fatalf("toggling toolExpanded must change rendered content; both renders are identical:\n%s", expanded)
	}

	// The expanded form embeds the full DisplayResult; the collapsed form
	// truncates it. Either way the line-count signature differs.
	expandedLines := strings.Count(expanded, "\n")
	collapsedLines := strings.Count(collapsed, "\n")
	if expandedLines == collapsedLines {
		t.Fatalf("expanded vs collapsed line counts must differ; both =%d", expandedLines)
	}

	// Re-expand via the same key path. Given identical inputs the
	// rendering must restore byte-for-byte to the original expanded form.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.fullContent != expanded {
		t.Fatal("re-expanding must restore the original expanded rendering")
	}
}

// TestSetParts_ResetsRendererCache is the F4 regression: SetParts dense
// back-fills IDs from 1..N when loading a session. Switching from a session
// whose part #1 was, say, a UserPart "alice" to a different session whose
// part #1 is UserPart "bob" must NOT serve the cached "alice" fragment for
// id=1 just because (id=1, version=1, width=W) matches. The fix resets the
// renderer cache on every SetParts.
func TestSetParts_ResetsRendererCache(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AddPart(DisplayPart{Type: PartTypeUser, User: &UserPart{Content: "alice-session-marker"}})
	first := m.fullContent
	if !strings.Contains(first, "alice-session-marker") {
		t.Fatalf("first render must contain alice-session-marker, got:\n%s", first)
	}

	// Load a different "session": completely disjoint content. SetParts'
	// dense back-fill will assign these IDs starting at 1 as well, which is
	// exactly the cache-collision scenario.
	m.SetParts([]DisplayPart{
		{Type: PartTypeUser, User: &UserPart{Content: "bob-session-marker"}},
	})
	second := m.fullContent

	if strings.Contains(second, "alice-session-marker") {
		t.Fatalf("post-SetParts render must not contain the previous session's content; got:\n%s", second)
	}
	if !strings.Contains(second, "bob-session-marker") {
		t.Fatalf("post-SetParts render must contain bob-session-marker, got:\n%s", second)
	}
}

// TestRefreshContent_Incremental_AppendsThenUpdatesTool exercises the
// incremental render path covered by the M1 testing gap. It verifies two
// things on a single ChatModel instance:
//  1. Appending a new part beyond the current end injects exactly its
//     fragment without disturbing the previously-cached fragments.
//  2. Mutating an existing ToolPart via UpdateToolByCallID invalidates that
//     part's cached fragment so the new state surfaces in fullContent.
func TestRefreshContent_Incremental_AppendsThenUpdatesTool(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	for i := 0; i < 5; i++ {
		m.AddPart(DisplayPart{
			Type: PartTypeUser,
			User: &UserPart{Content: "u-" + string(rune('A'+i))},
		})
	}
	before := m.fullContent
	if !strings.Contains(before, "u-A") || !strings.Contains(before, "u-E") {
		t.Fatalf("before-content missing seeded markers: %s", before)
	}

	// Add a Tool part in running state — its rendered fragment will say
	// "running" / lack a result body.
	m.AddPart(DisplayPart{
		Type: PartTypeTool,
		Tool: &ToolPart{
			Name:   "bash",
			CallID: "tool-Z",
			State:  "running",
		},
	})
	mid := m.fullContent
	if mid == before {
		t.Fatal("AppendPart(ToolPart) must change rendered fullContent")
	}
	// Earlier fragments should still appear (cache must not blow away unrelated entries).
	if !strings.Contains(mid, "u-A") || !strings.Contains(mid, "u-E") {
		t.Fatalf("incremental append must preserve unrelated parts: %s", mid)
	}

	// While running, the rendered fragment carries a "running..." sentinel.
	if !strings.Contains(mid, "running") {
		t.Fatalf("running tool fragment must contain 'running...', got:\n%s", mid)
	}

	// Mutate the tool via the callID path: the only way the new state
	// reaches the renderer is via the dirty set + version bump that
	// UpdateToolByCallID drives.
	m.UpdateToolByCallID("tool-Z", func(t *ToolPart) {
		t.State = "error"
		t.Error = "PERMISSION_DENIED_MARKER"
	})
	after := m.fullContent
	if after == mid {
		t.Fatal("UpdateToolByCallID must trigger a re-render of the touched part")
	}
	if strings.Contains(after, "running...") {
		t.Fatalf("post-update content must no longer carry the running sentinel:\n%s", after)
	}
	if !strings.Contains(after, "PERMISSION_DENIED_MARKER") {
		t.Fatalf("post-update content must contain the new Tool.Error marker, got:\n%s", after)
	}
}
