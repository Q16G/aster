package persistv2

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

	ShortSummary  string   `json:"short_summary,omitempty"`
	LongSummary   string   `json:"long_summary,omitempty"`
	OpenQuestions []string `json:"open_questions,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`

	Display *StepAttemptDisplay `json:"display,omitempty"`
	Result  *StepAttemptPayload `json:"result,omitempty"`
	Timing  *StepAttemptTiming  `json:"timing,omitempty"`
}

type StepAttemptDisplay struct {
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
}

type StepAttemptPayload struct {
	Structured any                 `json:"structured,omitempty"`
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

func (s *Store) stepAttemptDir(stepID, attemptID string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("store is nil")
	}
	stepID = sanitizePathComponent(stepID)
	attemptID = sanitizePathComponent(attemptID)
	if stepID == "" {
		return "", fmt.Errorf("step_id is empty")
	}
	if attemptID == "" {
		return "", fmt.Errorf("attempt_id is empty")
	}
	return filepath.Join(s.sessionDir, "steps", stepID, "attempts", attemptID), nil
}

func (s *Store) WriteStepAttemptResult(stepID, attemptID string, res *StepAttemptResult) (string, error) {
	if s == nil {
		return "", fmt.Errorf("store is nil")
	}
	if res == nil {
		return "", fmt.Errorf("result is nil")
	}
	dir, err := s.stepAttemptDir(stepID, attemptID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
	outPath := filepath.Join(dir, "result.json")
	if err := writeFileAtomic(outPath, data, 0o644); err != nil {
		return "", err
	}
	return outPath, nil
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

