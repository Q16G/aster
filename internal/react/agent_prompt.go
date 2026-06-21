package react

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aster/internal/builtin_tools"
)

//go:embed prompts/think_act_system.prompt
var thinkActSystemPrompt string

//go:embed prompts/think_act_user.prompt
var thinkActUserPrompt string

// BuildThinkActPrompt 构造 think_act phase 的 prompt。
//
// **snapshot 参数必传**（commit 修复 peer-prompt 注入 bug）：
//   - 主路径调用方传 a.state.Snapshot()
//   - peer 路径调用方传 promptSnapshot（已 swap CurrentStepID = runCtx.StepID）
//
// 旧版本内部 `snap := a.state.Snapshot()` 会忽略 caller-injected snapshot，
// 让所有 peer 都看到主路径 CurrentStepID（= 主 step），导致 STEP_FILE_PATH /
// dependencyItems 都按主 step 派生，3 个并发 peer 全部跑成同一个 step（症状 B）。
func (a *Agent) BuildThinkActPrompt(ctx context.Context, extra string, snapshot builtin_tools.StateSnapshot) PromptParts {
	if a == nil || a.promptManager == nil {
		return PromptParts{}
	}

	snap := snapshot
	currentStep := snap.CurrentStep()
	dependencyItems := SelectDependencyPlanItemCards(snap, currentStep, a.workspaceRootDir)
	skillsContext := a.buildSkillsPromptContext(ctx, snap)
	mcpContext := a.buildMCPPromptContext()

	supportsVision := ModelSupportsVision(a.getCurrentRunClient())
	canSpawnSubAgent := a.canSpawnSubAgent(ctx)

	stepFilePath := ""
	openItemsLedgerPath := ""
	taskContextPath := ""
	if a.workspaceRuntime != nil {
		sharedDir := a.workspaceRuntime.SharedDir()
		stepFilePath = stepFileAbs(sharedDir, snap.CurrentStepID)
		if strings.TrimSpace(sharedDir) != "" {
			openItemsLedgerPath = filepath.Join(sharedDir, openItemsFileName)
			taskContextPath = filepath.Join(sharedDir, taskContextFileName)
		}
	}

	parts, err := a.promptManager.BuildThinkActPrompt(ThinkActPromptInput{
		AgentRole:              strings.TrimSpace(a.cfg.Role),
		AgentBackground:        strings.TrimSpace(a.cfg.Background),
		GoalUnderstanding:      strings.TrimSpace(snap.GoalUnderstanding),
		SkillsContext:          skillsContext,
		CurrentStep:            currentStep,
		CurrentStepFilePath:    stepFilePath,
		OpenItemsLedgerPath:    openItemsLedgerPath,
		TaskContextPath:        taskContextPath,
		DependencyPlanItems:    dependencyItems,
		HasCurrentStep:         currentStep != nil,
		HasDependencyPlanItems: len(dependencyItems) > 0,
		HasSkillsTable:         skillsContext != nil && skillsContext.HasTable(),
		HasInjectedSkills:      skillsContext != nil && skillsContext.HasInjected(),
		MCPContext:             mcpContext,
		HasMCPTable:            mcpContext != nil && mcpContext.HasTable(),
		ExtraContext:           extra,
		SupportsVision:         supportsVision,
		CanSpawnSubAgent:       canSpawnSubAgent,
	})
	if err == nil {
		parts.SystemAgent = a.identityEnvBlock()
		return parts
	}

	fallbackState := FormatRuntimeStateJSON(snap, a.workspaceSessionID)
	return PromptParts{
		SystemRules: firstNonEmpty(strings.TrimSpace(a.cfg.Instruction), "你是 step 执行代理，基于运行时状态推进当前 step。"),
		SystemAgent: a.identityEnvBlock(),
		User:        fmt.Sprintf("运行时状态：\n%s", fallbackState),
	}
}

