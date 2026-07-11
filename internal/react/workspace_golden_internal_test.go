package react

// workspace golden 基线（M0）：为 workspace 存储收口重构提供等价性安全网。
// 三件套：
//  1. TestWorkspaceGoldenTree —— 固定输入驱动全部 workspace 写点，产出目录树快照
//     （rel 路径 -> sha256），与 testdata/workspace_golden.json 逐字节比对；
//     迁移各步（M3/M5/M6）之后必须保持不变。
//  2. TestLegacyHelperPathSnapshot —— 现存路径 helper 在固定输入下的输出快照，
//     作为 workspacefs.Layout 新旧等价测试（L08）的期望值锚点；helper 删除时随之删除。
//  3. seedLegacyWorkspaceFixture —— 旧布局 workspace 预置器，供 resume 读回退
//     测试（A01-A03/IT2）复用。
//
// 快照更新：go test ./internal/react/ -run 'WorkspaceGoldenTree|LegacyHelperPathSnapshot' -update-workspace-golden

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"aster/internal/workspacefs"
)

var updateWorkspaceGolden = flag.Bool("update-workspace-golden", false, "重新生成 workspace golden 基线快照")

var goldenFixedTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// goldenStubClient 满足 ai.ChatClient；golden 构建不应触发任何模型调用。
type goldenStubClient struct{}

func (goldenStubClient) Chat(context.Context, *ai.MsgInfo, ...*ai.FunctionTool) (string, error) {
	return "", fmt.Errorf("golden stub client: unexpected model call")
}

func (goldenStubClient) ChatEx(context.Context, []*ai.MsgInfo, ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, fmt.Errorf("golden stub client: unexpected model call")
}

func (goldenStubClient) ChatText(context.Context, string, ...*ai.FunctionTool) (string, error) {
	return "", fmt.Errorf("golden stub client: unexpected model call")
}

