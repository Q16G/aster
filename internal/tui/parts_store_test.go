package tui

import (
	"testing"
	"time"
)

// TestStore_Append_AssignsID verifies that Append mints monotonically-
// increasing IDs from 1 when the caller does not pre-assign one. Two appends
// should land at ID=1 and ID=2.
func TestStore_Append_AssignsID(t *testing.T) {
	s := NewPartsStore()
	id1 := s.Append(DisplayPart{Type: PartTypeUser, User: &UserPart{Content: "hi"}})
	id2 := s.Append(DisplayPart{Type: PartTypeText, Text: &TextPart{Content: "hello"}})
	if id1 != 1 {
		t.Fatalf("first Append: want ID=1, got %d", id1)
	}
	if id2 != 2 {
		t.Fatalf("second Append: want ID=2, got %d", id2)
	}
	if got := s.At(0).ID; got != 1 {
		t.Fatalf("parts[0].ID: want 1, got %d", got)
	}
	if got := s.At(1).ID; got != 2 {
		t.Fatalf("parts[1].ID: want 2, got %d", got)
	}
}

// TestStore_Append_MaintainsIdxByCallID_ForTool verifies that appending a Tool
// part registers the (Tool, callID) key so UpdateToolByCallID hits it.
func TestStore_Append_MaintainsIdxByCallID_ForTool(t *testing.T) {
	s := NewPartsStore()
	s.Append(DisplayPart{
		Type: PartTypeTool,
		Tool: &ToolPart{Name: "bash", CallID: "X", State: "running"},
	})

	idx, ok := s.IndexByToolCallID("X")
	if !ok || idx != 0 {
		t.Fatalf("IndexByToolCallID(X): want (0, true), got (%d, %v)", idx, ok)
	}
	if _, ok := s.IndexBySubAgentCallID("X"); ok {
		t.Fatal("IndexBySubAgentCallID(X) must miss: no SubAgent appended")
	}
}

// TestStore_Append_MaintainsIdxByCallID_ForSubAgent verifies SubAgent
// indexing is symmetric to the Tool path.
func TestStore_Append_MaintainsIdxByCallID_ForSubAgent(t *testing.T) {
	s := NewPartsStore()
	s.Append(DisplayPart{
		Type:     PartTypeSubAgent,
		SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "Y", Status: "running"},
	})

	idx, ok := s.IndexBySubAgentCallID("Y")
	if !ok || idx != 0 {
		t.Fatalf("IndexBySubAgentCallID(Y): want (0, true), got (%d, %v)", idx, ok)
	}
	if _, ok := s.IndexByToolCallID("Y"); ok {
		t.Fatal("IndexByToolCallID(Y) must miss: no Tool appended")
	}
}

// TestStore_UpdateToolByCallID_AfterSubAgentAppended is the F1 regression:
// when an is_agent tool start emits both a Tool and a SubAgent that share the
// same call_id, the legacy single-string index would have the SubAgent clobber
// the Tool, and the subsequent tool_end's UpdateToolByCallID would silently
// miss. After the composite-key fix, both lookups must hit their respective
// parts.
func TestStore_UpdateToolByCallID_AfterSubAgentAppended(t *testing.T) {
	s := NewPartsStore()
	s.Append(DisplayPart{
		Type: PartTypeTool,
		Tool: &ToolPart{Name: "sub_agent", CallID: "X", State: "running", IsAgent: true},
	})
	s.Append(DisplayPart{
		Type:     PartTypeSubAgent,
		SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "X", Status: "running"},
	})

	toolHit := s.UpdateToolByCallID("X", func(t *ToolPart) {
		t.State = "completed"
		t.Result = "ok"
	})
	if !toolHit {
		t.Fatal("UpdateToolByCallID must hit Tool even when SubAgent shares the CallID")
	}
	if got := s.At(0).Tool.State; got != "completed" {
		t.Fatalf("Tool.State: want completed, got %q", got)
	}
	if got := s.At(0).Tool.Result; got != "ok" {
		t.Fatalf("Tool.Result: want ok, got %q", got)
	}

	subIdx := s.UpdateSubAgentByCallID("X", func(sa *SubAgentPart) {
		sa.Status = "completed"
		sa.Summary = "done"
	})
	if subIdx < 0 {
		t.Fatal("UpdateSubAgentByCallID must hit SubAgent that shares the CallID")
	}
	if got := s.At(subIdx).SubAgent.Status; got != "completed" {
		t.Fatalf("SubAgent.Status: want completed, got %q", got)
	}
}

