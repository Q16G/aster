package react

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

type ThinkActPromptInput struct {
	AgentRole         string
	AgentBackground   string
	GoalUnderstanding string
	SkillsContext     *SkillsPromptContext
	CurrentStep       any
	// DependencyPlanItems 是前置依赖步骤的 plan_item 产出卡片（内联小字段 + 文件指针），
	// 替代旧的 DEPENDENCY_STEP_SUMMARIES / EXECUTION_CONTEXTS 全量注入。
	DependencyPlanItems any
	// CurrentStepFilePath 是 runtime 预创建的 step 过程文件绝对路径
	//（shared/step_<step_id>.md），step 内恒定，随首条 user message 冻结。
	CurrentStepFilePath    string
	HasCurrentStep         bool
	HasDependencyPlanItems bool
	HasSkillsTable         bool
	HasInjectedSkills      bool
	MCPContext             *MCPPromptContext
	HasMCPTable            bool
	ExtraContext           string
	SupportsVision         bool
	CanSpawnSubAgent       bool
}

// AvailableToolInfo is a render-friendly view of a tool that is actually
// available to the agent in a given phase, used to render the prompt's tool
// list dynamically instead of hard-coding it (so the advertised set always
// matches the registered set).
type AvailableToolInfo struct {
	Name        string
	Description string
}

type StepReplanPromptInput struct {
	AgentRole         string
	AgentBackground   string
	CurrentGoal       any
	GoalUnderstanding string
	InputTimeline     any
	// CurrentStepCard 是当前 step 产出的 plan_item 卡片投影（替代旧 STEP_OUTCOME/
	// CURRENT_STEP 全量注入）；PlanOverview 是全部步骤的 slim 全量卡片（去 digest，
	// 含产出小字段与文件指针）。账本与事实板全文默认注入（设计 3.1）。
	CurrentStepCard      any
	PlanOverview         any
	OpenItemsLedger      string
	TaskContextBoard     string
	StepFileContent      string
	StepResultPath       string
	StepContextsPath     string
	StepTranscriptPath   string
	StepTimelinePath     string
	OpenItemsArchivePath string
	// PlannerJournalPath 是 workspace/planner.jsonl（plan 唯一真相源）绝对路径，
	// 文件存在才注入，供卡片不足时按需回读。
	PlannerJournalPath string
	SkillsContext        *SkillsPromptContext
	HasSkillsTable       bool
	AvailableTools       []AvailableToolInfo
	HasAvailableTools    bool
}

type FinalAnswerPromptInput struct {
	AgentRole         string
	AgentBackground   string
	Status            any
	StateError        any
	InputTimeline     any
	GoalUnderstanding string
	// PlanItems 是 plan 真相源投影卡片（内联小字段 + 文件指针），替代旧
	// PLAN/STEP_OUTCOMES/CARRIED_* 全量注入；OpenItemsLedger 是账本全文（F6 归置对象）。
	PlanItems       any
	OpenItemsLedger string
	Warnings        any
	// PlannerJournalPath 是 workspace/planner.jsonl（plan 唯一真相源）绝对路径，
	// 文件存在才注入，供卡片不足时按需回读。
	PlannerJournalPath string
}

// AgentIdentityEnvPromptInput 渲染公共 system block2：AGENT_INSTRUCTION + <env> 块。
// 全部输入为 run 内稳定值，渲染结果在 Agent 上缓存一次、各阶段复用（字节一致）。
// 注：AgentRole / AgentBackground 已下沉至各 phase prompt 顶部（# Role / # Background
// 段），不再由本块渲染——见 internal/react/prompts/README.md。
type AgentIdentityEnvPromptInput struct {
	AgentInstruction   string
	WorkspaceRootDir   string
	WorkspaceNamespace string
	WorkspaceSharedDir string
	RuntimeRepoContext RuntimeRepoContext
	TaskContext        *TaskContextData
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
	AgentRole         string
	AgentBackground   string
	Input             string
	GoalUnderstanding string
	UserInputTurn     bool
	// TaskContextBoard 为共享区事实板（task_context.md）当前快照，
	// 仅 UserInputTurn=true 时注入，供 planner 对照当前输入做校正。
	TaskContextBoard string
	// HasReplanContext 标记本回合为重规划回合（输入含 <REPLAN_CONTEXT>），
	// 模板据此渲染重规划编排段。
	HasReplanContext   bool
	SkillsContext      *SkillsPromptContext
	MCPContext         *MCPPromptContext
	HasSkillsTable     bool
	HasMCPTable        bool
	SkillsOverflowPath string
	MCPOverflowPath    string
	AvailableTools     []AvailableToolInfo
	HasAvailableTools  bool
}

