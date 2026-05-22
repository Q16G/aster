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
	AgentPhaseStep                  AgentPhase = "step"
	AgentPhasePlan                  AgentPhase = "plan"
	AgentPhaseStepReplan            AgentPhase = "step_replan"
	AgentPhaseFinalAnswer           AgentPhase = "final_answer"
	AgentPhaseIntentClassification  AgentPhase = "intent_classification"
	AgentPhaseStepOutcomesReducer   AgentPhase = "step_outcomes_reducer"
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

// PlanItem 计划项
type PlanItem struct {
	ID                string         `json:"id,omitempty"`
	Step              string         `json:"step"`
	Status            PlanStepStatus `json:"status,omitempty"`
	DependsOn         []string       `json:"depends_on,omitempty"`
	ResolvedDependsOn []*PlanItem    `json:"-"`
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
	StatusSummary   string   `json:"status_summary,omitempty"`
	ShortSummary    string   `json:"short_summary,omitempty"`
	LongSummary     string   `json:"long_summary,omitempty"`
	KeyFacts        []string `json:"key_facts,omitempty"`
	OpenQuestions   []string `json:"open_questions,omitempty"`
	ToolCallsDigest []string `json:"tool_calls_digest,omitempty"`

	// ==================== C. Artifact 索引层（artifact writer 回填） ====================
	ArtifactDir string `json:"artifact_dir,omitempty"`
	SummaryFile string `json:"summary_file,omitempty"`
	ResultFile  string `json:"result_file,omitempty"`

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

	StatusSummary   string   `json:"status_summary"`
	ShortSummary    string   `json:"short_summary"`
	LongSummary     string   `json:"long_summary"`
	KeyFacts        []string `json:"key_facts"`
	OpenQuestions   []string `json:"open_questions"`
	ToolCallsDigest []string `json:"tool_calls_digest"`
}

type ReplanContext struct {
	SourceStepID   string   `json:"source_step_id,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	NextGoal       string   `json:"next_goal,omitempty"`
	MissingItems   []string `json:"missing_items,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
	ReplacePending bool     `json:"replace_pending,omitempty"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID         string         `json:"id,omitempty"`
	Name       string         `json:"name"`
	IsAgent    bool           `json:"is_agent,omitempty"`
	StackDepth int            `json:"stack_depth,omitempty"`
	Arguments  map[string]any `json:"arguments,omitempty"`
}

// ToolResult 工具调用结果
type ToolResult struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	IsAgent    bool   `json:"is_agent,omitempty"`
	StackDepth int    `json:"stack_depth,omitempty"`
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
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

	Warnings   []string `json:"warnings,omitempty"`
	Unresolved []string `json:"unresolved,omitempty"`
	Error      string   `json:"error,omitempty"`

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
