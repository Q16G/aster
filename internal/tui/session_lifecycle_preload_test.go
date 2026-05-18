package tui

import (
	"testing"

	"aster/internal/react"
)

func TestMergeDistinctNames_DedupesAndPreservesOrder(t *testing.T) {
	got := mergeDistinctNames([]string{"a", "a", ""}, []string{"b", "a"})
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestToggleSessionSkill_CannotDisablePreloadedSkill(t *testing.T) {
	m := NewModel(ModelDeps{
		AgentCtx: &AgentExecContext{
			Definition: react.AgentDefinition{PreloadSkills: []string{"skill-a"}},
		},
	})

	m.sessionMeta.ActiveSkillNames = []string{"skill-a"}
	m.toggleSessionSkill("skill-a", false)

	if len(m.sessionMeta.ActiveSkillNames) != 1 || m.sessionMeta.ActiveSkillNames[0] != "skill-a" {
		t.Fatalf("expected skill-a to remain enabled, got %v", m.sessionMeta.ActiveSkillNames)
	}

	effective := m.effectiveActiveSkillNames()
	if len(effective) != 1 || effective[0] != "skill-a" {
		t.Fatalf("expected effective skills to include skill-a, got %v", effective)
	}
}

