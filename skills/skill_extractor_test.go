package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func helperWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func helperFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestWriteManifest_SortedOutput(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, ".manifest")

	paths := map[string]struct{}{
		"pentest/agent-browser/SKILL.md":            {},
		"common/result-with-file/SKILL.md":          {},
		"code-audit/sast-scan/SKILL.md":             {},
		"host-defense/baseline-check/SKILL.md":      {},
		"code-audit/security-code-analysis/SKILL.md": {},
	}

	writeManifest(manifestPath, paths)

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(paths) {
		t.Fatalf("expected %d lines, got %d", len(paths), len(lines))
	}

	if !sort.StringsAreSorted(lines) {
		t.Fatalf("manifest lines not sorted: %v", lines)
	}

	for _, line := range lines {
		if _, ok := paths[line]; !ok {
			t.Fatalf("unexpected manifest line: %q", line)
		}
	}
}

func TestCleanStaleFiles_RemovesOldBuiltin(t *testing.T) {
	tmpDir := t.TempDir()

	// 模拟旧扁平结构的 builtin 文件
	helperWriteFile(t, filepath.Join(tmpDir, "sast-scan", "SKILL.md"), "old flat skill")
	helperWriteFile(t, filepath.Join(tmpDir, "dataflow-analysis", "SKILL.md"), "old flat skill")

	// 写旧 manifest
	oldManifest := "sast-scan/SKILL.md\ndataflow-analysis/SKILL.md\n"
	helperWriteFile(t, filepath.Join(tmpDir, ".manifest"), oldManifest)

	// 新 embedded 路径（已迁移到二级结构）
	newPaths := map[string]struct{}{
		"code-audit/sast-scan/SKILL.md":         {},
		"code-audit/dataflow-analysis/SKILL.md": {},
	}

	cleanStaleFiles(tmpDir, newPaths)

	// 旧文件应被删除
	if helperFileExists(filepath.Join(tmpDir, "sast-scan", "SKILL.md")) {
		t.Fatal("stale sast-scan/SKILL.md should have been removed")
	}
	if helperFileExists(filepath.Join(tmpDir, "dataflow-analysis", "SKILL.md")) {
		t.Fatal("stale dataflow-analysis/SKILL.md should have been removed")
	}
	// 旧空目录也应被清理
	if helperFileExists(filepath.Join(tmpDir, "sast-scan")) {
		t.Fatal("empty sast-scan/ dir should have been removed")
	}
}

func TestCleanStaleFiles_PreservesUserSkills(t *testing.T) {
	tmpDir := t.TempDir()

	// 用户自定义 skill（不在任何 manifest 中）
	helperWriteFile(t, filepath.Join(tmpDir, "my-custom-agent", "my-skill", "SKILL.md"), "user custom skill")

	// 一个旧 builtin 文件
	helperWriteFile(t, filepath.Join(tmpDir, "sast-scan", "SKILL.md"), "old builtin")
	oldManifest := "sast-scan/SKILL.md\n"
	helperWriteFile(t, filepath.Join(tmpDir, ".manifest"), oldManifest)

	newPaths := map[string]struct{}{
		"code-audit/sast-scan/SKILL.md": {},
	}

	cleanStaleFiles(tmpDir, newPaths)

	// 用户自定义 skill 必须保留
	if !helperFileExists(filepath.Join(tmpDir, "my-custom-agent", "my-skill", "SKILL.md")) {
		t.Fatal("user custom skill should NOT be removed")
	}
	// 旧 builtin 被删除
	if helperFileExists(filepath.Join(tmpDir, "sast-scan", "SKILL.md")) {
		t.Fatal("stale builtin should have been removed")
	}
}

func TestCleanStaleFiles_PreservesCurrentBuiltin(t *testing.T) {
	tmpDir := t.TempDir()

	// 当前版本的 builtin 文件
	helperWriteFile(t, filepath.Join(tmpDir, "code-audit", "sast-scan", "SKILL.md"), "current builtin")
	oldManifest := "code-audit/sast-scan/SKILL.md\n"
	helperWriteFile(t, filepath.Join(tmpDir, ".manifest"), oldManifest)

	// 新版本路径包含此文件（未变化）
	newPaths := map[string]struct{}{
		"code-audit/sast-scan/SKILL.md": {},
	}

	cleanStaleFiles(tmpDir, newPaths)

	// 仍在新路径集合中的文件不应被删除
	if !helperFileExists(filepath.Join(tmpDir, "code-audit", "sast-scan", "SKILL.md")) {
		t.Fatal("current builtin should NOT be removed")
	}
}

func TestCleanStaleFiles_NoManifest_NoDeletion(t *testing.T) {
	tmpDir := t.TempDir()

	// 首次安装场景：无 .manifest 文件
	helperWriteFile(t, filepath.Join(tmpDir, "my-agent", "my-skill", "SKILL.md"), "user skill")

	newPaths := map[string]struct{}{
		"code-audit/sast-scan/SKILL.md": {},
	}

	cleanStaleFiles(tmpDir, newPaths)

	// 没有 manifest 时不应删除任何文件
	if !helperFileExists(filepath.Join(tmpDir, "my-agent", "my-skill", "SKILL.md")) {
		t.Fatal("without manifest, no files should be removed")
	}
}

func TestCleanStaleFiles_NonEmptyParentDirPreserved(t *testing.T) {
	tmpDir := t.TempDir()

	// 旧 builtin 和用户文件共存于同一父目录
	helperWriteFile(t, filepath.Join(tmpDir, "mixed-dir", "SKILL.md"), "old builtin")
	helperWriteFile(t, filepath.Join(tmpDir, "mixed-dir", "notes.txt"), "user notes")

	oldManifest := "mixed-dir/SKILL.md\n"
	helperWriteFile(t, filepath.Join(tmpDir, ".manifest"), oldManifest)

	newPaths := map[string]struct{}{}

	cleanStaleFiles(tmpDir, newPaths)

	// SKILL.md 被删除
	if helperFileExists(filepath.Join(tmpDir, "mixed-dir", "SKILL.md")) {
		t.Fatal("stale SKILL.md should be removed")
	}
	// 父目录因含其他文件不应被删除
	if !helperFileExists(filepath.Join(tmpDir, "mixed-dir", "notes.txt")) {
		t.Fatal("parent dir with other files should be preserved")
	}
}

func TestCollectEmbeddedPaths_ReturnsNonEmpty(t *testing.T) {
	paths := collectEmbeddedPaths()
	if len(paths) == 0 {
		t.Fatal("expected non-empty embedded paths")
	}

	for p := range paths {
		if !strings.HasSuffix(strings.ToLower(p), "skill.md") {
			t.Fatalf("unexpected non-SKILL.md path: %q", p)
		}
		parts := strings.Split(p, "/")
		if len(parts) != 3 {
			t.Fatalf("expected <group>/<skill>/SKILL.md pattern, got %q", p)
		}
	}
}

func TestManifestRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, ".manifest")

	original := map[string]struct{}{
		"code-audit/sast-scan/SKILL.md":    {},
		"common/result-with-file/SKILL.md": {},
		"pentest/agent-browser/SKILL.md":   {},
	}

	writeManifest(manifestPath, original)

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	// 验证读回来的内容与写入的一致
	read := make(map[string]struct{})
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			read[line] = struct{}{}
		}
	}

	if len(read) != len(original) {
		t.Fatalf("roundtrip: expected %d paths, got %d", len(original), len(read))
	}
	for p := range original {
		if _, ok := read[p]; !ok {
			t.Fatalf("roundtrip: missing path %q", p)
		}
	}
}
