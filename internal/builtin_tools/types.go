package builtin_tools

import (
	"strings"
	"time"
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPreparing TaskStatus = "preparing"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
	TaskStatusPaused    TaskStatus = "paused"
	TaskStatusWaiting   TaskStatus = "waiting"
)

// RunResult Agent 执行结果
type RunResult struct {
	Success bool   `json:"success"`
	Result  string `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`

	// V2 semantics: one Execute() corresponds to a turn within a stable session.
	TurnID     string `json:"turn_id,omitempty"`
	TurnStatus string `json:"turn_status,omitempty"` // succeeded|failed|cancelled|interrupted

	// PendingInterrupt is populated when the turn ends in WAITING_FOR_HUMAN.
	PendingInterrupt *PendingInterrupt `json:"pending_interrupt,omitempty"`

	PlanSummary *PlanCompletionSummary `json:"plan_summary,omitempty"`

	// TranscriptBlobRef 仅 X2 远程 step（spawnRemoteStep）使用：goroutine 完成时把
	// child agent 的完整 history 落到父 agent v2Store 的 content-addressed blob，
	// 返回 sha256:xxx ref 通过本字段传到 drain 路径，drain 把 ref 写入
	// PlanItem 关联的 StepOutcome.TranscriptBlobRef 供 step_replan 指针化按需读。
	TranscriptBlobRef string `json:"transcript_blob_ref,omitempty"`
}

type PlanCompletionSummary struct {
	Total        int      `json:"total"`
	Completed    int      `json:"completed"`
	Pending      int      `json:"pending"`
	Failed       int      `json:"failed"`
	Skipped      int      `json:"skipped"`
	PendingSteps []string `json:"pending_steps,omitempty"`
}

