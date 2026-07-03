package builtin_tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type NotebookEditTool struct {
	observations *FileObservationStore
}

func NewNotebookEditTool(store *FileObservationStore) *NotebookEditTool {
	return &NotebookEditTool{observations: store}
}

func (t *NotebookEditTool) Name() string { return NotebookEditToolName }

func (t *NotebookEditTool) Description() string {
	return "编辑 Jupyter notebook 的单个 cell，支持 replace、insert、delete。"
}

func (t *NotebookEditTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"notebook_path": map[string]any{
				"type":        "string",
				"description": "要编辑的 .ipynb 文件绝对路径。",
			},
			"cell_id": map[string]any{
				"type":        "string",
				"description": "要编辑的 cell id；插入时表示插到该 cell 后方，省略则插到开头。也支持 cell-N 索引。",
			},
			"new_source": map[string]any{
				"type":        "string",
				"description": "新的 cell source 内容。",
			},
			"cell_type": map[string]any{
				"type":        "string",
				"enum":        []string{"code", "markdown"},
				"description": "cell 类型。insert 时必填；replace 时省略则保留原类型。",
			},
			"edit_mode": map[string]any{
				"type":        "string",
				"enum":        []string{"replace", "insert", "delete"},
				"description": "编辑模式，默认 replace。",
			},
		},
		"required":             []string{"notebook_path", "new_source"},
		"additionalProperties": false,
	}
}

func (t *NotebookEditTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	notebookPath, err := rawStringArg(args, "notebook_path", true)
	if err != nil {
		return "", err
	}
	newSource, err := rawStringArg(args, "new_source", false)
	if err != nil {
		return "", err
	}
	cellID, _ := rawStringArg(args, "cell_id", false)
	cellType, _ := rawStringArg(args, "cell_type", false)
	editMode, _ := rawStringArg(args, "edit_mode", false)
	editMode = strings.TrimSpace(editMode)
	if editMode == "" {
		editMode = "replace"
	}
	cellType = strings.TrimSpace(cellType)

	absPath, err := resolveAbsoluteToolPath(notebookPath)
	if err != nil {
		return "", err
	}
	if strings.ToLower(filepath.Ext(absPath)) != ".ipynb" {
		return "", fmt.Errorf("File must be a Jupyter notebook (.ipynb file). For editing other file types, use the %s tool.", EditToolName)
	}
	switch editMode {
	case "replace", "insert", "delete":
	default:
		return "", fmt.Errorf("Edit mode must be replace, insert, or delete.")
	}
	if editMode == "insert" && cellType == "" {
		return "", fmt.Errorf("Cell type is required when using edit_mode=insert.")
	}
	if cellType != "" && cellType != "code" && cellType != "markdown" {
		return "", fmt.Errorf("Cell type must be code or markdown.")
	}
	if editMode != "insert" && strings.TrimSpace(cellID) == "" {
		return "", fmt.Errorf("Cell ID must be specified when not inserting a new cell.")
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
		return "", fmt.Errorf("Notebook file does not exist.")
	}
	if err := validateReadBeforeWrite(store, absPath, snap); err != nil {
		return "", err
	}

	var notebook map[string]any
	if err := json.Unmarshal([]byte(snap.Content), &notebook); err != nil {
		return notebookEditErrorResult(absPath, newSource, cellType, editMode, cellID, "Notebook is not valid JSON.")
	}
	rawCells, ok := notebook["cells"].([]any)
	if !ok {
		return notebookEditErrorResult(absPath, newSource, cellType, editMode, cellID, "Notebook is not valid JSON.")
	}

	cellIndex, err := resolveNotebookCellIndex(rawCells, cellID, editMode)
	if err != nil {
		return "", err
	}
	actualEditMode := editMode
	if actualEditMode == "replace" && cellIndex == len(rawCells) {
		actualEditMode = "insert"
		if cellType == "" {
			cellType = "code"
		}
	}

	language := notebookLanguage(notebook)
	newCellID := ""
	if notebookHasCellIDs(notebook) {
		if actualEditMode == "insert" {
			newCellID = randomNotebookCellID()
		} else {
			newCellID = strings.TrimSpace(cellID)
		}
	}

	switch actualEditMode {
	case "delete":
		rawCells = append(rawCells[:cellIndex], rawCells[cellIndex+1:]...)
	case "insert":
		newCell := newNotebookCell(cellType, newSource, newCellID)
		rawCells = append(rawCells[:cellIndex], append([]any{newCell}, rawCells[cellIndex:]...)...)
	case "replace":
		cell, ok := rawCells[cellIndex].(map[string]any)
		if !ok {
			return "", fmt.Errorf("Notebook cell is not valid JSON.")
		}
		cell["source"] = newSource
		if strings.TrimSpace(fmt.Sprint(cell["cell_type"])) == "code" {
			cell["execution_count"] = nil
			cell["outputs"] = []any{}
		}
		if cellType != "" && cellType != strings.TrimSpace(fmt.Sprint(cell["cell_type"])) {
			cell["cell_type"] = cellType
			if cellType == "code" {
				cell["execution_count"] = nil
				cell["outputs"] = []any{}
			} else {
				delete(cell, "execution_count")
				delete(cell, "outputs")
			}
		}
	}

	notebook["cells"] = rawCells
	updatedBytes, err := json.MarshalIndent(notebook, "", " ")
	if err != nil {
		return "", fmt.Errorf("marshal notebook: %w", err)
	}
	updatedContent := string(updatedBytes)
	if err := writeTextFile(absPath, updatedContent, snap.Encoding, snap.Newline); err != nil {
		return "", err
	}
	recordPostWrite(store, absPath, updatedContent)

	resultCellType := cellType
	if resultCellType == "" && actualEditMode == "replace" && cellIndex < len(rawCells) {
		if cell, ok := rawCells[cellIndex].(map[string]any); ok {
			resultCellType = strings.TrimSpace(fmt.Sprint(cell["cell_type"]))
		}
	}
	if resultCellType == "" {
		resultCellType = "code"
	}
	payload := map[string]any{
		"new_source":    newSource,
		"cell_id":       emptyStringToNil(newCellID),
		"cell_type":     resultCellType,
		"language":      language,
		"edit_mode":     actualEditMode,
		"notebook_path": absPath,
		"original_file": snap.Content,
		"updated_file":  updatedContent,
	}
	return jsonResult(payload)
}

