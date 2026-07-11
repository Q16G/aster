package react

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aster/internal/workspacefs"
)

// I03：过期清理语义——mtime 早于保留期（7 天）的外置输出被删，期内的保留。
func TestCleanupToolOutputDir_RetentionWindow(t *testing.T) {
	root := t.TempDir()
	const relDir = "tool-output"
	store, err := workspacefs.NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}

	now := time.Now()
	files := map[string]time.Time{
		"expired.txt": now.Add(-8 * 24 * time.Hour),
		"fresh.txt":   now.Add(-1 * 24 * time.Hour),
	}
	for name, mtime := range files {
		if err := store.Write(relDir+"/"+name, []byte("payload")); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		abs := filepath.Join(root, relDir, name)
		if err := os.Chtimes(abs, mtime, mtime); err != nil {
			t.Fatalf("chtimes %s: %v", name, err)
		}
	}

	if err := cleanupToolOutputDir(store, relDir, now); err != nil {
		t.Fatalf("cleanupToolOutputDir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, relDir, "expired.txt")); !os.IsNotExist(err) {
		t.Fatalf("过期文件应被清理，err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, relDir, "fresh.txt")); err != nil {
		t.Fatalf("保留期内文件不应被清理: %v", err)
	}
}

// 截断落盘路径 / 内容锚点：outputPath 为 workspace 下 tool-output/ 绝对路径，全量内容落盘。
func TestTruncateToolOutput_WritesFullOutputUnderWorkspace(t *testing.T) {
	root := t.TempDir()
	output := strings.Repeat("tool output line\n", toolOutputTruncateMaxLines+100)

	res, err := truncateToolOutput(output, root, "tool-output")
	if err != nil {
		t.Fatalf("truncateToolOutput: %v", err)
	}
	if !res.Truncated {
		t.Fatal("超行数输出应被截断")
	}
	wantDir := filepath.Join(root, "tool-output") + string(filepath.Separator)
	if !strings.HasPrefix(res.OutputPath, wantDir) {
		t.Fatalf("OutputPath 应在 %s 下，got %s", wantDir, res.OutputPath)
	}
	data, err := os.ReadFile(res.OutputPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if string(data) != output {
		t.Fatal("落盘文件应为完整原始输出")
	}
	if !strings.Contains(res.Content, res.OutputPath) {
		t.Fatal("截断提示应内联落盘文件绝对路径")
	}
}
