package tui

import (
	"fmt"
	"strings"
	"time"
)

type PartType string

const (
	PartTypeUser        PartType = "user"
	PartTypeText        PartType = "text"
	PartTypeTool        PartType = "tool"
	PartTypePlan        PartType = "plan"
	PartTypeSystem      PartType = "system"
	PartTypeThinking    PartType = "thinking"
	PartTypeSummary     PartType = "summary"
	PartTypeStepResult  PartType = "step_result"
	PartTypeStepSummary PartType = "step_summary"
	PartTypeStepReplan  PartType = "step_replan"
	PartTypeStepTriage  PartType = "step_triage"
	PartTypeFinalAnswer PartType = "final_answer"
	PartTypeSubAgent    PartType = "sub_agent"
	PartTypeInlineStep  PartType = "inline_step"
	PartTypePhaseBanner PartType = "phase_banner"
)

// CardKind 用于 ExpandableCardPart 接口的子类型 ID（避免在每个消费方写 switch 区分）。
type CardKind int

const (
	CardKindSubAgent CardKind = iota
	CardKindInlineStep
	CardKindPlan
	CardKindStepResult
	CardKindStepSummary
	CardKindStepReplan
	CardKindStepTriage
	CardKindFinalAnswer
)

// ExpandableCardPart 是所有可展开/折叠卡片的统一接口（覆盖 sub_agent /
// inline_step / plan / step_result / step_summary / final_answer）。
//
// **展开态归属接口本身**：旧设计把展开状态存在 `m.toolExpanded[idx int]`
// 全局表里，导致：
//   - idx 在 part 列表前插（concurrent peer / sub_agent reflow）时漂移
//   - 每加一种可展开卡片就要在 key handler / expandAll / collapseAll 等
//     N 处加 type 白名单（PartTypeInlineStep 漏白名单就是这类 bug）
//   - 编译器不告警
//
// 改造后：展开态作为字段直接落在 part 实体上，key handler 退化为一处
// `if card, ok := part.AsExpandable(); ok { card.SetExpanded(...) }`。
// 新卡片类型只要实现接口就自动接入展开/折叠交互，编译期保证完备性。
//
// **不**用 SubAgentPart.Kind 字段做 discriminator——type bloat 教科书案例：每个消费方
// 都要散点 if Kind != "inline_step" 过滤，漏一个就串台。独立类型 + 接口让编译器在新增
// 消费方时自动指认是否兼容新卡片类型。
type ExpandableCardPart interface {
	Title() string
	StatusLabel() string
	GetCallID() string
	Elapsed() time.Duration
	CardKind() CardKind
	Expanded() bool
	SetExpanded(bool)
}

// AsExpandable 把 DisplayPart 投到 ExpandableCardPart 接口。
// 不是可展开卡片时第二返回值为 false。
//
// 添加新可展开卡片类型只需：（1）让对应 PartXxx 结构体实现接口；（2）在本
// switch 加一个 case。不再触碰 key handler / refreshContent 等其他位置。
func (p DisplayPart) AsExpandable() (ExpandableCardPart, bool) {
	switch p.Type {
	case PartTypeSubAgent:
		if p.SubAgent != nil {
			return p.SubAgent, true
		}
	case PartTypeInlineStep:
		if p.InlineStep != nil {
			return p.InlineStep, true
		}
	case PartTypePlan:
		if p.Plan != nil {
			return p.Plan, true
		}
	case PartTypeStepResult:
		if p.StepResult != nil {
			return p.StepResult, true
		}
	case PartTypeStepSummary:
		if p.StepSummary != nil {
			return p.StepSummary, true
		}
	case PartTypeStepReplan:
		if p.StepReplan != nil {
			return p.StepReplan, true
		}
	case PartTypeStepTriage:
		if p.StepTriage != nil {
			return p.StepTriage, true
		}
	case PartTypeFinalAnswer:
		if p.FinalAnswer != nil {
			return p.FinalAnswer, true
		}
	}
	return nil, false
}

