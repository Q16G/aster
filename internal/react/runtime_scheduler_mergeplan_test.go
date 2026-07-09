package react

import (
	"testing"

	"aster/internal/builtin_tools"
)

// This file adds comprehensive coverage for mergeReplannedPlan and the replan
// validation contract (validateTarget in runPlanPhaseWithTools). The base cases
// live in runtime_scheduler_internal_test.go; here we exercise the edges that
// the dependency-remap fix introduced: multi-dependent remap, canonical/
// whitespace/case-insensitive collision, first-wins anchoring, id-collision
// with a preserved item, input purity, the empty-next branch, and baked-field
// isolation. planIDs()/the noop client are defined in the sibling test file.

func depsOf(items []*builtin_tools.PlanItem, id string) []string {
	for _, it := range items {
		if it != nil && it.ID == id {
			return it.DependsOn
		}
	}
	return nil
}

func findItem(items []*builtin_tools.PlanItem, id string) *builtin_tools.PlanItem {
	for _, it := range items {
		if it != nil && it.ID == id {
			return it
		}
	}
	return nil
}

// TestMergeReplannedPlan_EmptyNextReturnsNext locks the len(next)==0 short
// circuit: a replan that submits no plan items yields next verbatim (nil),
// never the preserved prev — the caller treats an empty submission as "no plan".
func TestMergeReplannedPlan_EmptyNextReturnsNext(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "侦察", Status: builtin_tools.PlanStepCompleted},
	}
	if got := mergeReplannedPlan(prev, nil); got != nil {
		t.Fatalf("empty next must return next (nil), got %v", planIDs(got))
	}
	if got := mergeReplannedPlan(prev, []*builtin_tools.PlanItem{}); len(got) != 0 {
		t.Fatalf("empty next must return empty, got %v", planIDs(got))
	}
}

// TestMergeReplannedPlan_MultipleDependentsRemapped verifies a single dropped
// (text-deduped) next item is remapped for EVERY downstream referrer, not just
// the first — the remap pass walks all merged items.
func TestMergeReplannedPlan_MultipleDependentsRemapped(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "anchor", Step: "基础侦察", Status: builtin_tools.PlanStepCompleted},
	}
	next := []*builtin_tools.PlanItem{
		{ID: "dup", Step: "基础侦察", Status: builtin_tools.PlanStepPending}, // collides → dropped
		{ID: "a", Step: "枚举接口", Status: builtin_tools.PlanStepPending, DependsOn: []string{"dup"}},
		{ID: "b", Step: "分析响应", Status: builtin_tools.PlanStepPending, DependsOn: []string{"dup"}},
		{ID: "c", Step: "综合评估", Status: builtin_tools.PlanStepPending, DependsOn: []string{"dup", "a"}},
	}

	merged := mergeReplannedPlan(prev, next)

	if findItem(merged, "dup") != nil {
		t.Fatalf("text-colliding 'dup' must be dropped, got %v", planIDs(merged))
	}
	for _, id := range []string{"a", "b"} {
		if got := depsOf(merged, id); len(got) != 1 || got[0] != "anchor" {
			t.Fatalf("%s.DependsOn should remap [dup]→[anchor], got %v", id, got)
		}
	}
	if got := depsOf(merged, "c"); len(got) != 2 || got[0] != "anchor" || got[1] != "a" {
		t.Fatalf("c.DependsOn should remap only the dropped token, got %v", got)
	}
	if _, err := builtin_tools.NormalizePlanItems(merged, true); err != nil {
		t.Fatalf("remapped plan must validate, got: %v", err)
	}
}

// TestMergeReplannedPlan_RemapCanonicalizesTokens verifies both the dropped id
// and the depends_on reference are compared through CanonicalizePlanIDToken, so
// case/whitespace variants ("P1 Recon" vs "P1-RECON", both canonicalizing to
// "p1-recon") still resolve to the same dropped entry and get remapped. Note
// canonicalization folds case and turns spaces/punctuation into '-', but keeps
// '_' distinct from '-' — so the variance here is case + space, not _↔-.
func TestMergeReplannedPlan_RemapCanonicalizesTokens(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "recon-old", Step: "基础侦察", Status: builtin_tools.PlanStepCompleted},
	}
	next := []*builtin_tools.PlanItem{
		{ID: "P1 Recon", Step: "基础侦察", Status: builtin_tools.PlanStepPending}, // dropped; canon "p1-recon"
		{ID: "report", Step: "生成报告", Status: builtin_tools.PlanStepPending, DependsOn: []string{"P1-RECON"}},
	}

	merged := mergeReplannedPlan(prev, next)

	if got := depsOf(merged, "report"); len(got) != 1 || got[0] != "recon-old" {
		t.Fatalf("token-variant dep should remap to [recon-old], got %v", got)
	}
	if _, err := builtin_tools.NormalizePlanItems(merged, true); err != nil {
		t.Fatalf("plan must validate after canonical remap, got: %v", err)
	}
}

