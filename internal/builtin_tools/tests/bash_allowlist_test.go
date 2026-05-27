package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeCommand(t *testing.T) {
	tests := []struct {
		input    string
		family   ShellFamily
		expected string
	}{
		{"  npm   run   build  ", ShellFamilyPosix, "npm run build"},
		{`"npm" run build`, ShellFamilyPosix, "npm run build"},
		{"Get-ChildItem", ShellFamilyPowerShell, "get-childitem"},
		{"GET-CHILDITEM", ShellFamilyPowerShell, "get-childitem"},
		{"npm run build", ShellFamilyPosix, "npm run build"},
		{"FOO=bar npm run build", ShellFamilyPosix, "FOO=bar npm run build"},
	}
	for _, tc := range tests {
		got := NormalizeCommand(tc.input, tc.family)
		if got != tc.expected {
			t.Errorf("NormalizeCommand(%q, %s) = %q, want %q", tc.input, tc.family, got, tc.expected)
		}
	}
}

func TestMatchRuleExact(t *testing.T) {
	rule := &AllowlistRule{
		Tool:        BashToolName,
		ShellFamily: ShellFamilyPosix,
		Kind:        AllowlistRuleExact,
		Pattern:     "go test ./...",
	}
	if !MatchRule("go test ./...", ShellFamilyPosix, rule) {
		t.Error("exact match should succeed")
	}
	if MatchRule("go test ./... -v", ShellFamilyPosix, rule) {
		t.Error("exact match should fail with extra args")
	}
}

func TestMatchRulePrefix(t *testing.T) {
	rule := &AllowlistRule{
		Tool:        BashToolName,
		ShellFamily: ShellFamilyPosix,
		Kind:        AllowlistRulePrefix,
		Pattern:     "npm run",
	}
	if !MatchRule("npm run build", ShellFamilyPosix, rule) {
		t.Error("prefix match should succeed")
	}
	if !MatchRule("npm run", ShellFamilyPosix, rule) {
		t.Error("prefix match should succeed for exact prefix")
	}
	if MatchRule("npm runner", ShellFamilyPosix, rule) {
		t.Error("prefix match should fail when next char is not whitespace")
	}
}

func TestMatchRuleShellFamilyIsolation(t *testing.T) {
	rule := &AllowlistRule{
		Tool:        BashToolName,
		ShellFamily: ShellFamilyPosix,
		Kind:        AllowlistRuleExact,
		Pattern:     "ls -la",
	}
	if MatchRule("ls -la", ShellFamilyPowerShell, rule) {
		t.Error("rule should not match across shell families")
	}
}

func TestGenerateRulePrefix(t *testing.T) {
	rule := GenerateRule("npm run build", ShellFamilyPosix, false, BashToolName)
	if rule == nil {
		t.Fatal("expected rule to be generated")
	}
	if rule.Kind != AllowlistRulePrefix {
		t.Errorf("expected prefix rule, got %s", rule.Kind)
	}
	if rule.Pattern != "npm run" {
		t.Errorf("expected pattern 'npm run', got %q", rule.Pattern)
	}
}

func TestGenerateRuleInterpreterIncludesScript(t *testing.T) {
	// 解释器命令的前缀必须包含脚本路径，不能退化成裸命令名
	tests := []struct {
		cmd             string
		expectedKind    AllowlistRuleKind
		expectedPattern string
	}{
		{"python tools/lint.py --fix", AllowlistRulePrefix, "python tools/lint.py"},
		{"python3 manage.py runserver", AllowlistRulePrefix, "python3 manage.py"},
		{"node dist/server.js", AllowlistRulePrefix, "node dist/server.js"},
		{"java -jar app.jar", AllowlistRulePrefix, "java -jar"},
	}
	for _, tc := range tests {
		rule := GenerateRule(tc.cmd, ShellFamilyPosix, false, BashToolName)
		if rule == nil {
			t.Fatalf("expected rule for %q", tc.cmd)
		}
		if rule.Kind != tc.expectedKind {
			t.Errorf("GenerateRule(%q): kind = %s, want %s", tc.cmd, rule.Kind, tc.expectedKind)
		}
		if rule.Pattern != tc.expectedPattern {
			t.Errorf("GenerateRule(%q): pattern = %q, want %q", tc.cmd, rule.Pattern, tc.expectedPattern)
		}
	}
}

func TestGenerateRuleCompoundReturnsNil(t *testing.T) {
	rule := GenerateRule("echo hello && echo world", ShellFamilyPosix, true, BashToolName)
	if rule != nil {
		t.Error("compound command should not generate a persist rule")
	}
}

func TestSessionAllowlistAddRemove(t *testing.T) {
	al := NewSessionAllowlist()
	rule := &AllowlistRule{Tool: BashToolName, ShellFamily: ShellFamilyPosix, Kind: AllowlistRulePrefix, Pattern: "go test"}
	if err := al.Add(rule); err != nil {
		t.Fatal(err)
	}
	if len(al.Rules()) != 1 {
		t.Errorf("expected 1 rule, got %d", len(al.Rules()))
	}
	// 去重
	if err := al.Add(rule); err != nil {
		t.Fatal(err)
	}
	if len(al.Rules()) != 1 {
		t.Error("duplicate rule should be deduplicated")
	}
	// 删除
	if !al.Remove(0) {
		t.Error("remove should succeed")
	}
	if len(al.Rules()) != 0 {
		t.Error("expected 0 rules after remove")
	}
}

func TestPersistAllowlistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rules := []*AllowlistRule{
		{Tool: BashToolName, ShellFamily: ShellFamilyPosix, Kind: AllowlistRulePrefix, Pattern: "go test"},
		{Tool: BashToolName, ShellFamily: ShellFamilyPosix, Kind: AllowlistRuleExact, Pattern: "make lint"},
	}
	if err := SavePersistAllowlist(dir, rules); err != nil {
		t.Fatal(err)
	}
	// 验证文件存���
	fp := filepath.Join(dir, ".sastpro", "bash_allowlist.json")
	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("allowlist file should exist: %v", err)
	}
	loaded, err := LoadPersistAllowlist(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Errorf("expected 2 rules, got %d", len(loaded))
	}
	if loaded[0].Pattern != "go test" {
		t.Errorf("expected pattern 'go test', got %q", loaded[0].Pattern)
	}
}

func TestPersistAllowlistNotExist(t *testing.T) {
	dir := t.TempDir()
	rules, err := LoadPersistAllowlist(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rules != nil {
		t.Error("expected nil for nonexistent file")
	}
}

func TestSessionAllowlistLimit(t *testing.T) {
	al := NewSessionAllowlist()
	for i := 0; i < 200; i++ {
		rule := &AllowlistRule{Tool: BashToolName, ShellFamily: ShellFamilyPosix, Kind: AllowlistRuleExact, Pattern: string(rune('a'+i%26)) + string(rune(i))}
		if err := al.Add(rule); err != nil {
			t.Fatalf("add rule %d failed: %v", i, err)
		}
	}
	err := al.Add(&AllowlistRule{Tool: BashToolName, ShellFamily: ShellFamilyPosix, Kind: AllowlistRuleExact, Pattern: "overflow"})
	if err == nil {
		t.Error("expected limit error")
	}
}