type DisplayPart struct {
	Type PartType  `json:"type"`
	Time time.Time `json:"time"`

	// ID is a process-local, monotonically increasing identifier assigned by
	// ChatModel.newPart() at insertion time. It is the stable identity used by
	// the renderer's fragment cache and the store's dirty set; slice index
	// remains the ordinal/timeline position but is not the identity. ID is
	// omitempty in JSON so historical session files without an ID round-trip
	// unchanged (SetParts back-fills missing IDs on load).
	ID uint64 `json:"id,omitempty"`
	// Version is the content revision counter for this part. Any in-place
	// mutation that affects rendered output (thinking append, tool result
	// update, expand/collapse toggle) must bump Version so the renderer's
	// fragment cache invalidates the cached fragment for this ID.
	Version uint32 `json:"version,omitempty"`

	User        *UserPart        `json:"user,omitempty"`
	Text        *TextPart        `json:"text,omitempty"`
	Tool        *ToolPart        `json:"tool,omitempty"`
	Plan        *PlanPart        `json:"plan,omitempty"`
	System      *SystemPart      `json:"system,omitempty"`
	Thinking    *ThinkingPart    `json:"thinking,omitempty"`
	Summary     *SummaryPart     `json:"summary,omitempty"`
	StepResult  *StepResultPart  `json:"step_result,omitempty"`
	StepSummary *StepSummaryPart `json:"step_summary,omitempty"`
	StepReplan  *StepReplanPart  `json:"step_replan,omitempty"`
	StepTriage  *StepTriagePart  `json:"step_triage,omitempty"`
	FinalAnswer *FinalAnswerPart `json:"final_answer,omitempty"`
	SubAgent    *SubAgentPart    `json:"sub_agent,omitempty"`
	InlineStep  *InlineStepPart  `json:"inline_step,omitempty"`
	PhaseBanner *PhaseBannerPart `json:"phase_banner,omitempty"`
}

type UserPart struct {
	Content string `json:"content"`
}

type TextPart struct {
	Content   string `json:"content"`
	AgentName string `json:"agent_name,omitempty"`
	// StepID 标记该流式文本归属哪个 inline_step（commit 后 fix: 把 peer 流式输出归到
	// 各自卡片）。空字符串=主路径/非 inline 上下文。渲染层据此从主流中过滤、归集到
	// InlineStepPart 卡片的展开子序列。
	StepID string `json:"step_id,omitempty"`
}

type ToolPart struct {
	Name          string        `json:"name"`
	CallID        string        `json:"call_id,omitempty"`
	Arguments     string        `json:"args,omitempty"`
	Result        string        `json:"result,omitempty"`
	Error         string        `json:"error,omitempty"`
	State         string        `json:"state"`
	Duration      time.Duration `json:"duration,omitempty"`
	IsAgent       bool          `json:"is_agent,omitempty"`
	StackDepth    int           `json:"stack_depth,omitempty"`
	AgentName     string        `json:"agent_name,omitempty"`
	WorkspaceRoot string        `json:"workspace_root,omitempty"`
	Summary       string        `json:"summary,omitempty"`
	ChildRef      string        `json:"child_ref,omitempty"`
	// StepID 标记该 tool_result 归属哪个 inline_step（commit 12 事件路由填入）。
	// 空字符串表示主路径（current step）或非 inline_step 上下文（plan/replan/intent/final）。
	// 渲染层用此字段在 InlineStepPart 卡片下分组展示。
	StepID string `json:"step_id,omitempty"`
}

type PlanPart struct {
	AgentName    string         `json:"agent_name,omitempty"`
	ParentStepID string         `json:"parent_step_id,omitempty"`
	ParentAgent  string         `json:"parent_agent,omitempty"`
	Explanation  string         `json:"explanation,omitempty"`
	Items        []PlanItemView `json:"items,omitempty"`
	// IsExpanded 是 UI 展开/折叠状态——volatile session 态，不持久化。
	IsExpanded bool `json:"-"`
}

