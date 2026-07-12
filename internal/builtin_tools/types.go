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

// AnalysisTopicStatus 业务 lane（phase）的存储状态。
// active 不落盘、纯派生：phase 未 terminal 且其 depends_on 的 phase 全部 terminal 即 active。
type AnalysisTopicStatus string

const (
	AnalysisTopicPending   AnalysisTopicStatus = "pending"
	AnalysisTopicCompleted AnalysisTopicStatus = "completed"
	AnalysisTopicBlocked   AnalysisTopicStatus = "blocked"
)

// SyntheticTopicID 是 runtime 兜底 phase 的保留 id：plan item 缺失/悬空 phase_id 时
// 自动挂靠到该 phase。planner 不得主动使用（NormalizeAnalysisTopics 拒绝）。
const SyntheticTopicID = "phase-synthetic"

// AnalysisTopic 业务 lane / 分组，不拥有自己的 plan / step_replan / final_answer 生命周期。
// depends_on 引用其他 phase id，参与 step ready 门：phase 未解锁（依赖 phase 未全部
// terminal）时其下 step 不进入 frontier。completed/blocked 仅由 step_replan 的
// phase_assessments 或 planner 重提交写入。
type AnalysisTopic struct {
	ID        string          `json:"id"`
	Name      string          `json:"name,omitempty"`
	DependsOn []string        `json:"depends_on,omitempty"`
	Status    AnalysisTopicStatus `json:"status,omitempty"`
}

// Terminal 判断 phase 是否终态（completed/blocked 均视同 terminal 参与解锁下游）。
func (p *AnalysisTopic) Terminal() bool {
	if p == nil {
		return false
	}
	return p.Status == AnalysisTopicCompleted || p.Status == AnalysisTopicBlocked
}

// TopicAssessmentStatus 是 step_replan 对单个 active phase 的判定结论。
// continue 不是存储状态：仅表示下一轮 planner 应继续释放该 phase 的 step。
type TopicAssessmentStatus string

const (
	TopicAssessContinue  TopicAssessmentStatus = "continue"
	TopicAssessCompleted TopicAssessmentStatus = "completed"
	TopicAssessBlocked   TopicAssessmentStatus = "blocked"
)

// TopicAssessment 是全局 step_replan 在 frontier barrier 后对单个 active phase 的评估。
// completed/blocked 由 runtime 机械写回 AnalysisTopic.Status；三轴字段限该 phase 范围内的缺口。
type TopicAssessment struct {
	TopicID         string                `json:"topic_id"`
	Status          TopicAssessmentStatus `json:"status"`
	Reason          string                `json:"reason,omitempty"`
	IncompleteItems []string              `json:"incomplete_items,omitempty"`
	DepthGaps       []string              `json:"depth_gaps,omitempty"`
	NewSurfaces     []string              `json:"new_surfaces,omitempty"`
}

// PlanItem 计划项。
// 规划字段由 task_planner 产出；产出字段在 step 终态时由 runtime 从 StepOutcome 烘焙写回，
// 并随 planner.jsonl 落地（plan 提交全量 append + step 终态增量 append）。
type PlanItem struct {
	ID     string         `json:"id,omitempty"`
	Step   string         `json:"step"`
	Status PlanStepStatus `json:"status,omitempty"`
	// TopicID 指向所属业务 lane（AnalysisTopic.ID）。planner 提交时必填；
	// 旧数据缺失时由 SynthesizeTopicsIfMissing 挂到 synthetic phase。
	TopicID           string      `json:"topic_id,omitempty"`
	DependsOn         []string    `json:"depends_on,omitempty"`
	ResolvedDependsOn []*PlanItem `json:"-"`

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

	// TranscriptBlobRef 仅由 UpdateInlineStep 路径使用（inline step 完成回写）。
	// 主路径 UpdateCurrentStep 不填此字段——主路径的 transcript blob ref 由
	// ApplyStepReplan 路径管理（state.go:645）。upsertStepOutcomeLocked 在写入时
	// 只在非空时覆盖 outcome.TranscriptBlobRef，避免主路径调用误清空。
	TranscriptBlobRef string `json:"transcript_blob_ref,omitempty"`
}