type IntentClassificationPromptInput struct {
	AgentRole         string
	AgentBackground   string
	GoalUnderstanding string
	PreviousGoal      string
	Status            string
	HasFinalAnswer    bool
	Interrupted       bool
	CompletedCount    int
	TotalCount        int
	RecentOutcomes    []IntentOutcomeSummary
	PendingSteps      []IntentPendingStep
	InputTimeline     []IntentTimelineEntry
	LatestInput       string
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
	BuildThinkActPrompt(input ThinkActPromptInput) (PromptParts, error)
	BuildStepReplanPrompt(input StepReplanPromptInput) (PromptParts, error)
	BuildFinalAnswerPrompt(input FinalAnswerPromptInput) (PromptParts, error)
	BuildTaskPlannerPrompt(input TaskPlannerPromptInput) (PromptParts, error)
	BuildIntentClassificationPrompt(input IntentClassificationPromptInput) (PromptParts, error)
	BuildAgentIdentityEnvPrompt(input AgentIdentityEnvPromptInput) (string, error)
	BuildHistoryCompactionPrompt(input HistoryCompactionPromptInput) (string, error)
	BuildAgentHandoffPrompt(input AgentHandoffPromptInput) (string, error)
	BuildStepOutcomesReducerPrompt(input StepOutcomesReducerPromptInput) (string, error)
}

type defaultPromptManager struct {
	thinkActSystemTmpl             *template.Template
	thinkActUserTmpl               *template.Template
	stepReplanSystemTmpl           *template.Template
	stepReplanUserTmpl             *template.Template
	finalAnswerSystemTmpl          *template.Template
	finalAnswerUserTmpl            *template.Template
	taskPlannerSystemTmpl          *template.Template
	taskPlannerUserTmpl            *template.Template
	intentClassificationSystemTmpl *template.Template
	intentClassificationUserTmpl   *template.Template
	agentIdentityEnvTmpl           *template.Template
	historyCompactionTmpl          *template.Template
	agentHandoffTmpl               *template.Template
	stepOutcomesReducerTmpl        *template.Template
}

func newDefaultPromptManager() (PromptManager, error) {
	parse := func(name, text string) (*template.Template, error) {
		tmpl, err := template.New(name).Parse(text)
		if err != nil {
			return nil, fmt.Errorf("parse %s prompt failed: %w", name, err)
		}
		return tmpl, nil
	}
	m := &defaultPromptManager{}
	var err error
	if m.thinkActSystemTmpl, err = parse("think_act_system", thinkActSystemPrompt); err != nil {
		return nil, err
	}
	if m.thinkActUserTmpl, err = parse("think_act_user", thinkActUserPrompt); err != nil {
		return nil, err
	}
	if m.stepReplanSystemTmpl, err = parse("step_replan_system", stepReplanSystemPrompt); err != nil {
		return nil, err
	}
	if m.stepReplanUserTmpl, err = parse("step_replan_user", stepReplanUserPrompt); err != nil {
		return nil, err
	}
	if m.finalAnswerSystemTmpl, err = parse("final_answer_system", finalAnswerSystemPrompt); err != nil {
		return nil, err
	}
	if m.finalAnswerUserTmpl, err = parse("final_answer_user", finalAnswerUserPrompt); err != nil {
		return nil, err
	}
	if m.taskPlannerSystemTmpl, err = parse("task_planner_system", taskPlannerSystemPrompt); err != nil {
		return nil, err
	}
	if m.taskPlannerUserTmpl, err = parse("task_planner_user", taskPlannerUserPrompt); err != nil {
		return nil, err
	}
	if m.intentClassificationSystemTmpl, err = parse("intent_classification_system", intentClassificationSystemPrompt); err != nil {
		return nil, err
	}
	if m.intentClassificationUserTmpl, err = parse("intent_classification_user", intentClassificationUserPrompt); err != nil {
		return nil, err
	}
	if m.agentIdentityEnvTmpl, err = parse("agent_identity_env", agentIdentityEnvPrompt); err != nil {
		return nil, err
	}
	if m.historyCompactionTmpl, err = parse("history_compaction", historyCompactionPrompt); err != nil {
		return nil, err
	}
	if m.agentHandoffTmpl, err = parse("agent_handoff", agentHandoffPrompt); err != nil {
		return nil, err
	}
	if m.stepOutcomesReducerTmpl, err = parse("step_outcomes_reducer", stepOutcomesReducerPrompt); err != nil {
		return nil, err
	}
	return m, nil
}

