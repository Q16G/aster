package builtin_tools

import (
	"context"
	"fmt"
	"strings"
)

type WriteTool struct {
	observations *FileObservationStore
}

func NewWriteTool(store *FileObservationStore) *WriteTool {
	return &WriteTool{observations: store}
}

func (t *WriteTool) Name() string { return WriteToolName }

func (t *WriteTool) Description() string {
	return "新建文件或整体覆盖文件。覆盖已有文件前必须先用 read_file 完整读取该文件。"
}

func (t *WriteTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "要写入的文件绝对路径，必须是完整绝对路径。",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "要写入文件的完整内容。",
			},
		},
		"required":             []string{"file_path", "content"},
		"additionalProperties": false,
	}
}

func (t *WriteTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	filePath, err := rawStringArg(args, "file_path", true)
	if err != nil {
		return "", err
	}
	if args == nil || args["content"] == nil {
		return "", fmt.Errorf("content is required")
	}
	content, err := rawStringArg(args, "content", false)
	if err != nil {
		return "", err
	}
	absPath, err := resolveAbsoluteToolPath(filePath)
	if err != nil {
		return "", err
	}
	store := t.observations
	if store == nil {
		return "", fmt.Errorf("file observation store is unavailable")
	}

	snap, err := readTextFileSnapshot(absPath)
	if err != nil {
		return "", err
	}
	if err := validateReadBeforeWrite(store, absPath, snap); err != nil {
		return "", err
	}

	encoding := snap.Encoding
	newline := "LF"
	if err := writeTextFile(absPath, content, encoding, newline); err != nil {
		return "", err
	}
	recordPostWrite(store, absPath, content)

	writeType := "create"
	var originalFile *string
	if snap.Exists {
		writeType = "update"
		original := snap.Content
		originalFile = &original
	}
	payload := map[string]any{
		"type":            writeType,
		"filePath":        absPath,
		"content":         content,
		"structuredPatch": buildSimplePatch(firstNonNilString(originalFile), normalizeCRLF(content)),
		"originalFile":    originalFile,
	}
	return jsonResult(payload)
}

func firstNonNilString(s *string) string {
	if s == nil {
		return ""
	}
	return strings.ReplaceAll(*s, "\r\n", "\n")
}
