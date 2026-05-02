package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestImportSkillsFromMultipleSources_OverrideOrder(t *testing.T) {
	tmpDir := t.TempDir()

	userDir := filepath.Join(tmpDir, "home", ".agent", "skills", "my-skill")
	projectDir := filepath.Join(tmpDir, "workspace", ".agent", "skills", "my-skill")

	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	userSkill := `---
name: my-skill
description: user version
agent: UserAgent
when-to-use: user context
context: inline
---
User instructions
`
	projectSkill := `---
name: my-skill
description: project version
agent: ProjectAgent
when-to-use: project context
context: fork
---
Project instructions
`
	if err := os.WriteFile(filepath.Join(userDir, "SKILL.md"), []byte(userSkill), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "SKILL.md"), []byte(projectSkill), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewSkillServiceWithMemory()
	total, err := svc.ImportSkillsFromMultipleSources(
		context.Background(),
		filepath.Join(tmpDir, "workspace"),
		filepath.Join(tmpDir, "home"),
	)
	if err != nil {
		t.Fatalf("ImportSkillsFromMultipleSources failed: %v", err)
	}
	if total < 2 {
		t.Fatalf("expected at least 2 imports (user + project), got %d", total)
	}

	skills, err := svc.LoadSkills(context.Background(), []string{"my-skill"})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	skill := skills[0]
	if skill.Description != "project version" {
		t.Fatalf("expected project version to override user version, got description=%q", skill.Description)
	}
}

func TestImportSkillsFromDirWithSource_SetsSourceAndSkillDir(t *testing.T) {
	tmpDir := t.TempDir()

	skillDir := filepath.Join(tmpDir, "my-tool")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := `---
name: my-tool
description: tool skill
---
Tool instructions
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewSkillServiceWithMemory()
	n, err := svc.ImportSkillsFromDirWithSource(context.Background(), tmpDir, "user")
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 import, got %d", n)
	}

	skills, err := svc.LoadSkills(context.Background(), []string{"my-tool"})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Source != "user" {
		t.Fatalf("expected source 'user', got %q", skills[0].Source)
	}
	if skills[0].SkillDir == "" {
		t.Fatal("expected non-empty SkillDir")
	}
}

func TestInferSkillNameFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"my-skill/SKILL.md", "my-skill"},
		{"skills/data-flow/SKILL.md", "data-flow"},
		{"SKILL.md", ""},
		{"a/b/c/SKILL.md", "c"},
	}
	for _, tt := range tests {
		got := inferSkillNameFromPath(tt.path)
		if got != tt.expected {
			t.Errorf("inferSkillNameFromPath(%q) = %q, want %q", tt.path, got, tt.expected)
		}
	}
}

func TestImportSkillsFromFSWithSource_InfersNameFromDir(t *testing.T) {
	tmpDir := t.TempDir()

	skillDir := filepath.Join(tmpDir, "inferred-name")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := `---
description: nameless skill
---
No name in frontmatter
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewSkillServiceWithMemory()
	n, err := svc.ImportSkillsFromDirWithSource(context.Background(), tmpDir, "test")
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 import, got %d", n)
	}

	skills, err := svc.LoadSkills(context.Background(), []string{"inferred-name"})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "inferred-name" {
		t.Fatalf("expected inferred name 'inferred-name', got %q", skills[0].Name)
	}
}
