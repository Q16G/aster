package tui

import (
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

