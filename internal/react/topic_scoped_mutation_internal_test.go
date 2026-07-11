package react

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

// snapshotMergedPlan 把 merge 结果摊平为可读的 "id:status" 有序清单，供断言与结果打印。
func snapshotMergedPlan(items []*builtin_tools.PlanItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if it == nil {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%s", strings.TrimSpace(it.ID), it.Status))
	}
	sort.Strings(out)
	return out
}

// TestMergeReplannedPlan_ScopedToTopic 是第四阶段并发安全**红线**测试：
// per-topic 局部 review 的回流（ReplaceTopicID=topic-a）只替换 topic-a 的 pending，
// topic-b 的 in_progress / pending **原样保留**——不 clobber 在跑的他 topic。
// 并与全局模式（ReplaceTopicID=""，丢全部 pending）对照，暴露具体差异。
func TestMergeReplannedPlan_ScopedToTopic(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "a1", PhaseID: "topic-a", Status: builtin_tools.PlanStepCompleted},
		{ID: "a2", PhaseID: "topic-a", Status: builtin_tools.PlanStepPending},    // topic-a 旧 pending → 应被替换
		{ID: "b1", PhaseID: "topic-b", Status: builtin_tools.PlanStepInProgress}, // topic-b 在跑 → 必须保留
		{ID: "b2", PhaseID: "topic-b", Status: builtin_tools.PlanStepPending},    // topic-b pending → 必须保留（红线）
	}
	next := []*builtin_tools.PlanItem{ // planner 局部回流只产 topic-a 的新深 step
		{ID: "a3", PhaseID: "topic-a", Status: builtin_tools.PlanStepPending, Step: "topic-a 深化"},
	}

	scoped := snapshotMergedPlan(mergeReplannedPlan(prev, next, "topic-a"))
	t.Logf("scoped(topic-a) merged = %v", scoped)
	wantScoped := []string{"a1:completed", "a3:pending", "b1:in_progress", "b2:pending"}
	if strings.Join(scoped, ",") != strings.Join(wantScoped, ",") {
		t.Fatalf("红线违反：per-topic 收窄应 = %v，got %v（topic-b 的 b1/b2 必须保留、a2 被 a3 替换）", wantScoped, scoped)
	}

	global := snapshotMergedPlan(mergeReplannedPlan(prev, next, ""))
	t.Logf("global merged = %v", global)
	wantGlobal := []string{"a1:completed", "a3:pending", "b1:in_progress"} // 全局丢全部 pending：a2、b2 都没了
	if strings.Join(global, ",") != strings.Join(wantGlobal, ",") {
		t.Fatalf("全局模式应 = %v（a2、b2 都被丢），got %v", wantGlobal, global)
	}
}
