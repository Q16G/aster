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
	CurrentStepFilePath string
	// OpenItemsLedgerPath / TaskContextPath 是共享区账本（单文件三区）、事实板文件绝对路径
	//（workspace_runtime.EnsureSharedScaffold 已预置骨架）；think_act 按 system prompt 共享区
	// 维护契约即时维护账本、按"解决即归档"持续不变量在 step 执行中把闭环项就地迁入 `## 已闭环`、
	// 并把高利用价值具体值按入板闸门补入事实板 `## 执行中补充`，与 step_replan 的账本/事实板
	// 维护构成"边执行边归档 + 收尾复核"双写双校。
	OpenItemsLedgerPath    string
	TaskContextPath        string
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
	AgentRole       string
	AgentBackground string
	// IsSubAgent 与 task_planner 同语义：标记本回合发生在子 Agent 内部，
	// 用于让共享 planning_system.prompt 的子 Agent 守卫一致渲染。
	IsSubAgent        bool
	CurrentGoal       any
	GoalUnderstanding string
	InputTimeline     any
	// CurrentStepCard 是当前 step 产出的 plan_item 卡片投影（替代旧 STEP_OUTCOME/
	// CURRENT_STEP 全量注入）；PlanOverview 是全部步骤的 slim 全量卡片（去 digest，
	// 含产出小字段与文件指针）。账本与事实板全文默认注入（设计 3.1）。
	CurrentStepCard    any
	PlanOverview       any
	OpenItemsLedger    string
	TaskContextBoard   string
	StepFileContent    string
	StepResultPath     string
	StepContextsPath   string
	StepTranscriptPath string
	StepTimelinePath   string
	// OpenItemsPath / TaskContextPath / StepFilePath 是本相位直接维护的三个共享区
	// 文件绝对路径（账本单文件三区 / 事实板 / step 过程文件），供落盘补正直接寻址。
	OpenItemsPath   string
	TaskContextPath string
	StepFilePath    string
	// PlannerJournalPath 是 workspace/planner.jsonl（plan 唯一真相源）绝对路径，
	// 文件存在才注入；与 PlannerJournal 全文并存，作为超限截断兜底入口。
	PlannerJournalPath string
	// PlannerJournal 是 workspace/planner.jsonl 全文快照（plan 唯一真相源），
	// 与 OpenItemsLedger / TaskContextBoard 同为判定 SoT；超限走 readSharedFileForPromptWithLimit
	// 同款截断策略并在尾部追加路径提示，PlannerJournalPath 兜底回读。
	PlannerJournal    string
	SkillsContext     *SkillsPromptContext
	HasSkillsTable    bool
	AvailableTools    []AvailableToolInfo
	HasAvailableTools bool
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
	// IsSubAgent 标记本 planner 回合发生在子 Agent 内部；用于让模板对子 Agent
	// 关闭"顶层 planner 维护事实板终态"的契约段（子 Agent 工作区不承担顶层
	// 事实板维护责任，避免被强制注入）。
	IsSubAgent bool
	// CanSpawnSubAgent 控制 planner 阶段 sub_agent 委派条款的渲染——顶层
	// planner 允许委派调研深化子 Agent，子 Agent 与平台未开放委派时为 false。
	CanSpawnSubAgent bool
	// TaskContextBoard 为共享区事实板（task_context.md）当前快照，
	// 仅 UserInputTurn=true 时注入，供 planner 对照当前输入做校正。
	TaskContextBoard string
	// TaskContextPath / OpenItemsLedgerPath 是共享区事实板、账本（单文件三区）文件绝对路径
	//（workspace_runtime.EnsureSharedScaffold 已预置骨架）；planning_system.prompt 的
	// "共享区直接维护文件"段据此渲染绝对路径锚点，让 planner 在 submit_plan 前的"输入事实"
	// 落盘、维护账本至终态时直接寻址，不再靠 WORKSPACE_SHARED_DIR + 文件名拼装。
	TaskContextPath     string
	OpenItemsLedgerPath string
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
	thinkActSystemTmpl *template.Template
	thinkActUserTmpl   *template.Template
	// planningSystemTmpl 是 plan / step_replan 共用的 system 模板（单文件，
	// IS_REPLAN_PHASE 分阶段渲染）；两相位的 user 模板仍各自独立。
	planningSystemTmpl             *template.Template
	stepReplanUserTmpl             *template.Template
	finalAnswerSystemTmpl          *template.Template
	finalAnswerUserTmpl            *template.Template
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
	if m.planningSystemTmpl, err = parse("planning_system", planningSystemPrompt); err != nil {
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
		"OPEN_ITEMS_LEDGER_PATH":    strings.TrimSpace(input.OpenItemsLedgerPath),
		"TASK_CONTEXT_PATH":         strings.TrimSpace(input.TaskContextPath),
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
		"IS_REPLAN_PHASE":      true,
		"IS_SUB_AGENT":         input.IsSubAgent,
		"CAN_SPAWN_SUBAGENT":   false,
		// 共享区直接维护文件的绝对路径下沉到 system 模板，承担"哪个文件由本相位
		// 直接落盘"的稳定契约——user 模板里的同名条目已删除（见 step_replan_user.prompt）。
		"OPEN_ITEMS_PATH":   strings.TrimSpace(input.OpenItemsPath),
		"TASK_CONTEXT_PATH": strings.TrimSpace(input.TaskContextPath),
		"STEP_FILE_PATH":    strings.TrimSpace(input.StepFilePath),
	}
	userData := map[string]any{
		"CURRENT_GOAL":             fmt.Sprint(input.CurrentGoal),
		"GOAL_UNDERSTANDING":       strings.TrimSpace(input.GoalUnderstanding),
		"HAS_GOAL_UNDERSTANDING":   strings.TrimSpace(input.GoalUnderstanding) != "",
		"INPUT_TIMELINE":           prettyJSON(input.InputTimeline),
		"CURRENT_STEP_CARD":        prettyJSON(input.CurrentStepCard),
		"PLAN_OVERVIEW":            prettyJSON(input.PlanOverview),
		"OPEN_ITEMS_LEDGER":        strings.TrimSpace(input.OpenItemsLedger),
		"TASK_CONTEXT_BOARD":       strings.TrimSpace(input.TaskContextBoard),
		"STEP_FILE_CONTENT":        strings.TrimSpace(input.StepFileContent),
		"HAS_STEP_FILE_CONTENT":    strings.TrimSpace(input.StepFileContent) != "",
		"STEP_RESULT_PATH":         input.StepResultPath,
		"STEP_CONTEXTS_PATH":       input.StepContextsPath,
		"STEP_TRANSCRIPT_PATH":     input.StepTranscriptPath,
		"STEP_TIMELINE_PATH":       input.StepTimelinePath,
		"PLANNER_JOURNAL_PATH":     input.PlannerJournalPath,
		"HAS_PLANNER_JOURNAL_PATH": strings.TrimSpace(input.PlannerJournalPath) != "",
		"PLANNER_JOURNAL":          strings.TrimSpace(input.PlannerJournal),
		"HAS_PLANNER_JOURNAL":      strings.TrimSpace(input.PlannerJournal) != "",
		"SKILLS_CONTEXT":           input.SkillsContext,
		"HAS_SKILLS_TABLE":         input.HasSkillsTable,
		"AVAILABLE_TOOLS":          input.AvailableTools,
		"HAS_AVAILABLE_TOOLS":      input.HasAvailableTools,
	}
	return renderPromptParts("step_replan", m.planningSystemTmpl, m.stepReplanUserTmpl, systemData, userData)
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
		"IS_REPLAN_PHASE":      false,
		"IS_SUB_AGENT":         input.IsSubAgent,
		"CAN_SPAWN_SUBAGENT":   input.CanSpawnSubAgent,
		"USER_INPUT_TURN":      input.UserInputTurn,
		"HAS_REPLAN_CONTEXT":   input.HasReplanContext,
		// 共享区直接维护文件的绝对路径下沉到 system 模板（与 step_replan 相位共享同一段）。
		// plan 相位主要维护 task_context.md 的 `## 输入事实` 和（regen 期间）账本三区。
		"OPEN_ITEMS_PATH":   strings.TrimSpace(input.OpenItemsLedgerPath),
		"TASK_CONTEXT_PATH": strings.TrimSpace(input.TaskContextPath),
		"STEP_FILE_PATH":    "",
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
	return renderPromptParts("task_planner", m.planningSystemTmpl, m.taskPlannerUserTmpl, systemData, userData)
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
