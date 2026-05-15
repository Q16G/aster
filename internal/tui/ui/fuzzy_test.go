package ui

import "testing"

func TestFuzzyMatch_Basics(t *testing.T) {
	tests := []struct {
		pattern string
		text    string
		match   bool
	}{
		{"", "anything", true},
		{"gpt", "gpt-4o", true},
		{"g4o", "gpt-4o", true},
		{"xyz", "gpt-4o", false},
		{"GPT", "gpt-4o", true},
		{"clau", "claude-sonnet-4", true},
		{"cs4", "claude-sonnet-4", true},
		{"abc", "a_b_c", true},
	}
	for _, tt := range tests {
		matched, _ := FuzzyMatch(tt.pattern, tt.text)
		if matched != tt.match {
			t.Errorf("FuzzyMatch(%q, %q) = %v, want %v", tt.pattern, tt.text, matched, tt.match)
		}
	}
}

func TestFuzzyMatch_ScoreConsecutiveHigher(t *testing.T) {
	_, s1 := FuzzyMatch("gpt", "gpt-4o")
	_, s2 := FuzzyMatch("g4o", "gpt-4o")
	if s1 <= s2 {
		t.Errorf("consecutive match %q should score higher than scattered %q, got %d vs %d",
			"gpt", "g4o", s1, s2)
	}
}

func TestFuzzyFilter_ReturnsMatches(t *testing.T) {
	opts := []SelectOption{
		{Label: "── Header ──", Disabled: true},
		{Label: "gpt-4o", Value: "gpt-4o"},
		{Label: "claude-sonnet-4", Value: "claude-sonnet-4"},
		{Label: "deepseek-v3", Value: "deepseek-v3"},
	}
	result := FuzzyFilter("gpt", opts)
	if len(result) != 1 || result[0].Value != "gpt-4o" {
		t.Errorf("expected only gpt-4o, got %v", result)
	}
}

func TestFuzzyFilter_EmptyPattern(t *testing.T) {
	opts := []SelectOption{
		{Label: "a", Value: "a"},
		{Label: "b", Value: "b"},
	}
	result := FuzzyFilter("", opts)
	if len(result) != 2 {
		t.Errorf("expected all options returned for empty pattern, got %d", len(result))
	}
}