// buildGoldenWorkspace 用固定输入驱动全部 workspace 写点函数（迁移目标散点），
// 覆盖：scaffold、step 过程文件、timeline、coverage、plan/final artifacts、
// state.json、references、step_contexts、planner journal、tool-output、async_result。
func buildGoldenWorkspace(t *testing.T, root string) {
	t.Helper()

	rt, err := newLocalWorkspaceRuntime("golden-sess", root, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	seeder, ok := rt.(interface{ EnsureSharedScaffold() error })
	if !ok {
		t.Fatalf("runtime 未实现 EnsureSharedScaffold")
	}
	if err := seeder.EnsureSharedScaffold(); err != nil {
		t.Fatalf("EnsureSharedScaffold: %v", err)
	}

	// step 过程文件骨架 + 时间线
	if err := ensureStepFileScaffold(rt, "p1-s1", "golden step one"); err != nil {
		t.Fatalf("ensureStepFileScaffold: %v", err)
	}
	events := []*TimelineEvent{
		{TS: goldenFixedTime, Type: "tool_call", Key: "call-1", Tool: "rg", ArgsDigest: "pattern=foo", Result: "3 matches", ResultDigest: "3 matches", DurationMS: 12},
		{TS: goldenFixedTime, Type: "tool_call", Key: "call-2", Tool: "read_file", ArgsDigest: "path=/tmp/x", Error: "file not found"},
	}
	for _, ev := range events {
		if err := appendStepTimeline(rt, "p1-s1", ev); err != nil {
			t.Fatalf("appendStepTimeline: %v", err)
		}
	}

	// coverage 超阈值落盘（经 Agent 方法，即 M6 迁移目标）
	agent, err := NewReActAgent("golden-agent", goldenStubClient{}, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("NewReActAgent: %v", err)
	}
	agent.workspaceRuntime = rt
	items := make([]builtin_tools.CoverageChecklistItem, 0, coverageChecklistInlineMaxItems+5)
	for i := 0; i < coverageChecklistInlineMaxItems+5; i++ {
		items = append(items, builtin_tools.CoverageChecklistItem{
			Item:     fmt.Sprintf("golden coverage item %02d", i),
			Status:   "verified",
			Evidence: "golden evidence",
		})
	}
	if rel := agent.persistCoverageChecklist("p1-s1", &builtin_tools.StepOutcome{CoverageChecklist: items}); rel == "" {
		t.Fatalf("persistCoverageChecklist: expected coverage.json to be persisted")
	}

	// plan / final artifacts
	w, err := newArtifactWriter(rt)
	if err != nil {
		t.Fatalf("newArtifactWriter: %v", err)
	}
	checkpoint := planCurrentCheckpoint{
		SessionID:     "golden-sess",
		PlanVersion:   1,
		CurrentStepID: "p1-s1",
		Status:        builtin_tools.TaskStatusRunning,
		UpdatedAt:     goldenFixedTime,
		Explanation:   "golden checkpoint",
	}
	if err := w.WritePlanCurrentCheckpoint(checkpoint); err != nil {
		t.Fatalf("WritePlanCurrentCheckpoint: %v", err)
	}
	if err := w.writePlanHistoryCheckpoint(checkpoint); err != nil {
		t.Fatalf("writePlanHistoryCheckpoint: %v", err)
	}
	if err := w.writeFileRel(w.finalAnswerFileRel(1), []byte("# golden final answer\n")); err != nil {
		t.Fatalf("write final answer: %v", err)
	}
	if err := w.writeFileRel(w.finalAssessmentFileRel(1), []byte("{\n  \"status\": \"done\"\n}\n")); err != nil {
		t.Fatalf("write final assessment: %v", err)
	}

	// workspace 元数据：state / references / step_contexts / planner journal
	if err := rt.SaveWorkspaceState(&builtin_tools.WorkspaceState{
		SessionID:          "golden-sess",
		Status:             builtin_tools.TaskStatusRunning,
		CurrentPlanVersion: 1,
		CurrentStepKey:     "p1-s1#1",
		LatestFinalSeq:     1,
	}); err != nil {
		t.Fatalf("SaveWorkspaceState: %v", err)
	}
	if err := rt.AppendWorkspaceReferences([]*builtin_tools.WorkspaceReferenceRecord{
		{RefID: "ref-1", Kind: "url", Title: "golden reference", URI: "https://example.com/golden", StepKey: "p1-s1#1", CreatedAt: goldenFixedTime},
	}); err != nil {
		t.Fatalf("AppendWorkspaceReferences: %v", err)
	}
	if err := rt.AppendStepContextRecords([]*builtin_tools.StepContextRecord{
		{ContextKey: "p1-s1#1", StepID: "p1-s1", StepKey: "p1-s1#1", PlanVersion: 1, ShortSummary: "golden summary", KeyFacts: []string{"fact-1"}, CreatedAt: goldenFixedTime},
	}); err != nil {
		t.Fatalf("AppendStepContextRecords: %v", err)
	}
	if err := AppendPlannerJournalRecords(root, []*builtin_tools.PlannerJournalRecord{
		{Kind: builtin_tools.PlannerJournalKindPlan, PlanVersion: 1, Item: &builtin_tools.PlanItem{ID: "p1-s1", Step: "golden step one"}, CreatedAt: goldenFixedTime},
		{Kind: builtin_tools.PlannerJournalKindPhase, PlanVersion: 1, Phase: &builtin_tools.PlanPhase{ID: "ph1", Name: "golden phase"}, CreatedAt: goldenFixedTime},
	}); err != nil {
		t.Fatalf("AppendPlannerJournalRecords: %v", err)
	}

	// tool-output 超长输出外置
	if _, err := truncateToolOutput(strings.Repeat("golden tool output line\n", 3000), root, "tool-output"); err != nil {
		t.Fatalf("truncateToolOutput: %v", err)
	}

	// async agent 结果文件
	if got := writeAsyncResultFile(root, &AsyncAgentNotification{
		AgentID: "golden-child",
		Status:  "completed",
		Result:  &builtin_tools.RunResult{Success: true, Result: "golden child done"},
	}); got == "" {
		t.Fatalf("writeAsyncResultFile: expected result file path")
	}
}

// snapshotWorkspaceTree 产出 rel(slash) -> sha256 的树快照。
// 归一化规则：
//   - tool-output/ 下文件名含时间戳+uuid，按内容哈希排序重命名为 tool-output/output-<i>.txt；
//   - workspace/planner.jsonl 每行的 created_at 置零后再哈希（重写时内部盖 time.Now()）。
func snapshotWorkspaceTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := make(map[string]string)
	var toolOutputs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if rel == "workspace/planner.jsonl" {
			data = normalizePlannerJournal(t, data)
		}
		sum := sha256.Sum256(data)
		digest := hex.EncodeToString(sum[:])
		if strings.HasPrefix(rel, "tool-output/") {
			toolOutputs = append(toolOutputs, digest)
			return nil
		}
		snap[rel] = digest
		return nil
	})
	if err != nil {
		t.Fatalf("walk workspace tree: %v", err)
	}
	sort.Strings(toolOutputs)
	for i, digest := range toolOutputs {
		snap[fmt.Sprintf("tool-output/output-%d.txt", i)] = digest
	}
	return snap
}