// TestMergeReplannedPlan_DedupWhitespaceAndCase confirms the dedup key is
// normalizeStepText (case-fold + collapse spaces + fullwidth punct), so a next
// item that differs only by case/spacing/punctuation from a preserved anchor is
// deduped and its referrers remapped.
func TestMergeReplannedPlan_DedupWhitespaceAndCase(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "sqli", Step: "分析 SQL 注入风险（P0）", Status: builtin_tools.PlanStepCompleted},
	}
	next := []*builtin_tools.PlanItem{
		// differs only by extra spaces, lower-case sql, fullwidth paren
		{ID: "sqli-again", Step: "分析  sql  注入风险（p0）", Status: builtin_tools.PlanStepPending},
		{ID: "poc", Step: "构造 PoC", Status: builtin_tools.PlanStepPending, DependsOn: []string{"sqli-again"}},
	}

	merged := mergeReplannedPlan(prev, next)

	if findItem(merged, "sqli-again") != nil {
		t.Fatalf("whitespace/case variant should be deduped, got %v", planIDs(merged))
	}
	if got := depsOf(merged, "poc"); len(got) != 1 || got[0] != "sqli" {
		t.Fatalf("poc dep should remap to [sqli], got %v", got)
	}
}

// TestMergeReplannedPlan_FirstPreservedWins verifies that when two preserved
// (non-pending) prev items share a normalized step text, the FIRST one anchors
// the dedup remap (matching the original "存在即去重" first-writer semantics).
func TestMergeReplannedPlan_FirstPreservedWins(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "recon-a", Step: "基础侦察", Status: builtin_tools.PlanStepCompleted},
		{ID: "recon-b", Step: "基础侦察", Status: builtin_tools.PlanStepFailed}, // same text, later
	}
	next := []*builtin_tools.PlanItem{
		{ID: "dup", Step: "基础侦察", Status: builtin_tools.PlanStepPending},
		{ID: "report", Step: "报告", Status: builtin_tools.PlanStepPending, DependsOn: []string{"dup"}},
	}

	merged := mergeReplannedPlan(prev, next)

	// both anchors preserved
	if findItem(merged, "recon-a") == nil || findItem(merged, "recon-b") == nil {
		t.Fatalf("both preserved anchors must survive, got %v", planIDs(merged))
	}
	if got := depsOf(merged, "report"); len(got) != 1 || got[0] != "recon-a" {
		t.Fatalf("dedup remap should target FIRST anchor recon-a, got %v", got)
	}
}

// TestMergeReplannedPlan_NextIdCollidesWithPreserved verifies the id-collision
// branch: a next item reusing a preserved id is skipped (not re-added, not
// recorded as dropped) and references to that id resolve against the preserved
// item without any remap.
func TestMergeReplannedPlan_NextIdCollidesWithPreserved(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "基础侦察", Status: builtin_tools.PlanStepCompleted},
	}
	next := []*builtin_tools.PlanItem{
		// same id as preserved but DIFFERENT text — must be dropped by id, NOT revived
		{ID: "recon", Step: "换个说法的侦察", Status: builtin_tools.PlanStepPending},
		{ID: "next-step", Step: "深入分析", Status: builtin_tools.PlanStepPending, DependsOn: []string{"recon"}},
	}

	merged := mergeReplannedPlan(prev, next)

	// exactly one "recon", and it's the preserved completed one
	count := 0
	for _, it := range merged {
		if it.ID == "recon" {
			count++
			if it.Status != builtin_tools.PlanStepCompleted || it.Step != "基础侦察" {
				t.Fatalf("preserved recon must win, got status=%q step=%q", it.Status, it.Step)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one 'recon', got %d in %v", count, planIDs(merged))
	}
	if got := depsOf(merged, "next-step"); len(got) != 1 || got[0] != "recon" {
		t.Fatalf("dep on preserved id must be left intact, got %v", got)
	}
	if _, err := builtin_tools.NormalizePlanItems(merged, true); err != nil {
		t.Fatalf("plan must validate, got: %v", err)
	}
}

// TestMergeReplannedPlan_DoesNotMutateInputs is the purity guard: neither prev
// nor next items may be mutated by the merge/remap. The scheduler reuses the
// prior snapshot's plan objects, so a mutation would corrupt shared state.
func TestMergeReplannedPlan_DoesNotMutateInputs(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{
			ID:       "anchor",
			Step:     "基础侦察",
			Status:   builtin_tools.PlanStepCompleted,
			KeyFacts: []string{"fact-a"},
		},
	}
	next := []*builtin_tools.PlanItem{
		{ID: "dup", Step: "基础侦察", Status: builtin_tools.PlanStepPending},
		{ID: "report", Step: "报告", Status: builtin_tools.PlanStepPending, DependsOn: []string{"dup"}},
	}

	merged := mergeReplannedPlan(prev, next)

	// prev untouched
	if len(prev[0].KeyFacts) != 1 || prev[0].KeyFacts[0] != "fact-a" {
		t.Fatalf("prev KeyFacts mutated: %v", prev[0].KeyFacts)
	}
	// next[report].DependsOn must still point at the original "dup" (remap happened on the clone)
	if len(next[1].DependsOn) != 1 || next[1].DependsOn[0] != "dup" {
		t.Fatalf("next input mutated: report.DependsOn=%v", next[1].DependsOn)
	}

	// mutating the merged clone must NOT bleed back into prev (deep clone of baked fields)
	anchor := findItem(merged, "anchor")
	if anchor == nil {
		t.Fatal("anchor missing from merged")
	}
	if len(anchor.KeyFacts) > 0 {
		anchor.KeyFacts[0] = "mutated"
		if prev[0].KeyFacts[0] != "fact-a" {
			t.Fatalf("merged clone aliases prev KeyFacts backing array")
		}
	}
}

