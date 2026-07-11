package react

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestAppendStepTimeline_CreatesFileAndAppends(t *testing.T) {
	rt, err := newLocalWorkspaceRuntime("sess-tl", t.TempDir(), "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	stepID := "step-abc"

	events := []*TimelineEvent{
		{
			TS:   time.Date(2025, 5, 20, 10, 0, 0, 0, time.UTC),
			Type: "tool_call",
			Key:  "call-1",
			Payload: map[string]any{
				"tool":   "read_file",
				"result": "ok",
			},
		},
		{
			TS:   time.Date(2025, 5, 20, 10, 1, 0, 0, time.UTC),
			Type: "human_confirm",
			Key:  "int-1",
			Payload: map[string]any{
				"question": "proceed?",
				"status":   "pending",
			},
		},
	}

	for _, ev := range events {
		if err := appendStepTimeline(rt, stepID, ev); err != nil {
			t.Fatalf("appendStepTimeline: %v", err)
		}
	}

	fp := rt.Layout().StepTimeline(stepID)
	f, err := os.Open(fp)
	if err != nil {
		t.Fatalf("open timeline file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var decoded []*TimelineEvent
	for scanner.Scan() {
		var ev TimelineEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("unmarshal line: %v", err)
		}
		decoded = append(decoded, &ev)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}

	if len(decoded) != 2 {
		t.Fatalf("expected 2 events, got %d", len(decoded))
	}
	if decoded[0].Type != "tool_call" || decoded[0].Key != "call-1" {
		t.Errorf("event[0] mismatch: %+v", decoded[0])
	}
	if decoded[1].Type != "human_confirm" || decoded[1].Key != "int-1" {
		t.Errorf("event[1] mismatch: %+v", decoded[1])
	}
}

func TestAppendStepTimeline_EmptyInputs(t *testing.T) {
	ev := &TimelineEvent{TS: time.Now().UTC(), Type: "tool_call", Key: "x"}
	rt, err := newLocalWorkspaceRuntime("sess-tl", t.TempDir(), "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}

	if err := appendStepTimeline(nil, "step-1", ev); err != nil {
		t.Errorf("nil runtime should return nil, got %v", err)
	}
	if err := appendStepTimeline(rt, "", ev); err != nil {
		t.Errorf("empty stepID should return nil, got %v", err)
	}
	if err := appendStepTimeline(rt, "step-1", nil); err != nil {
		t.Errorf("nil event should return nil, got %v", err)
	}
}

func TestStepTimelineExists(t *testing.T) {
	rt, err := newLocalWorkspaceRuntime("sess-tl", t.TempDir(), "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}

	if stepTimelineExists(rt, "nonexistent") {
		t.Error("should return false for nonexistent file")
	}

	_ = appendStepTimeline(rt, "step-1", &TimelineEvent{
		TS: time.Now().UTC(), Type: "tool_call", Key: "c1",
	})

	if !stepTimelineExists(rt, "step-1") {
		t.Error("should return true after writing")
	}
}

func TestStepTimelineRelPath(t *testing.T) {
	got := stepTimelineRelPath("step-1")
	want := "shared/step-1/timeline.jsonl"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
