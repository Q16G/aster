package react

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

type ThinkActPromptInput struct {
	AgentInstruction        string
	TaskContext             *TaskContextData
	WorkspaceRootDir        string
	WorkspaceNamespace      string
	WorkspaceSharedDir      string
	SkillsContext           *SkillsPromptContext
	Phase                   any
	Status                  any
	CurrentGoal             any
	CurrentStepID           any
	CurrentStep             any
	LatestInput             any
	InputTimeline           any
	DependencyStepSummaries any
	ExecutionContexts       any
	HasCurrentGoal          bool
	HasCurrentStepID        bool
	HasCurrentStep          bool
	HasLatestInput          bool
	HasDependencySummaries  bool
	HasExecutionContexts    bool
	HasWarnings             bool
	HasUnresolved           bool
	HasSkillsTable          bool
	HasInjectedSkills       bool
	MCPContext              *MCPPromptContext
	HasMCPTable             bool
	Warnings                any
	Unresolved              any
	ExtraContext            string
	HasOutputContract       bool
	OutputContractName      string
	OutputContractSchema    string
	OutputContractExample   string
}

type StepReplanPromptInput struct {
	CurrentGoal  any
	CurrentStep  any
	StepOutcome  any
	TaskPlan     any
	StepOutcomes any
	Warnings     any
	Unresolved   any
}

type FinalAnswerPromptInput struct {
	Status                  any
	StateError              any
	InputTimeline           any
	ShowPlanSection         bool
	Plan                    any
	PlanVersion             any
	StepOutcomes            any
	Warnings                any
	Unresolved              any
	HasSummaryPolicy        bool
	SummaryPolicyName       string
	SummaryPolicyDetail     string
	PublishedOutputRequired bool
	PublishedOutputName     string
	PublishedOutputSchema   string
	PublishedOutputExample  string
}

type HistoryCompactionPromptInput struct {
	Instruction string
	PrevSummary string
}

type AgentHandoffPromptInput struct {
	HandoffTo        string
	AgentInstruction string
	PrevSummary      string
	Diff             string
}

type StepOutcomesReducerPromptInput struct {
	StepOutcomes string
}

type PromptManager interface {
	BuildThinkActPrompt(input ThinkActPromptInput) (string, error)
	BuildStepReplanPrompt(input StepReplanPromptInput) (string, error)
	BuildFinalAnswerPrompt(input FinalAnswerPromptInput) (string, error)
	BuildHistoryCompactionPrompt(input HistoryCompactionPromptInput) (string, error)
	BuildTaskPlannerPrompt(input string) (string, error)
	BuildAgentHandoffPrompt(input AgentHandoffPromptInput) (string, error)
	BuildStepOutcomesReducerPrompt(input StepOutcomesReducerPromptInput) (string, error)
}

type defaultPromptManager struct {
	thinkActTmpl              *template.Template
	stepReplanTmpl            *template.Template
	finalAnswerTmpl           *template.Template
	historyCompactionTmpl     *template.Template
	taskPlannerTmpl           *template.Template
	agentHandoffTmpl          *template.Template
	stepOutcomesReducerTmpl   *template.Template
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
	return &defaultPromptManager{
		thinkActTmpl:            thinkActTmpl,
		stepReplanTmpl:          stepReplanTmpl,
		finalAnswerTmpl:         finalAnswerTmpl,
		historyCompactionTmpl:   historyCompactionTmpl,
		taskPlannerTmpl:         taskPlannerTmpl,
		agentHandoffTmpl:        agentHandoffTmpl,
		stepOutcomesReducerTmpl: stepOutcomesReducerTmpl,
	}, nil
}

