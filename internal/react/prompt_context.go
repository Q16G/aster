package react

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"aster/internal/builtin_tools"
	"aster/internal/workspacefs"
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

// promptInjectionBudgetRatio 注入侧 preview 字段总预算占「可用输入预算」的比例。
// 单字段封顶（promptPreviewRatio=2%）只约束单字段；注入主导阶段（plan/step_replan/
// final_answer）会同时注入多个 preview 字段，Σ 可能远超单字段上限。本比例给注入块总量兜底，
// 超限时按阶段优先级把低优字段降级为纯指针。取 10× 单字段比例为保守缺省（后续可配）。
const promptInjectionBudgetRatio = 0.20

// promptInjectionBudget 返回注入侧 preview 字段的总 token 预算；<=0 走同款默认兜底。
func promptInjectionBudget(usableInputTokens int) int {
	uit := usableInputTokens
	if uit <= 0 {
		uit = defaultContextWindowTokens - DefaultOutputReserveTokens
	}
	return int(float64(uit) * promptInjectionBudgetRatio)
}

// injectionField 是聚合封顶的一个可降级字段：字段指针 + spill 名。无单义真相源的内存字段
// 降级前需先 spill 取得 Path；文件字段 Path 恒有，spillName 传空。
type injectionField struct {
	field     *PreviewField
	spillName string
}

// applyInjectionBudget 聚合封顶：priority 内字段 Text token 之和 > budget 时，按 priority
// 顺序（低优先→高优先）逐个把字段降级为纯指针（Text=pointerOnlyForPrompt(Path)），直到
// Σ ≤ budget。已截断/已空字段跳过；内存字段无 Path 时先 spill，spill 失败则跳过。
// budget <= 0 不封顶。
func (a *Agent) applyInjectionBudget(priority []injectionField, budget int) {
	if budget <= 0 {
		return
	}
	total := 0
	for _, pf := range priority {
		total += countTokens(pf.field.Text)
	}
	for _, pf := range priority {
		if total <= budget {
			return
		}
		f := pf.field
		if !f.Has() || f.Truncated {
			continue
		}
		path := f.Path
		if path == "" {
			path = a.spillPromptContextField(pf.spillName, f.Text)
			if path == "" {
				continue
			}
		}
		before := countTokens(f.Text)
		f.Text = pointerOnlyForPrompt(path)
		f.Path = path
		f.Truncated = true
		total += countTokens(f.Text) - before
	}
}

// planInjectionBudgetFields 构建 plan 阶段聚合封顶的字段优先级列表（低优先→高优先，先降级）。
// 只纳入本回合确实会注入 prompt 的字段：GoalUnderstanding 仅 !regenGoal 时注入、TaskContextBoard
// 仅 injectsTaskBoard 时注入——无条件计入会高估总量、过度降级 InputTimeline/Plan 等规划必需字段。
// Topics/事实板可先降级为指针，InputTimeline/Plan 排末端最后降级。
func planInjectionBudgetFields(pc *PromptContext, regenGoal, injectsTaskBoard bool) []injectionField {
	fields := []injectionField{{field: &pc.Topics, spillName: "phases"}}
	if injectsTaskBoard {
		fields = append(fields, injectionField{field: &pc.TaskContextBoard})
	}
	if !regenGoal {
		fields = append(fields, injectionField{field: &pc.GoalUnderstanding, spillName: "goal_understanding"})
	}
	fields = append(fields,
		injectionField{field: &pc.InputTimeline, spillName: "input_timeline"},
		injectionField{field: &pc.Plan},
	)
	return fields
}

// promptTruncatedMarker 是 preview 截断/外置标记串前缀；isTruncatedForPrompt 据此判定。
const promptTruncatedMarker = "（内容超长"

// PreviewField 是一个注入块的完整投影结果：预览文本 + 真相源路径 + 截断标记。
// 一次构建，下游不再重算路径、不再字符串嗅探截断、不再外置 HasXxx。
// Text 空串 = 缺失（模板 HAS gate 唯一判据）；Truncated 由 previewFieldForPrompt 的控制流
// 直接置位，不做字符串嗅探。
type PreviewField struct {
	Text      string
	Path      string
	Truncated bool
}

