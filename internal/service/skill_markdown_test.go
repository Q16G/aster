package service

import (
	"testing"
)

func TestParseSkillMarkdown_NewFormat(t *testing.T) {
	md := `---
name: test-skill
description: A test skill
version: 2.0.0
tags:
  - test
agent: TestAgent
when-to-use: 当需要测试时
context: fork
arguments:
  - target
  - version
argument-hint: "<target> <version>"
allowed-tools:
  - rg
  - read_file
user-invocable: false
---
# Test Skill

Do the thing.
`
	skill, err := ParseSkillMarkdown(md)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if skill.Name != "test-skill" {
		t.Fatalf("expected name 'test-skill', got %q", skill.Name)
	}
	if skill.Agent != "TestAgent" {
		t.Fatalf("expected agent 'TestAgent', got %q", skill.Agent)
	}
	if skill.WhenToUse != "当需要测试时" {
		t.Fatalf("expected when-to-use, got %q", skill.WhenToUse)
	}
	if skill.Context != "fork" {
		t.Fatalf("expected context 'fork', got %q", skill.Context)
	}
	if len(skill.Arguments) != 2 || skill.Arguments[0] != "target" || skill.Arguments[1] != "version" {
		t.Fatalf("unexpected arguments: %v", skill.Arguments)
	}
	if skill.ArgumentHint != "<target> <version>" {
		t.Fatalf("unexpected argument-hint: %q", skill.ArgumentHint)
	}
	if len(skill.AllowedTools) != 2 || skill.AllowedTools[0] != "rg" {
		t.Fatalf("unexpected allowed-tools: %v", skill.AllowedTools)
	}
	if skill.UserInvocable {
		t.Fatal("expected user-invocable = false")
	}
}

func TestParseSkillMarkdown_OldFormat(t *testing.T) {
	md := `---
name: old-skill
description: Old format skill
version: 1.0.0
author: SAST System
category: java-sast
type: guide
tools:
  - rg
trigger_keywords:
  - flow
  - taint
meta:
  agent: SASTAgent
---
# Old Skill

Instructions here.
`
	skill, err := ParseSkillMarkdown(md)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if skill.Agent != "SASTAgent" {
		t.Fatalf("expected agent 'SASTAgent' from meta.agent, got %q", skill.Agent)
	}
	if skill.WhenToUse != "flow, taint" {
		t.Fatalf("expected when-to-use from trigger_keywords, got %q", skill.WhenToUse)
	}
	if len(skill.AllowedTools) != 1 || skill.AllowedTools[0] != "rg" {
		t.Fatalf("expected allowed-tools from tools field, got %v", skill.AllowedTools)
	}
	if skill.Context != "inline" {
		t.Fatalf("expected default context 'inline', got %q", skill.Context)
	}
	if !skill.UserInvocable {
		t.Fatal("expected default user-invocable = true")
	}
}

func TestParseSkillMarkdown_MixedFormat(t *testing.T) {
	md := `---
name: mixed-skill
description: Mixed format
version: 1.0.0
agent: ExplicitAgent
meta:
  agent: FallbackAgent
trigger_keywords:
  - keyword1
when-to-use: 显式的 when-to-use
tools:
  - old_tool
allowed-tools:
  - new_tool
---
Body
`
	skill, err := ParseSkillMarkdown(md)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if skill.Agent != "ExplicitAgent" {
		t.Fatalf("expected explicit agent over meta.agent, got %q", skill.Agent)
	}
	if skill.WhenToUse != "显式的 when-to-use" {
		t.Fatalf("expected explicit when-to-use over trigger_keywords, got %q", skill.WhenToUse)
	}
	if len(skill.AllowedTools) != 1 || skill.AllowedTools[0] != "new_tool" {
		t.Fatalf("expected allowed-tools over tools, got %v", skill.AllowedTools)
	}
}

func TestParseSkillMarkdown_Defaults(t *testing.T) {
	md := `---
name: minimal
description: minimal skill
---
Instructions
`
	skill, err := ParseSkillMarkdown(md)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if skill.Agent != "all" {
		t.Fatalf("expected default agent 'all', got %q", skill.Agent)
	}
	if skill.Context != "inline" {
		t.Fatalf("expected default context 'inline', got %q", skill.Context)
	}
	if !skill.UserInvocable {
		t.Fatal("expected default user-invocable = true")
	}
	if skill.WhenToUse != "" {
		t.Fatalf("expected empty when-to-use, got %q", skill.WhenToUse)
	}
}

func TestParseSkillMarkdown_MissingFrontmatter(t *testing.T) {
	md := `# No Frontmatter

Just body text.
`
	_, err := ParseSkillMarkdown(md)
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseSkillMarkdown_EmptyTriggerKeywordsNotMapped(t *testing.T) {
	md := `---
name: no-kw
description: no keywords
trigger_keywords: []
---
Body
`
	skill, err := ParseSkillMarkdown(md)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if skill.WhenToUse != "" {
		t.Fatalf("expected empty when-to-use for empty trigger_keywords, got %q", skill.WhenToUse)
	}
}
