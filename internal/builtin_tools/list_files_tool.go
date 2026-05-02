package builtin_tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aster/internal/utils/argx"
)

const (
	listFilesDefaultMaxResults    = 5000
	listFilesMaxResultsCap        = 20000
	listFilesDefaultMaxOutputByte = 20000
	listFilesMaxOutputByteCap     = 10 * 1024 * 1024
	listFilesDefaultMaxDepth      = 0
	listFilesMaxDepthCap          = 100
)

type FileEntry struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
	Mode    string `json:"mode"`
}

type ListFilesTool struct{}

func NewListFilesTool() *ListFilesTool { return &ListFilesTool{} }

func (t *ListFilesTool) Name() string { return ListFilesToolName }

func (t *ListFilesTool) Description() string {
	return "列出目录下的文件和目录。"
}

func (t *ListFilesTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "项目分析目录中的绝对路径，必须传完整全路径，禁止使用 .、..、../ 或其他相对路径。",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     listFilesMaxResultsCap,
				"description": "可选：最多返回多少条路径（默认 5000）",
			},
			"max_output_bytes": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     listFilesMaxOutputByteCap,
				"description": "可选：最大输出字节数（默认 20000，超出会自动截断并标记 truncated）",
			},
			"max_depth": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"maximum":     listFilesMaxDepthCap,
				"description": "可选：递归最大目录深度（默认 0，仅当前目录）",
			},

			"include_hidden": map[string]any{
				"type":        "boolean",
				"description": "可选：是否包含隐藏文件/目录（默认 false）",
			},
			"include_exts": map[string]any{
				"type":        "array",
				"description": "可选：仅包含指定后缀（例如 ['.go','.java']）",
				"items": map[string]any{
					"type": "string",
				},
			},
			"exclude_dirs": map[string]any{
				"type":        "array",
				"description": "可选：额外忽略的目录名（仅匹配目录名，不含路径）",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (t *ListFilesTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	targetPath, err := argx.RequiredText(args, "path")
	if err != nil {
		return "", err
	}
	startAbs, err := resolveAbsoluteToolPath(targetPath)
	if err != nil {
		return "", err
	}

	startInfo, err := os.Stat(startAbs)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}

	maxResults := listFilesDefaultMaxResults
	if v, ok := args["max_results"]; ok && v != nil {
		if n, ok := asInt64Any(v); ok && n > 0 {
			maxResults = int(n)
		}
	}
	if maxResults <= 0 {
		maxResults = 1
	}
	if maxResults > listFilesMaxResultsCap {
		maxResults = listFilesMaxResultsCap
	}

	maxOutputBytes := int64(listFilesDefaultMaxOutputByte)
	if v, ok := args["max_output_bytes"]; ok && v != nil {
		if n, ok := asInt64Any(v); ok && n > 0 {
			maxOutputBytes = n
		}
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = 1
	}
	if maxOutputBytes > listFilesMaxOutputByteCap {
		maxOutputBytes = listFilesMaxOutputByteCap
	}

	maxDepth := listFilesDefaultMaxDepth
	if v, ok := args["max_depth"]; ok && v != nil {
		if n, ok := asInt64Any(v); ok && n >= 0 {
			maxDepth = int(n)
		}
	}
	if maxDepth < 0 {
		maxDepth = 0
	}
	if maxDepth > listFilesMaxDepthCap {
		maxDepth = listFilesMaxDepthCap
	}

	includeHidden := false
	if v, ok := args["include_hidden"]; ok {
		if b, ok := v.(bool); ok {
			includeHidden = b
		}
	}

	includeExts := map[string]struct{}{}
	for _, s := range argx.StringSlice(args["include_exts"]) {
		if !strings.HasPrefix(s, ".") {
			s = "." + s
		}
		includeExts[strings.ToLower(s)] = struct{}{}
	}

	ignoreDirs := make(map[string]struct{}, len(defaultIgnoredDirNames))
	for k := range defaultIgnoredDirNames {
		ignoreDirs[k] = struct{}{}
	}
	for _, s := range argx.StringSlice(args["exclude_dirs"]) {
		ignoreDirs[s] = struct{}{}
	}

	entries := make([]FileEntry, 0, minInt(maxResults, 256))
	truncated := false

	if !startInfo.IsDir() {
		if startInfo.Mode().IsRegular() {
			ext := strings.ToLower(filepath.Ext(startAbs))
			if len(includeExts) == 0 || hasKey(includeExts, ext) {
				entries = append(entries, FileEntry{
					Name:    filepath.Base(startAbs),
					Type:    "file",
					Size:    startInfo.Size(),
					ModTime: startInfo.ModTime().Format(time.RFC3339),
					Mode:    startInfo.Mode().String(),
				})
			}
		}
		out := marshalListFilesWithLimit(startAbs, entries, truncated, maxOutputBytes)
		return string(out), nil
	}

	errStopWalk := errors.New("stop_walk")
	startDepthCount := pathDepth(startAbs)

	walkErr := filepath.WalkDir(startAbs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}

		if d == nil {
			return nil
		}

		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		name := d.Name()
		if !includeHidden && strings.HasPrefix(name, ".") {
			if name != "." {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}

		if d.IsDir() {
			if name != "" {
				if _, ok := ignoreDirs[name]; ok {
					return fs.SkipDir
				}
			}

			if p == startAbs {
				return nil
			}

			depthDiff := pathDepth(p) - startDepthCount
			if depthDiff == 1 {
				info, err := d.Info()
				if err != nil {
					return nil
				}

				rel, err := filepath.Rel(startAbs, p)
				if err != nil {
					return nil
				}

				entries = append(entries, FileEntry{
					Name:    filepath.ToSlash(rel),
					Type:    "directory",
					Size:    0,
					ModTime: info.ModTime().Format(time.RFC3339),
					Mode:    info.Mode().String(),
				})

				if len(entries) >= maxResults {
					truncated = true
					return errStopWalk
				}
			}

			// 默认仅列出当前目录（max_depth=0）；当 max_depth>0 时允许继续递归。
			if maxDepth <= 0 {
				return fs.SkipDir
			}
			if depthDiff > maxDepth {
				return fs.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(p))
		if len(includeExts) > 0 && !hasKey(includeExts, ext) {
			return nil
		}

		rel, err := filepath.Rel(startAbs, p)
		if err != nil {
			return nil
		}

		entries = append(entries, FileEntry{
			Name:    filepath.ToSlash(rel),
			Type:    "file",
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
			Mode:    info.Mode().String(),
		})

		if len(entries) >= maxResults {
			truncated = true
			return errStopWalk
		}
		return nil
	})
	if walkErr != nil {
		if !errors.Is(walkErr, errStopWalk) {
			return "", walkErr
		}
	}

	out := marshalListFilesWithLimit(startAbs, entries, truncated, maxOutputBytes)
	return string(out), nil
}

