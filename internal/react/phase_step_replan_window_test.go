package react

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"aster/internal/builtin_tools"
)

func makePlanItem(id, step string, status builtin_tools.PlanStepStatus) *builtin_tools.PlanItem {
	return &builtin_tools.PlanItem{ID: id, Step: step, Status: status}
}

func makeOutcome(stepID, summary string, status builtin_tools.StepOutcomeStatus) *builtin_tools.StepOutcome {
	return &builtin_tools.StepOutcome{
		StepID:       stepID,
		Status:       status,
		ShortSummary: summary,
		KeyFacts:     []string{"k:" + stepID},
	}
}

// TestBuildReviewWindow_BoundaryEmpty: 边界为空（首跑/resume 兜底）时，窗口含全部 completed/failed step。
func TestBuildReviewWindow_BoundaryEmpty(t *testing.T) {
	a := &Agent{}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			makePlanItem("s1", "a", builtin_tools.PlanStepCompleted),
			makePlanItem("s2", "b", builtin_tools.PlanStepCompleted),
			makePlanItem("s3", "c", builtin_tools.PlanStepPending),
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			makeOutcome("s1", "done-1", builtin_tools.StepOutcomeCompleted),
			makeOutcome("s2", "done-2", builtin_tools.StepOutcomeCompleted),
		},
	}
	win := a.buildReviewWindow(snapshot, "", "", nil)
	if win == nil {
		t.Fatalf("expected non-nil window")
	}
	if got := len(win.Cards); got != 2 {
		t.Fatalf("expected 2 cards (s1,s2), got %d", got)
	}
	if win.Cards[0].ID != "s1" || win.Cards[1].ID != "s2" {
		t.Fatalf("cards order wrong: %v", []string{win.Cards[0].ID, win.Cards[1].ID})
	}
	if !win.Cards[len(win.Cards)-1].Latest {
		t.Fatalf("expected Latest=true on last card")
	}
	if win.Cards[0].Latest {
		t.Fatalf("did not expect Latest=true on first card")
	}
	if win.OmittedCount != 0 {
		t.Fatalf("expected OmittedCount=0, got %d", win.OmittedCount)
	}
}

// TestBuildReviewWindow_TopicFilter（Inc2）：topicFilter 非空时只收该 topic 的 step 卡；
// 空时收全部（现有全局行为）；与边界协同——边界后 + 同 topic 才入窗。
func TestBuildReviewWindow_TopicFilter(t *testing.T) {
	a := &Agent{}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			{ID: "a1", TopicID: "topic-a", Step: "a1", Status: builtin_tools.PlanStepCompleted},
			{ID: "b1", TopicID: "topic-b", Step: "b1", Status: builtin_tools.PlanStepCompleted},
			{ID: "a2", TopicID: "topic-a", Step: "a2", Status: builtin_tools.PlanStepCompleted},
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			makeOutcome("a1", "done-a1", builtin_tools.StepOutcomeCompleted),
			makeOutcome("b1", "done-b1", builtin_tools.StepOutcomeCompleted),
			makeOutcome("a2", "done-a2", builtin_tools.StepOutcomeCompleted),
		},
	}
	// 收窄 topic-a：只含 a1、a2（保持 plan 顺序）。
	win := a.buildReviewWindow(snapshot, "", "topic-a", nil)
	if got := len(win.Cards); got != 2 || win.Cards[0].ID != "a1" || win.Cards[1].ID != "a2" {
		ids := make([]string, len(win.Cards))
		for i, c := range win.Cards {
			ids[i] = c.ID
		}
		t.Fatalf("topic-a 应只含 a1,a2，got %v", ids)
	}
	// 空 filter：含全部 3 卡。
	if got := len(a.buildReviewWindow(snapshot, "", "", nil).Cards); got != 3 {
		t.Fatalf("空 filter 应含 3 卡，got %d", got)
	}
	// 边界=a1 + topic-a：a1 及之前不再重审，只含 a2。
	winB := a.buildReviewWindow(snapshot, "a1", "topic-a", nil)
	if got := len(winB.Cards); got != 1 || winB.Cards[0].ID != "a2" {
		ids := make([]string, len(winB.Cards))
		for i, c := range winB.Cards {
			ids[i] = c.ID
		}
		t.Fatalf("边界=a1 + topic-a 应只含 a2，got %v", ids)
	}
}