// PlanPart 实现 ExpandableCardPart。
func (p *PlanPart) Title() string          { return strings.TrimSpace(p.AgentName) }
func (p *PlanPart) StatusLabel() string    { return "" }
func (p *PlanPart) GetCallID() string      { return strings.TrimSpace(p.ParentStepID) }
func (p *PlanPart) Elapsed() time.Duration { return 0 }
func (p *PlanPart) CardKind() CardKind     { return CardKindPlan }
func (p *PlanPart) Expanded() bool         { return p.IsExpanded }
func (p *PlanPart) SetExpanded(v bool)     { p.IsExpanded = v }

type PlanItemView struct {
	ID        string `json:"id,omitempty"`
	Step      string `json:"step"`
	Status    string `json:"status"`
	Depth     int    `json:"depth,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
}

type SystemPart struct {
	Content string `json:"content"`
}

type ThinkingPart struct {
	Content   string `json:"content"`
	EventID   string `json:"event_id,omitempty"`
	GroupID   string `json:"group_id,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
	// StepID 标记该 think 块归属哪个 inline_step（commit 12+/fix/06 事件路由填入）。
	// 空字符串=主路径/plan/replan/intent/final 等非 inline 上下文。
	StepID string `json:"step_id,omitempty"`
}

type SummaryPart struct {
	AgentName    string        `json:"agent_name"`
	ModelID      string        `json:"model_id"`
	Duration     time.Duration `json:"duration"`
	Success      bool          `json:"success"`
	TokenCount   string        `json:"token_count,omitempty"`
	CostEstimate string        `json:"cost_estimate,omitempty"`
}

type StepResultPart struct {
	AgentName     string `json:"agent_name,omitempty"`
	StepID        string `json:"step_id,omitempty"`
	StepName      string `json:"step_name,omitempty"`
	Status        string `json:"status,omitempty"`
	DisplayResult string `json:"display_result,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Error         string `json:"error,omitempty"`

	// IsExpanded 是 UI 展开/折叠态——volatile session 态，不持久化。
	IsExpanded bool `json:"-"`
}

// StepResultPart 实现 ExpandableCardPart 接口。
func (p *StepResultPart) Title() string          { return strings.TrimSpace(p.StepName) }
func (p *StepResultPart) StatusLabel() string    { return strings.TrimSpace(p.Status) }
func (p *StepResultPart) GetCallID() string      { return strings.TrimSpace(p.StepID) }
func (p *StepResultPart) Elapsed() time.Duration { return 0 }
func (p *StepResultPart) CardKind() CardKind     { return CardKindStepResult }
func (p *StepResultPart) Expanded() bool         { return p.IsExpanded }
func (p *StepResultPart) SetExpanded(v bool)     { p.IsExpanded = v }

type StepSummaryPart struct {
	AgentName       string   `json:"agent_name,omitempty"`
	StepID          string   `json:"step_id"`
	StepName        string   `json:"step_name"`
	ShortSummary    string   `json:"short_summary"`
	LongSummary     string   `json:"long_summary,omitempty"`
	KeyFacts        []string `json:"key_facts,omitempty"`
	OpenQuestions   []string `json:"open_questions,omitempty"`
	ToolCallsDigest string   `json:"tool_calls_digest,omitempty"`
	References      []string `json:"references,omitempty"`

	// IsExpanded 是 UI 展开/折叠态——volatile session 态，不持久化。
	IsExpanded bool `json:"-"`
}

// StepSummaryPart 实现 ExpandableCardPart 接口。
func (p *StepSummaryPart) Title() string          { return strings.TrimSpace(p.StepName) }
func (p *StepSummaryPart) StatusLabel() string    { return "" }
func (p *StepSummaryPart) GetCallID() string      { return strings.TrimSpace(p.StepID) }
func (p *StepSummaryPart) Elapsed() time.Duration { return 0 }
func (p *StepSummaryPart) CardKind() CardKind     { return CardKindStepSummary }
func (p *StepSummaryPart) Expanded() bool         { return p.IsExpanded }
func (p *StepSummaryPart) SetExpanded(v bool)     { p.IsExpanded = v }

type StepReplanPart struct {
	AgentName       string   `json:"agent_name,omitempty"`
	StepID          string   `json:"step_id,omitempty"`
	StepName        string   `json:"step_name,omitempty"`
	ShouldReplan    bool     `json:"should_replan"`
	ReplanReason    string   `json:"replan_reason,omitempty"`
	NextGoal        string   `json:"next_goal,omitempty"`
	PlanSize        int      `json:"plan_size,omitempty"`
	IncompleteItems []string `json:"incomplete_items,omitempty"`
	NewSurfaces     []string `json:"new_surfaces,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`

	// IsExpanded 是 UI 展开/折叠态——volatile session 态，不持久化。
	IsExpanded bool `json:"-"`
}

