package builtin_tools

import "time"

type WorkspaceStepOutcomePointer struct {
	PlanVersion int               `json:"plan_version"`
	StepKey     string            `json:"step_key"`
	ArtifactID  string            `json:"artifact_id,omitempty"`
	Status      StepOutcomeStatus `json:"status,omitempty"`
	ArtifactDir string            `json:"artifact_dir,omitempty"`
	SummaryFile string            `json:"summary_file,omitempty"`
	ResultFile  string            `json:"result_file,omitempty"`
	ContextKey  string            `json:"context_key,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty"`
}

type WorkspaceChildAgentPointer struct {
	Status            string    `json:"status,omitempty"`
	ParentStepKey     string    `json:"parent_step_key,omitempty"`
	ArtifactRootDir   string    `json:"artifact_root_dir,omitempty"`
	StateFile         string    `json:"state_file,omitempty"`
	LatestFinalFile   string    `json:"latest_final_file,omitempty"`
	LatestSummaryFile string    `json:"latest_summary_file,omitempty"`
	ResumeKey         string    `json:"resume_key,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

type WorkspaceState struct {
	SessionID             string                                  `json:"session_id"`
	Status                TaskStatus                              `json:"status,omitempty"`
	CurrentPlanVersion    int                                     `json:"current_plan_version,omitempty"`
	CurrentStepKey        string                                  `json:"current_step_key,omitempty"`
	LatestStepOutcomes    map[string]*WorkspaceStepOutcomePointer `json:"latest_step_outcomes,omitempty"`
	LatestFinalSeq        int                                     `json:"latest_final_seq,omitempty"`
	ChildAgents           map[string]*WorkspaceChildAgentPointer  `json:"child_agents,omitempty"`
	ChildAgentResumeIndex map[string]string                       `json:"child_agent_resume_index,omitempty"`
	Warnings              []string                                `json:"warnings,omitempty"`
	Unresolved            []string                                `json:"unresolved,omitempty"`
	ReplanContext         *ReplanContext                          `json:"replan_context,omitempty"`
	ActiveSkillNames      []string                                `json:"active_skill_names,omitempty"`
	ActiveMCPServers      []string                                `json:"active_mcp_servers,omitempty"`
	ActiveReferenceIDs    []string                                `json:"active_reference_ids,omitempty"`
	Extensions            map[string]any                          `json:"extensions,omitempty"`
	UpdatedAt             time.Time                               `json:"updated_at,omitempty"`
}

type WorkspaceReferenceRecord struct {
	RefID        string         `json:"ref_id"`
	Kind         string         `json:"kind,omitempty"`
	Title        string         `json:"title,omitempty"`
	URI          string         `json:"uri,omitempty"`
	FilePath     string         `json:"file_path,omitempty"`
	CallID       string         `json:"call_id,omitempty"`
	StepKey      string         `json:"step_key,omitempty"`
	AgentProfile string         `json:"agent_profile,omitempty"`
	ArtifactPath string         `json:"artifact_path,omitempty"`
	CreatedAt    time.Time      `json:"created_at,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// StepContextRecord is an append-only execution lineage record persisted in workspace/step_contexts.jsonl.
//
// It is a "truth layer" index used to:
// - inject direct execution contexts into prompts
// - locate the latest coarse result (via ResultKeys shape)
// - trace replan/child-agent context inheritance via ContextKey chains
type StepContextRecord struct {
	ContextKey           string    `json:"context_key"`
	Namespace            string    `json:"namespace,omitempty"`
	StepID               string    `json:"step_id"`
	StepKey              string    `json:"step_key,omitempty"`
	PlanVersion          int       `json:"plan_version"`
	AgentProfile         string    `json:"agent_profile,omitempty"`
	SummaryFile          string    `json:"summary_file,omitempty"`
	ResultFile           string    `json:"result_file,omitempty"`
	ResultKeys           []string  `json:"result_keys,omitempty"`
	ShortSummary         string    `json:"short_summary,omitempty"`
	KeyFacts             []string  `json:"key_facts,omitempty"`
	ToolCallsDigest      []string  `json:"tool_calls_digest,omitempty"`
	References           []string  `json:"references,omitempty"`
	InheritedContextKeys []string  `json:"inherited_context_keys,omitempty"`
	InheritedRefIDs      []string  `json:"inherited_ref_ids,omitempty"`
	CreatedAt            time.Time `json:"created_at,omitempty"`
	TranscriptBlobRef    string    `json:"transcript_blob_ref,omitempty"`
}