// TestBuildReviewWindow_BoundaryAdvanced: 边界已推进到 s2 后，窗口只含 s3..s5（s2 不再重审）。
func TestBuildReviewWindow_BoundaryAdvanced(t *testing.T) {
	a := &Agent{}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			makePlanItem("s1", "a", builtin_tools.PlanStepCompleted),
			makePlanItem("s2", "b", builtin_tools.PlanStepCompleted),
			makePlanItem("s3", "c", builtin_tools.PlanStepCompleted),
			makePlanItem("s4", "d", builtin_tools.PlanStepCompleted),
			makePlanItem("s5", "e", builtin_tools.PlanStepCompleted),
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			makeOutcome("s1", "done-1", builtin_tools.StepOutcomeCompleted),
			makeOutcome("s2", "done-2", builtin_tools.StepOutcomeCompleted),
			makeOutcome("s3", "done-3", builtin_tools.StepOutcomeCompleted),
			makeOutcome("s4", "done-4", builtin_tools.StepOutcomeCompleted),
			makeOutcome("s5", "done-5", builtin_tools.StepOutcomeCompleted),
		},
	}
	win := a.buildReviewWindow(snapshot, "s2", "", nil)
	if got := len(win.Cards); got != 3 {
		t.Fatalf("expected 3 cards (s3,s4,s5), got %d", got)
	}
	for i, want := range []string{"s3", "s4", "s5"} {
		if win.Cards[i].ID != want {
			t.Fatalf("card[%d] ID = %q, want %q", i, win.Cards[i].ID, want)
		}
	}
	if !win.Cards[2].Latest {
		t.Fatalf("expected Latest=true on last card (s5)")
	}
}

// TestBuildReviewWindow_PendingExcluded: pending / in_progress step 不进窗口。
func TestBuildReviewWindow_PendingExcluded(t *testing.T) {
	a := &Agent{}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			makePlanItem("s1", "a", builtin_tools.PlanStepCompleted),
			makePlanItem("s2", "b", builtin_tools.PlanStepInProgress),
			makePlanItem("s3", "c", builtin_tools.PlanStepPending),
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			makeOutcome("s1", "done-1", builtin_tools.StepOutcomeCompleted),
		},
	}
	win := a.buildReviewWindow(snapshot, "", "", nil)
	if got := len(win.Cards); got != 1 {
		t.Fatalf("expected 1 card (only s1 completed), got %d", got)
	}
	if win.Cards[0].ID != "s1" {
		t.Fatalf("expected card s1, got %s", win.Cards[0].ID)
	}
}

// TestBuildReviewWindow_FailedIncluded: failed step 也进窗口（区间含失败步以触发 step_error 升级路径的复核）。
func TestBuildReviewWindow_FailedIncluded(t *testing.T) {
	a := &Agent{}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			makePlanItem("s1", "a", builtin_tools.PlanStepCompleted),
			makePlanItem("s2", "b", builtin_tools.PlanStepFailed),
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			makeOutcome("s1", "done-1", builtin_tools.StepOutcomeCompleted),
			makeOutcome("s2", "boom", builtin_tools.StepOutcomeFailed),
		},
	}
	win := a.buildReviewWindow(snapshot, "", "", nil)
	if got := len(win.Cards); got != 2 {
		t.Fatalf("expected 2 cards (s1 completed, s2 failed), got %d", got)
	}
	if win.Cards[1].ID != "s2" || win.Cards[1].Status != string(builtin_tools.StepOutcomeFailed) {
		t.Fatalf("expected last card s2 with failed status, got %s/%s", win.Cards[1].ID, win.Cards[1].Status)
	}
	if !win.Cards[1].Latest {
		t.Fatalf("expected Latest=true on failed last card")
	}
}

