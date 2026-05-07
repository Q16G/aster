package tui

import (
	"testing"
	"time"
)

func TestGroupPartsIntoTurns_Empty(t *testing.T) {
	turns := groupPartsIntoTurns(nil)
	if len(turns) != 0 {
		t.Fatalf("expected 0 turns, got %d", len(turns))
	}
}

func TestGroupPartsIntoTurns_SingleUser(t *testing.T) {
	parts := []DisplayPart{
		{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "hello"}},
	}
	turns := groupPartsIntoTurns(parts)
	if len(turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(turns))
	}
	if turns[0].Type != TurnTypeUser {
		t.Fatalf("expected user turn, got %s", turns[0].Type)
	}
	if len(turns[0].Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(turns[0].Parts))
	}
}

func TestGroupPartsIntoTurns_UserAssistantUser(t *testing.T) {
	parts := []DisplayPart{
		{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "q1"}},
		{Type: PartTypeThinking, Time: time.Now(), Thinking: &ThinkingPart{Content: "hmm"}},
		{Type: PartTypeText, Time: time.Now(), Text: &TextPart{Content: "answer1"}},
		{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "q2"}},
		{Type: PartTypeText, Time: time.Now(), Text: &TextPart{Content: "answer2"}},
	}
	turns := groupPartsIntoTurns(parts)
	if len(turns) != 4 {
		t.Fatalf("expected 4 turns (user,assistant,user,assistant), got %d", len(turns))
	}
	if turns[0].Type != TurnTypeUser {
		t.Errorf("turn 0: expected user, got %s", turns[0].Type)
	}
	if turns[1].Type != TurnTypeAssistant {
		t.Errorf("turn 1: expected assistant, got %s", turns[1].Type)
	}
	if len(turns[1].Parts) != 2 {
		t.Errorf("turn 1: expected 2 parts (thinking+text), got %d", len(turns[1].Parts))
	}
	if turns[1].Parts[0].Index != 1 {
		t.Errorf("turn 1 part 0: expected index 1, got %d", turns[1].Parts[0].Index)
	}
	if turns[2].Type != TurnTypeUser {
		t.Errorf("turn 2: expected user, got %s", turns[2].Type)
	}
	if turns[3].Type != TurnTypeAssistant {
		t.Errorf("turn 3: expected assistant, got %s", turns[3].Type)
	}
}

func TestGroupPartsIntoTurns_LeadingAssistant(t *testing.T) {
	parts := []DisplayPart{
		{Type: PartTypeSystem, Time: time.Now(), System: &SystemPart{Content: "welcome"}},
		{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "hi"}},
		{Type: PartTypeText, Time: time.Now(), Text: &TextPart{Content: "hello"}},
	}
	turns := groupPartsIntoTurns(parts)
	if len(turns) != 3 {
		t.Fatalf("expected 3 turns, got %d", len(turns))
	}
	if turns[0].Type != TurnTypeAssistant {
		t.Errorf("turn 0: expected assistant (system), got %s", turns[0].Type)
	}
}

func TestGroupPartsIntoTurns_PreservesIndices(t *testing.T) {
	parts := []DisplayPart{
		{Type: PartTypeUser, Time: time.Now(), User: &UserPart{Content: "q"}},
		{Type: PartTypeTool, Time: time.Now(), Tool: &ToolPart{Name: "bash"}},
		{Type: PartTypeText, Time: time.Now(), Text: &TextPart{Content: "r1"}},
		{Type: PartTypeText, Time: time.Now(), Text: &TextPart{Content: "r2"}},
	}
	turns := groupPartsIntoTurns(parts)
	assistantTurn := turns[1]
	for i, ip := range assistantTurn.Parts {
		expected := i + 1
		if ip.Index != expected {
			t.Errorf("part %d: expected index %d, got %d", i, expected, ip.Index)
		}
	}
}

func TestMergeTextRun(t *testing.T) {
	parts := []IndexedPart{
		{Index: 0, Part: DisplayPart{Type: PartTypeTool, Tool: &ToolPart{Name: "bash"}}},
		{Index: 1, Part: DisplayPart{Type: PartTypeText, Text: &TextPart{Content: "line1"}}},
		{Index: 2, Part: DisplayPart{Type: PartTypeText, Text: &TextPart{Content: "line2"}}},
		{Index: 3, Part: DisplayPart{Type: PartTypeText, Text: &TextPart{Content: "line3"}}},
		{Index: 4, Part: DisplayPart{Type: PartTypeTool, Tool: &ToolPart{Name: "rg"}}},
	}

	content, count := mergeTextRun(parts, 1)
	if count != 3 {
		t.Fatalf("expected 3 merged, got %d", count)
	}
	if content != "line1\n\nline2\n\nline3" {
		t.Fatalf("unexpected merged content: %q", content)
	}

	content, count = mergeTextRun(parts, 0)
	if count != 0 {
		t.Fatalf("expected 0 merged for non-text start, got %d", count)
	}
	if content != "" {
		t.Fatalf("expected empty content, got %q", content)
	}
}
