package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestParseDefaultAgentProfiles_IsSortedByFileName(t *testing.T) {
	got := ParseDefaultAgentProfiles()
	if len(got) == 0 {
		t.Fatal("expected non-empty default profiles")
	}

	keys := make([]string, 0, len(defaultAgentFiles))
	for k := range defaultAgentFiles {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	expectedNames := make([]string, 0, len(keys))
	for _, name := range keys {
		content := defaultAgentFiles[name]
		var p ProfileYAML
		if err := yaml.Unmarshal([]byte(content), &p); err != nil {
			continue
		}
		if p.Name == "" {
			p.Name = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
		}
		expectedNames = append(expectedNames, p.Name)
	}

	gotNames := make([]string, 0, len(got))
	for _, p := range got {
		gotNames = append(gotNames, p.Name)
	}

	if len(gotNames) != len(expectedNames) {
		t.Fatalf("expected %d profiles, got %d", len(expectedNames), len(gotNames))
	}
	for i := range expectedNames {
		if gotNames[i] != expectedNames[i] {
			t.Fatalf("expected profile order %v, got %v", expectedNames, gotNames)
		}
	}
}

func TestParseDefaultAgentProfiles_IncludesRedTeam(t *testing.T) {
	profiles := ParseDefaultAgentProfiles()
	var redTeam *ProfileYAML
	for i := range profiles {
		if profiles[i].Name == "red-team" {
			redTeam = &profiles[i]
			break
		}
	}
	if redTeam == nil {
		t.Fatal("expected default profiles to include red-team")
	}

	for _, name := range []string{
		"redteam-methodology",
		"external-recon",
		"fingerprint-triage",
		"nuclei-poc-verification",
		"redteam-report",
		"result-with-file",
		"web-security-testing",
		"SQL注入-多策略综合检测",
		"command-injection",
		"文件上传-多策略综合检测",
	} {
		if !containsString(redTeam.SkillNames, name) {
			t.Fatalf("expected red-team skill_names to include %q, got %v", name, redTeam.SkillNames)
		}
	}

	for _, name := range []string{"redteam-methodology", "result-with-file"} {
		if !containsString(redTeam.PreloadSkills, name) {
			t.Fatalf("expected red-team preload_skills to include %q, got %v", name, redTeam.PreloadSkills)
		}
	}

	for _, name := range []string{"list_files", "read_file", "rg", "list_skills", "load_skills"} {
		if !containsString(redTeam.ToolNames, name) {
			t.Fatalf("expected red-team tool_names to include %q, got %v", name, redTeam.ToolNames)
		}
	}
}

func TestParseDefaultAgentProfiles_RedTeamDoesNotBlockOnScopeForm(t *testing.T) {
	profiles := ParseDefaultAgentProfiles()
	var redTeam *ProfileYAML
	for i := range profiles {
		if profiles[i].Name == "red-team" {
			redTeam = &profiles[i]
			break
		}
	}
	if redTeam == nil {
		t.Fatal("expected default profiles to include red-team")
	}

	for _, expected := range []string{
		"用户给出域名、IP 或 URL 时，将其直接视为本轮授权范围",
		"不要要求用户填写完整授权表格",
	} {
		if !strings.Contains(redTeam.Instruction, expected) {
			t.Fatalf("expected red-team instruction to contain %q, got:\n%s", expected, redTeam.Instruction)
		}
	}

	if strings.Contains(redTeam.Instruction, "必须先要求用户补充范围") {
		t.Fatalf("red-team instruction should not force a scope form before work, got:\n%s", redTeam.Instruction)
	}
}

func TestDefaultConfigIncludesChromeMCP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(defaultConfigYAML), 0o644); err != nil {
		t.Fatalf("write default config: %v", err)
	}

	cfg, err := loadConfig(path, false)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	chrome := cfg.MCPServers["chrome"]
	if chrome == nil {
		t.Fatal("expected default config to include chrome MCP server")
	}
	if chrome.Type != "stdio" {
		t.Fatalf("expected chrome MCP type stdio, got %q", chrome.Type)
	}
	if chrome.Command != "npx" {
		t.Fatalf("expected chrome MCP command npx, got %q", chrome.Command)
	}

	expectedArgs := []string{"-y", "@playwright/mcp@0.0.75", "--browser", "chrome", "--isolated"}
	if !reflect.DeepEqual(chrome.Args, expectedArgs) {
		t.Fatalf("expected chrome MCP args %v, got %v", expectedArgs, chrome.Args)
	}
	if chrome.Resident {
		t.Fatal("expected chrome MCP to be non-resident by default")
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
