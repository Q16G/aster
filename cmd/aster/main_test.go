package main

import (
	"testing"

	"aster/internal/react"
)

func TestChooseDefaultAgentDefinition_PrefersCodeAudit(t *testing.T) {
	profiles := []react.AgentDefinition{
		{Name: "example"},
		{Name: "code-audit"},
		{Name: "other"},
	}
	got := chooseDefaultAgentDefinition(profiles, "")
	if got.Name != "code-audit" {
		t.Fatalf("expected code-audit, got %q", got.Name)
	}
}

func TestChooseDefaultAgentDefinition_FallsBackToFirst(t *testing.T) {
	profiles := []react.AgentDefinition{
		{Name: "example"},
		{Name: "other"},
	}
	got := chooseDefaultAgentDefinition(profiles, "")
	if got.Name != "example" {
		t.Fatalf("expected fallback to first (example), got %q", got.Name)
	}
}