// StepReplanPart 实现 ExpandableCardPart 接口（最小子集 + Expanded）。
func (p *StepReplanPart) Title() string          { return strings.TrimSpace(p.StepName) }
func (p *StepReplanPart) StatusLabel() string    { return "" }
func (p *StepReplanPart) GetCallID() string      { return strings.TrimSpace(p.StepID) }
func (p *StepReplanPart) Elapsed() time.Duration { return 0 }
func (p *StepReplanPart) CardKind() CardKind     { return CardKindStepReplan }
func (p *StepReplanPart) Expanded() bool         { return p.IsExpanded }
func (p *StepReplanPart) SetExpanded(v bool)     { p.IsExpanded = v }

// StepTriagePart 是 Triage 廉价决策门控的 UI 展示载体。
// 与 StepReplanPart 相比字段更瘦:Triage 是 prompt-only 调用,只产出 suggestion + reason,
// 不涉及 plan / next_goal / surfaces 等重产出字段。
type StepTriagePart struct {
	AgentName  string `json:"agent_name,omitempty"`
	StepID     string `json:"step_id,omitempty"`
	StepName   string `json:"step_name,omitempty"`
	Suggestion string `json:"suggestion"` // continue | replan
	Reason     string `json:"reason,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`

	// IsExpanded 是 UI 展开/折叠态——volatile session 态，不持久化。
	IsExpanded bool `json:"-"`
}

// StepTriagePart 实现 ExpandableCardPart 接口。
func (p *StepTriagePart) Title() string          { return strings.TrimSpace(p.StepName) }
func (p *StepTriagePart) StatusLabel() string    { return strings.TrimSpace(p.Suggestion) }
func (p *StepTriagePart) GetCallID() string      { return strings.TrimSpace(p.StepID) }
func (p *StepTriagePart) Elapsed() time.Duration { return time.Duration(p.DurationMs) * time.Millisecond }
func (p *StepTriagePart) CardKind() CardKind     { return CardKindStepTriage }
func (p *StepTriagePart) Expanded() bool         { return p.IsExpanded }
func (p *StepTriagePart) SetExpanded(v bool)     { p.IsExpanded = v }

type SubAgentPart struct {
	AgentName     string        `json:"agent_name"`
	CallID        string        `json:"call_id,omitempty"`
	Status        string        `json:"status"`
	Summary       string        `json:"summary,omitempty"`
	Description   string        `json:"description,omitempty"`
	WorkspaceRoot string        `json:"workspace_root,omitempty"`
	ChildRef      string        `json:"child_ref,omitempty"`
	Duration      time.Duration `json:"duration,omitempty"`
	StartedAt     time.Time     `json:"started_at,omitempty"`

	// StepID 是派生该子 agent 的 peer step；并发 step 下用它把卡片归到正确的
	// step detail（partStepID → idxByStepID），不再依赖会被其它 peer 覆盖的
	// activeStepByAgent 单槽。空串表示主路径/非并发场景。
	StepID string `json:"step_id,omitempty"`

	// Kind 旧字段，保留用于向后兼容反序列化老 session（曾承载 "remote_step"
	// 区分语义）。新代码（commit 10+）不再用 Kind 区分，inline_step 卡片走独立
	// InlineStepPart 类型；本字段在 commit 14 命名兜底时再删除。
	Kind string `json:"kind,omitempty"`

	// IsExpanded 是 UI 展开/折叠态——volatile session 态，不持久化。
	IsExpanded bool `json:"-"`
}