// TestMergeReplannedPlan_PreservesCompletedBakedFields complements the existing
// failed/skipped coverage: a completed item's full baked outcome survives merge
// (deep-cloned), so a direct-to-Step re-plan never resets it to a bare item.
func TestMergeReplannedPlan_PreservesCompletedBakedFields(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{
			ID:              "recon",
			Step:            "侦察",
			Status:          builtin_tools.PlanStepCompleted,
			ShortSummary:    "映射了 12 个入口",
			KeyFacts:        []string{"入口 A", "入口 B"},
			ToolCallsDigest: []string{"list_dir /", "read_file main.go"},
			References:      []string{"shared/recon/result.json"},
			StepFile:        "shared/step_recon.md",
			TimelineFile:    "shared/recon/timeline.jsonl",
		},
	}
	next := []*builtin_tools.PlanItem{
		{ID: "deep", Step: "深入分析", Status: builtin_tools.PlanStepPending, DependsOn: []string{"recon"}},
	}

	got := findItem(mergeReplannedPlan(prev, next), "recon")
	if got == nil {
		t.Fatal("completed recon must be preserved")
	}
	if got.ShortSummary == "" || len(got.KeyFacts) != 2 || len(got.ToolCallsDigest) != 2 ||
		len(got.References) != 1 || got.StepFile == "" || got.TimelineFile == "" {
		t.Fatalf("baked fields lost on completed item: %+v", got)
	}
}

// TestMergeReplannedPlan_NilEntriesSkipped verifies nil items in either slice
// are tolerated (defensive: planner output / snapshots can carry gaps).
func TestMergeReplannedPlan_NilEntriesSkipped(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		nil,
		{ID: "recon", Step: "侦察", Status: builtin_tools.PlanStepCompleted},
	}
	next := []*builtin_tools.PlanItem{
		nil,
		{ID: "deep", Step: "深入", Status: builtin_tools.PlanStepPending, DependsOn: []string{"recon"}},
		nil,
	}

	merged := mergeReplannedPlan(prev, next)
	if findItem(merged, "recon") == nil || findItem(merged, "deep") == nil {
		t.Fatalf("real items must survive nil gaps, got %v", planIDs(merged))
	}
	for _, it := range merged {
		if it == nil {
			t.Fatal("merged must not contain nil entries")
		}
	}
}

// TestMergeReplannedPlan_AllPrevPending confirms that when every prev item is
// pending (nothing to preserve), the merge behaves like a full replacement:
// no anchors, no remap, next chain returned intact and valid.
func TestMergeReplannedPlan_AllPrevPending(t *testing.T) {
	prev := []*builtin_tools.PlanItem{
		{ID: "old-1", Step: "旧步骤一", Status: builtin_tools.PlanStepPending},
		{ID: "old-2", Step: "旧步骤二", Status: builtin_tools.PlanStepPending, DependsOn: []string{"old-1"}},
	}
	next := []*builtin_tools.PlanItem{
		{ID: "new-1", Step: "新步骤一", Status: builtin_tools.PlanStepPending},
		{ID: "new-2", Step: "新步骤二", Status: builtin_tools.PlanStepPending, DependsOn: []string{"new-1"}},
	}

	merged := mergeReplannedPlan(prev, next)
	if ids := planIDs(merged); len(ids) != 2 || ids[0] != "new-1" || ids[1] != "new-2" {
		t.Fatalf("all-pending prev should be fully replaced, got %v", ids)
	}
	if _, err := builtin_tools.NormalizePlanItems(merged, true); err != nil {
		t.Fatalf("replacement chain must validate, got: %v", err)
	}
}

