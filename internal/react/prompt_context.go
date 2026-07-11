package react

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aster/internal/builtin_tools"
)

// promptPreviewRatio 单个 prompt 注入块的 preview 上限占「可用输入预算」的比例。
// 纯百分比、无 floor/ceil：大窗口下账本类字段 preview 自然更大、缓解截断；小窗口下
// 自动收窄，使 parts 从源头有界（这本身即溢出兜底）。取 2% 的定标见 docs 决策 ④。
const promptPreviewRatio = 0.02

// promptPreviewTokens 返回单个注入块的 preview token 上限 = usableInputTokens × ratio。
// 基准用 usableInputTokens（= 窗口 − 输出预留）而非整窗口——preview 属 input。
// usableInputTokens <= 0 时兜底同口径：默认窗口减默认输出预留（而非整窗口）。
func promptPreviewTokens(usableInputTokens int) int {
	uit := usableInputTokens
	if uit <= 0 {
		uit = defaultContextWindowTokens - DefaultOutputReserveTokens
	}
	return int(float64(uit) * promptPreviewRatio)
}

// promptTruncatedMarker 是 preview 截断/外置标记串前缀；isTruncatedForPrompt 据此判定。
const promptTruncatedMarker = "（内容超长"

// previewForPrompt 是统一的 prompt 注入块 preview helper：content 的 token 数 ≤ limitTokens
// 时返回原文；超限时在 token/UTF-8 边界截断并追加指针文案（工具名用 builtin 实名 read_file）。
// limitTokens <= 0 不截断。limit 连指针文案本身都放不下时全指针化——指针是不可压缩的
// 固定开销，此时再塞正文只会更超；判据显式比较 limit 与指针 token 成本（见 docs 决策④）。
// 截断维度按 token（复用 countTokens）而非字节，消除中文「字节≠token」偏差。
func previewForPrompt(raw string, absPath string, limitTokens int) string {
	content := strings.TrimSpace(raw)
	if content == "" {
		return "(文件为空)"
	}
	if limitTokens <= 0 || countTokens(content) <= limitTokens {
		return content
	}
	pointer := pointerOnlyForPrompt(absPath)
	if limitTokens < countTokens(pointer) {
		return pointer
	}
	truncated := truncateToTokenBudget(content, limitTokens)
	if strings.TrimSpace(truncated) == "" {
		return pointer
	}
	return truncated + "\n\n" + promptTruncatedMarker + "，仅显示前 " +
		fmt.Sprintf("%d", countTokens(truncated)) + " tokens；完整内容见文件：" +
		absPath + "，维护/验收前请先用 read_file 读取全量。）"
}

// pointerOnlyForPrompt 生成纯指针文案（limit 放不下任何正文时用）。
func pointerOnlyForPrompt(absPath string) string {
	return promptTruncatedMarker + "，完整内容见文件：" + absPath + "，请用 read_file 读取全量。）"
}

// truncateToTokenBudget 把 content 截到 token 数 ≤ limitTokens，尽量落在换行/UTF-8 边界。
// 先按「字节 × limit/total」比例估初始截点，再按 token 实测收缩兜底（tokenizer 非线性）。
func truncateToTokenBudget(content string, limitTokens int) string {
	total := countTokens(content)
	if total <= limitTokens {
		return content
	}
	cutByte := len(content) * limitTokens / total
	if cutByte >= len(content) {
		cutByte = len(content) - 1
	}
	if cutByte < 1 {
		return ""
	}
	// 尽量在换行边界截断（搜索范围 [cutByte/2, cutByte)，防截点太靠前损失过多）。
	if i := strings.LastIndexByte(content[:cutByte], '\n'); i >= cutByte/2 {
		cutByte = i
	}
	// 落到 UTF-8 字符边界。
	for cutByte > 0 && content[cutByte]&0xC0 == 0x80 {
		cutByte--
	}
	// token 实测收缩兜底：估算偏高时按 10% 逐步缩，直到达标。
	for cutByte > 0 && countTokens(content[:cutByte]) > limitTokens {
		cutByte = cutByte * 9 / 10
		for cutByte > 0 && content[cutByte]&0xC0 == 0x80 {
			cutByte--
		}
	}
	return content[:cutByte]
}

