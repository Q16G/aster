package react

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReduceStepTimelineToolCallsDigest(t *testing.T) {
	sharedDir := filepath.Join(t.TempDir(), "shared")
	stepID := "step-1"

	ev1 := newToolCallTimelineEvent("c1", "bash", map[string]any{
		"command": "rg -n 'sink' ./src",
		"risk":    "low",
	}, "src/a.go:12: sink(...)\nsrc/b.go:30: sink(...)", "", "", 1500*time.Millisecond)
	ev2 := newToolCallTimelineEvent("c2", "read_file", map[string]any{
		"path": "/tmp/x.go",
	}, "", "open /tmp/x.go: no such file", "", 20*time.Millisecond)
	// 同摘要重复事件应去重。
	ev3 := newToolCallTimelineEvent("c3", "bash", map[string]any{
		"command": "rg -n 'sink' ./src",
		"risk":    "low",
	}, "src/a.go:12: sink(...)\nsrc/b.go:30: sink(...)", "", "", 900*time.Millisecond)

	for _, ev := range []*TimelineEvent{ev1, ev2, ev3} {
		if err := appendStepTimeline(sharedDir, stepID, ev); err != nil {
			t.Fatalf("append timeline: %v", err)
		}
	}

	if ev1.Risk != "low" || ev1.DurationMS != 1500 {
		t.Fatalf("expected risk/duration captured, got %+v", ev1)
	}
	if ev1.ArgsDigest == "" || ev1.ResultDigest == "" {
		t.Fatalf("expected digests populated, got %+v", ev1)
	}

	digest := reduceStepTimelineToolCallsDigest(sharedDir, stepID)
	if len(digest) != 2 {
		t.Fatalf("expected 2 deduped digest entries, got %d: %v", len(digest), digest)
	}
	if digest[0] != "[bash] command=rg -n 'sink' ./src → src/a.go:12: sink(...)" {
		t.Fatalf("unexpected digest[0]: %q", digest[0])
	}
	if digest[1] != "[read_file] path=/tmp/x.go → error: open /tmp/x.go: no such file" {
		t.Fatalf("unexpected digest[1]: %q", digest[1])
	}
}

func TestReduceStepTimelineToolCallsDigest_MissingFile(t *testing.T) {
	if got := reduceStepTimelineToolCallsDigest(t.TempDir(), "nope"); got != nil {
		t.Fatalf("expected nil for missing timeline, got %v", got)
	}
}