func renderTemplate(tmpl *template.Template, data map[string]any) (string, error) {
	if tmpl == nil {
		return "", fmt.Errorf("template is nil")
	}
	buf := bytes.NewBuffer(nil)
	if err := tmpl.Execute(buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// renderPromptParts 渲染 system/user 双模板并校验双部分非空（五个 ReAct 形态
// prompt 的硬性约束：请求结构恒为 system + 首条 user message + stepHistory）。
func renderPromptParts(family string, systemTmpl, userTmpl *template.Template, systemData, userData map[string]any) (PromptParts, error) {
	system, err := renderTemplate(systemTmpl, systemData)
	if err != nil {
		return PromptParts{}, fmt.Errorf("render %s system prompt failed: %w", family, err)
	}
	user, err := renderTemplate(userTmpl, userData)
	if err != nil {
		return PromptParts{}, fmt.Errorf("render %s user prompt failed: %w", family, err)
	}
	if system == "" || user == "" {
		return PromptParts{}, fmt.Errorf("%s prompt requires non-empty system and user parts (system=%d user=%d bytes)", family, len(system), len(user))
	}
	return PromptParts{SystemRules: system, User: user}, nil
}

func (m *defaultPromptManager) BuildThinkActPrompt(input ThinkActPromptInput) (PromptParts, error) {
	if m == nil {
		return PromptParts{}, fmt.Errorf("prompt manager is nil")
	}
	systemData := map[string]any{
		"AGENT_ROLE":           strings.TrimSpace(input.AgentRole),
		"AGENT_BACKGROUND":     strings.TrimSpace(input.AgentBackground),
		"HAS_AGENT_ROLE":       strings.TrimSpace(input.AgentRole) != "",
		"HAS_AGENT_BACKGROUND": strings.TrimSpace(input.AgentBackground) != "",
		"SUPPORTS_VISION":      input.SupportsVision,
		"CAN_SPAWN_SUBAGENT":   input.CanSpawnSubAgent,
	}
	userData := map[string]any{
		"GOAL_UNDERSTANDING":        strings.TrimSpace(input.GoalUnderstanding),
		"SKILLS_CONTEXT":            input.SkillsContext,
		"CURRENT_STEP":              prettyJSON(input.CurrentStep),
		"STEP_FILE_PATH":            strings.TrimSpace(input.CurrentStepFilePath),
		"DEPENDENCY_PLAN_ITEMS":     prettyJSON(input.DependencyPlanItems),
		"HAS_CURRENT_STEP":          input.HasCurrentStep,
		"HAS_DEPENDENCY_PLAN_ITEMS": input.HasDependencyPlanItems,
		"HAS_SKILLS_TABLE":          input.HasSkillsTable,
		"HAS_INJECTED_SKILLS":       input.HasInjectedSkills,
		"MCP_CONTEXT":               input.MCPContext,
		"HAS_MCP_TABLE":             input.HasMCPTable,
		"EXTRA_CONTEXT":             strings.TrimSpace(input.ExtraContext),
	}
	return renderPromptParts("think_act", m.thinkActSystemTmpl, m.thinkActUserTmpl, systemData, userData)
}

func (m *defaultPromptManager) BuildStepReplanPrompt(input StepReplanPromptInput) (PromptParts, error) {
	if m == nil {
		return PromptParts{}, fmt.Errorf("prompt manager is nil")
	}
	systemData := map[string]any{
		"AGENT_ROLE":           strings.TrimSpace(input.AgentRole),
		"AGENT_BACKGROUND":     strings.TrimSpace(input.AgentBackground),
		"HAS_AGENT_ROLE":       strings.TrimSpace(input.AgentRole) != "",
		"HAS_AGENT_BACKGROUND": strings.TrimSpace(input.AgentBackground) != "",
	}
	userData := map[string]any{
		"CURRENT_GOAL":            fmt.Sprint(input.CurrentGoal),
		"GOAL_UNDERSTANDING":      strings.TrimSpace(input.GoalUnderstanding),
		"HAS_GOAL_UNDERSTANDING":  strings.TrimSpace(input.GoalUnderstanding) != "",
		"INPUT_TIMELINE":          prettyJSON(input.InputTimeline),
		"CURRENT_STEP_CARD":       prettyJSON(input.CurrentStepCard),
		"PLAN_OVERVIEW":           prettyJSON(input.PlanOverview),
		"OPEN_ITEMS_LEDGER":       strings.TrimSpace(input.OpenItemsLedger),
		"TASK_CONTEXT_BOARD":      strings.TrimSpace(input.TaskContextBoard),
		"STEP_FILE_CONTENT":       strings.TrimSpace(input.StepFileContent),
		"HAS_STEP_FILE_CONTENT":   strings.TrimSpace(input.StepFileContent) != "",
		"STEP_RESULT_PATH":        input.StepResultPath,
		"STEP_CONTEXTS_PATH":      input.StepContextsPath,
		"STEP_TRANSCRIPT_PATH":    input.StepTranscriptPath,
		"STEP_TIMELINE_PATH":      input.StepTimelinePath,
		"OPEN_ITEMS_ARCHIVE_PATH": input.OpenItemsArchivePath,
		"PLANNER_JOURNAL_PATH":    input.PlannerJournalPath,
		"SKILLS_CONTEXT":          input.SkillsContext,
		"HAS_SKILLS_TABLE":        input.HasSkillsTable,
		"AVAILABLE_TOOLS":         input.AvailableTools,
		"HAS_AVAILABLE_TOOLS":     input.HasAvailableTools,
	}
	return renderPromptParts("step_replan", m.stepReplanSystemTmpl, m.stepReplanUserTmpl, systemData, userData)
}

func (m *defaultPromptManager) BuildFinalAnswerPrompt(input FinalAnswerPromptInput) (PromptParts, error) {
	if m == nil {
		return PromptParts{}, fmt.Errorf("prompt manager is nil")
	}
	systemData := map[string]any{
		"AGENT_ROLE":           strings.TrimSpace(input.AgentRole),
		"AGENT_BACKGROUND":     strings.TrimSpace(input.AgentBackground),
		"HAS_AGENT_ROLE":       strings.TrimSpace(input.AgentRole) != "",
		"HAS_AGENT_BACKGROUND": strings.TrimSpace(input.AgentBackground) != "",
	}
	userData := map[string]any{
		"STATUS":                 fmt.Sprint(input.Status),
		"STATE_ERROR":            fmt.Sprint(input.StateError),
		"INPUT_TIMELINE":         prettyJSON(input.InputTimeline),
		"GOAL_UNDERSTANDING":     strings.TrimSpace(input.GoalUnderstanding),
		"HAS_GOAL_UNDERSTANDING": strings.TrimSpace(input.GoalUnderstanding) != "",
		"PLAN_ITEMS":             prettyJSON(input.PlanItems),
		"PLANNER_JOURNAL_PATH":   strings.TrimSpace(input.PlannerJournalPath),
		"OPEN_ITEMS_LEDGER":      strings.TrimSpace(input.OpenItemsLedger),
		"WARNINGS":               prettyJSON(input.Warnings),
	}
	return renderPromptParts("final_answer", m.finalAnswerSystemTmpl, m.finalAnswerUserTmpl, systemData, userData)
}

func (m *defaultPromptManager) BuildTaskPlannerPrompt(input TaskPlannerPromptInput) (PromptParts, error) {
	if m == nil {
		return PromptParts{}, fmt.Errorf("prompt manager is nil")
	}
	systemData := map[string]any{
		"AGENT_ROLE":           strings.TrimSpace(input.AgentRole),
		"AGENT_BACKGROUND":     strings.TrimSpace(input.AgentBackground),
		"HAS_AGENT_ROLE":       strings.TrimSpace(input.AgentRole) != "",
		"HAS_AGENT_BACKGROUND": strings.TrimSpace(input.AgentBackground) != "",
		"USER_INPUT_TURN":      input.UserInputTurn,
		"HAS_REPLAN_CONTEXT":   input.HasReplanContext,
	}
	userData := map[string]any{
		"INPUT":                  strings.TrimSpace(input.Input),
		"GOAL_UNDERSTANDING":     strings.TrimSpace(input.GoalUnderstanding),
		"HAS_GOAL_UNDERSTANDING": strings.TrimSpace(input.GoalUnderstanding) != "",
		"TASK_CONTEXT_BOARD":     strings.TrimSpace(input.TaskContextBoard),
		"HAS_TASK_CONTEXT_BOARD": strings.TrimSpace(input.TaskContextBoard) != "",
		"SKILLS_CONTEXT":         input.SkillsContext,
		"MCP_CONTEXT":            input.MCPContext,
		"HAS_SKILLS_TABLE":       input.HasSkillsTable,
		"HAS_MCP_TABLE":          input.HasMCPTable,
		"SKILLS_OVERFLOW_PATH":   strings.TrimSpace(input.SkillsOverflowPath),
		"MCP_OVERFLOW_PATH":      strings.TrimSpace(input.MCPOverflowPath),
		"AVAILABLE_TOOLS":        input.AvailableTools,
		"HAS_AVAILABLE_TOOLS":    input.HasAvailableTools,
	}
	return renderPromptParts("task_planner", m.taskPlannerSystemTmpl, m.taskPlannerUserTmpl, systemData, userData)
}

func (m *defaultPromptManager) BuildIntentClassificationPrompt(input IntentClassificationPromptInput) (PromptParts, error) {
	if m == nil {
		return PromptParts{}, fmt.Errorf("prompt manager is nil")
	}
	systemData := map[string]any{
		"AGENT_ROLE":           strings.TrimSpace(input.AgentRole),
		"AGENT_BACKGROUND":     strings.TrimSpace(input.AgentBackground),
		"HAS_AGENT_ROLE":       strings.TrimSpace(input.AgentRole) != "",
		"HAS_AGENT_BACKGROUND": strings.TrimSpace(input.AgentBackground) != "",
	}
	userData := map[string]any{
		"GOAL_UNDERSTANDING":     strings.TrimSpace(input.GoalUnderstanding),
		"HAS_GOAL_UNDERSTANDING": strings.TrimSpace(input.GoalUnderstanding) != "",
		"PREVIOUS_GOAL":          strings.TrimSpace(input.PreviousGoal),
		"STATUS":                 strings.TrimSpace(input.Status),
		"HAS_FINAL_ANSWER":       input.HasFinalAnswer,
		"INTERRUPTED":            input.Interrupted,
		"COMPLETED_COUNT":        input.CompletedCount,
		"TOTAL_COUNT":            input.TotalCount,
		"HAS_RECENT_OUTCOMES":    len(input.RecentOutcomes) > 0,
		"RECENT_OUTCOMES":        input.RecentOutcomes,
		"HAS_PENDING_STEPS":      len(input.PendingSteps) > 0,
		"PENDING_STEPS":          input.PendingSteps,
		"INPUT_TIMELINE":         input.InputTimeline,
		"LATEST_INPUT":           strings.TrimSpace(input.LatestInput),
	}
	return renderPromptParts("intent_classification", m.intentClassificationSystemTmpl, m.intentClassificationUserTmpl, systemData, userData)
}

func (m *defaultPromptManager) BuildAgentIdentityEnvPrompt(input AgentIdentityEnvPromptInput) (string, error) {
	if m == nil || m.agentIdentityEnvTmpl == nil {
		return "", fmt.Errorf("agent identity env template is nil")
	}
	hasRepoContext := strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir) != "" || strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir) != "" || input.RuntimeRepoContext.IsGitRepo

	var taskContextEntries []TaskContextEntry
	if input.TaskContext != nil {
		taskContextEntries = input.TaskContext.VisibleEntries()
	}

	return renderTemplate(m.agentIdentityEnvTmpl, map[string]any{
		"AGENT_INSTRUCTION":     strings.TrimSpace(input.AgentInstruction),
		"HAS_AGENT_INSTRUCTION": strings.TrimSpace(input.AgentInstruction) != "",
		"WORKSPACE_ROOT_DIR":    strings.TrimSpace(input.WorkspaceRootDir),
		"WORKSPACE_NAMESPACE":   strings.TrimSpace(input.WorkspaceNamespace),
		"WORKSPACE_SHARED_DIR":  strings.TrimSpace(input.WorkspaceSharedDir),
		"HAS_REPO_CONTEXT":      hasRepoContext,
		"SOURCE_WORKING_DIR":    strings.TrimSpace(input.RuntimeRepoContext.SourceWorkingDir),
		"REPO_ROOT_DIR":         strings.TrimSpace(input.RuntimeRepoContext.RepoRootDir),
		"IS_GIT_REPO":           input.RuntimeRepoContext.IsGitRepo,
		"CURRENT_BRANCH":        strings.TrimSpace(input.RuntimeRepoContext.Branch),
		"HAS_TASK_CONTEXT":      len(taskContextEntries) > 0,
		"TASK_CONTEXT_ENTRIES":  taskContextEntries,
	})
}