func resolveNotebookCellIndex(cells []any, cellID string, editMode string) (int, error) {
	cellID = strings.TrimSpace(cellID)
	if cellID == "" {
		if editMode == "insert" {
			return 0, nil
		}
		return 0, fmt.Errorf("Cell ID must be specified when not inserting a new cell.")
	}
	for i, raw := range cells {
		cell, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(cell["id"])) == cellID {
			if editMode == "insert" {
				return i + 1, nil
			}
			return i, nil
		}
	}
	if idx, ok := parseCellID(cellID); ok {
		if idx < 0 || idx >= len(cells) {
			return 0, fmt.Errorf("Cell with index %d does not exist in notebook.", idx)
		}
		if editMode == "insert" {
			return idx + 1, nil
		}
		return idx, nil
	}
	return 0, fmt.Errorf("Cell with ID %q not found in notebook.", cellID)
}

func notebookLanguage(notebook map[string]any) string {
	meta, _ := notebook["metadata"].(map[string]any)
	langInfo, _ := meta["language_info"].(map[string]any)
	if lang := strings.TrimSpace(fmt.Sprint(langInfo["name"])); lang != "" && lang != "<nil>" {
		return lang
	}
	return "python"
}

func notebookHasCellIDs(notebook map[string]any) bool {
	nbformat := intFromAny(notebook["nbformat"])
	minor := intFromAny(notebook["nbformat_minor"])
	return nbformat > 4 || (nbformat == 4 && minor >= 5)
}

func intFromAny(v any) int {
	switch typed := v.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}

func randomNotebookCellID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "cell"
	}
	return hex.EncodeToString(buf)
}

func newNotebookCell(cellType string, source string, id string) map[string]any {
	if cellType == "markdown" {
		cell := map[string]any{
			"cell_type": "markdown",
			"source":    source,
			"metadata":  map[string]any{},
		}
		if id != "" {
			cell["id"] = id
		}
		return cell
	}
	cell := map[string]any{
		"cell_type":       "code",
		"source":          source,
		"metadata":        map[string]any{},
		"execution_count": nil,
		"outputs":         []any{},
	}
	if id != "" {
		cell["id"] = id
	}
	return cell
}

func notebookEditErrorResult(path, newSource, cellType, editMode, cellID, message string) (string, error) {
	if cellType == "" {
		cellType = "code"
	}
	if editMode == "" {
		editMode = "replace"
	}
	payload := map[string]any{
		"new_source":    newSource,
		"cell_id":       emptyStringToNil(cellID),
		"cell_type":     cellType,
		"language":      "python",
		"edit_mode":     editMode,
		"error":         message,
		"notebook_path": path,
		"original_file": "",
		"updated_file":  "",
	}
	return jsonResult(payload)
}

func emptyStringToNil(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}
