package builtin_tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"aster/internal/utils/argx"
)

const (
	rgCaptureMaxBytes       int64 = 10 * 1024 * 1024
	rgCaptureStderrMaxBytes       = int64(512 * 1024)
	rgMaxColumns                  = 500
)

var rgExcludedVCSDirs = []string{".git", ".svn", ".hg", ".bzr", ".jj", ".sl"}

type RgTool struct{}

type rgOutputMode string

const (
	rgOutputModeContent          rgOutputMode = "content"
	rgOutputModeFilesWithMatches rgOutputMode = "files_with_matches"
	rgOutputModeCount            rgOutputMode = "count"
)

type rgCountEntry struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

func NewRgTool() *RgTool { return &RgTool{} }

func (t *RgTool) Name() string { return RgToolName }

func (t *RgTool) Description() string {
	return "使用 ripgrep(`rg`) 搜索文件内容。文本搜索统一使用该工具，不要再切换到其他搜索命令。"
}

func (t *RgTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "搜索起点，可以是文件或目录；支持绝对路径，也支持相对当前 workspace/cwd 的路径",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "ripgrep 搜索模式",
			},
			"pattern_mode": map[string]any{
				"type":        "string",
				"description": "可选：模式类型，支持 regex/fixed/pcre2（默认 regex）",
				"enum":        []string{"regex", "fixed", "pcre2"},
			},
			"glob": map[string]any{
				"type":        "string",
				"description": "可选：glob 过滤（例如 '*.go' 或 '*.{ts,tsx}'）",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "可选：ripgrep 文件类型过滤（例如 go/java/ts/py）",
			},
			"output_mode": map[string]any{
				"type":        "string",
				"description": "可选：输出模式，支持 content/files_with_matches/count（默认 files_with_matches）",
				"enum":        []string{"content", "files_with_matches", "count"},
			},
			"context": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"description": "可选：前后统一上下文行数，仅在 content 模式生效",
			},
			"before_context": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"description": "可选：前置上下文行数，仅在 content 模式生效",
			},
			"after_context": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"description": "可选：后置上下文行数，仅在 content 模式生效",
			},
			"show_line_numbers": map[string]any{
				"type":        "boolean",
				"description": "可选：content 模式是否显示行号（默认 true）",
			},
			"case_insensitive": map[string]any{
				"type":        "boolean",
				"description": "可选：是否忽略大小写（默认 false）",
			},
			"multiline": map[string]any{
				"type":        "boolean",
				"description": "可选：是否启用跨行匹配（默认 false）",
			},
		},
		"required":             []string{"path", "pattern"},
		"additionalProperties": false,
	}
}