func (m *defaultPromptManager) BuildHistoryCompactionPrompt(input HistoryCompactionPromptInput) (string, error) {
	if m == nil || m.historyCompactionTmpl == nil {
		return "", fmt.Errorf("history compaction template is nil")
	}
	return renderTemplate(m.historyCompactionTmpl, map[string]any{
		"INSTRUCTION":  strings.TrimSpace(input.Instruction),
		"PREV_SUMMARY": strings.TrimSpace(input.PrevSummary),
	})
}

func (m *defaultPromptManager) BuildAgentHandoffPrompt(input AgentHandoffPromptInput) (string, error) {
	if m == nil || m.agentHandoffTmpl == nil {
		return "", fmt.Errorf("agent handoff template is nil")
	}
	return renderTemplate(m.agentHandoffTmpl, map[string]any{
		"HANDOFF_TO":        strings.TrimSpace(input.HandoffTo),
		"AGENT_INSTRUCTION": strings.TrimSpace(input.AgentInstruction),
		"PREV_SUMMARY":      strings.TrimSpace(input.PrevSummary),
	})
}

func (m *defaultPromptManager) BuildStepOutcomesReducerPrompt(input StepOutcomesReducerPromptInput) (string, error) {
	if m == nil || m.stepOutcomesReducerTmpl == nil {
		return "", fmt.Errorf("step outcomes reducer template is nil")
	}
	return renderTemplate(m.stepOutcomesReducerTmpl, map[string]any{
		"STEP_OUTCOMES": strings.TrimSpace(input.StepOutcomes),
	})
}
