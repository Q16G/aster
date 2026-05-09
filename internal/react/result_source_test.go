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

func TestResolveContractStep_PublishContractMatch(t *testing.T) {
	match := resolveContractStep(
		[]*builtin_tools.PlanItem{
			{ID: "s1", OutputContractRef: "contract-a"},
			{ID: "s2", OutputContractRef: "contract-b"},
		},
		[]*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, Result: "r1"},
			{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted, Result: "r2"},
		},
		"contract-a",
	)
	if match == nil {
		t.Fatal("expected match")
	}
	if match.ContractRef != "contract-a" {
		t.Fatalf("expected contract-a, got %q", match.ContractRef)
	}
	if match.Outcome.Result != "r1" {
		t.Fatalf("expected r1, got %q", match.Outcome.Result)
	}
}

func TestResolveContractStep_PublishContractNoOutcome_DoesNotFallToOther(t *testing.T) {
	match := resolveContractStep(
		[]*builtin_tools.PlanItem{
			{ID: "s1", OutputContractRef: "contract-a"},
			{ID: "s2", OutputContractRef: "contract-b"},
		},
		[]*builtin_tools.StepOutcome{
			{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted, Result: "r2"},
		},
		"contract-a",
	)
	if match != nil {
		t.Fatalf("explicit publishContract with no outcome must not fall to another contract, got %+v", match)
	}
}

func TestLatestNonEmptyStepResultWithPlan_PublishContractNoOutcome_DegradedFallback(t *testing.T) {
	result, ok, degraded := latestNonEmptyStepResultWithPlan(
		[]*builtin_tools.StepOutcome{
			{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted, Result: "r2"},
		},
		[]*builtin_tools.PlanItem{
			{ID: "s1", OutputContractRef: "contract-a"},
			{ID: "s2", OutputContractRef: "contract-b"},
		},
		"contract-a",
	)
	if !ok {
		t.Fatal("expected degraded fallback to return ok=true")
	}
	if result != "r2" {
		t.Fatalf("expected r2 from degraded fallback, got %q", result)
	}
	if !degraded {
		t.Fatal("expected degraded=true")
	}
}

func TestResolveContractStep_NoPublishContract_PicksLast(t *testing.T) {
	match := resolveContractStep(
		[]*builtin_tools.PlanItem{
			{ID: "s1", OutputContractRef: "contract-a"},
			{ID: "s2", OutputContractRef: "contract-b"},
		},
		[]*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, Result: "r1"},
			{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted, Result: "r2"},
		},
		"",
	)
	if match == nil {
		t.Fatal("expected match")
	}
	if match.ContractRef != "contract-b" {
		t.Fatalf("expected contract-b (last), got %q", match.ContractRef)
	}
	if match.Outcome.Result != "r2" {
		t.Fatalf("expected r2, got %q", match.Outcome.Result)
	}
}

func TestResolveContractStep_SkipsFailedOutcome(t *testing.T) {
	match := resolveContractStep(
		[]*builtin_tools.PlanItem{
			{ID: "s1", OutputContractRef: "contract-a"},
			{ID: "s2", OutputContractRef: "contract-b"},
		},
		[]*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, Result: "r1"},
			{StepID: "s2", Status: builtin_tools.StepOutcomeFailed, Result: "r2-failed"},
		},
		"",
	)
	if match == nil {
		t.Fatal("expected match")
	}
	if match.ContractRef != "contract-a" {
		t.Fatalf("expected contract-a (last eligible), got %q", match.ContractRef)
	}
}

func TestResolveContractStep_NoEligibleOutcomes(t *testing.T) {
	match := resolveContractStep(
		[]*builtin_tools.PlanItem{
			{ID: "s1", OutputContractRef: "contract-a"},
		},
		[]*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeFailed, Result: "failed"},
		},
		"",
	)
	if match != nil {
		t.Fatalf("expected nil, got %+v", match)
	}
}

func TestResolveContractStep_NilPlan(t *testing.T) {
	match := resolveContractStep(nil, []*builtin_tools.StepOutcome{
		{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, Result: "r1"},
	}, "")
	if match != nil {
		t.Fatalf("expected nil for nil plan, got %+v", match)
	}
}

func TestLatestNonEmptyStepResultWithPlan_IgnoresFailedOutcome(t *testing.T) {
	result, ok, degraded := latestNonEmptyStepResultWithPlan([]*builtin_tools.StepOutcome{
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
	if degraded {
		t.Fatal("expected degraded=false for exact contract match")
	}
}

func TestLatestNonEmptyStepResultWithPlan_NoContractSteps_DegradedFallback(t *testing.T) {
	result, ok, degraded := latestNonEmptyStepResultWithPlan(
		[]*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, Result: "result-1"},
			{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted, Result: "result-2"},
		},
		[]*builtin_tools.PlanItem{
			{ID: "s1", Step: "explore"},
			{ID: "s2", Step: "summarize"},
		},
		"sast-findings",
	)
	if !ok {
		t.Fatal("expected degraded fallback when no contract steps exist")
	}
	if !degraded {
		t.Fatal("expected degraded=true when publishContract is set but no plan step has contract ref")
	}
	if result == "" {
		t.Fatal("expected non-empty result from degraded fallback")
	}
}

func TestLatestNonEmptyStepResultWithPlan_NoEligibleOutcomes_Degraded(t *testing.T) {
	_, ok, degraded := latestNonEmptyStepResultWithPlan(
		[]*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeFailed, Result: "fail"},
		},
		[]*builtin_tools.PlanItem{
			{ID: "s1", Step: "explore"},
		},
		"sast-findings",
	)
	if ok {
		t.Fatal("expected ok=false when no eligible outcomes exist")
	}
	if !degraded {
		t.Fatal("expected degraded=true")
	}
}
