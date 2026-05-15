package persistv2

// V2 persistence for session/turn execution.
//
// Design goals (per RFC):
// - Single source of truth: events.jsonl + snapshot.json.
// - Append-only event log (tail truncation is detectable and recoverable).
// - Snapshots are written atomically (temp + fsync + rename).

import "time"

const FormatVersion = 2

type SessionState string

const (
	SessionStateRecovering      SessionState = "RECOVERING"
	SessionStateIdle            SessionState = "IDLE"
	SessionStateBusy            SessionState = "BUSY"
	SessionStateWaitingForHuman SessionState = "WAITING_FOR_HUMAN"
)

type TurnStatus string

const (
	TurnStatusRunning     TurnStatus = "running"
	TurnStatusSucceeded   TurnStatus = "succeeded"
	TurnStatusFailed      TurnStatus = "failed"
	TurnStatusCancelled   TurnStatus = "cancelled"
	TurnStatusInterrupted TurnStatus = "interrupted"
)

// Event is the append-only record in events.jsonl (one JSON object per line).
//
// Required fields: format_version, seq, time, type, session_id.
// Recommended correlation fields: turn_id, step_id, attempt_id, interrupt_id.
type Event struct {
	FormatVersion int    `json:"format_version"`
	Seq           uint64 `json:"seq"`
	TimeUnixMs    int64  `json:"time"`
	Type          string `json:"type"`
	// EventID is an optional globally-unique identifier for referencing/deduplication.
	// Seq remains the stable ordering key within a session.
	EventID string `json:"event_id,omitempty"`
	// GroupID is an optional aggregation key for UI/event consumers.
	// Unlike event_id (record-unique), group_id is used to group multiple related events
	// into a single logical unit (e.g. a turn chain across interrupt raise/resolve).
	GroupID     string         `json:"group_id,omitempty"`
	SessionID   string         `json:"session_id"`
	TurnID      string         `json:"turn_id,omitempty"`
	StepID      string         `json:"step_id,omitempty"`
	AttemptID   string         `json:"attempt_id,omitempty"`
	InterruptID string         `json:"interrupt_id,omitempty"`
	BlobRef     string         `json:"blob_ref,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
}

type Turn struct {
	TurnID     string     `json:"turn_id"`
	GroupID    string     `json:"group_id,omitempty"`
	Status     TurnStatus `json:"status"`
	Input      string     `json:"input,omitempty"`
	StartedAt  int64      `json:"started_at,omitempty"`
	FinishedAt int64      `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type PendingInterrupt struct {
	InterruptID string         `json:"interrupt_id"`
	TurnID      string         `json:"turn_id,omitempty"`
	Question    string         `json:"question,omitempty"`
	InputType   string         `json:"input_type,omitempty"`
	Options     []string       `json:"options,omitempty"`
	Context     map[string]any `json:"context,omitempty"`

	// Optional: the tool call id that triggered the interrupt (if available).
	ToolCallID string `json:"tool_call_id,omitempty"`

	RaisedAt   int64  `json:"raised_at,omitempty"`
	ResolvedAt int64  `json:"resolved_at,omitempty"`
	Answer     string `json:"answer,omitempty"`
	AnswerBlob string `json:"answer_blob_ref,omitempty"`
}

type SystemDiagnostics struct {
	Degraded             bool     `json:"degraded,omitempty"`
	Notes                []string `json:"notes,omitempty"`
	EventsTailTruncated  bool     `json:"events_tail_truncated,omitempty"`
	EventsLastGoodSeq    uint64   `json:"events_last_good_seq,omitempty"`
	EventsLastParseError string   `json:"events_last_parse_error,omitempty"`
}

// Snapshot is the materialized view of events.jsonl.
//
// We keep this structure intentionally small and stable; large payloads should
// be written as blobs and referenced by blob_ref in events.
type Snapshot struct {
	FormatVersion int          `json:"format_version"`
	SessionID     string       `json:"session_id"`
	SessionState  SessionState `json:"session_state"`

	CurrentTurn      *Turn             `json:"current_turn,omitempty"`
	PendingInterrupt *PendingInterrupt `json:"pending_interrupt,omitempty"`

	// RuntimeStateBlobRef stores a serialized builtin_tools.StateSnapshot (or compatible)
	// that allows the engine to resume mid-turn after crashes/interrupts.
	// Keep large payloads out of snapshot.json; store the bytes in blobs/ and reference here.
	RuntimeStateBlobRef string `json:"runtime_state_blob_ref,omitempty"`
	// StepHistoryBlobRef stores a serialized []ai.MsgInfo transcript for the in-flight step window.
	// This is required to resume tool-call sequences (including human_confirm) correctly.
	StepHistoryBlobRef string `json:"step_history_blob_ref,omitempty"`
	// ConversationHistoryBlobRef stores the serialized long-term conversation history (a.history).
	// Without this, the model loses all prior-turn context after interrupt/resume.
	ConversationHistoryBlobRef string `json:"conversation_history_blob_ref,omitempty"`

	// Optional: latest deliverable output for fast resume paths.
	LatestFinal *FinalOutput `json:"latest_final,omitempty"`

	LastSeq uint64 `json:"last_seq,omitempty"`

	UpdatedAt time.Time          `json:"updated_at,omitempty"`
	System    *SystemDiagnostics `json:"system_diagnostics,omitempty"`
}

type FinalOutput struct {
	TurnID          string `json:"turn_id,omitempty"`
	Status          string `json:"status,omitempty"`
	Content         string `json:"content,omitempty"`
	BlobRef         string `json:"blob_ref,omitempty"`
	UpdatedAt       int64  `json:"updated_at,omitempty"`
}