func normalizePlannerJournal(t *testing.T, data []byte) []byte {
	t.Helper()
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("planner.jsonl 行不是合法 JSON: %v (%s)", err, line)
		}
		delete(row, "created_at")
		normalized, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("re-marshal planner.jsonl 行失败: %v", err)
		}
		out = append(out, string(normalized))
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

func goldenTestdataPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func loadOrUpdateGolden(t *testing.T, name string, got map[string]string) {
	t.Helper()
	path := goldenTestdataPath(t, name)
	if *updateWorkspaceGolden {
		data, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatalf("marshal golden: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		t.Logf("golden 已更新: %s（%d 条）", path, len(got))
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 golden 失败（首次生成请加 -update-workspace-golden）: %v", err)
	}
	var want map[string]string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("解析 golden %s: %v", path, err)
	}
	assertSnapshotEqual(t, want, got)
}

func assertSnapshotEqual(t *testing.T, want, got map[string]string) {
	t.Helper()
	for key, wantDigest := range want {
		gotDigest, ok := got[key]
		if !ok {
			t.Errorf("golden 路径缺失: %s", key)
			continue
		}
		if gotDigest != wantDigest {
			t.Errorf("golden 内容漂移: %s\n  want %s\n  got  %s", key, wantDigest, gotDigest)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("golden 之外的新路径: %s", key)
		}
	}
}

func TestWorkspaceGoldenTree(t *testing.T) {
	rootA := t.TempDir()
	buildGoldenWorkspace(t, rootA)
	snapA := snapshotWorkspaceTree(t, rootA)

	// 自证 harness 确定性：同一序列第二次构建必须逐字节一致。
	rootB := t.TempDir()
	buildGoldenWorkspace(t, rootB)
	snapB := snapshotWorkspaceTree(t, rootB)
	assertSnapshotEqual(t, snapA, snapB)
	if t.Failed() {
		t.Fatalf("golden harness 自身不确定，先修 harness 再谈基线")
	}

	loadOrUpdateGolden(t, "workspace_golden.json", snapA)
}

