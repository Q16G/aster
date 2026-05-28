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
	PartTypeFinalAnswer PartType = "final_answer"
	PartTypeSubAgent    PartType = "sub_agent"
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
	FinalAnswer *FinalAnswerPart `json:"final_answer,omitempty"`
	SubAgent    *SubAgentPart    `json:"sub_agent,omitempty"`
}

type UserPart struct {
	Content string `json:"content"`
}

type TextPart struct {
	Content string `json:"content"`
}

type ToolPart struct {
	Name       string        `json:"name"`
	CallID     string        `json:"call_id,omitempty"`
	Arguments  string        `json:"args,omitempty"`
	Result     string        `json:"result,omitempty"`
	Error      string        `json:"error,omitempty"`
	State      string        `json:"state"`
	Duration   time.Duration `json:"duration,omitempty"`
	IsAgent    bool          `json:"is_agent,omitempty"`
	StackDepth int           `json:"stack_depth,omitempty"`
	AgentName  string        `json:"agent_name,omitempty"`
	WorkspaceRoot string     `json:"workspace_root,omitempty"`
	Summary    string        `json:"summary,omitempty"`
	ChildRef   string        `json:"child_ref,omitempty"`
}

type PlanPart struct {
	AgentName    string         `json:"agent_name,omitempty"`
	ParentStepID string         `json:"parent_step_id,omitempty"`
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
	Content string `json:"content"`
	EventID string `json:"event_id,omitempty"`
	GroupID string `json:"group_id,omitempty"`
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
	StepID        string `json:"step_id,omitempty"`
	StepName      string `json:"step_name,omitempty"`
	Status        string `json:"status,omitempty"`
	DisplayResult string `json:"display_result,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Error         string `json:"error,omitempty"`
}

type StepSummaryPart struct {
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
	StepID       string   `json:"step_id,omitempty"`
	StepName     string   `json:"step_name,omitempty"`
	ShouldReplan bool     `json:"should_replan"`
	ReplanReason string   `json:"replan_reason,omitempty"`
	NextGoal     string   `json:"next_goal,omitempty"`
	MissingItems []string `json:"missing_items,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

type SubAgentPart struct {
	AgentName string        `json:"agent_name"`
	CallID    string        `json:"call_id,omitempty"`
	Status    string        `json:"status"`
	Summary   string        `json:"summary,omitempty"`
	WorkspaceRoot string    `json:"workspace_root,omitempty"`
	ChildRef      string    `json:"child_ref,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
}

type FinalAnswerPart struct {
	Content    string   `json:"content"`
	Source     string   `json:"source,omitempty"`
	References []string `json:"references,omitempty"`
}

var toolIcons = map[string]string{
	"bash":                "$",
	"read_file":           "→",
	"list_files":          "→",
	"rg":                  "✱",
	"load_skills":         "→",
	"list_skills":         "⚙",
	"delete_skill":        "✗",
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