// IntentContext 是意图恢复上下文：回合起点由 intent_classification 产出，注入 planner
// 让其据「用户输入 + 推荐动作」产 goal_understanding（猜测意图归 planner，submit_intent
// 不产 goal）。与内部回流的 ReplanContext 语义正交、注入时二选一互斥。
// RegenerateGoal/UserInitiated 语义由 Action 派生（Action=="replan"→重产 goal；
// IntentContext!=nil→用户驱动回合），不再落独立字段。
type IntentContext struct {
	LatestInput string `json:"latest_input"`         // 用户最新输入原文
	Action      string `json:"action"`               // 推荐后续动作：carry / replan / cold_start
	Reason      string `json:"reason,omitempty"`      // 一句判定依据（意图分类给出）
}

// ReplanContext 是内部回流上下文：step_replan 复核后把三轴缺口 + 各 topic 评估回流给
// planner 重编排。纯 gap 载荷，不含用户意图、不含流程控制标志、不含代码侧 topic scope
// （per-topic 局部替换/释放的 topic scope 由 Agent.replanScopeTopicID 代码承载，不进此 JSON）。
type ReplanContext struct {
	Reason          string      `json:"reason,omitempty"`
	IncompleteItems []*AxisItem `json:"incomplete_items,omitempty"` // 轴①存在性：声明范围内、根本没做的项
	DepthGaps       []*AxisItem `json:"depth_gaps,omitempty"`       // 轴②深度：做了但不扎实的项（判据见复核阶段 prompt）
	NewSurfaces     []*AxisItem `json:"new_surfaces,omitempty"`     // 轴③泛化：对照整体任务全集、尚未被任何已完成工作覆盖的面
	// TopicAssessments 是 step_replan 对本轮 frontier 内各 active phase 的全量评估，
	// 回流给下一轮 planner 决定各 lane 继续释放 step 还是收束。
	TopicAssessments []*TopicAssessment `json:"topic_assessments,omitempty"`
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
	// TopicFocus 是当前深度优先聚焦阶段的语义描述，跨阶段贯穿（planner 写、step_replan 读）；
	// step_replan 视角 B 据此把全局视角从 GoalUnderstanding 全集收窄为 GoalUnderstanding ∩ TopicFocus。
	// 切换面/纠偏重塑由 step_replan 在 ReplanContext 中携带 NextPhase 触发，回流 planner 写回此处。
	TopicFocus string `json:"topic_focus,omitempty"`
	// SimpleTask 标记简单单步任务：step 完成后跳过 step_replan 直达 final_answer。
	SimpleTask bool `json:"simple_task,omitempty"`

	Plan []*PlanItem `json:"plan,omitempty"`
	// Topics 是业务 lane 清单（Parallel Frontier 数据面）：plan item 经 phase_id 归属其一。
	// 与 Plan 同源共版本（随 plan 提交/重规划原子替换），completed/blocked 由
	// phase_assessments 或 planner 重提交写入。
	Topics        []*AnalysisTopic   `json:"topics,omitempty"`
	PlanVersion   int            `json:"plan_version,omitempty"`
	StepOutcomes  []*StepOutcome `json:"step_outcomes,omitempty"`
	FinalAnswer   *FinalAnswer   `json:"final_answer,omitempty"`
	ReplanContext *ReplanContext `json:"replan_context,omitempty"`
	// IntentContext 是意图恢复上下文（回合起点由 intent_classification 写、planner 读后清）。
	// 与 ReplanContext 语义正交、注入 planner 时二选一互斥。
	IntentContext *IntentContext `json:"intent_context,omitempty"`
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
