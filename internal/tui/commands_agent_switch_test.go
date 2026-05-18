package tui

import (
	"path/filepath"
	"testing"

	"aster/internal/react"
)

func TestCmdAgent_SyncsRuntimeState(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewSessionStore(filepath.Join(tmpDir, "data.db"), filepath.Join(tmpDir, "sessions"))
	if err != nil {
		t.Fatalf("NewSessionStore failed: %v", err)
	}
	defer store.Close()

	rec := &SessionRecord{AgentName: "agent-a"}
	if err := store.Create(rec); err != nil {
		t.Fatalf("create session failed: %v", err)
	}

	defA := react.AgentDefinition{Name: "agent-a", PreloadSkills: []string{"pre-a"}}
	defB := react.AgentDefinition{Name: "agent-b", PreloadSkills: []string{"pre-b"}}
	reg := NewProfileRegistry()
	reg.Register(defA)
	reg.Register(defB)

	ctx := &AgentExecContext{Definition: defA}
	m := NewModel(ModelDeps{Store: store, AgentCtx: ctx, ProfileRegistry: reg})
	m.currentSessionID = rec.ID
	m.sessionMeta.ActiveSkillNames = []string{"user-skill"}

	if _, ok := reg.Get("agent-b"); !ok {
		t.Fatal("expected agent-b to exist in registry")
	}

	_, _ = m.cmdAgent([]string{"agent-b"})

	if m.agentCtx == nil || m.agentCtx.Definition.Name != "agent-b" {
		name := ""
		if m.agentCtx != nil {
			name = m.agentCtx.Definition.Name
		}
		t.Fatalf("expected current agent to be agent-b, got %q", name)
	}

	got := m.agentCtx.InitialState.ActiveSkillNames
	want := []string{"pre-b", "user-skill"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}

	snap := m.buildSidebarSnapshot()
	if len(snap.ActiveSkills) != len(want) || snap.ActiveSkills[0] != "pre-b" || snap.ActiveSkills[1] != "user-skill" {
		t.Fatalf("expected sidebar active skills %v, got %v", want, snap.ActiveSkills)
	}
}

