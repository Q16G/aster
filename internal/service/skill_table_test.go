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

	table, err := svc.BuildSkillsTable(context.Background(), "SASTAgent", nil)
	if err != nil {
		t.Fatalf("BuildSkillsTableWithStatus failed: %v", err)
	}

	for _, expected := range []string{
		"| name | description | when-to-use | path | context |",
		"| data-flow | 数据流分析 | flow, sink | - | inline |",
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

	table, err := svc.BuildSkillsTable(context.Background(), "all", nil)
	if err != nil {
		t.Fatalf("BuildSkillsTableWithStatus failed: %v", err)
	}
	for _, expected := range []string{
		"| data-flow | 数据流分析 | - | - | inline |",
		"| project-analysis | 项目分析 | - | - | inline |",
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

	table, err := svc.BuildSkillsTable(context.Background(), "SASTAgent", nil)
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

	table, err := svc.BuildSkillsTable(context.Background(), "TestAgent", nil)
	if err != nil {
		t.Fatalf("BuildSkillsTableWithStatus failed: %v", err)
	}

	expected := "| v2-skill | New format skill | 需要测试时使用 | - | fork |"
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

	table, err := svc.BuildSkillsTable(context.Background(), "AgentA", nil)
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
			Name:         "sql-injection-comprehensive",
			Description:  "SQL 注入综合检测",
			Instructions: "blackbox sqli",
			Enabled:      &enabled,
			Agent:        "pentest",
		},
		{
			Name:         "sast-scan",
			Description:  "结构化漏洞扫描",
			Instructions: "whitebox sast",
			Enabled:      &enabled,
			Agent:        "code-audit",
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

	table, err := svc.BuildSkillsTable(
		context.Background(),
		"graybox-test",
		[]string{"sql-injection-comprehensive", "sast-scan", "result-with-file"},
	)
	if err != nil {
		t.Fatalf("BuildSkillsTableWithStatus failed: %v", err)
	}

	for _, expected := range []string{
		"sql-injection-comprehensive",
		"sast-scan",
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

	skills, err := svc.LoadSkills(context.Background(), []string{"sast-scan", "recon-methodology"})
	if err != nil {
		t.Fatalf("LoadSkills failed: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	var foundRecon bool
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
		case "recon-methodology":
			foundRecon = true
		}
	}
	if !foundSAST || !foundRecon {
		t.Fatalf("expected embedded imports to include sast-scan and recon-methodology, got %+v", skills)
	}
}

// TestImportEmbeddedSkills_SecuritySemanticsGuardrails 已删除（D4）：
// v3 重构后 security-code-analysis / graybox-p0 父 skill 已被拆解；guardrail 文本迁移到
// internal/tui/config.go 的 defaultAgentFiles["graybox-test.yaml"] / ["code-audit.yaml"] /
// ["pentest.yaml"] profile 字符串里。已无 SKILL.md 端被测对象。如需对 profile 字符串
// 做契约断言，应新建 internal/tui/config_test.go 而非保留本测试。

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