type PendingInterrupt struct {
	InterruptID string         `json:"interrupt_id"`
	Question    string         `json:"question,omitempty"`
	InputType   string         `json:"input_type,omitempty"`
	Options     []string       `json:"options,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
}

// ThoughtResult 思考结果
type ThoughtResult struct {
	ThoughtChain map[string]string `json:"thought_chain"`
	Progress     *ThoughtProgress  `json:"progress,omitempty"`
}

// ThoughtProgress 思考进度
type ThoughtProgress struct {
	NeedsPlanning  bool   `json:"needs_planning,omitempty"`
	PlanningReason string `json:"planning_reason,omitempty"`
	Step           string `json:"step,omitempty"`
	Status         string `json:"status,omitempty"`
	CompletionRate int    `json:"completion_rate,omitempty"`
}

type AgentPhase string

const (
	AgentPhaseStep                 AgentPhase = "step"
	AgentPhasePlan                 AgentPhase = "plan"
	AgentPhaseStepTriage           AgentPhase = "step_triage"
	AgentPhaseStepReplan           AgentPhase = "step_replan"
	AgentPhaseFinalAnswer          AgentPhase = "final_answer"
	AgentPhaseIntentClassification AgentPhase = "intent_classification"
	AgentPhaseStepOutcomesReducer  AgentPhase = "step_outcomes_reducer"
)

// PlanStepStatus 计划步骤状态
type PlanStepStatus string

const (
	PlanStepPending    PlanStepStatus = "pending"
	PlanStepInProgress PlanStepStatus = "in_progress"
	PlanStepCompleted  PlanStepStatus = "completed"
	PlanStepFailed     PlanStepStatus = "failed"
	PlanStepSkipped    PlanStepStatus = "skipped"
)

// PlanItem 计划项。
// 规划字段由 task_planner 产出；产出字段在 step 终态时由 runtime 从 StepOutcome 烘焙写回，
// 并随 planner.jsonl 落地（plan 提交全量 append + step 终态增量 append）。
type PlanItem struct {
	ID                string         `json:"id,omitempty"`
	Step              string         `json:"step"`
	Status            PlanStepStatus `json:"status,omitempty"`
	DependsOn         []string       `json:"depends_on,omitempty"`
	ResolvedDependsOn []*PlanItem    `json:"-"`

	// ==================== 产出字段（默认注入 prompt 的内联小字段） ====================
	ShortSummary      string                  `json:"short_summary,omitempty"`
	KeyFacts          []string                `json:"key_facts,omitempty"`
	ToolCallsDigest   []string                `json:"tool_calls_digest,omitempty"`
	CoverageChecklist []CoverageChecklistItem `json:"coverage_checklist,omitempty"`

	// ==================== 指针字段（按需解引用，need_expand 协议） ====================
	StepFile     string   `json:"step_file,omitempty"`
	ResultFile   string   `json:"result_file,omitempty"`
	TimelineFile string   `json:"timeline_file,omitempty"`
	CoverageFile string   `json:"coverage_file,omitempty"`
	References   []string `json:"references,omitempty"`
}

func (p *PlanItem) DependencyIDs() []string {
	if p == nil || len(p.DependsOn) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(p.DependsOn))
	out := make([]string, 0, len(p.DependsOn))
	for _, dep := range p.DependsOn {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if _, exists := seen[dep]; exists {
			continue
		}
		seen[dep] = struct{}{}
		out = append(out, dep)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (p *PlanItem) DependencyItems() []*PlanItem {
	if p == nil || len(p.ResolvedDependsOn) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(p.ResolvedDependsOn))
	out := make([]*PlanItem, 0, len(p.ResolvedDependsOn))
	for _, dep := range p.ResolvedDependsOn {
		if dep == nil {
			continue
		}
		depID := strings.TrimSpace(dep.ID)
		if depID == "" {
			continue
		}
		if _, exists := seen[depID]; exists {
			continue
		}
		seen[depID] = struct{}{}
		out = append(out, dep)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// CoverageChecklistItem 是 think_act 在执行现场逐项物化的覆盖对账条目，
// 由 step_replan / final_answer 按 status 归置消费。
type CoverageChecklistItem struct {
	Item     string `json:"item"`
	Status   string `json:"status"` // verified | uncovered | justified_skip | referenced_prior_coverage
	Evidence string `json:"evidence,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// BakeOutcome 在 step 终态时把 StepOutcome 的产出烘焙进计划项（plan_item 作为 plan 真相源的载体）。
// 覆盖清单超阈值落 coverage_file 时由调用方先填 CoverageFile 再烘焙，此处保持互斥：有文件指针则不内联。
func (p *PlanItem) BakeOutcome(o *StepOutcome) {
	if p == nil || o == nil {
		return
	}
	p.ShortSummary = strings.TrimSpace(o.ShortSummary)
	p.KeyFacts = CloneStringSlice(o.KeyFacts)
	p.ToolCallsDigest = CloneStringSlice(o.ToolCallsDigest)
	p.References = CloneStringSlice(o.References)
	p.ResultFile = strings.TrimSpace(o.ResultFile)
	p.TimelineFile = strings.TrimSpace(o.TimelineFile)
	p.CoverageFile = strings.TrimSpace(o.CoverageFile)
	if p.CoverageFile == "" && len(o.CoverageChecklist) > 0 {
		p.CoverageChecklist = append([]CoverageChecklistItem(nil), o.CoverageChecklist...)
	} else {
		p.CoverageChecklist = nil
	}
}

type StepOutcomeStatus string

const (
	StepOutcomeCompleted StepOutcomeStatus = "completed"
	StepOutcomeFailed    StepOutcomeStatus = "failed"
)

type StepOutcome struct {
	StepID    string            `json:"step_id,omitempty"`
	Status    StepOutcomeStatus `json:"status,omitempty"`
	UpdatedAt time.Time         `json:"updated_at,omitempty"`

	// ==================== A. 原始事实层（update_current_step 提交） ====================
	AttemptID string `json:"attempt_id,omitempty"`
	Summary   string `json:"summary,omitempty"`
	// DisplayResult: 面向用户的简洁结果
	DisplayResult string `json:"display_result,omitempty"`
	// Result: 结构化原始结果（文本化 JSON）
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`

	// References: 显式证据引用与 artifact-first 回填结果
	References []string `json:"references,omitempty"`

	// ==================== B. Summary 生成层（update_current_step 工具提交，必填） ====================
	StatusSummary     string                  `json:"status_summary,omitempty"`
	ShortSummary      string                  `json:"short_summary,omitempty"`
	LongSummary       string                  `json:"long_summary,omitempty"`
	KeyFacts          []string                `json:"key_facts,omitempty"`
	OpenQuestions     []string                `json:"open_questions,omitempty"`
	ToolCallsDigest   []string                `json:"tool_calls_digest,omitempty"`
	CoverageChecklist []CoverageChecklistItem `json:"coverage_checklist,omitempty"`

	// ==================== C. Artifact 索引层（artifact writer 回填） ====================
	ArtifactDir  string `json:"artifact_dir,omitempty"`
	SummaryFile  string `json:"summary_file,omitempty"`
	ResultFile   string `json:"result_file,omitempty"`
	CoverageFile string `json:"coverage_file,omitempty"`

	// ContextKey: execution lineage 的稳定锚点（写入 workspace/step_contexts.jsonl）
	ContextKey string `json:"context_key,omitempty"`

	// ==================== D. 执行谱系层（step_replan 回填） ====================
	TimelineFile         string   `json:"timeline_file,omitempty"`
	Namespace            string   `json:"namespace,omitempty"`
	PlanVersion          int      `json:"plan_version,omitempty"`
	AgentProfile         string   `json:"agent_profile,omitempty"`
	ResultKeys           []string `json:"result_keys,omitempty"`
	InheritedContextKeys []string `json:"inherited_context_keys,omitempty"`
	InheritedRefIDs      []string `json:"inherited_ref_ids,omitempty"`
	TranscriptBlobRef    string   `json:"transcript_blob_ref,omitempty"`
	StepKey              string   `json:"step_key,omitempty"`
}

func (o *StepOutcome) ToContextRecord() *StepContextRecord {
	if o == nil {
		return nil
	}
	return &StepContextRecord{
		ContextKey:           strings.TrimSpace(o.ContextKey),
		Namespace:            o.Namespace,
		StepID:               strings.TrimSpace(o.StepID),
		StepKey:              strings.TrimSpace(o.StepKey),
		PlanVersion:          o.PlanVersion,
		AgentProfile:         strings.TrimSpace(o.AgentProfile),
		SummaryFile:          strings.TrimSpace(o.SummaryFile),
		ResultFile:           strings.TrimSpace(o.ResultFile),
		ResultKeys:           CloneStringSlice(o.ResultKeys),
		ShortSummary:         strings.TrimSpace(o.ShortSummary),
		KeyFacts:             CloneStringSlice(o.KeyFacts),
		ToolCallsDigest:      CloneStringSlice(o.ToolCallsDigest),
		References:           CloneStringSlice(o.References),
		InheritedContextKeys: CloneStringSlice(o.InheritedContextKeys),
		InheritedRefIDs:      CloneStringSlice(o.InheritedRefIDs),
		TranscriptBlobRef:    o.TranscriptBlobRef,
		TimelineFile:         strings.TrimSpace(o.TimelineFile),
	}
}

type FinalAnswer struct {
	Content   string    `json:"content,omitempty"`
	Source    string    `json:"source,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`

	// References: final_answer 评估器给出的关键引用（可选）
	References []string `json:"references,omitempty"`
}

type ExternalInterrupt struct {
	ReasonCode       string   `json:"reason_code,omitempty"`
	Retryable        bool     `json:"retryable,omitempty"`
	Error            string   `json:"error,omitempty"`
	UserMessage      string   `json:"user_message,omitempty"`
	SuggestedActions []string `json:"suggested_actions,omitempty"`
}

type TimelineInput struct {
	Content   string    `json:"content,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// TaskStatusUpdate 任务状态更新
type TaskStatusUpdate struct {
	Task     string     `json:"task"`
	Status   TaskStatus `json:"status"`
	Message  string     `json:"message,omitempty"`
	Progress int        `json:"progress,omitempty"`
	Result   string     `json:"result,omitempty"`
	Error    string     `json:"error,omitempty"`
}

type CurrentStepUpdate struct {
	Status        PlanStepStatus `json:"status"`
	Summary       string         `json:"summary,omitempty"`
	DisplayResult string         `json:"display_result,omitempty"`
	Result        string         `json:"result,omitempty"`
	Error         string         `json:"error,omitempty"`
	References    []string       `json:"references,omitempty"`

	StatusSummary     string                  `json:"status_summary"`
	ShortSummary      string                  `json:"short_summary"`
	LongSummary       string                  `json:"long_summary"`
	KeyFacts          []string                `json:"key_facts"`
	CoverageChecklist []CoverageChecklistItem `json:"coverage_checklist,omitempty"`

	// TranscriptBlobRef 仅由 UpdateRemotePlanItem 路径使用（X2 远程 step 完成回写）。
	// 主路径 UpdateCurrentStep 不填此字段——主路径的 transcript blob ref 由
	// ApplyStepReplan 路径管理（state.go:645）。upsertStepOutcomeLocked 在写入时
	// 只在非空时覆盖 outcome.TranscriptBlobRef，避免主路径调用误清空。
	TranscriptBlobRef string `json:"transcript_blob_ref,omitempty"`
}

type ReplanContext struct {
	SourceStepID    string      `json:"source_step_id,omitempty"`
	Reason          string      `json:"reason,omitempty"`
	NextGoal        string      `json:"next_goal,omitempty"`
	IncompleteItems []*AxisItem `json:"incomplete_items,omitempty"` // 轴①存在性：源 step 自身声明范围内、根本没做的项
	DepthGaps       []*AxisItem `json:"depth_gaps,omitempty"`       // 轴②深度：源 step 做了但不扎实的项（判据见 DepthSmellsEnumeration）
	NewSurfaces     []*AxisItem `json:"new_surfaces,omitempty"`     // 轴③泛化：对照整体任务全集、尚未被任何已完成工作覆盖的面
	Warnings        []string    `json:"warnings,omitempty"`
	ReplacePending  bool        `json:"replace_pending,omitempty"`
	RegenerateGoal  bool        `json:"regenerate_goal,omitempty"` // 用户改向：planner 须重产 GOAL_UNDERSTANDING，不沿用旧理解
	UserInitiated   bool        `json:"user_initiated,omitempty"`  // 本回合由用户新输入经意图分类触发（carry/replan），区别于 step_replan 内部重规划与子 Agent 等待
	// CurrentPhase 透传给 planner 作为本回合的当前深度优先阶段（原样不变）：
	// 职责反转后 step_replan 不再选下一个 phase，planner 读账本「待承接排队」候选 + CurrentPhaseDone
	// 信号自决保持还是切换。submit_plan.current_phase 由 planner 自决回填到 TaskPlannerResult.CurrentPhase。
	CurrentPhase string `json:"current_phase,omitempty"`
	// CurrentPhaseDone 是 step_replan 报告的「当前 phase in-phase 深度已穷尽」信号（严格闸门：
	// 无可测未测、无可深未深）。planner 据此决定沿用当前阶段深推（false）还是切换下一阶段（true）。
	// 不加 omitempty：false 是「保持当前阶段继续深推」的关键信号，须显式出现在注入 planner 的
	// REPLAN_CONTEXT JSON 中，不能因零值被省略而让 planner 无法区分 false 与缺省。
	CurrentPhaseDone bool `json:"current_phase_done"`
}

// ReplanAxes 是跨步骤滚动复核的三轴未决盘点（sticky 状态）。
// 轴①存在性 / 轴②深度 / 轴③泛化，语义与 ReplanContext 同名字段一致。
// 条目为结构化 AxisItem（字符串形态经 AxisItem.UnmarshalJSON 兼容解析）。
type ReplanAxes struct {
	IncompleteItems []*AxisItem `json:"incomplete_items,omitempty"` // 轴①存在性：声明范围内、根本没做的项
	DepthGaps       []*AxisItem `json:"depth_gaps,omitempty"`       // 轴②深度：做了但不扎实（判据见 DepthSmellsEnumeration）
	NewSurfaces     []*AxisItem `json:"new_surfaces,omitempty"`     // 轴③泛化：对照整体任务全集尚未被覆盖的面
}

// ToolCall 工具调用
type ToolCall struct {
	ID         string         `json:"id,omitempty"`
	Name       string         `json:"name"`
	IsAgent    bool           `json:"is_agent,omitempty"`
	StackDepth int            `json:"stack_depth,omitempty"`
	Arguments  map[string]any `json:"arguments,omitempty"`
}

type ToolResultMedia struct {
	Kind      string `json:"kind,omitempty"`
	Path      string `json:"path,omitempty"`
	MIMEType  string `json:"mime_type,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

// ToolResult 工具调用结果
type ToolResult struct {
	ID         string            `json:"id,omitempty"`
	Name       string            `json:"name"`
	IsAgent    bool              `json:"is_agent,omitempty"`
	StackDepth int               `json:"stack_depth,omitempty"`
	Result     string            `json:"result,omitempty"`
	Error      string            `json:"error,omitempty"`
	Media      []ToolResultMedia `json:"media,omitempty"`
}

// StateSnapshot Agent 状态快照
type StateSnapshot struct {
	Iteration int        `json:"iteration"`
	Phase     AgentPhase `json:"phase,omitempty"`

	Status        TaskStatus `json:"status,omitempty"`
	Progress      int        `json:"progress,omitempty"`
	StatusSummary string     `json:"status_summary,omitempty"`
	NeedsPlanning bool       `json:"needs_planning,omitempty"`

	CurrentGoal   string           `json:"current_goal,omitempty"`
	CurrentStepID string           `json:"current_step_id,omitempty"`
	InputTimeline []*TimelineInput `json:"input_timeline,omitempty"`
	// GoalUnderstanding 是 planner 对原始输入的结构化理解，贯穿下游用于锚定原始意图。
	GoalUnderstanding string `json:"goal_understanding,omitempty"`
	// CurrentPhase 是当前深度优先聚焦阶段的语义描述，跨阶段贯穿（planner 写、step_replan 读）；
	// step_replan 视角 B 据此把全局视角从 GoalUnderstanding 全集收窄为 GoalUnderstanding ∩ CurrentPhase。
	// 切换面/纠偏重塑由 step_replan 在 ReplanContext 中携带 NextPhase 触发，回流 planner 写回此处。
	CurrentPhase string `json:"current_phase,omitempty"`
	// SimpleTask 标记简单单步任务：step 完成后跳过 step_replan 直达 final_answer。
	SimpleTask bool `json:"simple_task,omitempty"`

	Plan          []*PlanItem    `json:"plan,omitempty"`
	PlanVersion   int            `json:"plan_version,omitempty"`
	StepOutcomes  []*StepOutcome `json:"step_outcomes,omitempty"`
	FinalAnswer   *FinalAnswer   `json:"final_answer,omitempty"`
	ReplanContext *ReplanContext `json:"replan_context,omitempty"`
	// ExternalInterrupt 记录导致当前 run 提前收尾的外部依赖中断（如 provider quota/auth/rate limit）。
	ExternalInterrupt *ExternalInterrupt `json:"external_interrupt,omitempty"`
	// ActiveSkillNames 表示当前 runtime 已注入到 prompt 的 skill 名称集合。
	ActiveSkillNames []string `json:"active_skill_names,omitempty"`
	// ActiveMCPServers 表示当前已连接的 MCP Server 名称集合。
	ActiveMCPServers []string `json:"active_mcp_servers,omitempty"`

	Warnings       []string    `json:"warnings,omitempty"`
	UnresolvedAxes *ReplanAxes `json:"unresolved_axes,omitempty"`
	Error          string      `json:"error,omitempty"`

	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Terminal 判断是否为终态
func (s StateSnapshot) Terminal() bool {
	return s.Status == TaskStatusCompleted ||
		s.Status == TaskStatusFailed ||
		s.Status == TaskStatusCanceled
}

func (s StateSnapshot) CurrentStep() *PlanItem {
	return currentPlanStep(s.Plan, s.CurrentStepID)
}

func (s StateSnapshot) LatestInput() *TimelineInput {
	if len(s.InputTimeline) == 0 {
		return nil
	}
	return s.InputTimeline[len(s.InputTimeline)-1]
}
