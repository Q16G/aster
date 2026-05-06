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
	Nonce                   string
	HasOutputContract       bool
	OutputContractName      string
	OutputContractSchema    string
	OutputContractExample   string
}

type StepSummaryPromptInput struct {
	InputTimeline       any
	CurrentGoal         any
	CurrentStep         any
	TaskPlan            any
	RawOutcome          any
	StepWindow          any
	TimelineDiff        any
	References          any
	Artifacts           any
	Warnings            any
	Unresolved          any
	HasSummaryPolicy    bool
	SummaryPolicyName   string
	SummaryPolicyDetail string
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

type ReducerPromptInput struct {
	StepID          any
	InputTimeline   any
	CurrentStep     any
	RawTimelineDiff any
	DiffSummaryHint any
	References      any
	Artifacts       any
}

type IntentRecognitionPromptInput struct {
	UserInstruction string
	Input           string
	RecentHistory   any
	Nonce           string
}

type SimpleReplyPromptInput struct {
	UserInstruction     string
	IntentSummary       string
	IntentComplexity    string
	MatchedCapabilities any
	ReplyHint           string
	Nonce               string
}

type HistoryCompactionPromptInput struct {
	Instruction string
	PrevSummary string
	Nonce       string
}

type AgentHandoffPromptInput struct {
	HandoffTo        string
	AgentInstruction string
	PrevSummary      string
	Diff             string
	Nonce            string
}

type PromptManager interface {
	BuildThinkActPrompt(input ThinkActPromptInput) (string, error)
	BuildStepSummaryPrompt(input StepSummaryPromptInput) (string, error)
	BuildFinalAnswerPrompt(input FinalAnswerPromptInput) (string, error)
	BuildReducerPrompt(input ReducerPromptInput) (string, error)
	BuildIntentRecognitionPrompt(input IntentRecognitionPromptInput) (string, error)
	BuildSimpleReplyPrompt(input SimpleReplyPromptInput) (string, error)
	BuildHistoryCompactionPrompt(input HistoryCompactionPromptInput) (string, error)
	BuildTaskPlannerPrompt(input string) (string, error)
	BuildAgentHandoffPrompt(input AgentHandoffPromptInput) (string, error)
}

type defaultPromptManager struct {
	thinkActTmpl          *template.Template
	stepSummaryTmpl       *template.Template
	finalAnswerTmpl       *template.Template
	reducerTmpl           *template.Template
	intentRecognitionTmpl *template.Template
	simpleReplyTmpl       *template.Template
	historyCompactionTmpl *template.Template
	taskPlannerTmpl       *template.Template
	agentHandoffTmpl      *template.Template
}

func newDefaultPromptManager() (PromptManager, error) {
	thinkActTmpl, err := template.New("think_act").Parse(thinkActPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse think_act prompt failed: %w", err)
	}
	stepSummaryTmpl, err := template.New("step_summary").Parse(stepSummaryPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse step_summary prompt failed: %w", err)
	}
	finalAnswerTmpl, err := template.New("final_answer").Parse(finalAnswerPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse final_answer prompt failed: %w", err)
	}
	reducerTmpl, err := template.New("reducer").Parse(reducerPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse reducer prompt failed: %w", err)
	}
	intentRecognitionTmpl, err := template.New("intent_recognition").Parse(intentRecognitionPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse intent_recognition prompt failed: %w", err)
	}
	simpleReplyTmpl, err := template.New("simple_reply").Parse(simpleReplyPrompt)
	if err != nil {
		return nil, fmt.Errorf("parse simple_reply prompt failed: %w", err)
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
	return &defaultPromptManager{
		thinkActTmpl:          thinkActTmpl,
		stepSummaryTmpl:       stepSummaryTmpl,
		finalAnswerTmpl:       finalAnswerTmpl,
		reducerTmpl:           reducerTmpl,
		intentRecognitionTmpl: intentRecognitionTmpl,
		simpleReplyTmpl:       simpleReplyTmpl,
		historyCompactionTmpl: historyCompactionTmpl,
		taskPlannerTmpl:       taskPlannerTmpl,
		agentHandoffTmpl:      agentHandoffTmpl,
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
		"NONCE":                         strings.TrimSpace(input.Nonce),
		"HAS_OUTPUT_CONTRACT":           input.HasOutputContract,
		"OUTPUT_CONTRACT_NAME":          strings.TrimSpace(input.OutputContractName),
		"OUTPUT_CONTRACT_SCHEMA":        strings.TrimSpace(input.OutputContractSchema),
		"OUTPUT_CONTRACT_EXAMPLE":       strings.TrimSpace(input.OutputContractExample),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildStepSummaryPrompt(input StepSummaryPromptInput) (string, error) {
	if m == nil || m.stepSummaryTmpl == nil {
		return "", fmt.Errorf("step summary template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.stepSummaryTmpl.Execute(buf, map[string]any{
		"INPUT_TIMELINE":        prettyJSON(input.InputTimeline),
		"CURRENT_GOAL":          prettyJSON(input.CurrentGoal),
		"CURRENT_STEP":          prettyJSON(input.CurrentStep),
		"TASK_PLAN":             prettyJSON(input.TaskPlan),
		"RAW_OUTCOME":           prettyJSON(input.RawOutcome),
		"STEP_WINDOW":           prettyJSON(input.StepWindow),
		"TIMELINE_DIFF":         prettyJSON(input.TimelineDiff),
		"REFERENCES":            prettyJSON(input.References),
		"ARTIFACTS":             prettyJSON(input.Artifacts),
		"WARNINGS":              prettyJSON(input.Warnings),
		"UNRESOLVED":            prettyJSON(input.Unresolved),
		"HAS_SUMMARY_POLICY":    input.HasSummaryPolicy,
		"SUMMARY_POLICY_NAME":   strings.TrimSpace(input.SummaryPolicyName),
		"SUMMARY_POLICY_DETAIL": strings.TrimSpace(input.SummaryPolicyDetail),
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

func (m *defaultPromptManager) BuildReducerPrompt(input ReducerPromptInput) (string, error) {
	if m == nil || m.reducerTmpl == nil {
		return "", fmt.Errorf("reducer template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.reducerTmpl.Execute(buf, map[string]any{
		"STEP_ID":           prettyJSON(input.StepID),
		"INPUT_TIMELINE":    prettyJSON(input.InputTimeline),
		"CURRENT_STEP":      prettyJSON(input.CurrentStep),
		"RAW_TIMELINE_DIFF": prettyJSON(input.RawTimelineDiff),
		"DIFF_SUMMARY_HINT": prettyJSON(input.DiffSummaryHint),
		"REFERENCES":        prettyJSON(input.References),
		"ARTIFACTS":         prettyJSON(input.Artifacts),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildIntentRecognitionPrompt(input IntentRecognitionPromptInput) (string, error) {
	if m == nil || m.intentRecognitionTmpl == nil {
		return "", fmt.Errorf("intent recognition template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.intentRecognitionTmpl.Execute(buf, map[string]any{
		"USER_INSTRUCTION": strings.TrimSpace(input.UserInstruction),
		"INPUT":            strings.TrimSpace(input.Input),
		"RECENT_HISTORY":   prettyJSON(input.RecentHistory),
		"NONCE":            strings.TrimSpace(input.Nonce),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *defaultPromptManager) BuildSimpleReplyPrompt(input SimpleReplyPromptInput) (string, error) {
	if m == nil || m.simpleReplyTmpl == nil {
		return "", fmt.Errorf("simple reply template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := m.simpleReplyTmpl.Execute(buf, map[string]any{
		"USER_INSTRUCTION":     strings.TrimSpace(input.UserInstruction),
		"INTENT_SUMMARY":       strings.TrimSpace(input.IntentSummary),
		"INTENT_COMPLEXITY":    strings.TrimSpace(input.IntentComplexity),
		"MATCHED_CAPABILITIES": prettyJSON(input.MatchedCapabilities),
		"REPLY_HINT":           strings.TrimSpace(input.ReplyHint),
		"NONCE":                strings.TrimSpace(input.Nonce),
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
		"NONCE":        strings.TrimSpace(input.Nonce),
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
		"NONCE":             strings.TrimSpace(input.Nonce),
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