// Has 替代所有外置 HasXxx bool——Text 非空即存在。
func (f PreviewField) Has() bool { return f.Text != "" }

// NeedsPointer 替代 isTruncatedForPrompt 消费点嗅探——截断时下游才需暴露 Path。
func (f PreviewField) NeedsPointer() bool { return f.Truncated }

// PointerPath 截断时返回权威回读路径，否则空串（供模板条件渲染路径提示）。
func (f PreviewField) PointerPath() string {
	if f.Truncated {
		return f.Path
	}
	return ""
}

// previewFieldForPrompt 是统一的结构化 preview 原语：按三态产出 PreviewField，Truncated 由
// 控制流直接置位。content 的 token 数 ≤ limitTokens 时 Text=原文；超限时在 token/UTF-8 边界
// 截断并追加指针文案（工具名用 builtin 实名 read_file），Truncated=true；limit 连指针文案本身
// 都放不下时全指针化（指针是不可压缩固定开销）。limitTokens <= 0 不截断。截断维度按 token
// （复用 countTokens）而非字节，消除中文「字节≠token」偏差。空内容返回占位 "(文件为空)"
// （保留旧 previewForPrompt 语义；以空串作缺失 gate 的字段走 previewNonEmptyForPrompt/
// previewMemoryField 的空串短路，不经此占位）。
func previewFieldForPrompt(raw, absPath string, limitTokens int) PreviewField {
	content := strings.TrimSpace(raw)
	if content == "" {
		return PreviewField{Text: "(文件为空)", Path: absPath}
	}
	if limitTokens <= 0 || countTokens(content) <= limitTokens {
		return PreviewField{Text: content, Path: absPath}
	}
	pointer := pointerOnlyForPrompt(absPath)
	if limitTokens < countTokens(pointer) {
		return PreviewField{Text: pointer, Path: absPath, Truncated: true}
	}
	truncated := truncateToTokenBudget(content, limitTokens)
	if strings.TrimSpace(truncated) == "" {
		return PreviewField{Text: pointer, Path: absPath, Truncated: true}
	}
	text := truncated + "\n\n" + promptTruncatedMarker + "，仅显示前 " +
		fmt.Sprintf("%d", countTokens(truncated)) + " tokens；完整内容见文件：" +
		absPath + "，维护/验收前请先用 read_file 读取全量。）"
	return PreviewField{Text: text, Path: absPath, Truncated: true}
}

