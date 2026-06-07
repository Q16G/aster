package service

import (
	"context"
	"strings"
	"testing"

	skillspkg "aster/skills"
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
		"| name | description | when-to-use | path | context | status |",
		"| data-flow | 数据流分析 | flow, sink | - | inline | loaded |",
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
		"| data-flow | 数据流分析 | - | - | inline | available |",
		"| project-analysis | 项目分析 | - | - | inline | available |",
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

	expected := "| v2-skill | New format skill | 需要测试时使用 | - | fork | available |"
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

func TestBuildSkillsTableWithStatus_AllowedSkillNamesOverrideAgentVisibility(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	enabled := true

	for _, skill := range []*MCPSkill{
		{
			Name:         "security-code-analysis",
			Description:  "白盒总控",
			Instructions: "whitebox",
			Enabled:      &enabled,
			Agent:        "code-audit",
		},
		{
			Name:         "web-security-testing",
			Description:  "黑盒总控",
			Instructions: "blackbox",
			Enabled:      &enabled,
			Agent:        "pentest",
		},
		{
			Name:         "result-with-file",
			Description:  "输出",
			Instructions: "report",
			Enabled:      &enabled,
			Agent:        "all",
		},
	} {
		if err := svc.ImportSkill(context.Background(), skill); err != nil {
			t.Fatalf("import %s failed: %v", skill.Name, err)
		}
	}

	table, err := svc.BuildSkillsTableWithStatus(
		context.Background(),
		"graybox-test",
		[]string{"security-code-analysis", "web-security-testing", "result-with-file"},
		nil,
	)
	if err != nil {
		t.Fatalf("BuildSkillsTableWithStatus failed: %v", err)
	}

	for _, expected := range []string{
		"security-code-analysis",
		"web-security-testing",
		"result-with-file",
	} {
		if !strings.Contains(table, expected) {
			t.Fatalf("expected graybox allowlisted table to contain %q, got:\n%s", expected, table)
		}
	}
}

func TestImportEmbeddedSkills_V2FieldsPopulated(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	_, err := svc.ImportEmbeddedSkills(context.Background())
	if err != nil {
		t.Fatalf("ImportEmbeddedSkills failed: %v", err)
	}

	skills, err := svc.LoadSkills(context.Background(), []string{"sast-scan", "graybox-p0"})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	var foundGraybox bool
	var foundSAST bool
	for _, skill := range skills {
		if skill.Agent != "all" {
			t.Fatalf("expected agent 'all' for %q, got %q", skill.Name, skill.Agent)
		}
		if skill.WhenToUse == "" {
			t.Fatalf("expected non-empty when-to-use for %q", skill.Name)
		}
		if skill.Context != "inline" {
			t.Fatalf("expected context 'inline' for %q, got %q", skill.Name, skill.Context)
		}
		if skill.Source != "builtin" {
			t.Fatalf("expected source 'builtin' for %q, got %q", skill.Name, skill.Source)
		}
		switch skill.Name {
		case "sast-scan":
			foundSAST = true
		case "graybox-p0":
			foundGraybox = true
		}
	}
	if !foundSAST || !foundGraybox {
		t.Fatalf("expected embedded imports to include sast-scan and graybox-p0, got %+v", skills)
	}
}

func TestImportEmbeddedSkills_SecuritySemanticsGuardrails(t *testing.T) {
	svc := NewSkillServiceWithMemory()
	_, err := svc.ImportEmbeddedSkills(context.Background())
	if err != nil {
		t.Fatalf("ImportEmbeddedSkills failed: %v", err)
	}

	skills, err := svc.LoadSkills(context.Background(), []string{
		"security-code-analysis",
		"graybox-p0",
		"result-with-file",
	})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(skills))
	}

	byName := make(map[string]*Skill, len(skills))
	for _, skill := range skills {
		byName[skill.Name] = skill
	}

	audit := byName["security-code-analysis"]
	if audit == nil {
		t.Fatal("security-code-analysis not loaded")
	}
	for _, needle := range []string{
		"当前任务确属 pure code-audit",
		"存在可行动的运行目标",
		"不得在最终报告里直接落 `confirmed`",
	} {
		if !strings.Contains(audit.Instructions, needle) {
			t.Fatalf("security-code-analysis must keep pure-static confirmed guardrail %q, got:\n%s", needle, audit.Instructions)
		}
	}

	graybox := byName["graybox-p0"]
	if graybox == nil {
		t.Fatal("graybox-p0 not loaded")
	}
	for _, needle := range []string{
		"白盒可达性已确认（纯静态证据，不等于动态 `confirmed`）",
		"不得直接照抄成报告 `confirmed`",
		"尚无动态可观测效果",
	} {
		if !strings.Contains(graybox.Instructions, needle) {
			t.Fatalf("graybox-p0 must keep dynamic confirmed guardrail %q, got:\n%s", needle, graybox.Instructions)
		}
	}

	resultWithFile := byName["result-with-file"]
	if resultWithFile == nil {
		t.Fatal("result-with-file not loaded")
	}
	for _, needle := range []string{
		"仅适用于 pure code-audit 报告",
		"graybox / pentest / 有可运行目标的报告里，`confirmed` 仍要求运行时效果证据",
		"只能用于 pure code-audit 报告",
	} {
		if !strings.Contains(resultWithFile.Instructions, needle) {
			t.Fatalf("result-with-file must keep static confirmed scope guardrail %q, got:\n%s", needle, resultWithFile.Instructions)
		}
	}

	templateBytes, err := skillspkg.EmbeddedSkills.ReadFile("common/result-with-file/reference/code-audit-template.md")
	if err != nil {
		t.Fatalf("read code-audit template failed: %v", err)
	}
	template := string(templateBytes)
	for _, needle := range []string{
		"仅 pure code-audit 报告",
		"总结论里的 `confirmed` 仍需运行时效果证据",
		"静态 POC 仅作白盒佐证",
	} {
		if !strings.Contains(template, needle) {
			t.Fatalf("code-audit template must keep static confirmed scope guardrail %q, got:\n%s", needle, template)
		}
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
