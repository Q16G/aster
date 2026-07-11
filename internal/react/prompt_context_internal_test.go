package react

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aster/internal/builtin_tools"
	"aster/internal/workspacefs"
)

// promptBodyTokens 取 preview 产出中标记串之前的正文 token 数（未截断则全文）。
func promptBodyTokens(s string) int {
	if i := strings.Index(s, promptTruncatedMarker); i >= 0 {
		return countTokens(strings.TrimSpace(s[:i]))
	}
	return countTokens(s)
}

func newPromptContextTestAgent(t *testing.T, usableInputTokens int) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	return &Agent{
		workspaceRuntime:  mustRuntime(t, dir),
		workspaceRootDir:  dir,
		usableInputTokens: usableInputTokens,
	}, dir
}

// TestBuildPromptContextFieldsWithinLimit 验证：超长内存/文件字段的 preview 正文
// token 数均 ≤ promptPreviewTokens 上限。
func TestBuildPromptContextFieldsWithinLimit(t *testing.T) {
	a, dir := newPromptContextTestAgent(t, 5000) // limit = 100 tokens
	limit := promptPreviewTokens(a.usableInputTokens)

	longLine := strings.Repeat("很长的中文账本条目内容，", 8)
	var timeline []*builtin_tools.TimelineInput
	for i := 0; i < 60; i++ {
		timeline = append(timeline, &builtin_tools.TimelineInput{Content: longLine, CreatedAt: time.Unix(int64(1700000000+i), 0)})
	}
	var plan []*builtin_tools.PlanItem
	for i := 0; i < 40; i++ {
		plan = append(plan, &builtin_tools.PlanItem{ID: fmt.Sprintf("s%d", i), Step: longLine})
	}
	var outcomes []*builtin_tools.StepOutcome
	for i := 0; i < 40; i++ {
		outcomes = append(outcomes, &builtin_tools.StepOutcome{StepID: fmt.Sprintf("s%d", i), Summary: longLine})
	}
	snapshot := builtin_tools.StateSnapshot{
		InputTimeline:     timeline,
		GoalUnderstanding: strings.Repeat(longLine+"\n", 40),
		Plan:              plan,
		StepOutcomes:      outcomes,
		Warnings:          []string{strings.Repeat(longLine, 40)},
	}
	mustWriteShared(t, dir, taskContextFileName, strings.Repeat("事实板长内容行\n", 400))
	mustWriteShared(t, dir, openItemsFileName, strings.Repeat("OI-001 账本长内容行\n", 400))

	pc := a.buildPromptContext(snapshot, "")
	fields := map[string]string{
		"InputTimeline":     pc.InputTimeline,
		"GoalUnderstanding": pc.GoalUnderstanding,
		"Plan":              pc.Plan,
		"StepOutcomes":      pc.StepOutcomes,
		"Warnings":          pc.Warnings,
		"TaskContextBoard":  pc.TaskContextBoard,
		"OpenItemsLedger":   pc.OpenItemsLedger,
	}
	for name, v := range fields {
		if v == "" {
			t.Errorf("%s 不应为空", name)
			continue
		}
		if n := promptBodyTokens(v); n > limit {
			t.Errorf("%s preview 正文 %d tokens 超上限 %d", name, n, limit)
		}
	}
}

