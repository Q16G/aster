package react

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"aster/internal/workspacefs"
)

func TestReduceStepTimelineToolCallsDigest(t *testing.T) {
	l := workspacefs.New(t.TempDir(), "")
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
		if err := appendStepTimeline(l, stepID, ev); err != nil {
			t.Fatalf("append timeline: %v", err)
		}
	}

	if ev1.Risk != "low" || ev1.DurationMS != 1500 {
		t.Fatalf("expected risk/duration captured, got %+v", ev1)
	}
	if ev1.ArgsDigest == "" || ev1.ResultDigest == "" {
		t.Fatalf("expected digests populated, got %+v", ev1)
	}

	digest := reduceStepTimelineToolCallsDigest(l, stepID)
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
	if got := reduceStepTimelineToolCallsDigest(workspacefs.New(t.TempDir(), ""), "nope"); got != nil {
		t.Fatalf("expected nil for missing timeline, got %v", got)
	}
}

func TestReduceStepTimelineToolCallsDigest_TruncationMarker(t *testing.T) {
	l := workspacefs.New(t.TempDir(), "")
	stepID := "step-cap"

	appendRange := func(start, n int) {
		t.Helper()
		for i := start; i < start+n; i++ {
			ev := newToolCallTimelineEvent(
				fmt.Sprintf("c%d", i), "bash",
				map[string]any{"command": fmt.Sprintf("echo %d", i)},
				fmt.Sprintf("out-%d", i), "", "", time.Millisecond,
			)
			if err := appendStepTimeline(l, stepID, ev); err != nil {
				t.Fatalf("append timeline: %v", err)
			}
		}
	}

	// 恰好达上限：不追加标记。
	appendRange(0, stepTimelineDigestMaxEntries)
	digest := reduceStepTimelineToolCallsDigest(l, stepID)
	if len(digest) != stepTimelineDigestMaxEntries {
		t.Fatalf("expected %d entries, got %d", stepTimelineDigestMaxEntries, len(digest))
	}
	if strings.Contains(digest[len(digest)-1], "[截断]") {
		t.Fatalf("at-cap digest must not carry truncation marker, got %q", digest[len(digest)-1])
	}

	// 超上限：截断 + 末尾标记含总数。
	appendRange(stepTimelineDigestMaxEntries, 5)
	digest = reduceStepTimelineToolCallsDigest(l, stepID)
	if len(digest) != stepTimelineDigestMaxEntries+1 {
		t.Fatalf("expected %d entries incl marker, got %d", stepTimelineDigestMaxEntries+1, len(digest))
	}
	marker := digest[len(digest)-1]
	if !strings.Contains(marker, "[截断]") || !strings.Contains(marker, fmt.Sprintf("共 %d 条", stepTimelineDigestMaxEntries+5)) {
		t.Fatalf("unexpected truncation marker: %q", marker)
	}
}