// TestLegacyHelperPathSnapshot 固化现存路径 helper 的输出，作为 M1 L08
// 新旧等价测试的期望值。helper 全部删除（M3/M5）后本测试与 testdata 一并删除。
func TestLegacyHelperPathSnapshot(t *testing.T) {
	const root = "/ws-root"
	// M3b 起纯公式 helper（PlannerJournalFileAbs/StepContextsFileAbs/ArtifactsRootRel）
	// 已删除，其锚定职责由 workspacefs_equivalence_internal_test.go 的字面断言承接；
	// 此处仅保留仍存活的语义 helper 快照。
	paths := map[string]string{
		"builtin.artifact_path": builtin_tools.WorkspaceArtifactPath(root, "notes/x.md"),
		"react.step_file_abs":   workspacefs.New("/ws-root", "").StepFile("p1-s1"),
	}
	for _, ns := range []string{"root", "sub-a"} {
		artifactPath, absPath, err := builtin_tools.WorkspaceArtifactWritePath(root, ns, "plan/current.json")
		if err != nil {
			t.Fatalf("WorkspaceArtifactWritePath(%s): %v", ns, err)
		}
		paths["builtin.artifact_write_path."+ns+".rel"] = artifactPath
		paths["builtin.artifact_write_path."+ns+".abs"] = absPath
	}

	// artifactWriter 的 rel 方法族。M2 起 writer 已归一（顶层/root → artifacts/），
	// 此处仅快照子 namespace（新旧规则一致）；顶层归一的有意差异与旧写点锚点由
	// TestLayoutIntentionalNamespaceDivergence（L09）以字面断言固化。
	for _, ns := range []string{"sub-a"} {
		rt, err := newLocalWorkspaceRuntime("legacy-snap", t.TempDir(), ns)
		if err != nil {
			t.Fatalf("newLocalWorkspaceRuntime(%s): %v", ns, err)
		}
		w, err := newArtifactWriter(rt)
		if err != nil {
			t.Fatalf("newArtifactWriter(%s): %v", ns, err)
		}
		prefix := "react.artifact_writer." + ns + "."
		paths[prefix+"artifacts_root_rel"] = w.artifactsRootRel()
		paths[prefix+"plan_current_rel"] = w.planCurrentFileRel()
		paths[prefix+"plan_history_rel"] = w.planHistoryFileRel(2)
		paths[prefix+"final_dir_rel"] = w.finalDirRel(3)
		paths[prefix+"final_answer_rel"] = w.finalAnswerFileRel(3)
		paths[prefix+"final_assessment_rel"] = w.finalAssessmentFileRel(3)
	}

	loadOrUpdateGolden(t, "legacy_helper_paths.json", paths)
}

// seedLegacyWorkspaceFixture 预置「旧布局」workspace，供 resume 读回退测试复用：
//   - artifacts/root/…（namespace 未归一时代的顶层 checkpoint/final 写点）
//   - shared/<stepID>/step.md（老 step 过程文件布局）
//   - shared/<stepID>/timeline.jsonl 与 workspace/state.json
func seedLegacyWorkspaceFixture(t *testing.T, root string) {
	t.Helper()
	write := func(rel, content string) {
		t.Helper()
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("fixture mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("fixture write %s: %v", rel, err)
		}
	}
	write("artifacts/root/plan/current.json", `{
  "session_id": "legacy-sess",
  "plan_version": 1,
  "current_step_id": "p1-s1",
  "status": "running",
  "explanation": "legacy fixture checkpoint"
}
`)
	write("artifacts/root/plan/history/1.json", `{
  "session_id": "legacy-sess",
  "plan_version": 1
}
`)
	write("artifacts/root/final/1/final_answer.md", "# legacy final answer\n")
	write("artifacts/root/final/1/final_assessment.json", `{"status":"done"}`+"\n")
	write("shared/p1-s1/step.md", "# Step p1-s1 (legacy layout)\n\n## 进展\n- legacy progress\n")
	write("shared/p1-s1/timeline.jsonl", `{"ts":"2026-01-02T03:04:05Z","type":"tool_call","key":"legacy-1","tool":"rg","result":"ok"}`+"\n")
	write("shared/task_context.md", "# Task Context (legacy)\n")
	write("shared/open_items.md", "# Open Items (legacy)\n")
	write("workspace/state.json", `{
  "session_id": "legacy-sess",
  "current_plan_version": 1,
  "latest_final_seq": 1
}
`)
}

func TestLegacyWorkspaceFixture_Smoke(t *testing.T) {
	root := t.TempDir()
	seedLegacyWorkspaceFixture(t, root)

	for _, rel := range []string{
		"artifacts/root/plan/current.json",
		"artifacts/root/final/1/final_answer.md",
		"shared/p1-s1/step.md",
		"workspace/state.json",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("fixture 缺文件 %s: %v", rel, err)
		}
	}

	rt, err := newLocalWorkspaceRuntime("legacy-sess", root, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	state, err := rt.LoadWorkspaceState()
	if err != nil {
		t.Fatalf("LoadWorkspaceState: %v", err)
	}
	if state == nil || state.SessionID != "legacy-sess" || state.LatestFinalSeq != 1 {
		t.Fatalf("fixture state 解析异常: %+v", state)
	}
}
