package builtin_tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

type EditTool struct {
	observations *FileObservationStore
}

func NewEditTool(store *FileObservationStore) *EditTool {
	return &EditTool{observations: store}
}

func (t *EditTool) Name() string { return EditToolName }

func (t *EditTool) Description() string {
	return "精确字符串替换文件内容。编辑已有文件前必须先用 read_file 完整读取该文件。"
}

func (t *EditTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "要修改的文件绝对路径。",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "要被替换的原始文本。",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "替换后的新文本，必须与 old_string 不同。",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "是否替换所有匹配项，默认 false。",
			},
		},
		"required":             []string{"file_path", "old_string", "new_string"},
		"additionalProperties": false,
	}
}

func (t *EditTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	filePath, err := rawStringArg(args, "file_path", true)
	if err != nil {
		return "", err
	}
	oldString, err := rawStringArg(args, "old_string", false)
	if err != nil {
		return "", err
	}
	newString, err := rawStringArg(args, "new_string", false)
	if err != nil {
		return "", err
	}
	replaceAll := optionalBoolArg(args, "replace_all")
	if oldString == newString {
		return "", fmt.Errorf("No changes to make: old_string and new_string are exactly the same.")
	}
	absPath, err := resolveAbsoluteToolPath(filePath)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(filepath.Ext(absPath), ".ipynb") {
		return "", fmt.Errorf("File is a Jupyter Notebook. Use the %s tool to edit this file.", NotebookEditToolName)
	}
	store := t.observations
	if store == nil {
		return "", fmt.Errorf("file observation store is unavailable")
	}
	snap, err := readTextFileSnapshot(absPath)
	if err != nil {
		return "", err
	}
	if !snap.Exists {
		if oldString == "" {
			if err := writeTextFile(absPath, newString, "utf8", "LF"); err != nil {
				return "", err
			}
			recordPostWrite(store, absPath, newString)
			payload := map[string]any{
				"filePath":        absPath,
				"oldString":       oldString,
				"newString":       newString,
				"originalFile":    "",
				"structuredPatch": buildSimplePatch("", newString),
				"userModified":    false,
				"replaceAll":      replaceAll,
			}
			return jsonResult(payload)
		}
		return "", fmt.Errorf("File does not exist.")
	}
	if oldString == "" && strings.TrimSpace(snap.Content) != "" {
		return "", fmt.Errorf("Cannot create new file - file already exists.")
	}
	if err := validateReadBeforeWrite(store, absPath, snap); err != nil {
		return "", err
	}

	actualOldString := findActualString(snap.Content, oldString)
	if actualOldString == "" && oldString != "" {
		return "", fmt.Errorf("String to replace not found in file.\nString: %s", oldString)
	}
	if oldString == "" {
		actualOldString = oldString
	}
	matches := countOccurrences(snap.Content, actualOldString)
	if matches > 1 && !replaceAll {
		return "", fmt.Errorf("Found %d matches of the string to replace, but replace_all is false. To replace all occurrences, set replace_all to true. To replace only one occurrence, please provide more context to uniquely identify the instance.\nString: %s", matches, oldString)
	}

	actualNewString := preserveQuoteStyle(oldString, actualOldString, newString)
	updated := ""
	if oldString == "" {
		updated = actualNewString
	} else {
		updated = applyEditToFile(snap.Content, actualOldString, actualNewString, replaceAll)
	}
	if updated == snap.Content {
		return "", fmt.Errorf("Original and edited file match exactly. Failed to apply edit.")
	}
	if err := writeTextFile(absPath, updated, snap.Encoding, snap.Newline); err != nil {
		return "", err
	}
	recordPostWrite(store, absPath, updated)
	payload := map[string]any{
		"filePath":        absPath,
		"oldString":       actualOldString,
		"newString":       newString,
		"originalFile":    snap.Content,
		"structuredPatch": buildSimplePatch(snap.Content, updated),
		"userModified":    false,
		"replaceAll":      replaceAll,
	}
	return jsonResult(payload)
}