func (m *defaultPromptManager) BuildThinkActPrompt(input ThinkActPromptInput) (string, error) {
	if m == nil || m.thinkActTmpl == nil {
		return "", fmt.Errorf("think_act template is nil")
	}
	hasWorkspaceContext := strings.TrimSpace(input.WorkspaceRootDir) != "" || strings.TrimSpace(input.WorkspaceNamespace) != "" || strings.TrimSpace(input.WorkspaceSharedDir) != ""

	var taskContextEntries []TaskContextEntry
	if input.TaskContext != nil {
		taskContextEntries = input.TaskContext.VisibleEntries()
	}

	buf := bytes.NewBuffer(nil)
	if err := m.thinkActTmpl.Execute(buf, map[string]any{
		"AGENT_INSTRUCTION":             strings.TrimSpace(input.AgentInstruction),
		"HAS_WORKSPACE_CONTEXT":         hasWorkspaceContext,
		"WORKSPACE_ROOT_DIR":            strings.TrimSpace(input.WorkspaceRootDir),
		"WORKSPACE_NAMESPACE":           strings.TrimSpace(input.WorkspaceNamespace),
		"WORKSPACE_SHARED_DIR":          strings.TrimSpace(input.WorkspaceSharedDir),
		"HAS_TASK_CONTEXT":              len(taskContextEntries) > 0,
		"TASK_CONTEXT_ENTRIES":          taskContextEntries,
		"SKILLS_CONTEXT":                input.SkillsContext,
		"PHASE":                         prettyJSON(input.Phase),
		"STATUS":                        prettyJSON(input.Status),
		"CURRENT_GOAL":                  prettyJSON(input.CurrentGoal),
		"CURRENT_STEP_ID":               prettyJSON(input.CurrentStepID),
		"CURRENT_STEP":                  prettyJSON(input.CurrentStep),
		"LATEST_INPUT":                  prettyJSON(input.LatestInput),
		"INPUT_TIMELINE":                prettyJSON(input.InputTimeline),
		"DEPENDENCY_STEP_SUMMARIES":     prettyJSON(input.DependencyStepSummaries),
		"EXECUTION_CONTEXTS":            prettyJSON(input.ExecutionContexts),
		"HAS_CURRENT_GOAL":              input.HasCurrentGoal,
		"HAS_CURRENT_STEP_ID":           input.HasCurrentStepID,
		"HAS_CURRENT_STEP":              input.HasCurrentStep,
		"HAS_LATEST_INPUT":              input.HasLatestInput,
		"HAS_DEPENDENCY_STEP_SUMMARIES": input.HasDependencySummaries,
		"HAS_EXECUTION_CONTEXTS":        input.HasExecutionContexts,
		"HAS_WARNINGS":                  input.HasWarnings,
		"HAS_UNRESOLVED":                input.HasUnresolved,
		"HAS_SKILLS_TABLE":              input.HasSkillsTable,
		"HAS_INJECTED_SKILLS":           input.HasInjectedSkills,
		"MCP_CONTEXT":                   input.MCPContext,
		"HAS_MCP_TABLE":                 input.HasMCPTable,
		"WARNINGS":                      prettyJSON(input.Warnings),
		"UNRESOLVED":                    prettyJSON(input.Unresolved),
		"EXTRA_CONTEXT":                 strings.TrimSpace(input.ExtraContext),
		"HAS_OUTPUT_CONTRACT":           input.HasOutputContract,
		"OUTPUT_CONTRACT_NAME":          strings.TrimSpace(input.OutputContractName),
		"OUTPUT_CONTRACT_SCHEMA":        strings.TrimSpace(input.OutputContractSchema),
		"OUTPUT_CONTRACT_EXAMPLE":       strings.TrimSpace(input.OutputContractExample),
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
		"CURRENT_GOAL":  prettyJSON(input.CurrentGoal),
		"CURRENT_STEP":  prettyJSON(input.CurrentStep),
		"STEP_OUTCOME":  prettyJSON(input.StepOutcome),
		"TASK_PLAN":     prettyJSON(input.TaskPlan),
		"STEP_OUTCOMES": prettyJSON(input.StepOutcomes),
		"WARNINGS":      prettyJSON(input.Warnings),
		"UNRESOLVED":    prettyJSON(input.Unresolved),
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
		"STATUS":                    prettyJSON(input.Status),
		"STATE_ERROR":               prettyJSON(input.StateError),
		"INPUT_TIMELINE":            prettyJSON(input.InputTimeline),
		"SHOW_PLAN_SECTION":         input.ShowPlanSection,
		"PLAN":                      prettyJSON(input.Plan),
		"PLAN_VERSION":              prettyJSON(input.PlanVersion),
		"STEP_OUTCOMES":             prettyJSON(input.StepOutcomes),
		"WARNINGS":                  prettyJSON(input.Warnings),
		"UNRESOLVED":                prettyJSON(input.Unresolved),
		"HAS_SUMMARY_POLICY":        input.HasSummaryPolicy,
		"SUMMARY_POLICY_NAME":       strings.TrimSpace(input.SummaryPolicyName),
		"SUMMARY_POLICY_DETAIL":     strings.TrimSpace(input.SummaryPolicyDetail),
		"PUBLISHED_OUTPUT_REQUIRED": input.PublishedOutputRequired,
		"PUBLISHED_OUTPUT_NAME":     strings.TrimSpace(input.PublishedOutputName),
		"PUBLISHED_OUTPUT_SCHEMA":   strings.TrimSpace(input.PublishedOutputSchema),
		"PUBLISHED_OUTPUT_EXAMPLE":  strings.TrimSpace(input.PublishedOutputExample),
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

func (m *defaultPromptManager) BuildTaskPlannerPrompt(input string) (string, error) {
	if m == nil || m.taskPlannerTmpl == nil {
		return "", fmt.Errorf("task planner template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.taskPlannerTmpl.Execute(buf, map[string]any{
		"INPUT": strings.TrimSpace(input),
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
		"DIFF":              strings.TrimSpace(input.Diff),
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