// thinkActPartsForStep 返回当前 step 的 think_act PromptParts：首条 user message
// 按 step 入口冻结（按 stepID + planVersion 键缓存），step 内各轮字节恒定——
// mid-step 的 skill 加载经 tool result 通道即时生效，§Injected Skills 快照固定在
// step 入口，消息前缀的移动缓存断点得以全程命中。
// thinkActPartsForStep 为指定 snapshot 派生 think_act 入口 prompt。
//
// **缓存按 (stepID, planVer) 分桶**：旧版用 a.frozenStepParts 单字段，多 peer 并发
// 会 race + 抖动（详见 frozenStepPromptCache doc）。本版改用并发安全 cache type，
// 每个 (stepID, planVer) 独立 entry——主路径 + 多 peer 同时跑互不干扰。
func (a *Agent) thinkActPartsForStep(ctx context.Context, extra string, snapshot builtin_tools.StateSnapshot) PromptParts {
	stepID := strings.TrimSpace(snapshot.CurrentStepID)
	if stepID == "" {
		if cs := snapshot.CurrentStep(); cs != nil {
			stepID = strings.TrimSpace(cs.ID)
		}
	}
	if cached, ok := a.frozenStepCache.Get(stepID, snapshot.PlanVersion); ok && cached != nil {
		return *cached
	}
	parts := a.BuildThinkActPrompt(ctx, extra, snapshot)
	a.frozenStepCache.Put(stepID, snapshot.PlanVersion, parts)
	return parts
}

// identityEnvBlock 渲染并缓存公共 system block2（身份 + env）。输入全部为 run 内
// 稳定值，各阶段复用同一渲染结果以保证字节一致（同一缓存条目）。
// identityEnvBlock 渲染并缓存公共 system block2（身份 + env）。
//
// **并发安全**：旧版用 `identityEnvBuilt bool` + `identityEnvPrompt string` 双字段
// 无锁——多 peer 同时调 BuildThinkActPrompt 会 race（peer A 写 prompt 时 peer B 读
// builtFlag）。RWMutex + double-check 保护：fast path 持 RLock 直接读已 build 的值；
// 未 build 时升级 WLock 构建一次。
func (a *Agent) identityEnvBlock() string {
	if a == nil || a.promptManager == nil {
		return ""
	}
	// Fast path：已构造则 RLock 直读
	a.identityEnvMu.RLock()
	if a.identityEnvBuilt {
		out := a.identityEnvPrompt
		a.identityEnvMu.RUnlock()
		return out
	}
	a.identityEnvMu.RUnlock()

	// Slow path：未构造，升级 WLock 一次性构造（double-check 防多 goroutine 重复 build）
	a.identityEnvMu.Lock()
	defer a.identityEnvMu.Unlock()
	if a.identityEnvBuilt {
		return a.identityEnvPrompt
	}
	workspaceSharedDir := ""
	if a.workspaceRuntime != nil {
		workspaceSharedDir = a.workspaceRuntime.SharedDir()
	}
	out, err := a.promptManager.BuildAgentIdentityEnvPrompt(AgentIdentityEnvPromptInput{
		AgentInstruction:   strings.TrimSpace(a.cfg.Instruction),
		WorkspaceRootDir:   a.workspaceRootDir,
		WorkspaceNamespace: a.workspaceNamespace,
		WorkspaceSharedDir: workspaceSharedDir,
		RuntimeRepoContext: a.runtimeRepoContext,
		TaskContext:        a.currentTaskContext,
	})
	if err != nil {
		return ""
	}
	a.identityEnvPrompt = out
	a.identityEnvBuilt = true
	return out
}

// canSpawnSubAgent reports whether this agent can actually delegate to sub_agent.
// True only when the sub_agent tool is registered AND the agent is not itself a
// sub-agent. The sub_agent tool is registered for every agent but is disabled at
// runtime for stack_depth>0 (see sub_agent_tool.go), so registration alone is not
// a reliable signal; we mirror the same stack-depth gate via the tool runtime.
func (a *Agent) canSpawnSubAgent(ctx context.Context) bool {
	if _, ok := a.GetTool(builtin_tools.SubAgentToolName); !ok {
		return false
	}
	if rt, ok := builtin_tools.GetToolRuntime(ctx); ok && rt.StackDepth > 0 {
		return false
	}
	return true
}

func latestStepOutcome(outcomes []*builtin_tools.StepOutcome) *builtin_tools.StepOutcome {
	var latest *builtin_tools.StepOutcome
	for _, outcome := range outcomes {
		if outcome == nil {
			continue
		}
		if latest == nil || outcome.UpdatedAt.After(latest.UpdatedAt) {
			latest = outcome
		}
	}
	return latest
}