// isTruncatedForPrompt 判定 previewForPrompt 产出的文本是否被超限截断/外置。
// 截断尾部含固定标记串 promptTruncatedMarker；未截断或文件不存在的占位说明
// （如「(文件尚不存在)」/「(文件为空)」）一律返回 false。
func isTruncatedForPrompt(content string) bool {
	return strings.Contains(content, promptTruncatedMarker)
}

// promptContextDirName 是内存字段 spill 目录名（shared/ 之下）。
const promptContextDirName = "prompt_context"

// PromptContext 是各阶段 prompt 注入的统一投影：每字段一个 ≤动态上限的 preview 字符串。
// 两类来源合一：内存字段（snapshot 派生）超长时先 spill 全量到 shared/prompt_context/
// 再给指针；文件字段本就是落盘真相源，指针直指原文件。缺失/空一律空串（模板 HAS gate
// 按空串判缺失），不注「(文件为空)」占位。
type PromptContext struct {
	// —— 内存来源（snapshot 派生）——
	InputTimeline     string // "- [ts] content" 行格式（沿用 PlannerInputFromSnapshot 拼接）
	GoalUnderstanding string
	Plan              string // ProjectPlanItemCardsSlim JSON preview；指针 → planner.jsonl
	Phases            string // prettyJSON preview
	StepOutcomes      string // prettyJSON preview；指针 → step_contexts.jsonl
	Warnings          string
	ReplanContext     string // prettyJSON preview
	RecoveryContext   string // plan 恢复回合专用；由 plan 阶段经 previewMemoryField 单独填充
	// —— 文件来源（workspace 共享区）——
	TaskContextBoard string // shared/task_context.md
	OpenItemsLedger  string // shared/open_items.md
	PlannerJournal   string // workspace/planner.jsonl
	StepFileContent  string // shared/steps/<stepID>.md（stepID 空则空串）
}

// buildPromptContext 组装统一 PromptContext：react 层唯一同时持有 snapshot 与
// workspaceRuntime，故落此处。stepID 供 StepFileContent 定位（plan / final_answer 传空）。
// RecoveryContext 不在此填充——其构建（maybeBuildRecoveryChildContextJSON）含
// 「用后即清」副作用，由 plan 阶段单独调 previewMemoryField 注入。
func (a *Agent) buildPromptContext(snapshot builtin_tools.StateSnapshot, stepID string) *PromptContext {
	limit := promptPreviewTokens(a.usableInputTokens)
	pc := &PromptContext{}

	// —— 内存来源 ——
	pc.InputTimeline = a.previewMemoryField("input_timeline", formatInputTimelineLines(snapshot.InputTimeline), limit)
	pc.GoalUnderstanding = a.previewMemoryField("goal_understanding", snapshot.GoalUnderstanding, limit)
	if len(snapshot.Plan) > 0 {
		// plan 真相源已落盘 planner.jsonl，不 spill，指针直指真相源。
		journalAbs := builtin_tools.WorkspacePlannerJournalFileAbs(strings.TrimSpace(a.workspaceRootDir))
		pc.Plan = previewNonEmptyForPrompt(prettyJSON(ProjectPlanItemCardsSlim(snapshot.Plan, a.workspaceRootDir)), journalAbs, limit)
	}
	if len(snapshot.Phases) > 0 {
		pc.Phases = a.previewMemoryField("phases", prettyJSON(snapshot.Phases), limit)
	}
	if len(snapshot.StepOutcomes) > 0 {
		// step 产出真相源已落盘 step_contexts.jsonl，不 spill。
		pc.StepOutcomes = previewNonEmptyForPrompt(prettyJSON(snapshot.StepOutcomes), a.resolveStepContextsPath(), limit)
	}
	if len(snapshot.Warnings) > 0 {
		pc.Warnings = a.previewMemoryField("warnings", prettyJSON(snapshot.Warnings), limit)
	}
	if snapshot.ReplanContext != nil {
		pc.ReplanContext = a.previewMemoryField("replan_context", prettyJSON(snapshot.ReplanContext), limit)
	}

	// —— 文件来源 ——
	pc.TaskContextBoard = a.previewSharedFileForPrompt(taskContextFileName, limit)
	pc.OpenItemsLedger = a.previewSharedFileForPrompt(openItemsFileName, limit)
	pc.PlannerJournal = previewPlannerJournalForPrompt(a.workspaceRootDir, limit)
	if a.workspaceRuntime != nil {
		pc.StepFileContent = readSharedStepFileForPrompt(strings.TrimSpace(a.workspaceRuntime.SharedDir()), stepID, limit)
	}
	return pc
}