// TestStore_AppendThinkingDelta_ResumeAfterFlush verifies that a second
// AppendThinkingDelta with the same (agent, groupID) extends the existing
// part instead of creating a new one, even after a manual Append broke the
// streaming. This guards the in-flight thinking aggregation contract.
func TestStore_AppendThinkingDelta_ResumeAfterFlush(t *testing.T) {
	s := NewPartsStore()
	now := time.Now()

	id1, created1 := s.AppendThinkingDelta("root", "g1", "hello ", now)
	if !created1 {
		t.Fatal("first AppendThinkingDelta should report created=true")
	}
	id2, created2 := s.AppendThinkingDelta("root", "g1", "world", now)
	if created2 {
		t.Fatal("second AppendThinkingDelta for same (agent, group) should extend, not create")
	}
	if id1 != id2 {
		t.Fatalf("extended thinking part should reuse ID: id1=%d id2=%d", id1, id2)
	}
	if s.Len() != 1 {
		t.Fatalf("Len: want 1 thinking part, got %d", s.Len())
	}
	if got := s.At(0).Thinking.Content; got != "hello world" {
		t.Fatalf("Thinking.Content: want %q, got %q", "hello world", got)
	}
	if s.At(0).Version < 2 {
		t.Fatalf("Version: extending delta must bump version, got %d", s.At(0).Version)
	}
}

// TestStore_PartsForChild_GroupsCorrectly verifies the child-attribution
// index returns exactly the parts belonging to a sub-agent's call_id.
func TestStore_PartsForChild_GroupsCorrectly(t *testing.T) {
	s := NewPartsStore()
	// Parent registers spawn metadata as the launcher tool would.
	s.SetSpawnByCallID("CHILD-1", agentSpawnInfo{
		ParentAgent: "root",
		CallID:      "CHILD-1",
		SubScheme:   true,
	})

	// Root part (no agent attribution): should not land under the child.
	s.Append(DisplayPart{Type: PartTypeUser, User: &UserPart{Content: "go"}})

	// SubAgent card for CHILD-1: attributed to itself.
	s.Append(DisplayPart{
		Type:     PartTypeSubAgent,
		SubAgent: &SubAgentPart{AgentName: "sub-CHILD-1", CallID: "CHILD-1", Status: "running"},
	})

	// A text part produced by the child agent.
	s.Append(DisplayPart{
		Type: PartTypeText,
		Text: &TextPart{Content: "child says hi", AgentName: "sub-CHILD-1"},
	})

	// Unrelated text from root: must not appear in child's group.
	s.Append(DisplayPart{
		Type: PartTypeText,
		Text: &TextPart{Content: "root says hi", AgentName: "root"},
	})

	got := s.PartsForChild("CHILD-1")
	if len(got) != 2 {
		t.Fatalf("PartsForChild(CHILD-1): want 2 parts, got %d (%v)", len(got), got)
	}
	if got[0] != 1 || got[1] != 2 {
		t.Fatalf("PartsForChild(CHILD-1) indices: want [1 2], got %v", got)
	}

	if got := s.PartsForChild(""); got != nil {
		t.Fatalf("PartsForChild(\"\"): want nil, got %v", got)
	}
	if got := s.PartsForChild("UNKNOWN"); got != nil {
		t.Fatalf("PartsForChild(UNKNOWN): want nil, got %v", got)
	}
}