// dependencyPlanItemCard 是前置依赖步骤的 plan_item 产出投影：内联小字段默认注入，
// 大体量产出经文件指针按需 read_file（copy→pointer 无损降本）。
type dependencyPlanItemCard struct {
	ID              string   `json:"id"`
	Step            string   `json:"step"`
	Status          string   `json:"status"`
	DependsOn       []string `json:"depends_on,omitempty"`
	ShortSummary    string   `json:"short_summary,omitempty"`
	KeyFacts        []string `json:"key_facts,omitempty"`
	ToolCallsDigest []string `json:"tool_calls_digest,omitempty"`
	References      []string `json:"references,omitempty"`
	StepFile        string   `json:"step_file,omitempty"`
	ResultFile      string   `json:"result_file,omitempty"`
	TimelineFile    string   `json:"timeline_file,omitempty"`
	CoverageFile    string   `json:"coverage_file,omitempty"`
}

// 依赖卡片内联 digest 的条数上限：超出部分顺 timeline_file 指针按需回读，保持无损。
const dependencyCardDigestMax = 20

// SelectDependencyPlanItemCards 从 plan 真相源（终态烘焙后的 plan_item）投影当前 step
// 的传递依赖产出卡片。指针字段转为绝对路径供模型直接 read_file。
func SelectDependencyPlanItemCards(snapshot builtin_tools.StateSnapshot, currentStep *builtin_tools.PlanItem, workspaceRootDir string) []dependencyPlanItemCard {
	if currentStep == nil {
		return nil
	}
	dependencyIDs := collectTransitiveDependencyIDs(currentStep, snapshot.Plan)
	if len(dependencyIDs) == 0 {
		return nil
	}
	itemByID := make(map[string]*builtin_tools.PlanItem, len(snapshot.Plan))
	for _, item := range snapshot.Plan {
		if item == nil {
			continue
		}
		if id := strings.TrimSpace(item.ID); id != "" {
			itemByID[id] = item
		}
	}

	cards := make([]dependencyPlanItemCard, 0, len(dependencyIDs))
	for _, depID := range dependencyIDs {
		if card := planItemCard(itemByID[depID], workspaceRootDir); card != nil {
			cards = append(cards, *card)
		}
	}
	if len(cards) == 0 {
		return nil
	}
	return cards
}

// planItemCard 把烘焙后的 plan_item 投影为注入卡片（digest 截断、指针转绝对路径）。
func planItemCard(item *builtin_tools.PlanItem, workspaceRootDir string) *dependencyPlanItemCard {
	if item == nil || strings.TrimSpace(item.ID) == "" {
		return nil
	}
	abs := func(path string) string {
		path = strings.TrimSpace(path)
		if path == "" || filepath.IsAbs(path) || strings.TrimSpace(workspaceRootDir) == "" {
			return path
		}
		return filepath.Join(workspaceRootDir, filepath.FromSlash(path))
	}
	digest := item.ToolCallsDigest
	if len(digest) > dependencyCardDigestMax {
		digest = append(append([]string{}, digest[:dependencyCardDigestMax]...),
			fmt.Sprintf("...(其余 %d 条见 timeline_file)", len(item.ToolCallsDigest)-dependencyCardDigestMax))
	}
	return &dependencyPlanItemCard{
		ID:              strings.TrimSpace(item.ID),
		Step:            strings.TrimSpace(item.Step),
		Status:          strings.TrimSpace(string(item.Status)),
		DependsOn:       item.DependsOn,
		ShortSummary:    strings.TrimSpace(item.ShortSummary),
		KeyFacts:        item.KeyFacts,
		ToolCallsDigest: digest,
		References:      item.References,
		StepFile:        abs(item.StepFile),
		ResultFile:      abs(item.ResultFile),
		TimelineFile:    abs(item.TimelineFile),
		CoverageFile:    abs(item.CoverageFile),
	}
}

