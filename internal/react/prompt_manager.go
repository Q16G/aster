package react

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// anyHasItems 判断模板入参（通常是 []string）是否非空，用于三轴 CARRIED_* 段的渲染门控。
func anyHasItems(v any) bool {
	switch items := v.(type) {
	case []string:
		return len(items) > 0
	case nil:
		return false
	default:
		return false
	}
}

type ThinkActPromptInput struct {
	AgentRole               string
	AgentBackground         string
	AgentInstruction        string
	TaskContext             *TaskContextData
	WorkspaceRootDir        string
	WorkspaceNamespace      string
	WorkspaceSharedDir      string
	RuntimeRepoContext      RuntimeRepoContext
	SkillsContext           *SkillsPromptContext
	CurrentStep             any
	DependencyStepSummaries any
	ExecutionContexts       any
	HasCurrentStep          bool
	HasDependencySummaries  bool
	HasExecutionContexts    bool
	HasSkillsTable          bool
	HasInjectedSkills       bool
	MCPContext              *MCPPromptContext
	HasMCPTable             bool
	ExtraContext            string
	SupportsVision          bool
	CanSpawnSubAgent        bool
}

type StepReplanPromptInput struct {
	AgentRole              string
	AgentBackground        string
	AgentInstruction       string
	CurrentGoal            any
	GoalUnderstanding      string
	WorkspaceSharedDir     string
	RuntimeRepoContext     RuntimeRepoContext
	InputTimeline          any
	CurrentStep            any
	StepOutcome            any
	TaskPlan               any
	StepOutcomes           any
	CarriedIncompleteItems any
	CarriedDepthGaps       any
	CarriedNewSurfaces     any
	StepResultPath         string
	StepContextsPath       string
	StepTranscriptPath     string
	StepTimelinePath       string
	SkillsContext          *SkillsPromptContext
	HasSkillsTable         bool
}

type FinalAnswerPromptInput struct {
	AgentRole              string
	AgentBackground        string
	AgentInstruction       string
	Status                 any
	StateError             any
	InputTimeline          any
	GoalUnderstanding      string
	ShowPlanSection        bool
	Plan                   any
	PlanVersion            any
	StepOutcomes           any
	Warnings               any
	CarriedIncompleteItems any
	CarriedDepthGaps       any
	CarriedNewSurfaces     any
	WorkspaceSharedDir     string
	RuntimeRepoContext     RuntimeRepoContext
}

type HistoryCompactionPromptInput struct {
	Instruction string
	PrevSummary string
}

type AgentHandoffPromptInput struct {
	HandoffTo        string
	AgentInstruction string
	PrevSummary      string
}

type StepOutcomesReducerPromptInput struct {
	StepOutcomes string
}

type TaskPlannerPromptInput struct {
	Input              string
	GoalUnderstanding  string
	WorkspaceSharedDir string
	RuntimeRepoContext RuntimeRepoContext
	UserInputTurn      bool
	SkillsContext      *SkillsPromptContext
	MCPContext         *MCPPromptContext
	HasSkillsTable     bool
	HasMCPTable        bool
	SkillsOverflowPath string
	MCPOverflowPath    string
}

type IntentClassificationPromptInput struct {
	PreviousGoal   string
	CompletedCount int
	TotalCount     int
	RecentOutcomes []IntentOutcomeSummary
	PendingSteps   []IntentPendingStep
	InputTimeline  []IntentTimelineEntry
}

type IntentPendingStep struct {
	ID   string
	Step string
}

type IntentOutcomeSummary struct {
	StepID        string
	Status        string
	ShortSummary  string
	LongSummary   string
	KeyFacts      []string
	OpenQuestions []string
}

type IntentTimelineEntry struct {
	Time    string
	Content string
}

type PromptManager interface {
	BuildThinkActPrompt(input ThinkActPromptInput) (string, error)
	BuildStepReplanPrompt(input StepReplanPromptInput) (string, error)
	BuildFinalAnswerPrompt(input FinalAnswerPromptInput) (string, error)
	BuildHistoryCompactionPrompt(input HistoryCompactionPromptInput) (string, error)
	BuildTaskPlannerPrompt(input TaskPlannerPromptInput) (string, error)
	BuildAgentHandoffPrompt(input AgentHandoffPromptInput) (string, error)
	BuildStepOutcomesReducerPrompt(input StepOutcomesReducerPromptInput) (string, error)
	BuildIntentClassificationPrompt(input IntentClassificationPromptInput) (string, error)
}

