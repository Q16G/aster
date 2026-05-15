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

// --- New tests for agent-grouped builtin loading ---

func writeTestSkillMD(t *testing.T, dir, name string) {
	t.Helper()
	skillDir := filepath.Join(dir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: " + name + "\ndescription: test " + name + "\nagent: all\ncontext: inline\n---\nTest instructions for " + name + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImportBuiltinSkillsFromDir_AgentGroups(t *testing.T) {
	tmpDir := t.TempDir()

	commonDir := filepath.Join(tmpDir, "common")
	auditDir := filepath.Join(tmpDir, "code-audit")
	writeTestSkillMD(t, commonDir, "util-skill")
	writeTestSkillMD(t, auditDir, "audit-skill")

	svc := NewSkillServiceWithMemory()
	total, err := svc.ImportBuiltinSkillsFromDir(context.Background(), tmpDir)
	if err != nil {
		t.Fatalf("ImportBuiltinSkillsFromDir failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected 2 imports, got %d", total)
	}

	skills, err := svc.LoadSkills(context.Background(), []string{"util-skill"})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Agent != "all" {
		t.Fatalf("common skill agent: expected 'all', got %q", skills[0].Agent)
	}
	if skills[0].Source != "builtin" {
		t.Fatalf("common skill source: expected 'builtin', got %q", skills[0].Source)
	}

	skills, err = svc.LoadSkills(context.Background(), []string{"audit-skill"})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Agent != "code-audit" {
		t.Fatalf("agent-specific skill agent: expected 'code-audit', got %q", skills[0].Agent)
	}
	if skills[0].Source != "builtin" {
		t.Fatalf("agent-specific skill source: expected 'builtin', got %q", skills[0].Source)
	}
}

func TestImportSkillsFromMultipleSources_EmptyDir_NoFallback(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	appSkillsDir := filepath.Join(homeDir, ".aster", "skills")
	if err := os.MkdirAll(appSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	svc := NewSkillServiceWithMemory()
	total, err := svc.ImportSkillsFromMultipleSources(context.Background(), "", homeDir)
	if err != nil {
		t.Fatalf("ImportSkillsFromMultipleSources failed: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 imports from empty dir, got %d", total)
	}

	skills, err := svc.ListSkills(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListSkills failed: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected empty skill list (no embedded fallback), got %d skills", len(skills))
	}
}

func TestImportSkillsFromMultipleSources_MissingDir_NoFallback(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")

	svc := NewSkillServiceWithMemory()
	total, err := svc.ImportSkillsFromMultipleSources(context.Background(), "", homeDir)
	if err != nil {
		t.Fatalf("ImportSkillsFromMultipleSources failed: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 imports from missing dir, got %d", total)
	}

	skills, err := svc.ListSkills(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListSkills failed: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected empty skill list (no embedded fallback), got %d skills", len(skills))
	}
}

func TestImportSkillsFromMultipleSources_AllSources(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	workspace := filepath.Join(tmpDir, "workspace")

	builtinCommon := filepath.Join(homeDir, ".aster", "skills", "common")
	builtinAgent := filepath.Join(homeDir, ".aster", "skills", "pentest")
	userDir := filepath.Join(homeDir, ".agent", "skills")
	projectDir := filepath.Join(workspace, ".agent", "skills")

	writeTestSkillMD(t, builtinCommon, "common-tool")
	writeTestSkillMD(t, builtinAgent, "pentest-tool")
	writeTestSkillMD(t, userDir, "user-tool")
	writeTestSkillMD(t, projectDir, "project-tool")

	svc := NewSkillServiceWithMemory()
	total, err := svc.ImportSkillsFromMultipleSources(context.Background(), workspace, homeDir)
	if err != nil {
		t.Fatalf("ImportSkillsFromMultipleSources failed: %v", err)
	}
	if total != 4 {
		t.Fatalf("expected 4 total imports, got %d", total)
	}

	checks := []struct {
		name           string
		expectedSource string
		expectedAgent  string
	}{
		{"common-tool", "builtin", "all"},
		{"pentest-tool", "builtin", "pentest"},
		{"user-tool", "user", "all"},
		{"project-tool", "project", "all"},
	}
	for _, c := range checks {
		skills, err := svc.LoadSkills(context.Background(), []string{c.name})
		if err != nil {
			t.Fatalf("LoadSkills(%s) failed: %v", c.name, err)
		}
		if len(skills) != 1 {
			t.Fatalf("expected 1 skill for %s, got %d", c.name, len(skills))
		}
		if skills[0].Source != c.expectedSource {
			t.Fatalf("%s: expected source %q, got %q", c.name, c.expectedSource, skills[0].Source)
		}
		if skills[0].Agent != c.expectedAgent {
			t.Fatalf("%s: expected agent %q, got %q", c.name, c.expectedAgent, skills[0].Agent)
		}
	}
}

func TestImportSkillsFromMultipleSources_ExactCount(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	builtinDir := filepath.Join(homeDir, ".aster", "skills", "common")
	writeTestSkillMD(t, builtinDir, "skill-one")
	writeTestSkillMD(t, builtinDir, "skill-two")

	svc := NewSkillServiceWithMemory()
	total, err := svc.ImportSkillsFromMultipleSources(context.Background(), "", homeDir)
	if err != nil {
		t.Fatalf("ImportSkillsFromMultipleSources failed: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected exactly 2 imports, got %d", total)
	}

	skills, err := svc.ListSkills(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListSkills failed: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected exactly 2 skills in store, got %d", len(skills))
	}

	knownEmbedded := map[string]bool{
		"sast-scan": true, "security-code-analysis": true, "dataflow-analysis": true,
		"agent-browser": true, "baseline-check": true,
	}
	for _, s := range skills {
		if knownEmbedded[s.Name] {
			t.Fatalf("embedded skill %q should not be loaded, only disk skills expected", s.Name)
		}
	}
}
