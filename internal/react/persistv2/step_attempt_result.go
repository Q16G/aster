package persistv2

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type StepAttemptResult struct {
	FormatVersion int    `json:"format_version"`
	SessionID     string `json:"session_id"`
	TurnID        string `json:"turn_id,omitempty"`
	StepID        string `json:"step_id,omitempty"`
	AttemptID     string `json:"attempt_id,omitempty"`

	Status string `json:"status,omitempty"` // succeeded|failed|cancelled

	ShortSummary    string   `json:"short_summary,omitempty"`
	LongSummary     string   `json:"long_summary,omitempty"`
	OpenQuestions   []string `json:"open_questions,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	ToolCallsDigest []string `json:"tool_calls_digest,omitempty"`

	Display *StepAttemptDisplay `json:"display,omitempty"`
	Result  *StepAttemptPayload `json:"result,omitempty"`
	Timing  *StepAttemptTiming  `json:"timing,omitempty"`
}

type StepAttemptDisplay struct {
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type StepAttemptPayload struct {
	Structured any                   `json:"structured,omitempty"`
	Artifacts  []StepAttemptArtifact `json:"artifacts,omitempty"`
}

type StepAttemptArtifact struct {
	Kind  string `json:"kind,omitempty"`
	Ref   string `json:"ref,omitempty"`
	Title string `json:"title,omitempty"`
}

type StepAttemptTiming struct {
	StartedAt  int64 `json:"started_at,omitempty"`
	FinishedAt int64 `json:"finished_at,omitempty"`
}

// stepAttemptIDs 消毒并校验 step/attempt 两段路径组件。
func (s *Store) stepAttemptIDs(stepID, attemptID string) (string, string, error) {
	if s == nil {
		return "", "", fmt.Errorf("store is nil")
	}
	stepID = sanitizePathComponent(stepID)
	attemptID = sanitizePathComponent(attemptID)
	if stepID == "" {
		return "", "", fmt.Errorf("step_id is empty")
	}
	if attemptID == "" {
		return "", "", fmt.Errorf("attempt_id is empty")
	}
	return stepID, attemptID, nil
}

func (s *Store) StepAttemptResultPath(stepID, attemptID string) (string, error) {
	stepID, attemptID, err := s.stepAttemptIDs(stepID, attemptID)
	if err != nil {
		return "", err
	}
	return s.layout.StepAttemptResult(s.sessionID, stepID, attemptID), nil
}

func (s *Store) WriteStepAttemptResult(stepID, attemptID string, res *StepAttemptResult) (string, error) {
	if s == nil {
		return "", fmt.Errorf("store is nil")
	}
	if res == nil {
		return "", fmt.Errorf("result is nil")
	}
	stepID, attemptID, err := s.stepAttemptIDs(stepID, attemptID)
	if err != nil {
		return "", err
	}
	res.FormatVersion = FormatVersion
	if strings.TrimSpace(res.SessionID) == "" {
		res.SessionID = s.sessionID
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal step attempt result: %w", err)
	}
	data = append(data, '\n')
	// WriteAtomic 自动建父目录，原 MkdirAll 前置随之收编。
	if err := s.fsStore.WriteAtomic(s.layout.StepAttemptResultRel(s.sessionID, stepID, attemptID), data); err != nil {
		return "", err
	}
	return s.layout.StepAttemptResult(s.sessionID, stepID, attemptID), nil
}

func sanitizePathComponent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Keep it filesystem-friendly and traversal-safe: only allow a small safe charset.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.' || r == ':':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	out = strings.Trim(out, "._-")
	if out == "" {
		// Fallback to a stable-ish placeholder.
		out = fmt.Sprintf("x-%d", time.Now().UnixNano())
	}
	return out
}