type defaultPromptManager struct {
	thinkActTmpl             *template.Template
	stepReplanTmpl           *template.Template
	finalAnswerTmpl          *template.Template
	historyCompactionTmpl    *template.Template
	taskPlannerTmpl          *template.Template
	agentHandoffTmpl         *template.Template
	stepOutcomesReducerTmpl  *template.Template
	intentClassificationTmpl *template.Template
}

func newDefaultPromptManager() (PromptManager, error) {
	thinkActTmpl, err := template.New("think_act").Parse(thinkActPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse think_act prompt failed: %w", err)
	}
	stepReplanTmpl, err := template.New("step_replan").Parse(stepReplanPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse step_replan prompt failed: %w", err)
	}
	finalAnswerTmpl, err := template.New("final_answer").Parse(finalAnswerPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse final_answer prompt failed: %w", err)
	}
	historyCompactionTmpl, err := template.New("history_compaction").Parse(historyCompactionPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse history_compaction prompt failed: %w", err)
	}
	taskPlannerTmpl, err := template.New("task_planner").Parse(taskPlanPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse task_planner prompt failed: %w", err)
	}
	agentHandoffTmpl, err := template.New("agent_handoff").Parse(agentHandoffPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse agent_handoff prompt failed: %w", err)
	}
	stepOutcomesReducerTmpl, err := template.New("step_outcomes_reducer").Parse(stepOutcomesReducerPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse step_outcomes_reducer prompt failed: %w", err)
	}
	intentClassificationTmpl, err := template.New("intent_classification").Parse(intentClassificationPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse intent_classification prompt failed: %w", err)
	}
	return &defaultPromptManager{
		thinkActTmpl:             thinkActTmpl,
		stepReplanTmpl:           stepReplanTmpl,
		finalAnswerTmpl:          finalAnswerTmpl,
		historyCompactionTmpl:    historyCompactionTmpl,
		taskPlannerTmpl:          taskPlannerTmpl,
		agentHandoffTmpl:         agentHandoffTmpl,
		stepOutcomesReducerTmpl:  stepOutcomesReducerTmpl,
		intentClassificationTmpl: intentClassificationTmpl,
	}, nil
}