// TestBuildPromptContextPointerTargets 验证指针目标：文件字段指原文件、
// Plan/StepOutcomes 指既有真相源（planner.jsonl / step_contexts.jsonl），均不 spill；
// 内存字段（InputTimeline 等）spill 到 shared/prompt_context/<field>.md 且全量无损。
func TestBuildPromptContextPointerTargets(t *testing.T) {
	a, dir := newPromptContextTestAgent(t, 3000) // limit = 60 tokens

	longLine := strings.Repeat("很长的中文内容条目，", 8)
	var timeline []*builtin_tools.TimelineInput
	for i := 0; i < 60; i++ {
		timeline = append(timeline, &builtin_tools.TimelineInput{Content: longLine})
	}
	var plan []*builtin_tools.PlanItem
	for i := 0; i < 40; i++ {
		plan = append(plan, &builtin_tools.PlanItem{ID: fmt.Sprintf("s%d", i), Step: longLine})
	}
	var outcomes []*builtin_tools.StepOutcome
	for i := 0; i < 40; i++ {
		outcomes = append(outcomes, &builtin_tools.StepOutcome{StepID: fmt.Sprintf("s%d", i), Summary: longLine})
	}
	snapshot := builtin_tools.StateSnapshot{InputTimeline: timeline, Plan: plan, StepOutcomes: outcomes}
	mustWriteShared(t, dir, taskContextFileName, strings.Repeat("事实板长内容行\n", 400))

	pc := a.buildPromptContext(snapshot, "")

	// 文件字段指针 → 原文件绝对路径，且不产生 spill 文件。
	boardPath := filepath.Join(dir, "shared", taskContextFileName)
	if !strings.Contains(pc.TaskContextBoard, boardPath) {
		t.Errorf("TaskContextBoard 指针应指原文件 %s", boardPath)
	}
	if _, err := os.Stat(filepath.Join(dir, "shared", promptContextDirName, "task_context.md")); err == nil {
		t.Errorf("文件字段不应 spill")
	}

	// Plan / StepOutcomes 指针 → 既有真相源。
	if p := workspacefs.New(dir, "").PlannerJournal(); !strings.Contains(pc.Plan, p) {
		t.Errorf("Plan 指针应指 %s，got 尾部 %q", p, tail(pc.Plan, 120))
	}
	if p := workspacefs.New(dir, "").StepContexts(); !strings.Contains(pc.StepOutcomes, p) {
		t.Errorf("StepOutcomes 指针应指 %s", p)
	}

	// 内存字段 spill：全量无损 + 指针路径正确。
	spillPath := filepath.Join(dir, "shared", promptContextDirName, "input_timeline.md")
	data, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("InputTimeline 超长应 spill 到 %s：%v", spillPath, err)
	}
	if string(data) != formatInputTimelineLines(timeline) {
		t.Errorf("spill 内容应为全量无损原文")
	}
	if !strings.Contains(pc.InputTimeline, spillPath) {
		t.Errorf("InputTimeline 指针应指 spill 文件 %s", spillPath)
	}
}

// TestBuildPromptContextEmptyGates 验证缺失/空来源一律空串（HAS gate 语义），
// 不注「(文件为空)」占位。
func TestBuildPromptContextEmptyGates(t *testing.T) {
	a, _ := newPromptContextTestAgent(t, 5000)
	pc := a.buildPromptContext(builtin_tools.StateSnapshot{}, "")
	fields := map[string]string{
		"InputTimeline":     pc.InputTimeline,
		"GoalUnderstanding": pc.GoalUnderstanding,
		"Plan":              pc.Plan,
		"Phases":            pc.Phases,
		"StepOutcomes":      pc.StepOutcomes,
		"Warnings":          pc.Warnings,
		"ReplanContext":     pc.ReplanContext,
		"RecoveryContext":   pc.RecoveryContext,
		"TaskContextBoard":  pc.TaskContextBoard,
		"OpenItemsLedger":   pc.OpenItemsLedger,
		"PlannerJournal":    pc.PlannerJournal,
		"StepFileContent":   pc.StepFileContent,
	}
	for name, v := range fields {
		if v != "" {
			t.Errorf("%s 缺失来源应为空串，got %q", name, tail(v, 60))
		}
	}
}

// TestPreviewMemoryFieldSpillFallback 验证 spill 失败（无 runtime）时注全文兜底。
func TestPreviewMemoryFieldSpillFallback(t *testing.T) {
	a := &Agent{usableInputTokens: 3000} // 无 workspaceRuntime → spill 必然失败
	long := strings.Repeat("超长内容行需要截断处理\n", 200)
	got := a.previewMemoryField("warnings", long, 10)
	if got != strings.TrimSpace(long) {
		t.Errorf("spill 失败应注全文兜底（不截断不给指针）")
	}
}

// TestPreviewMemoryFieldWithinLimit 验证 ≤limit 原样返回、不落 spill 文件。
func TestPreviewMemoryFieldWithinLimit(t *testing.T) {
	a, dir := newPromptContextTestAgent(t, 5000)
	got := a.previewMemoryField("warnings", "短告警内容", 100)
	if got != "短告警内容" {
		t.Errorf("limit 内应原样返回，got %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "shared", promptContextDirName, "warnings.md")); err == nil {
		t.Errorf("limit 内不应落 spill 文件")
	}
}

func mustWriteShared(t *testing.T, rootDir, name, content string) {
	t.Helper()
	sharedDir := filepath.Join(rootDir, "shared")
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedDir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