// TestBuildReviewWindow_PerBatchCeilingTruncation: 默认 per-batch（K<0）下，批次 > ceiling(32)
// 时截断保最新 ceiling 张并写 OmittedCount，更早 step 由 journal 指针回读。
func TestBuildReviewWindow_PerBatchCeilingTruncation(t *testing.T) {
	overshoot := reviewWindowMaxCardsBatchCeiling + 4 // 36 > ceiling 32
	a := &Agent{}
	var plan []*builtin_tools.PlanItem
	var outcomes []*builtin_tools.StepOutcome
	for i := 1; i <= overshoot; i++ {
		id := stepID(i)
		plan = append(plan, makePlanItem(id, "step "+id, builtin_tools.PlanStepCompleted))
		outcomes = append(outcomes, makeOutcome(id, "done-"+id, builtin_tools.StepOutcomeCompleted))
	}
	snapshot := builtin_tools.StateSnapshot{Plan: plan, StepOutcomes: outcomes}
	win := a.buildReviewWindow(snapshot, "", "", nil)
	if got := len(win.Cards); got != reviewWindowMaxCardsBatchCeiling {
		t.Fatalf("expected %d cards after ceiling truncation, got %d", reviewWindowMaxCardsBatchCeiling, got)
	}
	if win.TotalCards != overshoot {
		t.Fatalf("expected TotalCards=%d, got %d", overshoot, win.TotalCards)
	}
	if win.OmittedCount != overshoot-reviewWindowMaxCardsBatchCeiling {
		t.Fatalf("expected OmittedCount=%d, got %d", overshoot-reviewWindowMaxCardsBatchCeiling, win.OmittedCount)
	}
	// 截断保最新：第一张应是 s5 (36-32+1)
	if win.Cards[0].ID != stepID(overshoot-reviewWindowMaxCardsBatchCeiling+1) {
		t.Fatalf("expected first card id=%s, got %s", stepID(overshoot-reviewWindowMaxCardsBatchCeiling+1), win.Cards[0].ID)
	}
	if win.Cards[len(win.Cards)-1].ID != stepID(overshoot) {
		t.Fatalf("expected last card id=%s, got %s", stepID(overshoot), win.Cards[len(win.Cards)-1].ID)
	}
	if !win.Cards[len(win.Cards)-1].Latest {
		t.Fatalf("expected Latest=true on last card after truncation")
	}
}

// TestBuildReviewWindow_PerBatchCoversWholeBatch: 默认 per-batch 下，批次 <= ceiling 时整批入窗，不截断。
func TestBuildReviewWindow_PerBatchCoversWholeBatch(t *testing.T) {
	const batch = 12                          // <= ceiling 32
	a := &Agent{}
	var plan []*builtin_tools.PlanItem
	var outcomes []*builtin_tools.StepOutcome
	for i := 1; i <= batch; i++ {
		id := stepID(i)
		plan = append(plan, makePlanItem(id, "step "+id, builtin_tools.PlanStepCompleted))
		outcomes = append(outcomes, makeOutcome(id, "done-"+id, builtin_tools.StepOutcomeCompleted))
	}
	snapshot := builtin_tools.StateSnapshot{Plan: plan, StepOutcomes: outcomes}
	win := a.buildReviewWindow(snapshot, "", "", nil)
	if got := len(win.Cards); got != batch {
		t.Fatalf("expected whole batch %d cards (no truncation), got %d", batch, got)
	}
	if win.OmittedCount != 0 {
		t.Fatalf("expected OmittedCount=0, got %d", win.OmittedCount)
	}
	if win.Cards[0].ID != stepID(1) {
		t.Fatalf("expected first card id=%s, got %s", stepID(1), win.Cards[0].ID)
	}
}

// TestBuildReviewWindow_BoundaryStaleFallsBackToFull: 边界 stepID 在 plan 中找不到时回退为「无边界」，窗口含全部 completed。
func TestBuildReviewWindow_BoundaryStaleFallsBackToFull(t *testing.T) {
	a := &Agent{}
	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			makePlanItem("s1", "a", builtin_tools.PlanStepCompleted),
			makePlanItem("s2", "b", builtin_tools.PlanStepCompleted),
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			makeOutcome("s1", "done-1", builtin_tools.StepOutcomeCompleted),
			makeOutcome("s2", "done-2", builtin_tools.StepOutcomeCompleted),
		},
	}
	win := a.buildReviewWindow(snapshot, "stale-id-not-in-plan", "", nil)
	if got := len(win.Cards); got != 2 {
		t.Fatalf("expected 2 cards (fallback to full when boundary not found), got %d", got)
	}
}

