package react

import (
	"aster/internal/builtin_tools"
	"strings"
	"testing"
	"time"
)

func TestTruncateForHistory_NonStepResultPassthrough(t *testing.T) {
	long := strings.Repeat("x", 10000)
	for _, source := range []string{"fast_close", "final_assessment", "", "unknown"} {
		got := truncateForHistory(long, source)
		if got != long {
			t.Fatalf("source=%q: expected passthrough, got len=%d", source, len(got))
		}
	}
}

func TestTruncateForHistory_StepResultTruncates(t *testing.T) {
	long := strings.Repeat("漏", 10000)
	got := truncateForHistory(long, "step_result")
	runes := []rune(got)
	if len(runes) > historyMaxRunes+100 {
		t.Fatalf("expected truncation, got %d runes", len(runes))
	}
	if !strings.Contains(got, "完整结果已持久化到 artifact 文件") {
		t.Fatalf("expected truncation suffix")
	}
}

func TestTruncateForHistory_ShortStepResultNotTruncated(t *testing.T) {
	short := "small result"
	got := truncateForHistory(short, "step_result")
	if got != short {
		t.Fatalf("expected no truncation for short content, got %q", got)
	}
}

func TestTruncateForHistory_EmptyString(t *testing.T) {
	got := truncateForHistory("", "step_result")
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestTruncateForHistory_ExactBoundary(t *testing.T) {
	exact := strings.Repeat("a", historyMaxRunes)
	got := truncateForHistory(exact, "step_result")
	if got != exact {
		t.Fatalf("expected no truncation at exact boundary, got len=%d", len([]rune(got)))
	}
	oneOver := exact + "b"
	got = truncateForHistory(oneOver, "step_result")
	if !strings.Contains(got, "完整结果已持久化到 artifact 文件") {
		t.Fatalf("expected truncation at boundary+1")
	}
}

func TestLatestNonEmptyStepResultWithPlan_FallsBackToLatestByTime(t *testing.T) {
	result, ok := latestNonEmptyStepResultWithPlan(
		[]*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, Result: "result-1", UpdatedAt: time.Unix(10, 0)},
			{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted, Result: "result-2", UpdatedAt: time.Unix(20, 0)},
		},
		[]*builtin_tools.PlanItem{
			{ID: "s1", Step: "explore"},
			{ID: "s2", Step: "summarize"},
		},
	)
	if !ok {
		t.Fatal("expected fallback to return ok=true")
	}
	if result != "result-2" {
		t.Fatalf("expected latest result (by updatedAt), got %q", result)
	}
}
