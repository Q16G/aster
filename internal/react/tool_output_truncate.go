package react

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"aster/internal/workspacefs"
)

const (
	toolOutputTruncateMaxLines = 2000
	toolOutputTruncateMaxBytes = 50 * 1024
	toolOutputRetention        = 7 * 24 * time.Hour
)

// TruncateToolOutput 截断超长工具输出；截断时完整输出落盘并返回文件路径（第三个返回值）。
func TruncateToolOutput(toolName string, output string, workspaceRootDir string) (string, bool, string) {
	if strings.TrimSpace(output) == "" {
		return output, false, ""
	}

	root, relDir := resolveToolOutputTarget(workspaceRootDir)
	res, err := truncateToolOutput(output, root, relDir)
	if err != nil {
		return output, false, ""
	}
	return res.Content, res.Truncated, res.OutputPath
}

type toolOutputResult struct {
	Content    string
	Truncated  bool
	OutputPath string
}

// resolveToolOutputTarget 返回工具输出落盘位置 (root, relDir)。
// workspace root 缺失时回退系统临时目录——非 workspace 路径，Store 在此仅作
// 带防穿越与 per-key 锁的本地 IO 原语复用（root 换成 TempDir）。
func resolveToolOutputTarget(workspaceRootDir string) (root, relDir string) {
	l := workspacefs.New(workspaceRootDir, "")
	if l.ToolOutputDir() != "" {
		return l.Root, l.ToolOutputDirRel()
	}
	return os.TempDir(), "sastpro-tool-output"
}

func truncateToolOutput(output string, root, relDir string) (toolOutputResult, error) {
	lines := strings.Split(output, "\n")
	totalBytes := len([]byte(output))
	if len(lines) <= toolOutputTruncateMaxLines && totalBytes <= toolOutputTruncateMaxBytes {
		return toolOutputResult{Content: output}, nil
	}

	out := make([]string, 0, toolOutputTruncateMaxLines)
	bytesUsed := 0
	hitBytes := false
	for idx := 0; idx < len(lines) && idx < toolOutputTruncateMaxLines; idx++ {
		size := len([]byte(lines[idx]))
		if idx > 0 {
			size += 1
		}
		if bytesUsed+size > toolOutputTruncateMaxBytes {
			hitBytes = true
			break
		}
		out = append(out, lines[idx])
		bytesUsed += size
	}

	removed := 0
	unit := "lines"
	if hitBytes {
		removed = totalBytes - bytesUsed
		unit = "bytes"
	} else {
		removed = len(lines) - len(out)
	}
	preview := strings.Join(out, "\n")

	store, err := workspacefs.NewLocalStore(root)
	if err != nil {
		return toolOutputResult{}, fmt.Errorf("resolve output root %s failed: %w", root, err)
	}
	if err := store.EnsureDir(relDir); err != nil {
		return toolOutputResult{}, fmt.Errorf("create output dir %s failed: %w", relDir, err)
	}
	_ = cleanupToolOutputDir(store, relDir, time.Now())

	rel := relDir + "/" + fmt.Sprintf("%d-%s.txt", time.Now().UnixMilli(), uuid.NewString())
	if err := store.Write(rel, []byte(output)); err != nil {
		return toolOutputResult{}, fmt.Errorf("write %s failed: %w", rel, err)
	}
	outputPath, err := store.AbsPath(rel)
	if err != nil {
		return toolOutputResult{}, fmt.Errorf("resolve %s failed: %w", rel, err)
	}

	content := fmt.Sprintf(
		"%s\n\n...%d %s truncated...\n\nThe tool call succeeded but the output was truncated. Full output saved to: %s\nUse Grep to search the full content or Read with offset/limit to view specific sections.",
		preview,
		removed,
		unit,
		outputPath,
	)
	return toolOutputResult{
		Content:    content,
		Truncated:  true,
		OutputPath: outputPath,
	}, nil
}

// cleanupToolOutputDir 清理 relDir 下超过保留期（mtime 早于 7 天前）的外置输出文件。
func cleanupToolOutputDir(store workspacefs.Store, relDir string, now time.Time) error {
	entries, err := store.List(relDir)
	if err != nil {
		return err
	}
	cutoff := now.Add(-toolOutputRetention)
	for _, entry := range entries {
		if entry == nil || entry.IsDir() {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		_ = store.Remove(relDir + "/" + entry.Name())
	}
	return nil
}
