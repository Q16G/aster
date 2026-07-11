package react

// M2（A01-A05 + IT2 首版）：namespace 归一后的旧布局读回退。
// 旧布局 fixture 由 workspace_golden_internal_test.go 的 seedLegacyWorkspaceFixture 预置。

import (
	"os"
	"path/filepath"
	"testing"

	"aster/internal/builtin_tools"
)

// newTopLevelArtifactWriter 以历史顶层语义（runtime namespace = "root"）构造 writer。
func newTopLevelArtifactWriter(t *testing.T, root string) *artifactWriter {
	t.Helper()
	rt, err := newLocalWorkspaceRuntime("legacy-sess", root, "root")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	w, err := newArtifactWriter(rt)
	if err != nil {
		t.Fatalf("newArtifactWriter: %v", err)
	}
	return w
}

// A01 仅旧布局有 checkpoint → 读回退命中。
func TestLoadPlanCurrentCheckpoint_LegacyFallback(t *testing.T) {
	root := t.TempDir()
	seedLegacyWorkspaceFixture(t, root)
	w := newTopLevelArtifactWriter(t, root)

	cp, err := w.LoadPlanCurrentCheckpoint()
	if err != nil {
		t.Fatalf("LoadPlanCurrentCheckpoint: %v", err)
	}
	if cp == nil || cp.SessionID != "legacy-sess" || cp.PlanVersion != 1 {
		t.Fatalf("读回退未命中旧 checkpoint: %+v", cp)
	}
}

// A02 新旧并存 → 优先读新路径。
func TestLoadPlanCurrentCheckpoint_PrefersNewLayout(t *testing.T) {
	root := t.TempDir()
	seedLegacyWorkspaceFixture(t, root)
	w := newTopLevelArtifactWriter(t, root)

	if err := w.WritePlanCurrentCheckpoint(planCurrentCheckpoint{SessionID: "new-sess", PlanVersion: 2}); err != nil {
		t.Fatalf("WritePlanCurrentCheckpoint: %v", err)
	}
	cp, err := w.LoadPlanCurrentCheckpoint()
	if err != nil {
		t.Fatalf("LoadPlanCurrentCheckpoint: %v", err)
	}
	if cp == nil || cp.SessionID != "new-sess" || cp.PlanVersion != 2 {
		t.Fatalf("应优先命中新布局 checkpoint: %+v", cp)
	}
}

// A03 final 序号推导：新旧两目录取 max。
func TestPersistFinalArtifacts_FinalSeqAcrossLegacyDir(t *testing.T) {
	snapshot := builtin_tools.StateSnapshot{Status: builtin_tools.TaskStatusCompleted, PlanVersion: 1}

	t.Run("仅旧目录 seq=3 → 下一个 4", func(t *testing.T) {
		root := t.TempDir()
		seedLegacyWorkspaceFixture(t, root)
		if err := os.MkdirAll(filepath.Join(root, "artifacts", "root", "final", "3"), 0o755); err != nil {
			t.Fatal(err)
		}
		w := newTopLevelArtifactWriter(t, root)
		record, err := w.PersistFinalArtifacts(snapshot, "legacy-sess", FinalAssessmentArtifact{SessionID: "legacy-sess"}, "answer")
		if err != nil {
			t.Fatalf("PersistFinalArtifacts: %v", err)
		}
		if record.FinalSeq != 4 {
			t.Fatalf("FinalSeq = %d, want 4（legacy max 3 + 1）", record.FinalSeq)
		}
	})

	t.Run("旧 3 新 5 → 下一个 6", func(t *testing.T) {
		root := t.TempDir()
		seedLegacyWorkspaceFixture(t, root)
		if err := os.MkdirAll(filepath.Join(root, "artifacts", "root", "final", "3"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "artifacts", "final", "5"), 0o755); err != nil {
			t.Fatal(err)
		}
		w := newTopLevelArtifactWriter(t, root)
		record, err := w.PersistFinalArtifacts(snapshot, "legacy-sess", FinalAssessmentArtifact{SessionID: "legacy-sess"}, "answer")
		if err != nil {
			t.Fatalf("PersistFinalArtifacts: %v", err)
		}
		if record.FinalSeq != 6 {
			t.Fatalf("FinalSeq = %d, want 6（max(3,5)+1）", record.FinalSeq)
		}
	})
}

// A04 全新 session：顶层写点落新布局，不再产生 artifacts/root/。
func TestPersistFinalArtifacts_FreshSessionUsesNormalizedLayout(t *testing.T) {
	root := t.TempDir()
	w := newTopLevelArtifactWriter(t, root)

	record, err := w.PersistFinalArtifacts(
		builtin_tools.StateSnapshot{Status: builtin_tools.TaskStatusCompleted, PlanVersion: 1},
		"fresh-sess", FinalAssessmentArtifact{SessionID: "fresh-sess"}, "fresh answer")
	if err != nil {
		t.Fatalf("PersistFinalArtifacts: %v", err)
	}
	if record.FinalSeq != 1 {
		t.Fatalf("FinalSeq = %d, want 1", record.FinalSeq)
	}
	if record.FinalAssessmentFile != "artifacts/final/1/final_assessment.json" {
		t.Fatalf("FinalAssessmentFile = %q（应为归一后新布局）", record.FinalAssessmentFile)
	}
	if _, err := os.Stat(filepath.Join(root, "artifacts", "final", "1", "final_answer.md")); err != nil {
		t.Fatalf("新布局 final answer 未落盘: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "artifacts", "root")); !os.IsNotExist(err) {
		t.Fatalf("全新 session 不应产生 artifacts/root/（err=%v）", err)
	}
	if _, err := os.Stat(filepath.Join(root, "artifacts", "plan", "current.json")); err != nil {
		t.Fatalf("checkpoint 未落新布局: %v", err)
	}
}

// A05 子 namespace 不受归一影响：见既有 TestPersistFinalArtifacts_UsesNamespacedFinalSequence
//（tests/durable_resume_test.go），其路径断言 artifacts/<ns>/final/… 原样通过。

// IT2 首版：旧布局 session 的 final assessment 经读回退可恢复。
func TestLoadLatestFinalAssessment_LegacyLayout(t *testing.T) {
	root := t.TempDir()
	seedLegacyWorkspaceFixture(t, root)
	w := newTopLevelArtifactWriter(t, root)

	state := &builtin_tools.WorkspaceState{SessionID: "legacy-sess", LatestFinalSeq: 1}
	artifact, seq, err := LoadLatestFinalAssessment(w, state, nil)
	if err != nil {
		t.Fatalf("LoadLatestFinalAssessment: %v", err)
	}
	if artifact == nil || seq != 1 {
		t.Fatalf("旧布局 final assessment 读回退未命中: artifact=%v seq=%d", artifact, seq)
	}
}