// largeChecklist 构造超出内联阈值的覆盖清单（>30 项），强制走落盘路径。
func largeChecklist(prefix string) []builtin_tools.CoverageChecklistItem {
	out := make([]builtin_tools.CoverageChecklistItem, 0, coverageChecklistInlineMaxItems+5)
	for i := 0; i < coverageChecklistInlineMaxItems+5; i++ {
		out = append(out, builtin_tools.CoverageChecklistItem{
			Item:   fmt.Sprintf("%s-item-%d", prefix, i),
			Status: "verified",
		})
	}
	return out
}

// TestBuildReviewWindow_HistoricalCardReusesCoverageFile 验证：
//   - 历史卡（窗口非 latest）已存在 coverage.json → resolveCoverageFile 命中 stat 复用，不重写文件；
//   - latest 卡走 persistCoverageChecklist 强制写盘；
//   - 每卡 coverage_file 字段是**绝对路径**（workspaceRootDir + rel），与 result_file 风格对齐。
func TestBuildReviewWindow_HistoricalCardReusesCoverageFile(t *testing.T) {
	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	a := &Agent{workspaceRuntime: runtime, workspaceRootDir: rootDir}

	// 预先种下历史 step 的 coverage.json，并记录初始 mtime + 内容哨兵。
	historicalRel := filepath.Join("shared", "s1", "coverage.json")
	historicalAbs := filepath.Join(rootDir, historicalRel)
	if err := os.MkdirAll(filepath.Dir(historicalAbs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sentinel := []byte(`[{"item":"sentinel","status":"verified"}]`)
	if err := os.WriteFile(historicalAbs, sentinel, 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	beforeInfo, err := os.Stat(historicalAbs)
	if err != nil {
		t.Fatalf("stat sentinel: %v", err)
	}

	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			makePlanItem("s1", "historical", builtin_tools.PlanStepCompleted),
			makePlanItem("s2", "latest", builtin_tools.PlanStepCompleted),
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{
				StepID:            "s1",
				Status:            builtin_tools.StepOutcomeCompleted,
				ShortSummary:      "historical done",
				CoverageChecklist: largeChecklist("s1"),
			},
			{
				StepID:            "s2",
				Status:            builtin_tools.StepOutcomeCompleted,
				ShortSummary:      "latest done",
				CoverageChecklist: largeChecklist("s2"),
			},
		},
	}

	win := a.buildReviewWindow(snapshot, "", "", runtime)
	if len(win.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(win.Cards))
	}

	// 历史卡（s1）coverage.json 内容 + mtime 不变（resolveCoverageFile 走 stat 命中分支）。
	afterInfo, err := os.Stat(historicalAbs)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Errorf("historical coverage.json mtime changed: before=%v after=%v (resolveCoverageFile should reuse, not rewrite)",
			beforeInfo.ModTime(), afterInfo.ModTime())
	}
	current, err := os.ReadFile(historicalAbs)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(current) != string(sentinel) {
		t.Errorf("historical coverage.json content changed: expected sentinel, got %q", string(current))
	}

	// latest 卡（s2）coverage.json 由 persistCoverageChecklist 写盘存在。
	latestAbs := filepath.Join(rootDir, "shared", "s2", "coverage.json")
	if _, err := os.Stat(latestAbs); err != nil {
		t.Errorf("expected latest coverage.json written, stat err: %v", err)
	}

	// 每卡 coverage_file 字段为绝对路径，且与 workspaceRootDir 同前缀。
	for _, c := range win.Cards {
		if c.CoverageFile == "" {
			t.Errorf("card %s: expected non-empty coverage_file", c.ID)
			continue
		}
		if !filepath.IsAbs(c.CoverageFile) {
			t.Errorf("card %s: coverage_file is not absolute: %s", c.ID, c.CoverageFile)
		}
		expectedAbs := filepath.Join(rootDir, "shared", c.ID, "coverage.json")
		if c.CoverageFile != expectedAbs {
			t.Errorf("card %s: coverage_file=%s, want %s", c.ID, c.CoverageFile, expectedAbs)
		}
	}
}