func marshalListFilesWithLimit(startPath string, entries []FileEntry, truncated bool, maxOutputBytes int64) []byte {
	if maxOutputBytes <= 0 {
		maxOutputBytes = 1
	}

	keep := len(entries)
	for keep >= 0 {
		trimmedByOutput := keep < len(entries)
		payload := map[string]any{
			"ok":               true,
			"path":             startPath,
			"entries":          entries[:keep],
			"truncated":        truncated || trimmedByOutput,
			"max_output_bytes": maxOutputBytes,
		}
		if trimmedByOutput {
			payload["message"] = ListFilesTruncationMessage(maxOutputBytes)
		}
		out, _ := json.Marshal(payload)
		if int64(len(out)) <= maxOutputBytes || keep == 0 {
			return out
		}
		keep--
	}

	// 理论上不会到达这里，兜底返回最小可用结构
	out, _ := json.Marshal(map[string]any{
		"ok":               true,
		"path":             startPath,
		"entries":          []FileEntry{},
		"truncated":        true,
		"max_output_bytes": maxOutputBytes,
		"message":          ListFilesTruncationMessage(maxOutputBytes),
	})
	return out
}

func pathDepth(p string) int {
	p = filepath.Clean(p)
	sep := string(os.PathSeparator)
	if p == sep {
		return 0
	}
	p = strings.TrimSuffix(p, sep)
	if p == "" || p == "." {
		return 0
	}
	return strings.Count(p, sep)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func hasKey(m map[string]struct{}, k string) bool {
	_, ok := m[k]
	return ok
}