// SubAgentPart 实现 ExpandableCardPart 接口。
func (p *SubAgentPart) Title() string          { return p.AgentName }
func (p *SubAgentPart) StatusLabel() string    { return p.Status }
func (p *SubAgentPart) GetCallID() string      { return p.CallID }
func (p *SubAgentPart) Elapsed() time.Duration { return p.Duration }
func (p *SubAgentPart) CardKind() CardKind     { return CardKindSubAgent }
func (p *SubAgentPart) Expanded() bool         { return p.IsExpanded }
func (p *SubAgentPart) SetExpanded(v bool)     { p.IsExpanded = v }

// InlineStepPart 是 inline_step（commit 7-9 重构后的并发 step）独立卡片类型。
// 与 SubAgentPart 同结构但语义不同——inline_step 共享主 agent workspace / state，
// 没有 ChildRef 概念；WorkspaceRoot 指向父 workspace 而非独立子目录。
type InlineStepPart struct {
	StepID        string        `json:"step_id"`
	Description   string        `json:"description,omitempty"` // 通常是 PlanItem.Step
	Status        string        `json:"status"`                // running / completed / failed / cancelled
	Summary       string        `json:"summary,omitempty"`
	WorkspaceRoot string        `json:"workspace_root,omitempty"`
	Duration      time.Duration `json:"duration,omitempty"`
	StartedAt     time.Time     `json:"started_at,omitempty"`

	// IsExpanded 是 UI 展开/折叠态——volatile session 态，不持久化。
	IsExpanded bool `json:"-"`
}

// InlineStepPart 实现 ExpandableCardPart 接口。
func (p *InlineStepPart) Title() string          { return p.StepID }
func (p *InlineStepPart) StatusLabel() string    { return p.Status }
func (p *InlineStepPart) GetCallID() string      { return p.StepID }
func (p *InlineStepPart) Elapsed() time.Duration { return p.Duration }
func (p *InlineStepPart) CardKind() CardKind     { return CardKindInlineStep }
func (p *InlineStepPart) Expanded() bool         { return p.IsExpanded }
func (p *InlineStepPart) SetExpanded(v bool)     { p.IsExpanded = v }

type FinalAnswerPart struct {
	AgentName  string   `json:"agent_name,omitempty"`
	Content    string   `json:"content"`
	Source     string   `json:"source,omitempty"`
	References []string `json:"references,omitempty"`

	// IsExpanded 是 UI 展开/折叠态——volatile session 态，不持久化。
	IsExpanded bool `json:"-"`
}

// FinalAnswerPart 实现 ExpandableCardPart 接口。
func (p *FinalAnswerPart) Title() string          { return strings.TrimSpace(p.Source) }
func (p *FinalAnswerPart) StatusLabel() string    { return "" }
func (p *FinalAnswerPart) GetCallID() string      { return "" }
func (p *FinalAnswerPart) Elapsed() time.Duration { return 0 }
func (p *FinalAnswerPart) CardKind() CardKind     { return CardKindFinalAnswer }
func (p *FinalAnswerPart) Expanded() bool         { return p.IsExpanded }
func (p *FinalAnswerPart) SetExpanded(v bool)     { p.IsExpanded = v }

type PhaseBannerPart struct {
	Phase     string `json:"phase"`
	Label     string `json:"label"`
	Iteration int    `json:"iteration,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
}

func phaseLabel(phase string) string {
	switch phase {
	case "plan":
		return "Plan"
	case "step":
		return "Step"
	case "step_replan":
		return "Step Replan"
	case "step_triage":
		return "Step Triage"
	case "step_summary":
		return "Step Summary"
	case "final_answer":
		return "Final Answer"
	case "step_outcomes_reducer":
		return "History Compression"
	default:
		return phase
	}
}

var toolIcons = map[string]string{
	"bash":                "$",
	"read_file":           "→",
	"list_files":          "→",
	"rg":                  "✱",
	"list_skills":         "⚙",
	"eject_skill":         "⏏",
	"human_confirm":       "?",
	"update_current_step": "⚙",
}

func ToolIcon(name string) string {
	if icon, ok := toolIcons[name]; ok {
		return icon
	}
	return "⚙"
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
