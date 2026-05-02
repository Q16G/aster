package builtin_tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"aster/internal/utils/argx"
)

const (
	readFileDefaultMaxBytes   int64 = 20000
	readFileLargeThreshold    int64 = 20000
	readFileMaxAllowedBytes   int64 = 10 * 1024 * 1024
	readFileBinaryDetectBytes       = 8192
)

type ReadFileTool struct{}

func NewReadFileTool() *ReadFileTool { return &ReadFileTool{} }

func (t *ReadFileTool) Name() string { return ReadFileToolName }

func (t *ReadFileTool) Description() string {
	return "读取指定文件内容。传入文件绝对路径即可，读不到直接报错。"
}

func (t *ReadFileTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "文件绝对路径",
			},
			"start_line": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "可选：起始行号（1-based，默认 1）",
			},
			"end_line": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "可选：结束行号（1-based，包含；默认读到文件末尾或达到 max_bytes）",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     10485760,
				"description": "可选：最大读取字节数（默认 20000，最大 10MB）",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := argx.RequiredText(args, "path")
	if err != nil {
		return "", err
	}
	absPath := filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
	if !filepath.IsAbs(absPath) {
		return "", fmt.Errorf("path must be an absolute path, got: %s", path)
	}

	f, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path is not a regular file")
	}

	maxBytes := readFileDefaultMaxBytes
	if v, ok := args["max_bytes"]; ok && v != nil {
		if n, ok := asInt64Any(v); ok && n > 0 {
			maxBytes = n
		}
	}
	if maxBytes <= 0 {
		maxBytes = 1
	}
	if maxBytes > readFileMaxAllowedBytes {
		maxBytes = readFileMaxAllowedBytes
	}

	if info.Size() > readFileLargeThreshold {
		head := make([]byte, readFileBinaryDetectBytes)
		n, _ := f.Read(head)
		if bytes.IndexByte(head[:n], 0) >= 0 {
			return "", fmt.Errorf("binary file is not supported")
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return "", fmt.Errorf("seek file: %w", err)
		}

		previewLimit := maxBytes
		if previewLimit > readFileLargeThreshold {
			previewLimit = readFileLargeThreshold
		}
		preview, err := io.ReadAll(io.LimitReader(f, previewLimit))
		if err != nil {
			return "", fmt.Errorf("read file preview: %w", err)
		}

		content := string(preview)
		omittedBytes := info.Size() - int64(len(preview))
		if omittedBytes < 0 {
			omittedBytes = 0
		}
		if omittedBytes > 0 {
			if strings.HasSuffix(content, "\n") {
				content += "..."
			} else {
				content += "\n..."
			}
		}

		message := ReadFileLargeTruncationMessage(readFileLargeThreshold, len(preview), omittedBytes)

		out, _ := json.Marshal(map[string]any{
			"ok":              true,
			"path":            absPath,
			"large_file":      true,
			"size_bytes":      info.Size(),
			"threshold_bytes": readFileLargeThreshold,
			"truncated":       true,
			"preview_bytes":   len(preview),
			"omitted_bytes":   omittedBytes,
			"content":         content,
			"message":         message,
		})
		return string(out), nil
	}

	startLine := int64(1)
	if v, ok := args["start_line"]; ok && v != nil {
		if n, ok := asInt64Any(v); ok && n > 0 {
			startLine = n
		}
	}
	if startLine <= 0 {
		startLine = 1
	}

	endLine := int64(0)
	if v, ok := args["end_line"]; ok && v != nil {
		if n, ok := asInt64Any(v); ok && n > 0 {
			endLine = n
		}
	}
	if endLine > 0 && endLine < startLine {
		return "", fmt.Errorf("end_line must be >= start_line")
	}

	// binary detect
	head := make([]byte, readFileBinaryDetectBytes)
	n, _ := f.Read(head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return "", fmt.Errorf("binary file is not supported")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek file: %w", err)
	}

	reader := bufio.NewReaderSize(f, 32*1024)
	var sb strings.Builder
	var written int64
	truncated := false

	lineNo := int64(1)
	for {
		if ctx != nil && ctx.Err() != nil {
			return "", ctx.Err()
		}

		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read file: %w", err)
		}

		if lineNo >= startLine && (endLine == 0 || lineNo <= endLine) {
			b := []byte(line)
			if bytes.IndexByte(b, 0) >= 0 {
				return "", fmt.Errorf("binary file is not supported")
			}

			remain := maxBytes - written
			if remain <= 0 {
				truncated = true
				break
			}
			if int64(len(b)) > remain {
				sb.Write(b[:remain])
				written += remain
				truncated = true
				break
			}
			sb.WriteString(line)
			written += int64(len(b))
		}

		if err == io.EOF {
			break
		}
		if endLine > 0 && lineNo >= endLine {
			break
		}
		lineNo++
	}

	result := map[string]any{
		"ok":         true,
		"path":       absPath,
		"start_line": startLine,
		"end_line":   endLine,
		"truncated":  truncated,
		"content":    sb.String(),
		"max_bytes":  maxBytes,
	}
	if truncated {
		if content, ok := result["content"].(string); ok {
			trimmed := strings.TrimRight(content, "\n")
			if trimmed == "" {
				result["content"] = "..."
			} else if strings.HasSuffix(trimmed, "...") {
				result["content"] = trimmed
			} else {
				result["content"] = trimmed + "\n..."
			}
		}
		result["message"] = ReadFileTruncationMessage(maxBytes)
	}
	out, _ := json.Marshal(result)
	return string(out), nil
}
