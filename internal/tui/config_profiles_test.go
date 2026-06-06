package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

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

func anyDefaultAgent(t *testing.T) (string, string) {
	t.Helper()
	names := DefaultAgentNames()
	if len(names) == 0 {
		t.Fatal("no default agent files")
	}
	name := names[0]
	return name, defaultAgentFiles[name]
}

func TestSyncAgentDefaults_FirstRunSeedsAndWritesMarker(t *testing.T) {
	dir := t.TempDir()

	if err := syncAgentDefaults(dir); err != nil {
		t.Fatalf("syncAgentDefaults: %v", err)
	}

	for name, content := range defaultAgentFiles {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != content {
			t.Fatalf("file %s not seeded with built-in content", name)
		}
	}

	marker := readAgentHashMarker(dir)
	for name, content := range defaultAgentFiles {
		if marker[name] != agentContentHash(content) {
			t.Fatalf("marker hash mismatch for %s", name)
		}
	}
}

func TestSyncAgentDefaults_OverwritesWhenBuiltinChanged(t *testing.T) {
	dir := t.TempDir()
	name, content := anyDefaultAgent(t)

	// Pre-seed a stale file with a stale marker hash (simulating an old built-in).
	if err := os.WriteFile(filepath.Join(dir, name), []byte("stale: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAgentHashMarker(dir, map[string]string{name: "deadbeefdeadbeef"}); err != nil {
		t.Fatal(err)
	}

	if err := syncAgentDefaults(dir); err != nil {
		t.Fatalf("syncAgentDefaults: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Fatalf("expected %s to be overwritten with built-in content", name)
	}
}

func TestSyncAgentDefaults_PreservesEditsWhenBuiltinUnchanged(t *testing.T) {
	dir := t.TempDir()
	name, content := anyDefaultAgent(t)

	// Marker says current built-in hash, but the on-disk file was edited by the user.
	edited := "user: customized\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAgentHashMarker(dir, map[string]string{name: agentContentHash(content)}); err != nil {
		t.Fatal(err)
	}

	if err := syncAgentDefaults(dir); err != nil {
		t.Fatalf("syncAgentDefaults: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != edited {
		t.Fatalf("expected user edit to be preserved for %s, got %q", name, string(got))
	}
}

func TestSyncAgentDefaults_RemovesStaleBuiltins(t *testing.T) {
	dir := t.TempDir()
	ghost := filepath.Join(dir, "ghost.yaml")
	if err := os.WriteFile(ghost, []byte("name: ghost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAgentHashMarker(dir, map[string]string{"ghost.yaml": "cafebabecafebabe"}); err != nil {
		t.Fatal(err)
	}

	if err := syncAgentDefaults(dir); err != nil {
		t.Fatalf("syncAgentDefaults: %v", err)
	}

	if _, err := os.Stat(ghost); !os.IsNotExist(err) {
		t.Fatalf("expected stale built-in ghost.yaml to be removed, stat err=%v", err)
	}
	if _, ok := readAgentHashMarker(dir)["ghost.yaml"]; ok {
		t.Fatal("ghost.yaml should not remain in marker after removal")
	}
}

func TestSyncAgentDefaults_KeepsUserAuthoredAgents(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(dir, "myagent.yaml")
	body := "name: myagent\n"
	if err := os.WriteFile(custom, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := syncAgentDefaults(dir); err != nil {
		t.Fatalf("syncAgentDefaults: %v", err)
	}

	got, err := os.ReadFile(custom)
	if err != nil {
		t.Fatalf("user-authored agent should be kept: %v", err)
	}
	if string(got) != body {
		t.Fatalf("user-authored agent content changed: %q", string(got))
	}
}

// staleBuiltinYAML mimics an old on-disk built-in profile that still lists the
// removed `load_skills` tool — reproducing the reported startup failure
// `tool "load_skills" not registered`.
func staleBuiltinYAML(name string) string {
	agent := strings.TrimSuffix(name, ".yaml")
	return "name: " + agent + "\n" +
		"role: stale\n" +
		"tool_names:\n" +
		"  - list_files\n" +
		"  - list_skills\n" +
		"  - load_skills\n"
}

// TestSyncAgentDefaults_RegressionStaleLoadSkillsRemoved reproduces the original
// bug: a user machine already has built-in profiles on disk (referencing the
// removed load_skills tool) and no .hash marker. After sync the stale tool entry
// must be gone for every built-in agent.
func TestSyncAgentDefaults_RegressionStaleLoadSkillsRemoved(t *testing.T) {
	dir := t.TempDir()

	// Seed every built-in name with a stale version and NO marker (fresh upgrade).
	for name := range defaultAgentFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(staleBuiltinYAML(name)), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := syncAgentDefaults(dir); err != nil {
		t.Fatalf("syncAgentDefaults: %v", err)
	}

	for name := range defaultAgentFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(data), "- load_skills") {
			t.Fatalf("built-in %s still references removed load_skills tool after sync", name)
		}
	}

	// Loading the synced profiles must not surface load_skills in any ToolNames.
	profiles, err := LoadProfilesFromDir(dir)
	if err != nil {
		t.Fatalf("LoadProfilesFromDir: %v", err)
	}
	for _, p := range profiles {
		for _, tool := range p.ToolNames {
			if tool == "load_skills" {
				t.Fatalf("profile %q still resolves load_skills tool", p.Name)
			}
		}
	}
}

// TestSyncAgentDefaults_IdempotentNoRewrite verifies that a second sync with a
// matching marker does not rewrite unchanged built-in files. We pin an old mtime
// after the first sync; if the file is untouched the mtime survives.
func TestSyncAgentDefaults_IdempotentNoRewrite(t *testing.T) {
	dir := t.TempDir()
	name, _ := anyDefaultAgent(t)

	if err := syncAgentDefaults(dir); err != nil {
		t.Fatalf("first sync: %v", err)
	}

	agentPath := filepath.Join(dir, name)
	pinned := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := os.Chtimes(agentPath, pinned, pinned); err != nil {
		t.Fatal(err)
	}

	if err := syncAgentDefaults(dir); err != nil {
		t.Fatalf("second sync: %v", err)
	}

	info, err := os.Stat(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(pinned) {
		t.Fatalf("expected %s untouched on idempotent sync; mtime changed from %v to %v", name, pinned, info.ModTime())
	}
}

// TestSyncAgentDefaults_MissingFileRewrittenDespiteMatchingMarker covers the case
// where the marker says a file is up to date but the file was deleted on disk —
// sync must recreate it from the built-in content.
func TestSyncAgentDefaults_MissingFileRewrittenDespiteMatchingMarker(t *testing.T) {
	dir := t.TempDir()
	name, content := anyDefaultAgent(t)

	// Marker claims the current built-in hash, but the file does not exist.
	if err := writeAgentHashMarker(dir, map[string]string{name: agentContentHash(content)}); err != nil {
		t.Fatal(err)
	}

	if err := syncAgentDefaults(dir); err != nil {
		t.Fatalf("syncAgentDefaults: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("expected %s recreated: %v", name, err)
	}
	if string(got) != content {
		t.Fatalf("recreated %s does not match built-in content", name)
	}
}

