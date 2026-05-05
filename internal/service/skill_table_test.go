package service

import (
	"context"
	"strings"
	"testing"
)

func TestBuildSkillsTableWithStatus(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	enabled := true
	if err := svc.ImportSkill(context.Background(), &MCPSkill{
		Name:         "data-flow",
		Description:  "数据流分析",
		Instructions: "follow flows",
		Enabled:      &enabled,
		Agent:        "SASTAgent",
		WhenToUse:    "flow, sink",
	}); err != nil {
		t.Fatalf("import skill failed: %v", err)
	}

	table, err := svc.BuildSkillsTableWithStatus(context.Background(), "SASTAgent", nil, []string{"data-flow"})
	if err != nil {
		t.Fatalf("BuildSkillsTableWithStatus failed: %v", err)
	}

	for _, expected := range []string{
		"| name | description | when-to-use | context | status |",
		"| data-flow | 数据流分析 | flow, sink | inline | loaded |",
	} {
		if !strings.Contains(table, expected) {
			t.Fatalf("expected table to contain %q, got:\n%s", expected, table)
		}
	}
}

func TestBuildSkillsTableWithStatus_AllAgentWildcard(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	enabled := true
	for _, skill := range []*MCPSkill{
		{
			Name:         "data-flow",
			Description:  "数据流分析",
			Instructions: "follow flows",
			Enabled:      &enabled,
			Agent:        "SASTAgent",
		},
		{
			Name:         "project-analysis",
			Description:  "项目分析",
			Instructions: "inspect project",
			Enabled:      &enabled,
			Agent:        "ProjectAnalysisAgent",
		},
	} {
		if err := svc.ImportSkill(context.Background(), skill); err != nil {
			t.Fatalf("import skill %s failed: %v", skill.Name, err)
		}
	}

	table, err := svc.BuildSkillsTableWithStatus(context.Background(), "all", nil, nil)
	if err != nil {
		t.Fatalf("BuildSkillsTableWithStatus failed: %v", err)
	}
	for _, expected := range []string{
		"| data-flow | 数据流分析 | - | inline | available |",
		"| project-analysis | 项目分析 | - | inline | available |",
	} {
		if !strings.Contains(table, expected) {
			t.Fatalf("expected wildcard table to contain %q, got:\n%s", expected, table)
		}
	}
}

func TestBuildInjectedSkillsSection_DedupAndPreserveOrder(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	enabled := true
	for _, skill := range []*MCPSkill{
		{
			Name:         "skill-a",
			Description:  "A",
			Instructions: "instructions-a",
			Enabled:      &enabled,
		},
		{
			Name:         "skill-b",
			Description:  "B",
			Instructions: "instructions-b",
			Enabled:      &enabled,
		},
	} {
		if err := svc.ImportSkill(context.Background(), skill); err != nil {
			t.Fatalf("import skill %s failed: %v", skill.Name, err)
		}
	}

	section, err := svc.BuildInjectedSkillsSection(context.Background(), nil, []string{"skill-b", "skill-a", "skill-b"})
	if err != nil {
		t.Fatalf("BuildInjectedSkillsSection failed: %v", err)
	}

	first := strings.Index(section, "#### skill-b")
	second := strings.Index(section, "#### skill-a")
	if first < 0 || second < 0 || first >= second {
		t.Fatalf("expected section to preserve normalized input order, got:\n%s", section)
	}
	if strings.Count(section, "#### skill-b") != 1 {
		t.Fatalf("expected deduped skill-b section, got:\n%s", section)
	}
}

func TestImportEmbeddedSkills(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	count, err := svc.ImportEmbeddedSkills(context.Background())
	if err != nil {
		t.Fatalf("ImportEmbeddedSkills failed: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected embedded skills to be imported")
	}

	table, err := svc.BuildSkillsTableWithStatus(context.Background(), "SASTAgent", nil, nil)
	if err != nil {
		t.Fatalf("BuildSkillsTableWithStatus failed: %v", err)
	}
	if strings.TrimSpace(table) == "" {
		t.Fatalf("expected non-empty skills table after embedded import")
	}
}

