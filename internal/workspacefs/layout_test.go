package workspacefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L01 方法族全量拼法：Rel 输出对照 docs/workspace-fs-layout.md §三路径表。
func TestLayoutRelPaths(t *testing.T) {
	top := New("/ws", "")
	sub := New("/ws", "sub-a")

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"PlannerJournalRel", top.PlannerJournalRel(), "workspace/planner.jsonl"},
		{"StepContextsRel", top.StepContextsRel(), "workspace/step_contexts.jsonl"},
		{"StateJSONRel", top.StateJSONRel(), "workspace/state.json"},
		{"ReferencesRel", top.ReferencesRel(), "workspace/references.jsonl"},
		{"SessionDirRel", top.SessionDirRel("s1"), "workspace/sessions/s1"},
		{"SessionEventsRel", top.SessionEventsRel("s1"), "workspace/sessions/s1/events.jsonl"},
		{"SessionSnapshotRel", top.SessionSnapshotRel("s1"), "workspace/sessions/s1/snapshot.json"},
		{"SessionBlobsDirRel", top.SessionBlobsDirRel("s1"), "workspace/sessions/s1/blobs"},
		{"SessionBlobRel.bare", top.SessionBlobRel("s1", "abc123"), "workspace/sessions/s1/blobs/abc123"},
		{"SessionBlobRel.prefixed", top.SessionBlobRel("s1", "sha256:abc123"), "workspace/sessions/s1/blobs/abc123"},
		{"StepAttemptDirRel", top.StepAttemptDirRel("s1", "p1-s1", "a1"), "workspace/sessions/s1/steps/p1-s1/attempts/a1"},
		{"StepAttemptResultRel", top.StepAttemptResultRel("s1", "p1-s1", "a1"), "workspace/sessions/s1/steps/p1-s1/attempts/a1/result.json"},
		{"SharedDirRel", top.SharedDirRel(), "shared"},
		{"TaskContextRel", top.TaskContextRel(), "shared/task_context.md"},
		{"OpenItemsRel", top.OpenItemsRel(), "shared/open_items.md"},
		{"StepFileRel", top.StepFileRel("p1-s1"), "shared/step_p1-s1.md"},
		{"LegacyStepFileRel", top.LegacyStepFileRel("p1-s1"), "shared/p1-s1/step.md"},
		{"StepDirRel", top.StepDirRel("p1-s1"), "shared/p1-s1"},
		{"StepTimelineRel", top.StepTimelineRel("p1-s1"), "shared/p1-s1/timeline.jsonl"},
		{"StepCoverageRel", top.StepCoverageRel("p1-s1"), "shared/p1-s1/coverage.json"},
		{"ArtifactsRootRel.top", top.ArtifactsRootRel(), "artifacts"},
		{"ArtifactsRootRel.sub", sub.ArtifactsRootRel(), "artifacts/sub-a"},
		{"PlanCurrentRel.top", top.PlanCurrentRel(), "artifacts/plan/current.json"},
		{"PlanCurrentRel.sub", sub.PlanCurrentRel(), "artifacts/sub-a/plan/current.json"},
		{"PlanHistoryRel", top.PlanHistoryRel(2), "artifacts/plan/history/2.json"},
		{"FinalRootRel", top.FinalRootRel(), "artifacts/final"},
		{"FinalDirRel", top.FinalDirRel(3), "artifacts/final/3"},
		{"FinalAnswerRel", top.FinalAnswerRel(3), "artifacts/final/3/final_answer.md"},
		{"FinalAssessmentRel", top.FinalAssessmentRel(3), "artifacts/final/3/final_assessment.json"},
		{"FinalDirRel.sub", sub.FinalDirRel(3), "artifacts/sub-a/final/3"},
		{"SubAgentDirRel", top.SubAgentDirRel("child-1"), "sub_agents/child-1"},
		{"AsyncResultRel", top.AsyncResultRel(), "async_result.json"},
		{"ToolOutputDirRel", top.ToolOutputDirRel(), "tool-output"},
		{"LegacyPlanCurrentRel.top", top.LegacyPlanCurrentRel(), "artifacts/root/plan/current.json"},
		{"LegacyPlanHistoryRel.top", top.LegacyPlanHistoryRel(1), "artifacts/root/plan/history/1.json"},
		{"LegacyFinalRootRel.top", top.LegacyFinalRootRel(), "artifacts/root/final"},
		{"LegacyFinalDirRel.top", top.LegacyFinalDirRel(1), "artifacts/root/final/1"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// relAbsPairs 枚举全部 Rel/Abs 方法对，供恒等式与分隔符断言遍历。
func relAbsPairs(l Layout) map[string][2]string {
	return map[string][2]string{
		"PlannerJournal":    {l.PlannerJournalRel(), l.PlannerJournal()},
		"StepContexts":      {l.StepContextsRel(), l.StepContexts()},
		"StateJSON":         {l.StateJSONRel(), l.StateJSON()},
		"References":        {l.ReferencesRel(), l.References()},
		"SessionDir":        {l.SessionDirRel("s1"), l.SessionDir("s1")},
		"SessionEvents":     {l.SessionEventsRel("s1"), l.SessionEvents("s1")},
		"SessionSnapshot":   {l.SessionSnapshotRel("s1"), l.SessionSnapshot("s1")},
		"SessionBlobsDir":   {l.SessionBlobsDirRel("s1"), l.SessionBlobsDir("s1")},
		"SessionBlob":       {l.SessionBlobRel("s1", "ref1"), l.SessionBlob("s1", "ref1")},
		"StepAttemptDir":    {l.StepAttemptDirRel("s1", "st", "a1"), l.StepAttemptDir("s1", "st", "a1")},
		"StepAttemptResult": {l.StepAttemptResultRel("s1", "st", "a1"), l.StepAttemptResult("s1", "st", "a1")},
		"SharedDir":         {l.SharedDirRel(), l.SharedDir()},
		"TaskContext":       {l.TaskContextRel(), l.TaskContext()},
		"OpenItems":         {l.OpenItemsRel(), l.OpenItems()},
		"StepFile":          {l.StepFileRel("p1-s1"), l.StepFile("p1-s1")},
		"LegacyStepFile":    {l.LegacyStepFileRel("p1-s1"), l.LegacyStepFile("p1-s1")},
		"StepDir":           {l.StepDirRel("p1-s1"), l.StepDir("p1-s1")},
		"StepTimeline":      {l.StepTimelineRel("p1-s1"), l.StepTimeline("p1-s1")},
		"StepCoverage":      {l.StepCoverageRel("p1-s1"), l.StepCoverage("p1-s1")},
		"ArtifactsRoot":     {l.ArtifactsRootRel(), l.ArtifactsRoot()},
		"PlanCurrent":       {l.PlanCurrentRel(), l.PlanCurrent()},
		"PlanHistory":       {l.PlanHistoryRel(2), l.PlanHistory(2)},
		"FinalRoot":         {l.FinalRootRel(), l.FinalRoot()},
		"FinalDir":          {l.FinalDirRel(3), l.FinalDir(3)},
		"FinalAnswer":       {l.FinalAnswerRel(3), l.FinalAnswer(3)},
		"FinalAssessment":   {l.FinalAssessmentRel(3), l.FinalAssessment(3)},
		"SubAgentDir":       {l.SubAgentDirRel("c1"), l.SubAgentDir("c1")},
		"AsyncResult":       {l.AsyncResultRel(), l.AsyncResult()},
		"ToolOutputDir":     {l.ToolOutputDirRel(), l.ToolOutputDir()},
	}
}

// L02 Rel/Abs 恒等式；L06 分隔符（Rel 恒 slash、Abs 用平台分隔符）。
func TestLayoutRelAbsConsistency(t *testing.T) {
	for _, l := range []Layout{New("/ws", ""), New("/ws", "sub-a")} {
		for name, pair := range relAbsPairs(l) {
			rel, abs := pair[0], pair[1]
			if rel == "" || abs == "" {
				t.Errorf("[ns=%q] %s 意外为空: rel=%q abs=%q", l.Namespace, name, rel, abs)
				continue
			}
			want := filepath.Join(l.Root, filepath.FromSlash(rel))
			if abs != want {
				t.Errorf("[ns=%q] %s Abs = %q, want Join(Root, FromSlash(rel)) = %q", l.Namespace, name, abs, want)
			}
			if strings.Contains(rel, "\\") {
				t.Errorf("[ns=%q] %s Rel 含反斜杠: %q", l.Namespace, name, rel)
			}
			if os.PathSeparator == '/' && strings.Contains(abs, "\\") {
				t.Errorf("[ns=%q] %s Abs 含反斜杠: %q", l.Namespace, name, abs)
			}
		}
	}
}

// L03 namespace 归一 4 态。
func TestLayoutNamespaceNormalization(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"root", ""},
		{"  root  ", ""},
		{"/root/", ""},
		{"sub-a", "sub-a"},
		{" sub-a ", "sub-a"},
		{"/sub-a/", "sub-a"},
	}
	for _, tc := range cases {
		if got := New("/ws", tc.in).Namespace; got != tc.want {
			t.Errorf("New(%q).Namespace = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// L05 legacy 回退仅顶层返回非空。
func TestLayoutLegacyOnlyTopLevel(t *testing.T) {
	top := New("/ws", "")
	sub := New("/ws", "sub-a")

	if top.LegacyPlanCurrentRel() == "" || top.LegacyPlanHistoryRel(1) == "" ||
		top.LegacyFinalRootRel() == "" || top.LegacyFinalDirRel(1) == "" {
		t.Fatalf("顶层 legacy 路径不应为空")
	}
	if got := sub.LegacyPlanCurrentRel(); got != "" {
		t.Errorf("子 ns LegacyPlanCurrentRel = %q, want 空", got)
	}
	if got := sub.LegacyPlanHistoryRel(1); got != "" {
		t.Errorf("子 ns LegacyPlanHistoryRel = %q, want 空", got)
	}
	if got := sub.LegacyFinalRootRel(); got != "" {
		t.Errorf("子 ns LegacyFinalRootRel = %q, want 空", got)
	}
	if got := sub.LegacyFinalDirRel(1); got != "" {
		t.Errorf("子 ns LegacyFinalDirRel = %q, want 空", got)
	}
	if got := sub.LegacyFinalDir(1); got != "" {
		t.Errorf("子 ns LegacyFinalDir(Abs) = %q, want 空", got)
	}
}

// L07 带参方法输入边界：空串/空白入参统一返回空串（与现存 helper 防呆口径一致）；
// Root 为空时 Abs 一律空串。
func TestLayoutEmptyInputs(t *testing.T) {
	l := New("/ws", "")
	if got := l.StepFileRel(""); got != "" {
		t.Errorf("StepFileRel(\"\") = %q", got)
	}
	if got := l.StepFileRel("   "); got != "" {
		t.Errorf("StepFileRel(blank) = %q", got)
	}
	if got := l.StepTimelineRel(""); got != "" {
		t.Errorf("StepTimelineRel(\"\") = %q", got)
	}
	if got := l.SubAgentDirRel(" "); got != "" {
		t.Errorf("SubAgentDirRel(blank) = %q", got)
	}
	if got := l.SessionEventsRel(""); got != "" {
		t.Errorf("SessionEventsRel(\"\") = %q", got)
	}
	if got := l.SessionBlobRel("s1", "sha256:"); got != "" {
		t.Errorf("SessionBlobRel(空 ref) = %q", got)
	}
	if got := l.StepAttemptResultRel("s1", "", "a1"); got != "" {
		t.Errorf("StepAttemptResultRel(空 step) = %q", got)
	}

	empty := New("", "")
	if got := empty.TaskContext(); got != "" {
		t.Errorf("空 Root 的 Abs 应为空, got %q", got)
	}
	if got := empty.TaskContextRel(); got != "shared/task_context.md" {
		t.Errorf("空 Root 不影响 Rel, got %q", got)
	}
}

// L10 persistv2 路径族等价：与 persistv2/store.go 现拼法逐字一致。
func TestLayoutPersistV2PathEquivalence(t *testing.T) {
	root := "/ws"
	l := New(root, "")
	sessionDir := filepath.Join(root, "workspace", "sessions", "sess-1")
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"SessionDir", l.SessionDir("sess-1"), sessionDir},
		{"SessionEvents", l.SessionEvents("sess-1"), filepath.Join(sessionDir, "events.jsonl")},
		{"SessionSnapshot", l.SessionSnapshot("sess-1"), filepath.Join(sessionDir, "snapshot.json")},
		{"SessionBlobsDir", l.SessionBlobsDir("sess-1"), filepath.Join(sessionDir, "blobs")},
		{"SessionBlob", l.SessionBlob("sess-1", "sha256:deadbeef"), filepath.Join(sessionDir, "blobs", "deadbeef")},
		{"StepAttemptDir", l.StepAttemptDir("sess-1", "st-1", "at-1"), filepath.Join(sessionDir, "steps", "st-1", "attempts", "at-1")},
		{"StepAttemptResult", l.StepAttemptResult("sess-1", "st-1", "at-1"), filepath.Join(sessionDir, "steps", "st-1", "attempts", "at-1", "result.json")},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// legacy Abs 双形态与 int 参数防呆分支补充覆盖。
func TestLayoutLegacyAbsAndGuards(t *testing.T) {
	top := New("/ws", "")
	pairs := map[string][2]string{
		"LegacyPlanCurrent":     {top.LegacyPlanCurrentRel(), top.LegacyPlanCurrent()},
		"LegacyPlanHistory":     {top.LegacyPlanHistoryRel(2), top.LegacyPlanHistory(2)},
		"LegacyFinalRoot":       {top.LegacyFinalRootRel(), top.LegacyFinalRoot()},
		"LegacyFinalDir":        {top.LegacyFinalDirRel(2), top.LegacyFinalDir(2)},
		"LegacyFinalAnswer":     {top.LegacyFinalAnswerRel(2), top.LegacyFinalAnswer(2)},
		"LegacyFinalAssessment": {top.LegacyFinalAssessmentRel(2), top.LegacyFinalAssessment(2)},
	}
	for name, pair := range pairs {
		rel, abs := pair[0], pair[1]
		if rel == "" || abs != filepath.Join("/ws", filepath.FromSlash(rel)) {
			t.Errorf("%s: rel=%q abs=%q 不满足恒等式", name, rel, abs)
		}
	}
	// int 参数防呆（与旧 artifactWriter 行为一致）
	if got := top.PlanHistoryRel(0); got != "artifacts/plan/history/1.json" {
		t.Errorf("PlanHistoryRel(0) = %q, want 归一为版本 1", got)
	}
	if got := top.FinalDirRel(0); got != "" {
		t.Errorf("FinalDirRel(0) = %q, want 空", got)
	}
	if got := top.FinalAnswerRel(-1); got != "" {
		t.Errorf("FinalAnswerRel(-1) = %q, want 空", got)
	}
	sub := New("/ws", "sub-a")
	if got := sub.LegacyFinalAnswerRel(1); got != "" {
		t.Errorf("子 ns LegacyFinalAnswerRel = %q, want 空", got)
	}
	if got := sub.LegacyFinalAssessment(1); got != "" {
		t.Errorf("子 ns LegacyFinalAssessment = %q, want 空", got)
	}
}

// SubAgentLayout 派生：子 workspace 与父同构（顶层语义）。
func TestLayoutSubAgentDerivation(t *testing.T) {
	parent := New("/ws", "")
	child := parent.SubAgentLayout("child-1")
	if child.Root != filepath.Join("/ws", "sub_agents", "child-1") {
		t.Fatalf("child Root = %q", child.Root)
	}
	if child.Namespace != "" {
		t.Fatalf("child Namespace = %q, want 顶层", child.Namespace)
	}
	if got := child.PlanCurrentRel(); got != "artifacts/plan/current.json" {
		t.Fatalf("child PlanCurrentRel = %q", got)
	}
	if got := parent.SubAgentLayout(""); got != (Layout{}) {
		t.Fatalf("空 child 应返回零值 Layout, got %+v", got)
	}
}
