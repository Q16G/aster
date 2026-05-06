package react

import (
	"aster/internal/builtin_tools"
	"strings"
	"testing"
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

func TestLatestNonEmptyStepResultWithPlan_IgnoresFailedOutcome(t *testing.T) {
	result, ok := latestNonEmptyStepResultWithPlan([]*builtin_tools.StepOutcome{
		{
			StepID: "step-1",
			Status: builtin_tools.StepOutcomeFailed,
			Result: `{"partial":true}`,
		},
		{
			StepID: "step-2",
			Status: builtin_tools.StepOutcomeCompleted,
			Result: `{"confirmed":true}`,
		},
	}, []*builtin_tools.PlanItem{
		{ID: "step-1", Step: "partial", OutputContractRef: "contract-a"},
		{ID: "step-2", Step: "confirmed", OutputContractRef: "contract-b"},
	}, "contract-b")
	if !ok {
		t.Fatal("expected completed outcome to be selected")
	}
	if result != `{"confirmed":true}` {
		t.Fatalf("expected completed outcome result, got %q", result)
	}
}