// TestReplanValidation_MergedVsParsedBranch locks the validateTarget selection
// in runPlanPhaseWithTools: replan回流（ReplacePending）必须校验合并后的 plan，
// 否则「parsed 自身闭包完整、合并后才悬空」的提交会绕过重试通道、只在调度侧终态崩。
// 这里用与该处相同的分支逻辑复现两种事故形态：
//   - 可 remap 的撞文案悬空 → 合并后已被治愈，校验放行；
//   - 不可 remap 的真悬空（依赖旧 pending，非撞文案）→ 合并后仍悬空，校验拒绝，
//     而只看 parsed 会漏过（parsed 自身闭包完整）。
func TestReplanValidation_MergedVsParsedBranch(t *testing.T) {
	// 事故形态一：撞文案悬空，合并治愈。
	prevA := []*builtin_tools.PlanItem{
		{ID: "recon-old", Step: "基础侦察", Status: builtin_tools.PlanStepCompleted},
	}
	parsedA := []*builtin_tools.PlanItem{
		{ID: "step-35", Step: "基础侦察", Status: builtin_tools.PlanStepPending},
		{ID: "report", Step: "报告", Status: builtin_tools.PlanStepPending, DependsOn: []string{"step-35"}},
	}
	// parsed 自身闭包完整 → 只校验 parsed 会放行。
	if _, err := builtin_tools.NormalizePlanItems(parsedA, true); err != nil {
		t.Fatalf("parsedA 应自身闭包完整, got: %v", err)
	}
	mergedA := selectValidateTarget(true, prevA, parsedA)
	if _, err := builtin_tools.NormalizePlanItems(mergedA, true); err != nil {
		t.Fatalf("撞文案悬空经合并 remap 后应放行, got: %v", err)
	}

	// 事故形态二：真悬空（依赖被替换的旧 pending），合并后仍非法。
	prevB := []*builtin_tools.PlanItem{
		{ID: "recon", Step: "侦察", Status: builtin_tools.PlanStepCompleted},
		{ID: "get-cred", Step: "获取凭证", Status: builtin_tools.PlanStepPending},
	}
	parsedB := []*builtin_tools.PlanItem{
		// parsed 把 get-cred 也带上 → 自身闭包完整，只校验 parsed 会漏过。
		{ID: "get-cred", Step: "获取凭证", Status: builtin_tools.PlanStepPending},
		{ID: "auth", Step: "认证测试", Status: builtin_tools.PlanStepPending, DependsOn: []string{"get-cred"}},
	}
	if _, err := builtin_tools.NormalizePlanItems(parsedB, true); err != nil {
		t.Fatalf("parsedB 应自身闭包完整（漏过点）, got: %v", err)
	}
	// 但 get-cred 与 auth 同为 pending，parsedB 中 get-cred 存在，合并后其实也保留——
	// 需要制造合并后悬空：让 parsed 依赖一个既非撞文案、又不在 parsed 中的旧 pending id。
	parsedBDangling := []*builtin_tools.PlanItem{
		{ID: "auth", Step: "认证测试", Status: builtin_tools.PlanStepPending, DependsOn: []string{"get-cred"}},
	}
	mergedB := selectValidateTarget(true, prevB, parsedBDangling)
	if _, err := builtin_tools.NormalizePlanItems(mergedB, true); err == nil {
		t.Fatal("依赖被替换旧 pending 的真悬空，合并后校验必须拒绝")
	}
	// 反证：非 replan 分支（ReplacePending=false）只校验 parsed 本身。
	if got := selectValidateTarget(false, prevB, parsedBDangling); len(got) != 1 || got[0].ID != "auth" {
		t.Fatalf("非 replan 分支必须返回 parsed 原样, got %v", planIDs(got))
	}
}

// selectValidateTarget mirrors the inline branch in runPlanPhaseWithTools that
// decides what NormalizePlanItems validates. Kept in the test to lock the
// contract without standing up a full plan-phase run.
func selectValidateTarget(replacePending bool, prev, parsed []*builtin_tools.PlanItem) []*builtin_tools.PlanItem {
	if replacePending {
		return mergeReplannedPlan(prev, parsed)
	}
	return parsed
}