func (m *defaultPromptManager) BuildThinkActPrompt(input ThinkActPromptInput) (string, error) {
	if m == nil || m.thinkActTmpl == nil {
		return "", fmt.Errorf("think_act template is nil")
	}
	hasWorkspaceContext := strings.TrimSpace(input.WorkspaceRootDir) != "" || strings.TrimSpace(input.WorkspaceNamespace) != "" || strings.TrimSpace(input.WorkspaceSharedDir) != ""
	hasRepoContext := strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir) != "" || strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir) != "" || input.RuntimeRepoContext.IsGitRepo

	var taskContextEntries []TaskContextEntry
	if input.TaskContext != nil {
		taskContextEntries = input.TaskContext.VisibleEntries()
	}

	buf := bytes.NewBuffer(nil)
	if err := m.thinkActTmpl.Execute(buf, map[string]any{
		"AGENT_ROLE":                    strings.TrimSpace(input.AgentRole),
		"AGENT_BACKGROUND":              strings.TrimSpace(input.AgentBackground),
		"AGENT_INSTRUCTION":             strings.TrimSpace(input.AgentInstruction),
		"HAS_AGENT_ROLE":                strings.TrimSpace(input.AgentRole) != "",
		"HAS_AGENT_BACKGROUND":          strings.TrimSpace(input.AgentBackground) != "",
		"HAS_AGENT_INSTRUCTION":         strings.TrimSpace(input.AgentInstruction) != "",
		"HAS_WORKSPACE_CONTEXT":         hasWorkspaceContext,
		"WORKSPACE_ROOT_DIR":            strings.TrimSpace(input.WorkspaceRootDir),
		"WORKSPACE_NAMESPACE":           strings.TrimSpace(input.WorkspaceNamespace),
		"WORKSPACE_SHARED_DIR":          strings.TrimSpace(input.WorkspaceSharedDir),
		"HAS_REPO_CONTEXT":              hasRepoContext,
		"SOURCE_WORKING_DIR":            strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir),
		"REPO_ROOT_DIR":                 strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir),
		"IS_GIT_REPO":                   input.RuntimeRepoContext.IsGitRepo,
		"CURRENT_BRANCH":                strings.TrimSpace(input.RuntimeRepoContext.Branch),
		"IS_GIT_WORKTREE":               input.RuntimeRepoContext.IsWorktree,
		"HAS_TASK_CONTEXT":              len(taskContextEntries) > 0,
		"TASK_CONTEXT_ENTRIES":          taskContextEntries,
		"SKILLS_CONTEXT":                input.SkillsContext,
		"CURRENT_STEP":                  prettyJSON(input.CurrentStep),
		"DEPENDENCY_STEP_SUMMARIES":     prettyJSON(input.DependencyStepSummaries),
		"EXECUTION_CONTEXTS":            prettyJSON(input.ExecutionContexts),
		"HAS_CURRENT_STEP":              input.HasCurrentStep,
		"HAS_DEPENDENCY_STEP_SUMMARIES": input.HasDependencySummaries,
		"HAS_EXECUTION_CONTEXTS":        input.HasExecutionContexts,
		"HAS_SKILLS_TABLE":              input.HasSkillsTable,
		"HAS_INJECTED_SKILLS":           input.HasInjectedSkills,
		"MCP_CONTEXT":                   input.MCPContext,
		"HAS_MCP_TABLE":                 input.HasMCPTable,
		"EXTRA_CONTEXT":                 strings.TrimSpace(input.ExtraContext),
		"SUPPORTS_VISION":               input.SupportsVision,
		"CAN_SPAWN_SUBAGENT":            input.CanSpawnSubAgent,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildStepReplanPrompt(input StepReplanPromptInput) (string, error) {
	if m == nil || m.stepReplanTmpl == nil {
		return "", fmt.Errorf("step replan template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.stepReplanTmpl.Execute(buf, map[string]any{
		"AGENT_ROLE":                   strings.TrimSpace(input.AgentRole),
		"AGENT_BACKGROUND":             strings.TrimSpace(input.AgentBackground),
		"AGENT_INSTRUCTION":            strings.TrimSpace(input.AgentInstruction),
		"HAS_AGENT_ROLE":               strings.TrimSpace(input.AgentRole) != "",
		"HAS_AGENT_BACKGROUND":         strings.TrimSpace(input.AgentBackground) != "",
		"HAS_AGENT_INSTRUCTION":        strings.TrimSpace(input.AgentInstruction) != "",
		"CURRENT_GOAL":                 fmt.Sprint(input.CurrentGoal),
		"GOAL_UNDERSTANDING":           strings.TrimSpace(input.GoalUnderstanding),
		"HAS_GOAL_UNDERSTANDING":       strings.TrimSpace(input.GoalUnderstanding) != "",
		"WORKSPACE_SHARED_DIR":         strings.TrimSpace(input.WorkspaceSharedDir),
		"HAS_REPO_CONTEXT":             strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir) != "" || strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir) != "" || input.RuntimeRepoContext.IsGitRepo,
		"SOURCE_WORKING_DIR":           strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir),
		"REPO_ROOT_DIR":                strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir),
		"IS_GIT_REPO":                  input.RuntimeRepoContext.IsGitRepo,
		"CURRENT_BRANCH":               strings.TrimSpace(input.RuntimeRepoContext.Branch),
		"IS_GIT_WORKTREE":              input.RuntimeRepoContext.IsWorktree,
		"INPUT_TIMELINE":               prettyJSON(input.InputTimeline),
		"CURRENT_STEP":                 prettyJSON(input.CurrentStep),
		"STEP_OUTCOME":                 prettyJSON(input.StepOutcome),
		"TASK_PLAN":                    prettyJSON(input.TaskPlan),
		"STEP_OUTCOMES":                prettyJSON(input.StepOutcomes),
		"HAS_CARRIED_INCOMPLETE_ITEMS": anyHasItems(input.CarriedIncompleteItems),
		"CARRIED_INCOMPLETE_ITEMS":     prettyJSON(input.CarriedIncompleteItems),
		"HAS_CARRIED_DEPTH_GAPS":       anyHasItems(input.CarriedDepthGaps),
		"CARRIED_DEPTH_GAPS":           prettyJSON(input.CarriedDepthGaps),
		"HAS_CARRIED_NEW_SURFACES":     anyHasItems(input.CarriedNewSurfaces),
		"CARRIED_NEW_SURFACES":         prettyJSON(input.CarriedNewSurfaces),
		"STEP_RESULT_PATH":             input.StepResultPath,
		"STEP_CONTEXTS_PATH":           input.StepContextsPath,
		"STEP_TRANSCRIPT_PATH":         input.StepTranscriptPath,
		"STEP_TIMELINE_PATH":           input.StepTimelinePath,
		"SKILLS_CONTEXT":               input.SkillsContext,
		"HAS_SKILLS_TABLE":             input.HasSkillsTable,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildFinalAnswerPrompt(input FinalAnswerPromptInput) (string, error) {
	if m == nil || m.finalAnswerTmpl == nil {
		return "", fmt.Errorf("final answer template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.finalAnswerTmpl.Execute(buf, map[string]any{
		"AGENT_ROLE":                   strings.TrimSpace(input.AgentRole),
		"AGENT_BACKGROUND":             strings.TrimSpace(input.AgentBackground),
		"AGENT_INSTRUCTION":            strings.TrimSpace(input.AgentInstruction),
		"HAS_AGENT_ROLE":               strings.TrimSpace(input.AgentRole) != "",
		"HAS_AGENT_BACKGROUND":         strings.TrimSpace(input.AgentBackground) != "",
		"HAS_AGENT_INSTRUCTION":        strings.TrimSpace(input.AgentInstruction) != "",
		"STATUS":                       fmt.Sprint(input.Status),
		"STATE_ERROR":                  fmt.Sprint(input.StateError),
		"INPUT_TIMELINE":               prettyJSON(input.InputTimeline),
		"GOAL_UNDERSTANDING":           strings.TrimSpace(input.GoalUnderstanding),
		"HAS_GOAL_UNDERSTANDING":       strings.TrimSpace(input.GoalUnderstanding) != "",
		"SHOW_PLAN_SECTION":            input.ShowPlanSection,
		"PLAN":                         prettyJSON(input.Plan),
		"PLAN_VERSION":                 prettyJSON(input.PlanVersion),
		"STEP_OUTCOMES":                prettyJSON(input.StepOutcomes),
		"WARNINGS":                     prettyJSON(input.Warnings),
		"HAS_CARRIED_INCOMPLETE_ITEMS": anyHasItems(input.CarriedIncompleteItems),
		"CARRIED_INCOMPLETE_ITEMS":     prettyJSON(input.CarriedIncompleteItems),
		"HAS_CARRIED_DEPTH_GAPS":       anyHasItems(input.CarriedDepthGaps),
		"CARRIED_DEPTH_GAPS":           prettyJSON(input.CarriedDepthGaps),
		"HAS_CARRIED_NEW_SURFACES":     anyHasItems(input.CarriedNewSurfaces),
		"CARRIED_NEW_SURFACES":         prettyJSON(input.CarriedNewSurfaces),
		"WORKSPACE_SHARED_DIR":         strings.TrimSpace(input.WorkspaceSharedDir),
		"HAS_REPO_CONTEXT":             strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir) != "" || strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir) != "" || input.RuntimeRepoContext.IsGitRepo,
		"SOURCE_WORKING_DIR":           strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir),
		"REPO_ROOT_DIR":                strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir),
		"IS_GIT_REPO":                  input.RuntimeRepoContext.IsGitRepo,
		"CURRENT_BRANCH":               strings.TrimSpace(input.RuntimeRepoContext.Branch),
		"IS_GIT_WORKTREE":              input.RuntimeRepoContext.IsWorktree,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildHistoryCompactionPrompt(input HistoryCompactionPromptInput) (string, error) {
	if m == nil || m.historyCompactionTmpl == nil {
		return "", fmt.Errorf("history compaction template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.historyCompactionTmpl.Execute(buf, map[string]any{
		"INSTRUCTION":  strings.TrimSpace(input.Instruction),
		"PREV_SUMMARY": strings.TrimSpace(input.PrevSummary),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildTaskPlannerPrompt(input TaskPlannerPromptInput) (string, error) {
	if m == nil || m.taskPlannerTmpl == nil {
		return "", fmt.Errorf("task planner template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.taskPlannerTmpl.Execute(buf, map[string]any{
		"INPUT":                  strings.TrimSpace(input.Input),
		"GOAL_UNDERSTANDING":     strings.TrimSpace(input.GoalUnderstanding),
		"HAS_GOAL_UNDERSTANDING": strings.TrimSpace(input.GoalUnderstanding) != "",
		"WORKSPACE_SHARED_DIR":   strings.TrimSpace(input.WorkspaceSharedDir),
		"HAS_REPO_CONTEXT":       strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir) != "" || strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir) != "" || input.RuntimeRepoContext.IsGitRepo,
		"SOURCE_WORKING_DIR":     strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir),
		"REPO_ROOT_DIR":          strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir),
		"IS_GIT_REPO":            input.RuntimeRepoContext.IsGitRepo,
		"CURRENT_BRANCH":         strings.TrimSpace(input.RuntimeRepoContext.Branch),
		"IS_GIT_WORKTREE":        input.RuntimeRepoContext.IsWorktree,
		"USER_INPUT_TURN":        input.UserInputTurn,
		"SKILLS_CONTEXT":         input.SkillsContext,
		"MCP_CONTEXT":            input.MCPContext,
		"HAS_SKILLS_TABLE":       input.HasSkillsTable,
		"HAS_MCP_TABLE":          input.HasMCPTable,
		"SKILLS_OVERFLOW_PATH":   strings.TrimSpace(input.SkillsOverflowPath),
		"MCP_OVERFLOW_PATH":      strings.TrimSpace(input.MCPOverflowPath),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildAgentHandoffPrompt(input AgentHandoffPromptInput) (string, error) {
	if m == nil || m.agentHandoffTmpl == nil {
		return "", fmt.Errorf("agent handoff template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.agentHandoffTmpl.Execute(buf, map[string]any{
		"HANDOFF_TO":        strings.TrimSpace(input.HandoffTo),
		"AGENT_INSTRUCTION": strings.TrimSpace(input.AgentInstruction),
		"PREV_SUMMARY":      strings.TrimSpace(input.PrevSummary),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildStepOutcomesReducerPrompt(input StepOutcomesReducerPromptInput) (string, error) {
	if m == nil || m.stepOutcomesReducerTmpl == nil {
		return "", fmt.Errorf("step outcomes reducer template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.stepOutcomesReducerTmpl.Execute(buf, map[string]any{
		"STEP_OUTCOMES": strings.TrimSpace(input.StepOutcomes),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildIntentClassificationPrompt(input IntentClassificationPromptInput) (string, error) {
	if m == nil || m.intentClassificationTmpl == nil {
		return "", fmt.Errorf("intent classification template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.intentClassificationTmpl.Execute(buf, map[string]any{
		"PREVIOUS_GOAL":       strings.TrimSpace(input.PreviousGoal),
		"COMPLETED_COUNT":     input.CompletedCount,
		"TOTAL_COUNT":         input.TotalCount,
		"HAS_RECENT_OUTCOMES": len(input.RecentOutcomes) > 0,
		"RECENT_OUTCOMES":     input.RecentOutcomes,
		"HAS_PENDING_STEPS":   len(input.PendingSteps) > 0,
		"PENDING_STEPS":       input.PendingSteps,
		"INPUT_TIMELINE":      input.InputTimeline,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
