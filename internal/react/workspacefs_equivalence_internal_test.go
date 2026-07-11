package react

// L08/L09（M1）：workspacefs.Layout 与现存路径 helper 的等价性安全网。
// - TestLayoutMatchesLegacyHelpers：语义未变的路径，新旧输出必须逐字相等；
//   旧 helper 全部删除（M3/M5）后本测试随之删除。
// - TestLayoutIntentionalNamespaceDivergence：顶层 namespace 归一是有意行为变更
//   （旧写点 artifacts/root/… → 新写点 artifacts/…），此处固化差异并断言
//   Layout 的 Legacy 回退恰好覆盖旧写点——这不是回归，勿"修复"。

import (
	"path/filepath"
	"testing"

	"aster/internal/builtin_tools"
	"aster/internal/workspacefs"
)

func eqPath(t *testing.T, name, legacy, layout string) {
	t.Helper()
	if legacy != layout {
		t.Errorf("%s: 旧=%q 新=%q（应相等）", name, legacy, layout)
	}
}

func TestLayoutMatchesLegacyHelpers(t *testing.T) {
	const root = "/ws-root"
	l := workspacefs.New(root, "")

	// builtin_tools 导出 helper（abs 族）
	eqPath(t, "planner_journal_abs", builtin_tools.WorkspacePlannerJournalFileAbs(root), l.PlannerJournal())
	eqPath(t, "step_contexts_abs", builtin_tools.WorkspaceStepContextsFileAbs(root), l.StepContexts())

	// builtin_tools 归一版 artifacts root（其语义与新 Layout 相同）
	for _, ns := range []string{"", "root", "sub-a"} {
		eqPath(t, "artifacts_root_rel/"+ns,
			builtin_tools.WorkspaceArtifactsRootRel(ns),
			workspacefs.New(root, ns).ArtifactsRootRel())
	}

	// react step 过程文件 helper
	shared := filepath.Join(root, "shared")
	eqPath(t, "step_file_abs", stepFileAbs(shared, "p1-s1"), l.StepFile("p1-s1"))
	eqPath(t, "step_file_rel", stepFileRelPath("p1-s1"), l.StepFileRel("p1-s1"))

	// react 内联拼接散点（timeline/coverage/tool-output/async/sub_agents/state）
	eqPath(t, "timeline", filepath.Join(shared, "p1-s1", "timeline.jsonl"), l.StepTimeline("p1-s1"))
	eqPath(t, "coverage", filepath.Join(shared, "p1-s1", "coverage.json"), l.StepCoverage("p1-s1"))
	eqPath(t, "tool_output_dir", filepath.Join(root, "tool-output"), l.ToolOutputDir())
	eqPath(t, "async_result", filepath.Join(root, "async_result.json"), l.AsyncResult())
	eqPath(t, "sub_agent_dir", filepath.Join(root, "sub_agents", "c1"), l.SubAgentDir("c1"))
	eqPath(t, "state_json_rel", "workspace/state.json", l.StateJSONRel())
	eqPath(t, "references_rel", "workspace/references.jsonl", l.ReferencesRel())

	// artifactWriter rel 方法族：非 root namespace 下新旧两套规则本就一致
	rt, err := newLocalWorkspaceRuntime("eq-sess", t.TempDir(), "sub-a")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	w, err := newArtifactWriter(rt)
	if err != nil {
		t.Fatalf("newArtifactWriter: %v", err)
	}
	lsub := workspacefs.New(rt.RootDir(), "sub-a")
	eqPath(t, "aw.sub.artifacts_root", w.artifactsRootRel(), lsub.ArtifactsRootRel())
	eqPath(t, "aw.sub.plan_current", w.planCurrentFileRel(), lsub.PlanCurrentRel())
	eqPath(t, "aw.sub.plan_history", w.planHistoryFileRel(2), lsub.PlanHistoryRel(2))
	eqPath(t, "aw.sub.plan_history_guard", w.planHistoryFileRel(0), lsub.PlanHistoryRel(0))
	eqPath(t, "aw.sub.final_dir", w.finalDirRel(3), lsub.FinalDirRel(3))
	eqPath(t, "aw.sub.final_answer", w.finalAnswerFileRel(3), lsub.FinalAnswerRel(3))
	eqPath(t, "aw.sub.final_assessment", w.finalAssessmentFileRel(3), lsub.FinalAssessmentRel(3))
	eqPath(t, "aw.sub.final_dir_guard", w.finalDirRel(0), lsub.FinalDirRel(0))
}

// L09：顶层 namespace 归一的有意差异——旧 artifactWriter（ns="root"）写
// artifacts/root/…，新 Layout 顶层写 artifacts/…；Legacy 回退恰好指向旧写点。
func TestLayoutIntentionalNamespaceDivergence(t *testing.T) {
	rt, err := newLocalWorkspaceRuntime("eq-sess", t.TempDir(), "root")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	w, err := newArtifactWriter(rt)
	if err != nil {
		t.Fatalf("newArtifactWriter: %v", err)
	}
	ltop := workspacefs.New(rt.RootDir(), "root")

	// 旧侧（不归一）：ns="root" 落 artifacts/root/ 子树
	if got := w.planCurrentFileRel(); got != "artifacts/root/plan/current.json" {
		t.Fatalf("旧 planCurrentFileRel = %q（旧语义锚点漂移）", got)
	}
	// 新侧（归一）：顶层落 artifacts/
	if got := ltop.PlanCurrentRel(); got != "artifacts/plan/current.json" {
		t.Fatalf("新 PlanCurrentRel = %q（归一语义漂移）", got)
	}
	// Legacy 回退 == 旧写点，保证存量 session 读回退可命中
	eqPath(t, "legacy_plan_current", w.planCurrentFileRel(), ltop.LegacyPlanCurrentRel())
	eqPath(t, "legacy_plan_history", w.planHistoryFileRel(1), ltop.LegacyPlanHistoryRel(1))
	eqPath(t, "legacy_final_dir", w.finalDirRel(1), ltop.LegacyFinalDirRel(1))
}
