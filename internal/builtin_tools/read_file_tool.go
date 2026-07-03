package builtin_tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"strings"

	"aster/internal/utils/argx"
)

const (
	readFileLargeThreshold       int64 = 20000
	readFileDefaultLimit         int64 = 200
	readFileMaxLimit             int64 = 2000
	readFileTextPageMaxBytes     int64 = 20000
	readFileImageContextMaxBytes int64 = 5 * 1024 * 1024
	readFileBinaryDetectBytes          = 8192
)

type ReadFileTool struct {
	observations *FileObservationStore
}

func NewReadFileTool() *ReadFileTool { return &ReadFileTool{} }

func NewReadFileToolWithObservations(store *FileObservationStore) *ReadFileTool {
	return &ReadFileTool{observations: store}
}

func (t *ReadFileTool) Name() string { return ReadFileToolName }

func (t *ReadFileTool) Description() string {
	return "读取指定绝对路径。文本文件按 0-based offset/limit 行分页，目录按直接子项分页，常见图片会返回元数据并让 runtime 附加视觉上下文。"
}

func (t *ReadFileTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "目标绝对路径。支持文本文件、目录和常见图片。",
			},
			"offset": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"description": "可选：分页起点。文本文件按行偏移，目录按直接子项偏移；默认 0。",
			},
			"limit": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     readFileMaxLimit,
				"description": "可选：单页条数。文本文件按行数，目录按子项数；默认 200，最大 2000。",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	targetPath, err := argx.RequiredText(args, "path")
	if err != nil {
		return "", err
	}
	absPath, err := resolveAbsoluteToolPath(targetPath)
	if err != nil {
		return "", err
	}

	offset := int64(0)
	if v, ok := args["offset"]; ok && v != nil {
		if n, ok := asInt64Any(v); ok && n >= 0 {
			offset = n
		}
	}
	if offset < 0 {
		offset = 0
	}

	limit := readFileDefaultLimit
	if v, ok := args["limit"]; ok && v != nil {
		if n, ok := asInt64Any(v); ok && n > 0 {
			limit = n
		}
	}
	if limit <= 0 {
		limit = 1
	}
	if limit > readFileMaxLimit {
		limit = readFileMaxLimit
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return "", fmt.Errorf("stat path: %w", err)
	}
	if info.IsDir() {
		return readDirectoryPage(absPath, info, offset, limit)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path is not a regular file or directory")
	}

	f, err := os.Open(absPath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	head := make([]byte, readFileBinaryDetectBytes)
	n, _ := f.Read(head)
	head = head[:n]

	if imagePayload, ok := buildReadFileImagePayload(absPath, info, head); ok {
		out, _ := json.Marshal(imagePayload)
		return string(out), nil
	}

	if bytes.IndexByte(head, 0) >= 0 {
		return "", fmt.Errorf("binary file is not supported; supported kinds are text, directory, and image")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("seek file: %w", err)
	}
	return readTextPage(ctx, absPath, f, info, offset, limit, t.observations)
}

func readDirectoryPage(absPath string, _ os.FileInfo, offset, limit int64) (string, error) {
	dirEntries, err := os.ReadDir(absPath)
	if err != nil {
		return "", fmt.Errorf("read directory: %w", err)
	}

	totalEntries := int64(len(dirEntries))
	if offset > totalEntries {
		offset = totalEntries
	}
	end := offset + limit
	if end > totalEntries {
		end = totalEntries
	}

	entries := make([]FileEntry, 0, int(end-offset))
	for _, entry := range dirEntries[int(offset):int(end)] {
		if entry == nil {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return "", fmt.Errorf("stat directory entry %s: %w", entry.Name(), infoErr)
		}
		entries = append(entries, FileEntry{
			Name:    entry.Name(),
			Type:    classifyDirEntryType(info.Mode()),
			Size:    info.Size(),
			ModTime: info.ModTime().Format(timeRFC3339),
			Mode:    info.Mode().String(),
		})
	}

	hasMore := end < totalEntries
	payload := map[string]any{
		"ok":               true,
		"path":             absPath,
		"kind":             "directory",
		"page_unit":        "entries",
		"offset":           offset,
		"limit":            limit,
		"returned_entries": len(entries),
		"total_entries":    totalEntries,
		"has_more":         hasMore,
		"entries":          entries,
	}
	if hasMore {
		nextOffset := end
		payload["next_offset"] = nextOffset
		payload["message"] = ReadFilePageMessage(nextOffset, limit, "条目")
	}

	out, _ := json.Marshal(payload)
	return string(out), nil
}