// previewMemoryField 对无单义真相源文件的内存字段做 preview：≤limit 原样返回；
// 超限先 spill 全量到 shared/prompt_context/<field>.md（copy→pointer 无损前提），
// 再按统一 preview 截断给指针。spill 失败注全文兜底——宁可超预算也不给无效指针丢内容。
func (a *Agent) previewMemoryField(field, content string, limitTokens int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if limitTokens <= 0 || countTokens(content) <= limitTokens {
		return content
	}
	absPath := a.spillPromptContextField(field, content)
	if absPath == "" {
		return content
	}
	return previewForPrompt(content, absPath, limitTokens)
}

// spillPromptContextField 把内存字段全量落盘 shared/prompt_context/<field>.md
// （固定名覆盖写，不产生碎片；经 WriteFileRel 走 sharedFileLocks 保护），返回绝对路径。
// 失败返回空串，由调用方注全文兜底。
func (a *Agent) spillPromptContextField(field, content string) string {
	if a == nil || a.workspaceRuntime == nil {
		return ""
	}
	sharedDir := strings.TrimSpace(a.workspaceRuntime.SharedDir())
	if sharedDir == "" {
		return ""
	}
	name := field + ".md"
	rel := filepath.ToSlash(filepath.Join("shared", promptContextDirName, name))
	if err := a.workspaceRuntime.WriteFileRel(rel, []byte(content)); err != nil {
		return ""
	}
	return filepath.Join(sharedDir, promptContextDirName, name)
}

// previewSharedFileForPrompt 读共享区文件并做 preview：缺失/空返回空串（HAS gate 语义），
// 读取经 runtime.ReadFileRel 走 sharedFileLocks 的 RLock，与并发写者串行化。
func (a *Agent) previewSharedFileForPrompt(name string, limitTokens int) string {
	if a == nil || a.workspaceRuntime == nil {
		return ""
	}
	sharedDir := strings.TrimSpace(a.workspaceRuntime.SharedDir())
	if sharedDir == "" {
		return ""
	}
	raw := readSharedFileOptional(a.workspaceRuntime, sharedDir, name)
	return previewNonEmptyForPrompt(raw, filepath.Join(sharedDir, name), limitTokens)
}

// previewPlannerJournalForPrompt 读 workspace/planner.jsonl 并做 preview；
// 文件缺失/空返回空串（模板 HAS_PLANNER_JOURNAL gate 判缺失）。
func previewPlannerJournalForPrompt(workspaceRootDir string, limitTokens int) string {
	absPath := resolvePlannerJournalPointer(workspaceRootDir)
	if absPath == "" {
		return ""
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return ""
	}
	return previewNonEmptyForPrompt(string(data), absPath, limitTokens)
}

// previewNonEmptyForPrompt 与 previewForPrompt 相同，但空内容保持空串
// （供以空串作缺失 gate 的 PromptContext 字段使用，不注「(文件为空)」占位）。
func previewNonEmptyForPrompt(raw, absPath string, limitTokens int) string {
	content := strings.TrimSpace(raw)
	if content == "" {
		return ""
	}
	return previewForPrompt(content, absPath, limitTokens)
}

// formatInputTimelineLines 把输入时间线渲染为 "- [ts] content" 行格式
// （沿用 PlannerInputFromSnapshot 的拼接口径，各阶段统一消费）。
func formatInputTimelineLines(items []*builtin_tools.TimelineInput) string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		if item.CreatedAt.IsZero() {
			lines = append(lines, "- "+content)
			continue
		}
		lines = append(lines, "- ["+item.CreatedAt.Format(time.RFC3339)+"] "+content)
	}
	return strings.Join(lines, "\n")
}
