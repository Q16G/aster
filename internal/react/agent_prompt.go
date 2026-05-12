package react

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
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
	dependencySummaries := SelectDependencyStepSummaryCards(snap, currentStep)
	executionContexts := a.executionContextsForPrompt(snap)
	hasDependencySummaries := len(dependencySummaries) > 0
	hasExecutionContexts := len(executionContexts) > 0
	hasWarnings := len(snap.Warnings) > 0
	hasUnresolved := len(snap.Unresolved) > 0
	latestInput := snap.LatestInput()
	skillsContext := a.buildSkillsPromptContext(ctx, snap)
	mcpContext := a.buildMCPPromptContext()

	workspaceSharedDir := ""
	if a.workspaceRuntime != nil {
		workspaceSharedDir = a.workspaceRuntime.SharedDir()
	}

	var contractName, contractSchema, contractExample string
	hasContract := false
	if currentStep != nil && currentStep.OutputContractRef != "" && a.cfg != nil {
		if c := a.cfg.LookupOutputContract(currentStep.OutputContractRef); c != nil {
			hasContract = true
			contractName = c.Name
			contractSchema = c.Schema
			contractExample = c.Example
		}
	}

	prompt, err := a.promptManager.BuildThinkActPrompt(ThinkActPromptInput{
		AgentInstruction:        strings.TrimSpace(a.cfg.Instruction),
		TaskContext:             taskContext,
		WorkspaceRootDir:        a.workspaceRootDir,
		WorkspaceNamespace:      a.workspaceNamespace,
		WorkspaceSharedDir:      workspaceSharedDir,
		SkillsContext:           skillsContext,
		Phase:                   snap.Phase,
		Status:                  snap.Status,
		CurrentGoal:             snap.CurrentGoal,
		CurrentStepID:           snap.CurrentStepID,
		CurrentStep:             currentStep,
		LatestInput:             latestInput,
		InputTimeline:           snap.InputTimeline,
		DependencyStepSummaries: dependencySummaries,
		ExecutionContexts:       executionContexts,
		HasCurrentGoal:          strings.TrimSpace(snap.CurrentGoal) != "",
		HasCurrentStepID:        strings.TrimSpace(snap.CurrentStepID) != "",
		HasCurrentStep:          currentStep != nil,
		HasLatestInput:          latestInput != nil,
		HasDependencySummaries:  hasDependencySummaries,
		HasExecutionContexts:    hasExecutionContexts,
		HasWarnings:             hasWarnings,
		HasUnresolved:           hasUnresolved,
		HasSkillsTable:          skillsContext != nil && skillsContext.HasTable(),
		HasInjectedSkills:       skillsContext != nil && skillsContext.HasInjected(),
		MCPContext:              mcpContext,
		HasMCPTable:             mcpContext != nil && mcpContext.HasTable(),
		Warnings:                snap.Warnings,
		Unresolved:              snap.Unresolved,
		ExtraContext:            extra,
		HasOutputContract:       hasContract,
		OutputContractName:      contractName,
		OutputContractSchema:    contractSchema,
		OutputContractExample:   contractExample,
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

type stepSummaryCard struct {
	StepID          string   `json:"step_id"`
	Status          string   `json:"status"`
	ShortSummary    string   `json:"short_summary,omitempty"`
	KeyFacts        []string `json:"key_facts,omitempty"`
	ToolCallsDigest []string `json:"tool_calls_digest,omitempty"`
	References      []string `json:"references,omitempty"`
	SummaryFile     string   `json:"summary_file,omitempty"`
	StatusSummary   string   `json:"status_summary,omitempty"`
	OpenQuestions   []string `json:"open_questions,omitempty"`
}

func SelectDependencyStepSummaryCards(snapshot builtin_tools.StateSnapshot, currentStep *builtin_tools.PlanItem) []stepSummaryCard {
	if currentStep == nil {
		return []stepSummaryCard{}
	}
	dependencyIDs := collectTransitiveDependencyIDs(currentStep, snapshot.Plan)
	if len(dependencyIDs) == 0 {
		return []stepSummaryCard{}
	}

	outcomesByID := make(map[string]*builtin_tools.StepOutcome, len(snapshot.StepOutcomes))
	for _, outcome := range snapshot.StepOutcomes {
		if outcome == nil {
			continue
		}
		stepID := strings.TrimSpace(outcome.StepID)
		if stepID == "" {
			continue
		}
		prev, exists := outcomesByID[stepID]
		if !exists || prev == nil || outcome.UpdatedAt.After(prev.UpdatedAt) {
			outcomesByID[stepID] = outcome
		}
	}
	if len(outcomesByID) == 0 {
		return []stepSummaryCard{}
	}

	cards := make([]stepSummaryCard, 0, len(dependencyIDs))
	addOutcome := func(outcome *builtin_tools.StepOutcome) {
		if outcome == nil {
			return
		}
		stepID := strings.TrimSpace(outcome.StepID)
		if stepID == "" {
			return
		}
		short := strings.TrimSpace(outcome.ShortSummary)
		if short == "" {
			short = strings.TrimSpace(outcome.Summary)
		}
		if short == "" {
			short = strings.TrimSpace(outcome.DisplayResult)
		}

		card := stepSummaryCard{
			StepID:          stepID,
			Status:          strings.TrimSpace(string(outcome.Status)),
			ShortSummary:    short,
			KeyFacts:        outcome.KeyFacts,
			ToolCallsDigest: outcome.ToolCallsDigest,
			References:      outcome.References,
			SummaryFile:     strings.TrimSpace(outcome.SummaryFile),
			StatusSummary:   strings.TrimSpace(outcome.StatusSummary),
			OpenQuestions:   outcome.OpenQuestions,
		}
		if len(card.KeyFacts) == 0 {
			card.KeyFacts = nil
		}
		if len(card.ToolCallsDigest) == 0 {
			card.ToolCallsDigest = nil
		}
		if len(card.References) == 0 {
			card.References = nil
		}
		if len(card.OpenQuestions) == 0 {
			card.OpenQuestions = nil
		}
		cards = append(cards, card)
	}

	for _, depID := range dependencyIDs {
		addOutcome(outcomesByID[depID])
	}
	if len(cards) == 0 {
		return []stepSummaryCard{}
	}
	return cards
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
		"unresolved":         snapshot.Unresolved,
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