func (t *RgTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	searchPath, err := argx.RequiredText(args, "path")
	if err != nil {
		return "", err
	}
	pattern, err := argx.RequiredText(args, "pattern")
	if err != nil {
		return "", err
	}

	targetAbs, targetIsDir, err := resolveSearchTargetAbs(ctx, searchPath)
	if err != nil {
		return "", err
	}
	mode, err := parseRgOutputMode(args)
	if err != nil {
		return "", err
	}
	patternMode, err := parseRgPatternMode(args)
	if err != nil {
		return "", err
	}
	contextValue := intArgDefault(args, "context", -1)
	beforeContext := intArgDefault(args, "before_context", -1)
	afterContext := intArgDefault(args, "after_context", -1)
	showLineNumbers := boolArgDefault(args, "show_line_numbers", true)
	caseInsensitive := boolArgDefault(args, "case_insensitive", false)
	multiline := boolArgDefault(args, "multiline", false)
	if !multiline && strings.Contains(pattern, "\\n") {
		multiline = true
	}
	glob := argx.OptionalText(args, "glob")
	typeFilter := argx.OptionalText(args, "type")

	workDir, targetArg := rgExecutionRoot(targetAbs, targetIsDir)
	cmdArgs := buildRgCommandArgs(pattern, targetArg, mode, patternMode, glob, typeFilter, contextValue, beforeContext, afterContext, showLineNumbers, caseInsensitive, multiline)

	EmitToolRuntimeProgress(ctx, "prepare", "解析 rg 参数", map[string]any{
		"path":             targetAbs,
		"output_mode":      mode,
		"pattern_mode":     patternMode,
		"glob":             glob,
		"type":             typeFilter,
		"context":          contextValue,
		"before_context":   beforeContext,
		"after_context":    afterContext,
		"show_line_number": showLineNumbers,
		"case_insensitive": caseInsensitive,
		"multiline":        multiline,
	})

	rgCfg, err := RipgrepConfig()
	if err != nil {
		EmitToolRuntimeError(ctx, "未找到 rg 可执行文件", map[string]any{
			"phase": "resolve_executable",
			"error": err.Error(),
		})
		return "", err
	}
	EmitToolRuntimeProgress(ctx, "resolve_executable", "已定位 rg 可执行文件", map[string]any{
		"exe":  rgCfg.Command,
		"mode": rgCfg.Mode,
	})
	runArgs := append(append([]string{}, rgCfg.Args...), cmdArgs...)
	EmitToolRuntimeProgress(ctx, "run", "执行 rg 命令", map[string]any{
		"command": ToolRuntimeLabel(rgCfg.Command, runArgs),
		"cwd":     workDir,
		"mode":    rgCfg.Mode,
	})

	res := runCommandLimitedInDir(ctx, workDir, rgCfg.Command, runArgs, rgCaptureMaxBytes, rgCaptureStderrMaxBytes, bashDefaultWaitDelay)
	if err := validateCommandRunResult("rg", res, 0, 1); err != nil {
		EmitToolRuntimeError(ctx, "rg 执行失败", map[string]any{
			"phase":     "result",
			"error":     err.Error(),
			"command":   ToolRuntimeLabel(rgCfg.Command, runArgs),
			"cwd":       workDir,
			"exit_code": res.ExitCode,
			"stderr":    trimCapturedText(res.Stderr, res.StderrTruncated),
			"mode":      rgCfg.Mode,
		})
		return "", err
	}

	payload, err := buildRgResult(mode, targetAbs, pattern, res.Stdout, res.StdoutTruncated)
	if err != nil {
		return "", err
	}
	if res.StdoutTruncated || res.StderrTruncated {
		payload["truncated"] = true
		payload["truncated_reason"] = "capture_limit"
		payload["capture_limit_bytes"] = rgCaptureMaxBytes
		payload["message"] = RgTruncationMessage(rgCaptureMaxBytes)
	}

	EmitToolRuntimeProgress(ctx, "result", "rg 执行完成", map[string]any{
		"command":          ToolRuntimeLabel(rgCfg.Command, runArgs),
		"cwd":              workDir,
		"exit_code":        res.ExitCode,
		"stdout_bytes":     len(res.Stdout),
		"stderr_bytes":     len(res.Stderr),
		"stdout_truncated": res.StdoutTruncated,
		"stderr_truncated": res.StderrTruncated,
		"mode":             mode,
		"ripgrep_mode":     rgCfg.Mode,
	})

	out, _ := json.Marshal(payload)
	return string(out), nil
}

func parseRgOutputMode(args map[string]any) (rgOutputMode, error) {
	mode := strings.ToLower(strings.TrimSpace(argx.OptionalText(args, "output_mode")))
	if mode == "" {
		return rgOutputModeFilesWithMatches, nil
	}
	switch rgOutputMode(mode) {
	case rgOutputModeContent, rgOutputModeFilesWithMatches, rgOutputModeCount:
		return rgOutputMode(mode), nil
	default:
		return "", fmt.Errorf("unsupported rg output_mode: %s", mode)
	}
}

func parseRgPatternMode(args map[string]any) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(argx.OptionalText(args, "pattern_mode")))
	if mode == "" {
		return "regex", nil
	}
	switch mode {
	case "regex", "fixed", "pcre2":
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported rg pattern_mode: %s", mode)
	}
}

func buildRgCommandArgs(pattern string, targetArg string, mode rgOutputMode, patternMode string, glob string, typeFilter string, contextValue int, beforeContext int, afterContext int, showLineNumbers bool, caseInsensitive bool, multiline bool) []string {
	args := []string{"--hidden", "--color", "never", "--max-columns", strconv.Itoa(rgMaxColumns)}
	for _, dir := range rgExcludedVCSDirs {
		args = append(args, "--glob", "!"+dir, "--glob", "!"+dir+"/**", "--glob", "!**/"+dir+"/**")
	}

	switch patternMode {
	case "fixed":
		args = append(args, "-F")
	case "pcre2":
		args = append(args, "-P")
	}
	if multiline {
		args = append(args, "-U", "--multiline-dotall")
	}
	if caseInsensitive {
		args = append(args, "-i")
	}

	switch mode {
	case rgOutputModeFilesWithMatches:
		args = append(args, "-l")
	case rgOutputModeCount:
		args = append(args, "-c")
	case rgOutputModeContent:
		args = append(args, "-H")
		if showLineNumbers {
			args = append(args, "-n")
		}
		if contextValue >= 0 {
			args = append(args, "-C", strconv.Itoa(contextValue))
		} else {
			if beforeContext >= 0 {
				args = append(args, "-B", strconv.Itoa(beforeContext))
			}
			if afterContext >= 0 {
				args = append(args, "-A", strconv.Itoa(afterContext))
			}
		}
	}

	if strings.TrimSpace(typeFilter) != "" {
		args = append(args, "--type", strings.TrimSpace(typeFilter))
	}
	for _, item := range parseGlobPatterns(glob) {
		args = append(args, "--glob", item)
	}

	if strings.HasPrefix(pattern, "-") {
		args = append(args, "-e", pattern)
	} else {
		args = append(args, pattern)
	}
	args = append(args, targetArg)
	return args
}