// TestBuildReviewWindow_HistoricalCardWritesIfMissing 验证：历史卡若 coverage.json 不存在
// （比如本回合是首次升级，历史 step 完成时清单较小未触发持久化，本次清单虽达阈但前几轮没写过），
// resolveCoverageFile 兜底调 persistCoverageChecklist 写一次再返回 rel，不静默丢失。
func TestBuildReviewWindow_HistoricalCardWritesIfMissing(t *testing.T) {
	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	a := &Agent{workspaceRuntime: runtime, workspaceRootDir: rootDir}

	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			makePlanItem("s1", "historical-no-file", builtin_tools.PlanStepCompleted),
			makePlanItem("s2", "latest", builtin_tools.PlanStepCompleted),
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "s1", Status: builtin_tools.StepOutcomeCompleted, CoverageChecklist: largeChecklist("s1")},
			{StepID: "s2", Status: builtin_tools.StepOutcomeCompleted, CoverageChecklist: largeChecklist("s2")},
		},
	}

	win := a.buildReviewWindow(snapshot, "", "", runtime)
	if len(win.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(win.Cards))
	}

	// 历史卡 s1 兜底写盘后 coverage.json 应存在。
	historicalAbs := filepath.Join(rootDir, "shared", "s1", "coverage.json")
	if _, err := os.Stat(historicalAbs); err != nil {
		t.Errorf("historical coverage.json should be written on miss, stat err: %v", err)
	}
}

// TestBuildReviewWindow_InlineCoverageNoPath 验证：清单足够小（内联）时
// coverage_file 字段为空，模型走内联 coverage_checklist 而非指针。
func TestBuildReviewWindow_InlineCoverageNoPath(t *testing.T) {
	rootDir := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	a := &Agent{workspaceRuntime: runtime, workspaceRootDir: rootDir}

	snapshot := builtin_tools.StateSnapshot{
		Plan: []*builtin_tools.PlanItem{
			makePlanItem("s1", "a", builtin_tools.PlanStepCompleted),
		},
		StepOutcomes: []*builtin_tools.StepOutcome{
			{
				StepID: "s1",
				Status: builtin_tools.StepOutcomeCompleted,
				CoverageChecklist: []builtin_tools.CoverageChecklistItem{
					{Item: "small-1", Status: "verified"},
					{Item: "small-2", Status: "uncovered"},
				},
			},
		},
	}
	win := a.buildReviewWindow(snapshot, "", "", runtime)
	if len(win.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(win.Cards))
	}
	if win.Cards[0].CoverageFile != "" {
		t.Errorf("expected empty coverage_file for inline-sized checklist, got %s", win.Cards[0].CoverageFile)
	}
	if len(win.Cards[0].CoverageChecklist) != 2 {
		t.Errorf("expected inline coverage_checklist preserved, got %d items", len(win.Cards[0].CoverageChecklist))
	}
}

// TestReviewWindowMaxCards_FollowsHeartbeatK 验证软上限按 K 与窗口规模联动：
//   - K<0（per-batch，默认）：clamp(total, baseline=8, ceiling=32)。
//   - K=0（per-step）：baseline。
//   - K>0：max(K+3, baseline)。
// TestReviewWindowMaxCards_PerBatch 校验窗口上限为纯 per-batch clamp(total, baseline=8, ceiling=32)——
// heartbeat 已退休，窗口不再随 K 变化。
func TestReviewWindowMaxCards_PerBatch(t *testing.T) {
	t.Run("covers_whole_batch", func(t *testing.T) {
		if got := reviewWindowMaxCards(12); got != 12 {
			t.Fatalf("expected 12 (whole batch), got %d", got)
		}
	})
	t.Run("clamps_up_to_baseline", func(t *testing.T) {
		if got := reviewWindowMaxCards(3); got != reviewWindowMaxCardsBaseline {
			t.Fatalf("expected baseline %d, got %d", reviewWindowMaxCardsBaseline, got)
		}
	})
	t.Run("clamps_down_to_ceiling", func(t *testing.T) {
		if got := reviewWindowMaxCards(100); got != reviewWindowMaxCardsBatchCeiling {
			t.Fatalf("expected ceiling %d, got %d", reviewWindowMaxCardsBatchCeiling, got)
		}
	})
}

func stepID(i int) string {
	return fmt.Sprintf("s%d", i)
}