func readTextPage(ctx context.Context, absPath string, f *os.File, info os.FileInfo, offset, limit int64, observations *FileObservationStore) (string, error) {
	reader := bufio.NewReaderSize(f, 32*1024)

	var (
		sb            strings.Builder
		totalLines    int64
		returnedLines int64
		pageBytes     int64
		truncated     bool
		collecting    = true
	)

	for {
		if ctx != nil && ctx.Err() != nil {
			return "", ctx.Err()
		}

		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read file: %w", err)
		}

		if len(line) > 0 {
			if bytes.IndexByte([]byte(line), 0) >= 0 {
				return "", fmt.Errorf("binary file is not supported; supported kinds are text, directory, and image")
			}

			lineIndex := totalLines
			totalLines++

			if collecting && lineIndex >= offset && returnedLines < limit {
				lineBytes := int64(len(line))
				if pageBytes+lineBytes > readFileTextPageMaxBytes {
					if returnedLines == 0 {
						prefix := line
						if int64(len(prefix)) > readFileTextPageMaxBytes {
							prefix = prefix[:readFileTextPageMaxBytes]
						}
						sb.WriteString(AppendTruncationMarker(prefix))
						returnedLines = 1
						pageBytes = int64(len(prefix))
						truncated = true
					}
					collecting = false
				} else {
					sb.WriteString(line)
					pageBytes += lineBytes
					returnedLines++
					if returnedLines >= limit {
						collecting = false
					}
				}
			}
		}

		if err == io.EOF {
			break
		}
	}

	if offset > totalLines {
		offset = totalLines
	}
	if returnedLines > totalLines-offset {
		returnedLines = totalLines - offset
	}

	nextOffset := offset + returnedLines
	hasMore := nextOffset < totalLines
	payload := map[string]any{
		"ok":             true,
		"path":           absPath,
		"kind":           "text",
		"page_unit":      "lines",
		"offset":         offset,
		"limit":          limit,
		"returned_lines": returnedLines,
		"total_lines":    totalLines,
		"has_more":       hasMore,
		"content":        sb.String(),
	}
	if hasMore {
		payload["next_offset"] = nextOffset
	}
	if truncated {
		payload["truncated"] = true
		payload["message"] = "read_file 当前页首行过长，已在单行内部截断并以 ... 标记；若需要精确定位内容，请优先使用 rg。"
	} else if hasMore {
		payload["message"] = ReadFilePageMessage(nextOffset, limit, "行")
	}

	if observations != nil && offset == 0 && !hasMore && !truncated {
		observations.Record(FileObservation{
			Path:           absPath,
			Content:        normalizeCRLF(sb.String()),
			ModTime:        info.ModTime(),
			ModTimeMillis:  info.ModTime().UnixMilli(),
			ReadCredential: true,
		})
	}

	out, _ := json.Marshal(payload)
	return string(out), nil
}

type readFileImagePayload struct {
	OK               bool   `json:"ok"`
	Path             string `json:"path"`
	Kind             string `json:"kind"`
	MIMEType         string `json:"mime_type"`
	SizeBytes        int64  `json:"size_bytes"`
	Width            int    `json:"width,omitempty"`
	Height           int    `json:"height,omitempty"`
	ContextAvailable bool   `json:"context_available"`
	Message          string `json:"message,omitempty"`
}

func buildReadFileImagePayload(absPath string, info os.FileInfo, head []byte) (*readFileImagePayload, bool) {
	mimeType := http.DetectContentType(head)
	if !isSupportedReadFileImageMIME(mimeType) {
		return nil, false
	}

	payload := &readFileImagePayload{
		OK:               true,
		Path:             absPath,
		Kind:             "image",
		MIMEType:         mimeType,
		SizeBytes:        info.Size(),
		ContextAvailable: info.Size() <= readFileImageContextMaxBytes,
	}

	if cfg, _, err := image.DecodeConfig(bytes.NewReader(head)); err == nil {
		payload.Width = cfg.Width
		payload.Height = cfg.Height
	}

	if payload.ContextAvailable {
		payload.Message = "图片已返回元数据，runtime 会附加视觉上下文供模型直接查看。"
	} else {
		payload.Message = fmt.Sprintf("图片大小超过 %dMB，本次仅返回元数据，未附加视觉上下文。", readFileImageContextMaxBytes/(1024*1024))
	}
	return payload, true
}

func isSupportedReadFileImageMIME(mimeType string) bool {
	switch strings.TrimSpace(strings.ToLower(mimeType)) {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func classifyDirEntryType(mode os.FileMode) string {
	if mode&os.ModeSymlink != 0 {
		return "symlink"
	}
	if mode.IsDir() {
		return "directory"
	}
	if mode.IsRegular() {
		return "file"
	}
	return "other"
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"
