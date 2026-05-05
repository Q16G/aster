package tui

import (
	"fmt"
	"time"
)

type PartType string

const (
	PartTypeUser     PartType = "user"
	PartTypeText     PartType = "text"
	PartTypeTool     PartType = "tool"
	PartTypePlan     PartType = "plan"
	PartTypeSystem   PartType = "system"
	PartTypeThinking PartType = "thinking"
	PartTypeSummary      PartType = "summary"
	PartTypeStepSummary  PartType = "step_summary"
	PartTypeFinalAnswer  PartType = "final_answer"
)

type DisplayPart struct {
	Type PartType  `json:"type"`
	Time time.Time `json:"time"`

	User     *UserPart     `json:"user,omitempty"`
	Text     *TextPart     `json:"text,omitempty"`
	Tool     *ToolPart     `json:"tool,omitempty"`
	Plan     *PlanPart     `json:"plan,omitempty"`
	System   *SystemPart   `json:"system,omitempty"`
	Thinking *ThinkingPart `json:"thinking,omitempty"`
	Summary     *SummaryPart     `json:"summary,omitempty"`
	StepSummary *StepSummaryPart `json:"step_summary,omitempty"`
	FinalAnswer *FinalAnswerPart `json:"final_answer,omitempty"`
}

type UserPart struct {
	Content string `json:"content"`
}

type TextPart struct {
	Content string `json:"content"`
}

type ToolPart struct {
	Name      string        `json:"name"`
	CallID    string        `json:"call_id,omitempty"`
	Arguments string        `json:"args,omitempty"`
	Result    string        `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
	State     string        `json:"state"`
	Duration  time.Duration `json:"duration,omitempty"`
	IsAgent   bool          `json:"is_agent,omitempty"`
}

type PlanPart struct {
	Explanation string         `json:"explanation,omitempty"`
	Items       []PlanItemView `json:"items,omitempty"`
}

type PlanItemView struct {
	ID     string `json:"id,omitempty"`
	Step   string `json:"step"`
	Status string `json:"status"`
}

type SystemPart struct {
	Content string `json:"content"`
}

type ThinkingPart struct {
	Content string `json:"content"`
}

type SummaryPart struct {
	AgentName string        `json:"agent_name"`
	ModelID   string        `json:"model_id"`
	Duration  time.Duration `json:"duration"`
	Success   bool          `json:"success"`
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