// TestStore_DrainDirty_IdempotentReset verifies that DrainDirty returns the
// pending IDs once and then clears the set. A second drain on an unchanged
// store must yield nil (renderer relies on this to avoid double-eviction).
func TestStore_DrainDirty_IdempotentReset(t *testing.T) {
	s := NewPartsStore()
	id1 := s.Append(DisplayPart{Type: PartTypeUser, User: &UserPart{Content: "u"}})
	id2 := s.Append(DisplayPart{Type: PartTypeText, Text: &TextPart{Content: "t"}})

	got := s.DrainDirty()
	if len(got) != 2 {
		t.Fatalf("first DrainDirty: want 2 ids, got %d (%v)", len(got), got)
	}
	seen := map[uint64]bool{}
	for _, id := range got {
		seen[id] = true
	}
	if !seen[id1] || !seen[id2] {
		t.Fatalf("first DrainDirty: want {%d, %d}, got %v", id1, id2, got)
	}

	if again := s.DrainDirty(); again != nil {
		t.Fatalf("second DrainDirty: want nil, got %v", again)
	}
	if s.IsDirty() {
		t.Fatal("IsDirty after drain: want false")
	}

	// MarkDirty puts an entry back; next drain surfaces it.
	s.MarkDirty(id1)
	got2 := s.DrainDirty()
	if len(got2) != 1 || got2[0] != id1 {
		t.Fatalf("after MarkDirty(%d): drain want [%d], got %v", id1, id1, got2)
	}
}

// TestStore_SetAll_RebuildsAllIndexes verifies that SetAll wipes prior state
// and rebuilds every side index (idxByCallID for both kinds, last-user index,
// child-parts, sub-agent version) from the supplied slice.
func TestStore_SetAll_RebuildsAllIndexes(t *testing.T) {
	s := NewPartsStore()
	// Pre-populate to make sure SetAll really clears.
	s.Append(DisplayPart{Type: PartTypeUser, User: &UserPart{Content: "old"}})
	s.Append(DisplayPart{
		Type: PartTypeTool,
		Tool: &ToolPart{Name: "old", CallID: "OLD", State: "running"},
	})
	s.SetSpawnByCallID("OLD-CHILD", agentSpawnInfo{CallID: "OLD-CHILD", SubScheme: true})

	parts := []DisplayPart{
		{ID: 10, Version: 1, Type: PartTypeUser, User: &UserPart{Content: "u1"}},
		{ID: 11, Version: 1, Type: PartTypeTool, Tool: &ToolPart{Name: "bash", CallID: "T1", State: "running", IsAgent: true, AgentName: "root"}},
		{ID: 12, Version: 1, Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "T1", Status: "running"}},
		{ID: 13, Version: 1, Type: PartTypeThinking, Thinking: &ThinkingPart{Content: "x", GroupID: "g", AgentName: "root"}},
	}
	// SetAll inherits spawn metadata: the spawning tool round-trips with
	// IsAgent + AgentName, the live caller (SetParts in chat.go) rebuilds
	// spawnByCallID; SetAll does not — that's intentional. Still verify the
	// index maps it owns.
	s.SetAll(parts)

	if s.Len() != 4 {
		t.Fatalf("Len: want 4, got %d", s.Len())
	}
	// Stale OLD callID must be gone after SetAll.
	if _, ok := s.IndexByToolCallID("OLD"); ok {
		t.Fatal("IndexByToolCallID(OLD) must miss after SetAll")
	}
	// New Tool + SubAgent both indexed at their respective slots under T1.
	if idx, ok := s.IndexByToolCallID("T1"); !ok || idx != 1 {
		t.Fatalf("IndexByToolCallID(T1): want (1, true), got (%d, %v)", idx, ok)
	}
	if idx, ok := s.IndexBySubAgentCallID("T1"); !ok || idx != 2 {
		t.Fatalf("IndexBySubAgentCallID(T1): want (2, true), got (%d, %v)", idx, ok)
	}
	if s.LastUserIndex() != 0 {
		t.Fatalf("LastUserIndex: want 0, got %d", s.LastUserIndex())
	}
	// Thinking group index: same-agent same-group resume should hit slot 3.
	id, created := s.AppendThinkingDelta("root", "g", "y", time.Now())
	if created {
		t.Fatal("AppendThinkingDelta after SetAll: want extend, got create")
	}
	if id != s.At(3).ID {
		t.Fatalf("AppendThinkingDelta did not extend slot 3 (expected id=%d, got %d)", s.At(3).ID, id)
	}
}
