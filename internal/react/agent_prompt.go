package react

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"aster/internal/builtin_tools"
)

//go:embed prompts/think_act.prompt
var thinkActPrompt string

func (a *Agent) BuildThinkActPrompt(ctx context.Context, extra string, taskContext *TaskContextData) string {
	if a == nil || a.promptManager == nil {
		return ""
	}

	snap := a.state.Snapshot()
	currentStep := snap.CurrentStep()
	dependencyItems := SelectDependencyPlanItemCards(snap, currentStep, a.workspaceRootDir)
	skillsContext := a.buildSkillsPromptContext(ctx, snap)
	mcpContext := a.buildMCPPromptContext()

	workspaceSharedDir := ""
	if a.workspaceRuntime != nil {
		workspaceSharedDir = a.workspaceRuntime.SharedDir()
	}

	supportsVision := ModelSupportsVision(a.getCurrentRunClient())
	canSpawnSubAgent := a.canSpawnSubAgent(ctx)

	prompt, err := a.promptManager.BuildThinkActPrompt(ThinkActPromptInput{
		AgentRole:              strings.TrimSpace(a.cfg.Role),
		AgentBackground:        strings.TrimSpace(a.cfg.Background),
		AgentInstruction:       strings.TrimSpace(a.cfg.Instruction),
		GoalUnderstanding:      strings.TrimSpace(snap.GoalUnderstanding),
		TaskContext:            taskContext,
		WorkspaceRootDir:       a.workspaceRootDir,
		WorkspaceNamespace:     a.workspaceNamespace,
		WorkspaceSharedDir:     workspaceSharedDir,
		RuntimeRepoContext:     a.runtimeRepoContext,
		SkillsContext:          skillsContext,
		CurrentStep:            currentStep,
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
		return prompt
	}

	fallbackState := FormatRuntimeStateJSON(snap, a.workspaceSessionID)
	if fallbackState == "{}" {
		return strings.TrimSpace(a.cfg.Instruction)
	}
	return fmt.Sprintf("%s\n\n运行时状态：\n%s", strings.TrimSpace(a.cfg.Instruction), fallbackState)
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
	OpenItemIDs     []string `json:"open_item_ids,omitempty"`
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
		OpenItemIDs:     item.OpenItemIDs,
		References:      item.References,
		StepFile:        abs(item.StepFile),
		ResultFile:      abs(item.ResultFile),
		TimelineFile:    abs(item.TimelineFile),
		CoverageFile:    abs(item.CoverageFile),
	}
}

// projectPlanItemsForPlanner 把全量 plan 投影为 planner 的 TASK_ITEMS 注入视图。
func projectPlanItemsForPlanner(plan []*builtin_tools.PlanItem, workspaceRootDir string) []dependencyPlanItemCard {
	out := make([]dependencyPlanItemCard, 0, len(plan))
	for _, item := range plan {
		if card := planItemCard(item, workspaceRootDir); card != nil {
			out = append(out, *card)
		}
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
