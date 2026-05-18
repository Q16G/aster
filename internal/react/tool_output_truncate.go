package react

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	toolOutputTruncateMaxLines = 2000
	toolOutputTruncateMaxBytes = 50 * 1024
	toolOutputRetention        = 7 * 24 * time.Hour
)

func TruncateToolOutput(toolName string, output string, workspaceRootDir string) (string, bool) {
	if strings.TrimSpace(output) == "" {
		return output, false
	}

	res, err := truncateToolOutput(output, resolveToolOutputDir(workspaceRootDir))
	if err != nil {
		return output, false
	}
	return res.Content, res.Truncated
}

type toolOutputResult struct {
	Content    string
	Truncated  bool
	OutputPath string
}

func resolveToolOutputDir(workspaceRootDir string) string {
	workspaceRootDir = strings.TrimSpace(workspaceRootDir)
	if workspaceRootDir != "" {
		return filepath.Join(workspaceRootDir, "tool-output")
	}
	return filepath.Join(os.TempDir(), "sastpro-tool-output")
}

func truncateToolOutput(output string, outputDir string) (toolOutputResult, error) {
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

	absDir, err := filepath.Abs(filepath.Clean(outputDir))
	if err != nil {
		return toolOutputResult{}, fmt.Errorf("resolve output dir %s failed: %w", outputDir, err)
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return toolOutputResult{}, fmt.Errorf("create output dir %s failed: %w", absDir, err)
	}
	_ = cleanupToolOutputDir(absDir, time.Now())

	outputPath := filepath.Join(absDir, fmt.Sprintf("%d-%s.txt", time.Now().UnixMilli(), uuid.NewString()))
	if err := os.WriteFile(outputPath, []byte(output), 0o644); err != nil {
		return toolOutputResult{}, fmt.Errorf("write %s failed: %w", outputPath, err)
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

func cleanupToolOutputDir(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
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
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
	return nil
}
