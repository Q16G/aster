package react

import (
	"time"

	"aster/internal/builtin_tools"
)

// planCurrentCheckpoint is the durable skeleton checkpoint persisted at:
// - artifacts[/<namespace>]/plan/current.json
//
// It intentionally mirrors the JSON payload written by artifactWriter so it can be
// used for both write and resume load.
type planCurrentCheckpoint struct {
	SessionID        string                         `json:"session_id,omitempty"`
	PlanVersion      int                            `json:"plan_version,omitempty"`
	CurrentStepID    string                         `json:"current_step_id,omitempty"`
	Status           builtin_tools.TaskStatus       `json:"status,omitempty"`
	UpdatedAt        time.Time                      `json:"updated_at,omitempty"`
	Explanation      string                         `json:"explanation,omitempty"`
	Plan             []*builtin_tools.PlanItem      `json:"plan,omitempty"`
	Warnings         []string                       `json:"warnings,omitempty"`
	Unresolved       []string                       `json:"unresolved,omitempty"`
	ReplanContext    *builtin_tools.ReplanContext   `json:"replan_context,omitempty"`
	StatusSummary    string                         `json:"status_summary,omitempty"`
	CurrentGoal      string                         `json:"current_goal,omitempty"`
	InputTimeline    []*builtin_tools.TimelineInput `json:"input_timeline,omitempty"`
	ActiveSkillNames []string                       `json:"active_skill_names,omitempty"`
	ActiveMCPServers []string                       `json:"active_mcp_servers,omitempty"`
	LatestFinalSeq   int                            `json:"latest_final_seq,omitempty"`
}

type assessedStatePayload struct {
	Status            builtin_tools.TaskStatus         `json:"status,omitempty"`
	StateError        string                           `json:"state_error,omitempty"`
	InputTimeline     []*builtin_tools.TimelineInput   `json:"input_timeline,omitempty"`
	NeedsPlanning     bool                             `json:"needs_planning,omitempty"`
	Plan              []*builtin_tools.PlanItem        `json:"plan,omitempty"`
	PlanVersion       int                              `json:"plan_version,omitempty"`
	StepOutcomes      []*builtin_tools.StepOutcome     `json:"step_outcomes,omitempty"`
	ExternalInterrupt *builtin_tools.ExternalInterrupt `json:"external_interrupt,omitempty"`
	Warnings          []string                         `json:"warnings,omitempty"`
	Unresolved        []string                         `json:"unresolved,omitempty"`
	ReplanContext     *builtin_tools.ReplanContext     `json:"replan_context,omitempty"`
	ActiveSkillNames  []string                         `json:"active_skill_names,omitempty"`
	ActiveMCPServers  []string                         `json:"active_mcp_servers,omitempty"`
}

type FinalAssessmentArtifact struct {
	SessionID     string                 `json:"session_id,omitempty"`
	PlanVersion   int                    `json:"plan_version,omitempty"`
	AssessedState assessedStatePayload   `json:"assessed_state,omitempty"`
	Assessment    FinalAnswerModelOutput `json:"assessment,omitempty"`
}

type stepResultArtifact struct {
	SessionID    string    `json:"session_id,omitempty"`
	PlanVersion  int       `json:"plan_version,omitempty"`
	StepID       string    `json:"step_id,omitempty"`
	StepKey      string    `json:"step_key,omitempty"`
	ArtifactID   string    `json:"artifact_id,omitempty"`
	ContextKey   string    `json:"context_key,omitempty"`
	Step         string    `json:"step,omitempty"`
	Status       string    `json:"status,omitempty"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
	References   []string  `json:"references,omitempty"`
	AgentProfile string    `json:"agent_profile,omitempty"`
	Raw          struct {
		Summary       string   `json:"summary,omitempty"`
		DisplayResult string   `json:"display_result,omitempty"`
		Result        string   `json:"result,omitempty"`
		Error         string   `json:"error,omitempty"`
		References    []string `json:"references,omitempty"`
		ArtifactID    string   `json:"artifact_id,omitempty"`
		ArtifactDir   string   `json:"artifact_dir,omitempty"`
		SummaryFile   string   `json:"summary_file,omitempty"`
		ResultFile    string   `json:"result_file,omitempty"`
		ContextKey    string   `json:"context_key,omitempty"`
		TimelineDiff  string   `json:"timeline_diff,omitempty"`
		StatusSummary string   `json:"status_summary,omitempty"`
		ShortSummary  string   `json:"short_summary,omitempty"`
		LongSummary   string   `json:"long_summary,omitempty"`
		KeyFacts      []string `json:"key_facts,omitempty"`
		OpenQuestions []string `json:"open_questions,omitempty"`
	} `json:"raw,omitempty"`
}
