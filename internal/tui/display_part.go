package tui

import (
	"fmt"
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
	PartTypePhaseBanner PartType = "phase_banner"
)

type DisplayPart struct {
	Type PartType  `json:"type"`
	Time time.Time `json:"time"`

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
	PhaseBanner *PhaseBannerPart `json:"phase_banner,omitempty"`
}

type UserPart struct {
	Content string `json:"content"`
}

type TextPart struct {
	Content   string `json:"content"`
	AgentName string `json:"agent_name,omitempty"`
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
}

type PlanPart struct {
	AgentName    string         `json:"agent_name,omitempty"`
	ParentStepID string         `json:"parent_step_id,omitempty"`
	ParentAgent  string         `json:"parent_agent,omitempty"`
	Explanation  string         `json:"explanation,omitempty"`
	Items        []PlanItemView `json:"items,omitempty"`
}

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
}

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
}

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
}

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
}

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
}

type FinalAnswerPart struct {
	AgentName  string   `json:"agent_name,omitempty"`
	Content    string   `json:"content"`
	Source     string   `json:"source,omitempty"`
	References []string `json:"references,omitempty"`
}

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