func TestBuildSkillsTableWithStatus_V2Fields(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	enabled := true
	if err := svc.ImportSkill(context.Background(), &MCPSkill{
		Name:         "v2-skill",
		Description:  "New format skill",
		Instructions: "do stuff",
		Enabled:      &enabled,
		Agent:        "TestAgent",
		WhenToUse:    "需要测试时使用",
		Context:      "fork",
	}); err != nil {
		t.Fatalf("import failed: %v", err)
	}

	table, err := svc.BuildSkillsTableWithStatus(context.Background(), "TestAgent", nil, nil)
	if err != nil {
		t.Fatalf("BuildSkillsTableWithStatus failed: %v", err)
	}

	expected := "| v2-skill | New format skill | 需要测试时使用 | fork | available |"
	if !strings.Contains(table, expected) {
		t.Fatalf("expected table to contain %q, got:\n%s", expected, table)
	}
}

func TestBuildSkillsTableWithStatus_AgentFilterWithV2Field(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	enabled := true

	for _, skill := range []*MCPSkill{
		{
			Name:         "agent-a-skill",
			Description:  "For Agent A",
			Instructions: "a instructions",
			Enabled:      &enabled,
			Agent:        "AgentA",
		},
		{
			Name:         "agent-b-skill",
			Description:  "For Agent B",
			Instructions: "b instructions",
			Enabled:      &enabled,
			Agent:        "AgentB",
		},
		{
			Name:         "all-agents-skill",
			Description:  "For all agents",
			Instructions: "all instructions",
			Enabled:      &enabled,
			Agent:        "all",
		},
	} {
		if err := svc.ImportSkill(context.Background(), skill); err != nil {
			t.Fatalf("import %s failed: %v", skill.Name, err)
		}
	}

	table, err := svc.BuildSkillsTableWithStatus(context.Background(), "AgentA", nil, nil)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	if !strings.Contains(table, "agent-a-skill") {
		t.Fatalf("expected agent-a-skill visible to AgentA, got:\n%s", table)
	}
	if !strings.Contains(table, "all-agents-skill") {
		t.Fatalf("expected all-agents-skill visible to AgentA, got:\n%s", table)
	}
	if strings.Contains(table, "agent-b-skill") {
		t.Fatalf("agent-b-skill should NOT be visible to AgentA, got:\n%s", table)
	}
}

func TestImportEmbeddedSkills_V2FieldsPopulated(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	_, err := svc.ImportEmbeddedSkills(context.Background())
	if err != nil {
		t.Fatalf("ImportEmbeddedSkills failed: %v", err)
	}

	skills, err := svc.LoadSkills(context.Background(), []string{"sast-scan"})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	skill := skills[0]
	if skill.Agent != "all" {
		t.Fatalf("expected agent 'all', got %q", skill.Agent)
	}
	if skill.WhenToUse == "" {
		t.Fatal("expected non-empty when-to-use for sast-scan skill")
	}
	if skill.Context != "inline" {
		t.Fatalf("expected context 'inline', got %q", skill.Context)
	}
	if skill.Source != "builtin" {
		t.Fatalf("expected source 'builtin', got %q", skill.Source)
	}
}

func TestImportEmbeddedSkills_AllHaveAgent(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	_, err := svc.ImportEmbeddedSkills(context.Background())
	if err != nil {
		t.Fatalf("ImportEmbeddedSkills failed: %v", err)
	}

	enabled := true
	skills, err := svc.ListSkills(context.Background(), &SkillFilter{Enabled: &enabled})
	if err != nil {
		t.Fatalf("ListSkills failed: %v", err)
	}

	for _, skill := range skills {
		if skill.Agent == "" {
			t.Fatalf("skill %q has empty Agent field", skill.Name)
		}
		if skill.Context == "" {
			t.Fatalf("skill %q has empty Context field", skill.Name)
		}
		if skill.Context != "inline" && skill.Context != "fork" {
			t.Fatalf("skill %q has invalid context %q", skill.Name, skill.Context)
		}
	}
}