// ProjectPlanItemCards 把全量 plan 投影为 TASK_ITEMS / PLAN_ITEMS 注入视图。
func ProjectPlanItemCards(plan []*builtin_tools.PlanItem, workspaceRootDir string) []dependencyPlanItemCard {
	out := make([]dependencyPlanItemCard, 0, len(plan))
	for _, item := range plan {
		if card := planItemCard(item, workspaceRootDir); card != nil {
			out = append(out, *card)
		}
	}
	return out
}

// resolvePlannerJournalPointer 解析 workspace/planner.jsonl（plan 唯一真相源）的绝对路径，
// 仅当文件存在且大小 > 0 时返回；否则返回空串。三个判定阶段（task_planner 续规划 /
// step_replan / final_answer）共用，避免相同 5 行逻辑在多处复制后漂移。
func resolvePlannerJournalPointer(workspaceRootDir string) string {
	root := strings.TrimSpace(workspaceRootDir)
	if root == "" {
		return ""
	}
	p := builtin_tools.WorkspacePlannerJournalFileAbs(root)
	if p == "" {
		return ""
	}
	info, err := os.Stat(p)
	if err != nil || info.Size() <= 0 {
		return ""
	}
	return p
}

// ProjectPlanItemCardsSlim 是去 tool_calls_digest 的瘦身全量投影，供 task_planner
// 与 step_replan 注入：digest 体量大且这两个阶段可顺 timeline_file 指针按需回读。
func ProjectPlanItemCardsSlim(plan []*builtin_tools.PlanItem, workspaceRootDir string) []dependencyPlanItemCard {
	out := ProjectPlanItemCards(plan, workspaceRootDir)
	for i := range out {
		out[i].ToolCallsDigest = nil
	}
	return out
}

func collectTransitiveDependencyIDs(step *builtin_tools.PlanItem, plan []*builtin_tools.PlanItem) []string {
	if step == nil {
		return nil
	}
	planByID := make(map[string]*builtin_tools.PlanItem, len(plan))
	for _, item := range plan {
		if item == nil {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id != "" {
			planByID[id] = item
		}
	}
	visited := make(map[string]bool)
	var result []string
	var walk func(ids []string)
	walk = func(ids []string) {
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" || visited[id] {
				continue
			}
			visited[id] = true
			result = append(result, id)
			if dep := planByID[id]; dep != nil {
				walk(dep.DependencyIDs())
			}
		}
	}
	walk(step.DependencyIDs())
	return result
}

func FormatRuntimeStateJSON(snapshot builtin_tools.StateSnapshot, sessionID string) string {
	raw, err := json.MarshalIndent(map[string]any{
		"session_id":         strings.TrimSpace(sessionID),
		"phase":              snapshot.Phase,
		"status":             snapshot.Status,
		"current_goal":       snapshot.CurrentGoal,
		"current_step_id":    snapshot.CurrentStepID,
		"current_step":       snapshot.CurrentStep(),
		"latest_input":       snapshot.LatestInput(),
		"input_timeline":     snapshot.InputTimeline,
		"last_outcome":       latestStepOutcome(snapshot.StepOutcomes),
		"active_skill_names": snapshot.ActiveSkillNames,
		"warnings":           snapshot.Warnings,
		"unresolved_axes":    snapshot.UnresolvedAxes,
	}, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func (a *Agent) buildMCPPromptContext() *MCPPromptContext {
	if a == nil || a.cfg == nil || a.cfg.MCPManager == nil {
		return nil
	}
	entries := a.cfg.MCPManager.ServerEntries()
	if len(entries) == 0 {
		return nil
	}
	table := BuildMCPPromptTable(entries)
	if strings.TrimSpace(table) == "" {
		return nil
	}
	return &MCPPromptContext{Table: table}
}

func (a *Agent) buildSkillsPromptContext(ctx context.Context, snapshot builtin_tools.StateSnapshot) *SkillsPromptContext {
	if a == nil || a.cfg == nil || a.cfg.SkillsPromptProvider == nil {
		return nil
	}
	result, err := a.cfg.SkillsPromptProvider.BuildSkillsPrompt(ctx, a.Name(), snapshot)
	if err != nil || result == nil || !result.HasVisibleData() {
		return nil
	}
	return &SkillsPromptContext{
		Table:    strings.TrimSpace(result.Table),
		Injected: strings.TrimSpace(result.Injected),
	}
}
