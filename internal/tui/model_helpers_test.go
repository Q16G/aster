package tui

import "testing"

func TestBuildModelSelectOptionsGroupsCurrentAndRecent(t *testing.T) {
	options := buildModelSelectOptions(
		[]ModelOption{
			{ID: "gpt-4o", OwnedBy: "openai"},
			{ID: "gpt-4.1-mini", OwnedBy: "openai"},
			{ID: "claude-3-7-sonnet", OwnedBy: "anthropic"},
		},
		"gpt-4o",
		nil,
		[]string{"gpt-4.1-mini", "gpt-4o"},
	)

	if len(options) != 6 {
		t.Fatalf("expected 6 options, got %d", len(options))
	}
	if !options[0].Disabled || options[0].Label != "Current" {
		t.Fatalf("expected Current heading, got %+v", options[0])
	}
	if options[1].Value != "gpt-4o" {
		t.Fatalf("expected gpt-4o as current, got %+v", options[1])
	}
	if !options[2].Disabled || options[2].Label != "Recent" {
		t.Fatalf("expected Recent heading, got %+v", options[2])
	}
	if options[3].Value != "gpt-4.1-mini" {
		t.Fatalf("expected gpt-4.1-mini as recent, got %+v", options[3])
	}
	if !options[4].Disabled || options[4].Label != "All Models" {
		t.Fatalf("expected All Models heading, got %+v", options[4])
	}
	if options[5].Value != "claude-3-7-sonnet" {
		t.Fatalf("expected claude-3-7-sonnet in all, got %+v", options[5])
	}
}
