package react

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type TimelineEvent struct {
	TS      time.Time      `json:"ts"`
	Type    string         `json:"type"`
	Key     string         `json:"key"`
	Payload map[string]any `json:"payload,omitempty"`
}

func appendStepTimeline(sharedDir, stepID string, event *TimelineEvent) error {
	if sharedDir == "" || stepID == "" || event == nil {
		return nil
	}
	dir := filepath.Join(sharedDir, stepID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(filepath.Join(dir, "timeline.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func stepTimelineRelPath(stepID string) string {
	return fmt.Sprintf("shared/%s/timeline.jsonl", stepID)
}

func stepTimelineExists(sharedDir, stepID string) bool {
	if sharedDir == "" || stepID == "" {
		return false
	}
	p := filepath.Join(sharedDir, stepID, "timeline.jsonl")
	info, err := os.Stat(p)
	return err == nil && info.Size() > 0
}