// previewForPrompt 保留字符串 API（现有 preview 单测与 previewStepFileForPrompt 复用），
// 语义 = previewFieldForPrompt(...).Text。
func previewForPrompt(raw string, absPath string, limitTokens int) string {
	return previewFieldForPrompt(raw, absPath, limitTokens).Text
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

// isTruncatedForPrompt 判定 preview 文本是否被超限截断/外置（字符串谓词）。
// 现有 preview 单测复用；PromptContext 字段改用 PreviewField.Truncated，消费点不再嗅探。
func isTruncatedForPrompt(content string) bool {
	return strings.Contains(content, promptTruncatedMarker)
}

// promptContextDirName 是内存字段 spill 目录名（shared/ 之下）。
const promptContextDirName = "prompt_context"

// PromptContext 是各阶段 prompt 注入的统一投影：每字段一个 PreviewField（≤动态上限的预览
// + 真相源路径 + 截断标记）。两类来源合一：内存字段（snapshot 派生）超长时先 spill 全量到
// shared/prompt_context/ 再给指针；文件字段本就是落盘真相源，指针直指原文件。缺失/空 = 空
// PreviewField（Has()=false，模板 HAS gate 按此判缺失）。ReplanContext / RecoveryContext 是
// plan 阶段瞬态（含副作用），不在此投影——由 buildReplanContext / buildRecoveryContext 显式构建。
type PromptContext struct {
	// —— 意图锚 ——
	InputTimeline     PreviewField
	GoalUnderstanding PreviewField
	// —— 计划与产出 ——
	Plan         PreviewField // 指针 → planner.jsonl
	Topics       PreviewField
	StepOutcomes PreviewField // 指针 → step_contexts.jsonl
	// —— 共享区三件套 + step 文件 ——
	TaskContextBoard PreviewField // shared/task_context.md
	OpenItemsLedger  PreviewField // shared/open_items.md
	PlannerJournal   PreviewField // workspace/planner.jsonl
	StepFile         PreviewField // shared/steps/<stepID>.md（stepID 空则空）
	// —— 阶段瞬态 ——
	Warnings PreviewField
}

// buildPromptContext 组装统一 PromptContext（纯函数：给定 snapshot+stepID 完整产出，无隐藏
// 后续补齐、无副作用；内存字段超限 spill 除外）。react 层唯一同时持有 snapshot 与
// workspaceRuntime，故落此处。stepID 供 StepFile 定位（plan / final_answer 传空）。
func (a *Agent) buildPromptContext(snapshot builtin_tools.StateSnapshot, stepID string) *PromptContext {
	limit := promptPreviewTokens(a.usableInputTokens)
	pc := &PromptContext{}

	// —— 内存来源 ——
	pc.InputTimeline = a.previewMemoryField("input_timeline", formatInputTimelineLines(snapshot.InputTimeline), limit)
	pc.GoalUnderstanding = a.previewMemoryField("goal_understanding", snapshot.GoalUnderstanding, limit)
	if len(snapshot.Plan) > 0 {
		// plan 真相源已落盘 planner.jsonl，不 spill，指针直指真相源。
		journalAbs := a.wsLayout().PlannerJournal()
		pc.Plan = previewNonEmptyForPrompt(prettyJSON(ProjectPlanItemCardsSlim(snapshot.Plan, a.workspaceRootDir)), journalAbs, limit)
	}
	if len(snapshot.Topics) > 0 {
		pc.Topics = a.previewMemoryField("phases", prettyJSON(snapshot.Topics), limit)
	}
	if len(snapshot.StepOutcomes) > 0 {
		// step 产出真相源已落盘 step_contexts.jsonl，不 spill。
		pc.StepOutcomes = previewNonEmptyForPrompt(prettyJSON(snapshot.StepOutcomes), a.resolveStepContextsPath(), limit)
	}
	if len(snapshot.Warnings) > 0 {
		pc.Warnings = a.previewMemoryField("warnings", prettyJSON(snapshot.Warnings), limit)
	}

	// —— 文件来源 ——
	pc.TaskContextBoard = a.previewSharedFileForPrompt(taskContextFileName, limit)
	pc.OpenItemsLedger = a.previewSharedFileForPrompt(openItemsFileName, limit)
	pc.PlannerJournal = previewPlannerJournalForPrompt(a.workspaceRootDir, limit)
	if a.workspaceRuntime != nil {
		pc.StepFile = readSharedStepFileForPrompt(a.workspaceRuntime, stepID, limit)
	}
	return pc
}

// buildReplanContext 构建 plan 阶段的 replan 上下文投影（原 buildPromptContext 内瞬态，
// 剥离为显式构建，让 buildPromptContext 纯函数化）。snapshot 无 ReplanContext 时返回空。
func (a *Agent) buildReplanContext(snapshot builtin_tools.StateSnapshot) PreviewField {
	if snapshot.ReplanContext == nil {
		return PreviewField{}
	}
	return a.previewMemoryField("replan_context", prettyJSON(snapshot.ReplanContext), promptPreviewTokens(a.usableInputTokens))
}

// buildRecoveryContext 构建 plan 恢复回合的现场投影。maybeBuildRecoveryChildContextJSON 含
// 「用后即清」副作用，故显式构建、不进 buildPromptContext（避免投影层携带隐藏副作用）。
func (a *Agent) buildRecoveryContext(snapshot builtin_tools.StateSnapshot) PreviewField {
	return a.previewMemoryField("recovery_context", a.maybeBuildRecoveryChildContextJSON(snapshot), promptPreviewTokens(a.usableInputTokens))
}

// previewMemoryField 对无单义真相源文件的内存字段做 preview：≤limit 原样（无 Path）；
// 超限先 spill 全量到 shared/prompt_context/<field>.md（copy→pointer 无损前提），再按统一
// preview 截断给指针（Path=spill 落点）。spill 失败注全文兜底——宁可超预算也不给无效指针丢内容。
func (a *Agent) previewMemoryField(field, content string, limitTokens int) PreviewField {
	content = strings.TrimSpace(content)
	if content == "" {
		return PreviewField{}
	}
	if limitTokens <= 0 || countTokens(content) <= limitTokens {
		return PreviewField{Text: content}
	}
	absPath := a.spillPromptContextField(field, content)
	if absPath == "" {
		return PreviewField{Text: content}
	}
	return previewFieldForPrompt(content, absPath, limitTokens)
}

// spillPromptContextField 把内存字段全量落盘 shared/prompt_context/<field>.md
// （固定名覆盖写，不产生碎片；经 WriteFileRel 走 sharedFileLocks 保护），返回绝对路径。
// 失败返回空串，由调用方注全文兜底。
func (a *Agent) spillPromptContextField(field, content string) string {
	if a == nil || a.workspaceRuntime == nil {
		return ""
	}
	l := a.wsLayout()
	if l.SharedDir() == "" {
		return ""
	}
	name := field + ".md"
	rel := filepath.ToSlash(filepath.Join(l.SharedDirRel(), promptContextDirName, name))
	if err := a.workspaceRuntime.WriteFileRel(rel, []byte(content)); err != nil {
		return ""
	}
	return filepath.Join(l.SharedDir(), promptContextDirName, name)
}

// previewSharedFileForPrompt 读共享区文件并做 preview：缺失/空返回空 PreviewField（HAS gate
// 语义），读取经 runtime.ReadFileRel 走 sharedFileLocks 的 RLock，与并发写者串行化。
func (a *Agent) previewSharedFileForPrompt(name string, limitTokens int) PreviewField {
	if a == nil || a.workspaceRuntime == nil {
		return PreviewField{}
	}
	l := a.wsLayout()
	if l.SharedDir() == "" {
		return PreviewField{}
	}
	raw := readSharedFileOptional(a.workspaceRuntime, name)
	return previewNonEmptyForPrompt(raw, filepath.Join(l.SharedDir(), name), limitTokens)
}

// previewPlannerJournalForPrompt 读 workspace/planner.jsonl 并做 preview；
// 文件缺失/空返回空 PreviewField（模板 HAS_PLANNER_JOURNAL gate 判缺失）。
// 调用侧仅持裸 root string，经 NewLocalStore 读取（防穿越 + per-key 锁）。
func previewPlannerJournalForPrompt(workspaceRootDir string, limitTokens int) PreviewField {
	absPath := resolvePlannerJournalPointer(workspaceRootDir)
	if absPath == "" {
		return PreviewField{}
	}
	store, err := workspacefs.NewLocalStore(workspaceRootDir)
	if err != nil {
		return PreviewField{}
	}
	data, err := store.Read(workspacefs.New(workspaceRootDir, "").PlannerJournalRel())
	if err != nil {
		return PreviewField{}
	}
	return previewNonEmptyForPrompt(string(data), absPath, limitTokens)
}

// previewNonEmptyForPrompt 与 previewFieldForPrompt 相同，但空内容返回空 PreviewField
// （供以空串作缺失 gate 的字段使用，不注「(文件为空)」占位）。
func previewNonEmptyForPrompt(raw, absPath string, limitTokens int) PreviewField {
	content := strings.TrimSpace(raw)
	if content == "" {
		return PreviewField{}
	}
	return previewFieldForPrompt(content, absPath, limitTokens)
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