func parseGlobPatterns(glob string) []string {
	glob = strings.TrimSpace(glob)
	if glob == "" {
		return nil
	}
	rawItems := strings.Fields(glob)
	items := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		if strings.Contains(raw, "{") && strings.Contains(raw, "}") {
			items = append(items, raw)
			continue
		}
		parts := strings.Split(raw, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				items = append(items, part)
			}
		}
	}
	return items
}

func buildRgResult(mode rgOutputMode, targetAbs string, pattern string, stdout string, stdoutTruncated bool) (map[string]any, error) {
	lines := splitOutputLines(stdout, stdoutTruncated)
	result := map[string]any{
		"ok":      true,
		"mode":    string(mode),
		"path":    targetAbs,
		"pattern": pattern,
	}
	switch mode {
	case rgOutputModeFilesWithMatches:
		files := make([]string, 0, len(lines))
		for _, line := range lines {
			files = append(files, normalizeOutputPath(line))
		}
		sort.Strings(files)
		result["num_files"] = len(files)
		result["filenames"] = files
	case rgOutputModeCount:
		counts := make([]rgCountEntry, 0, len(lines))
		total := 0
		for _, line := range lines {
			idx := strings.LastIndex(line, ":")
			if idx <= 0 || idx >= len(line)-1 {
				continue
			}
			n, err := strconv.Atoi(strings.TrimSpace(line[idx+1:]))
			if err != nil {
				continue
			}
			total += n
			counts = append(counts, rgCountEntry{
				Path:  normalizeOutputPath(line[:idx]),
				Count: n,
			})
		}
		sort.Slice(counts, func(i, j int) bool { return counts[i].Path < counts[j].Path })
		result["num_files"] = len(counts)
		result["num_matches"] = total
		result["counts"] = counts
	case rgOutputModeContent:
		content := strings.Join(lines, "\n")
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		result["content"] = content
		result["num_lines"] = len(lines)
	default:
		return nil, fmt.Errorf("unsupported rg output mode: %s", mode)
	}
	return result, nil
}

func splitOutputLines(stdout string, stdoutTruncated bool) []string {
	if strings.TrimSpace(stdout) == "" {
		return nil
	}
	lines := strings.Split(stdout, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	} else if stdoutTruncated && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}

func normalizeOutputPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	return filepath.ToSlash(path)
}

func resolveSearchTargetAbs(ctx context.Context, rawPath string) (abs string, isDir bool, err error) {
	rawPath = expandHomePath(rawPath)
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" || rawPath == "<nil>" {
		return "", false, fmt.Errorf("path is required")
	}

	base := ""
	if runtimeInfo, ok := GetToolRuntime(ctx); ok {
		base = strings.TrimSpace(runtimeInfo.WorkspaceRootDir)
	}
	if base == "" {
		base, err = os.Getwd()
		if err != nil {
			return "", false, fmt.Errorf("resolve cwd: %w", err)
		}
	}

	candidate := rawPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}
	abs, err = filepath.Abs(candidate)
	if err != nil {
		return "", false, fmt.Errorf("resolve path: %w", err)
	}
	abs = filepath.Clean(abs)
	if real, realErr := filepath.EvalSymlinks(abs); realErr == nil && strings.TrimSpace(real) != "" {
		abs = filepath.Clean(real)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", false, fmt.Errorf("stat path: %w", err)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("path must be a regular file or directory")
	}
	return abs, info.IsDir(), nil
}

func rgExecutionRoot(targetAbs string, targetIsDir bool) (workDir string, targetArg string) {
	if targetIsDir {
		return targetAbs, "."
	}
	return filepath.Dir(targetAbs), filepath.Base(targetAbs)
}

func intArgDefault(args map[string]any, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	if v, ok := args[key]; ok && v != nil {
		if n, ok := asInt64Any(v); ok {
			return int(n)
		}
	}
	return fallback
}

func boolArgDefault(args map[string]any, key string, fallback bool) bool {
	if args == nil {
		return fallback
	}
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch typed := v.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err != nil {
			return fallback
		}
		return parsed
	default:
		return fallback
	}
}

// runCommandLimitedInDir / validateCommandRunResult / trimCapturedText 已提取到 cmd_exec.go
// rg 内部保留函数别名以减少改动扩散
var runCommandLimitedInDir = RunCommandLimited
var validateCommandRunResult = ValidateCommandResult
var trimCapturedText = TrimCapturedText

func AppendTruncationMarker(text string) string {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return "..."
	}
	if strings.HasSuffix(trimmed, "...") {
		return trimmed
	}
	return trimmed + "\n..."
}
