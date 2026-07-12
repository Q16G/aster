package builtin_tools

import (
	"strings"
	"testing"
)

// TestReadyFrontierPlanStepIDsScoped 校验 topic 收窄 frontier：topicID 非空时只返回该 topic 的
// 就绪 step，跨 topic 被过滤；topicID=="" 时等价全局 ReadyFrontierPlanStepIDs（Part C）。
func TestReadyFrontierPlanStepIDsScoped(t *testing.T) {
	topics := []*AnalysisTopic{
		{ID: "t-a", Status: ""},
		{ID: "t-b", Status: ""},
	}
	plan := []*PlanItem{
		{ID: "a1", TopicID: "t-a", Status: PlanStepPending},
		{ID: "a2", TopicID: "t-a", Status: PlanStepPending},
		{ID: "b1", TopicID: "t-b", Status: PlanStepPending},
		{ID: "done", TopicID: "t-a", Status: PlanStepCompleted},
	}

	global := ReadyFrontierPlanStepIDs(plan, topics)
	if len(global) != 3 {
		t.Fatalf("global frontier expected 3 ready, got %v", global)
	}

	// scope 到 t-a：只放 a1/a2，b1 被过滤。
	scopedA := ReadyFrontierPlanStepIDsScoped(plan, topics, "t-a")
	if strings.Join(scopedA, ",") != "a1,a2" {
		t.Fatalf("scoped(t-a) expected [a1 a2], got %v", scopedA)
	}
	// scope 到 t-b：只放 b1。
	scopedB := ReadyFrontierPlanStepIDsScoped(plan, topics, "t-b")
	if strings.Join(scopedB, ",") != "b1" {
		t.Fatalf("scoped(t-b) expected [b1], got %v", scopedB)
	}
	// 空 scope 等价全局。
	if strings.Join(ReadyFrontierPlanStepIDsScoped(plan, topics, ""), ",") != strings.Join(global, ",") {
		t.Fatalf("empty scope must equal global frontier")
	}
	// NextFrontierPlanStepIDScoped 取 scope 内首个。
	if got := NextFrontierPlanStepIDScoped(plan, topics, "t-b"); got != "b1" {
		t.Fatalf("NextFrontierPlanStepIDScoped(t-b) = %q, want b1", got)
	}
	// scope 到无就绪的 topic → 空。
	if got := ReadyFrontierPlanStepIDsScoped(plan, topics, "t-missing"); got != nil {
		t.Fatalf("scoped(missing) expected nil, got %v", got)
	}
}
