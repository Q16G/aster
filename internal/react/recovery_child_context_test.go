package react

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aster/internal/builtin_tools"
	"aster/internal/react/persistv2"
)

const recoveryTestSessionID = "recovery-sess"

// setupRecoveryParent 构造一个父 workspace：写 child_agents、子 snapshot instruction、shared 产出目录。
// 返回组装好的 Agent（含 workspaceRuntime + workspaceSessionID）。
func setupRecoveryParent(t *testing.T, children map[string]*builtin_tools.WorkspaceChildAgentPointer) *Agent {
	t.Helper()
	parentRoot := t.TempDir()
	runtime, err := newLocalWorkspaceRuntime(recoveryTestSessionID, parentRoot, "root")
	if err != nil {
		t.Fatalf("create parent runtime: %v", err)
	}
	// 默认每个 child 的 ArtifactRootDir 指向 parentRoot/sub_agents/<name>（与生产一致），
	// 保证后续 writeChildInstruction 写入的子 store 与 LoadWorkspaceState 读出的指针一致。
	for name, ptr := range children {
		if ptr != nil && strings.TrimSpace(ptr.ArtifactRootDir) == "" {
			ptr.ArtifactRootDir = filepath.Join(parentRoot, "sub_agents", name)
		}
	}
	state := &builtin_tools.WorkspaceState{
		SessionID:   recoveryTestSessionID,
		ChildAgents: children,
	}
	if err := runtime.SaveWorkspaceState(state); err != nil {
		t.Fatalf("save parent state: %v", err)
	}
	return &Agent{
		workspaceRuntime:   runtime,
		workspaceRootDir:   parentRoot,
		workspaceSessionID: recoveryTestSessionID,
	}
}

// writeChildInstruction 在子 V2 store 写 runtime_state blob，使 input_timeline[0] 可读出 instruction。
func writeChildInstruction(t *testing.T, childRootDir, instruction string) {
	t.Helper()
	store, err := persistv2.Open(childRootDir, recoveryTestSessionID)
	if err != nil {
		t.Fatalf("open child store: %v", err)
	}
	rt := builtin_tools.StateSnapshot{
		InputTimeline: []*builtin_tools.TimelineInput{{Content: instruction}},
	}
	raw, _ := json.Marshal(rt)
	ref, err := store.WriteBlob(raw)
	if err != nil {
		t.Fatalf("write child runtime_state blob: %v", err)
	}
	if err := store.SaveSnapshotAtomic(&persistv2.Snapshot{RuntimeStateBlobRef: ref}); err != nil {
		t.Fatalf("save child snapshot: %v", err)
	}
}

func writeSharedFile(t *testing.T, parentRoot, stepID, name, content string) {
	t.Helper()
	dir := filepath.Join(parentRoot, "shared", stepID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir shared step dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write shared file: %v", err)
	}
}

func TestBuildRecoveryChildContext_Hit(t *testing.T) {
	const parentStep = "audit-sast"
	children := map[string]*builtin_tools.WorkspaceChildAgentPointer{
		"sub-A": {Status: "completed", ParentStepKey: parentStep, LatestFinalFile: "/abs/sub-A/final_assessment.json"},
		"sub-B": {Status: "running", ParentStepKey: parentStep},
		"sub-C": {Status: "failed", ParentStepKey: parentStep},
	}
	agent := setupRecoveryParent(t, children)
	parentRoot := agent.workspaceRootDir

	// sub-B 的派生指令（从子 snapshot 读出）。
	writeChildInstruction(t, children["sub-B"].ArtifactRootDir, "扫描认证模块的越权漏洞")

	// 中断 step 的 shared 产出 + in_progress step 的 shared 产出。
	writeSharedFile(t, parentRoot, parentStep, "secret_detection_results.txt", "SECRET_FULL_ASSESSMENT body")
	writeSharedFile(t, parentRoot, "auto-scan-1", "timeline.jsonl", "{}")

	snapshot := builtin_tools.StateSnapshot{
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "recon-1", Status: builtin_tools.StepOutcomeCompleted},
		},
		Plan: []*builtin_tools.PlanItem{
			{ID: "recon-1", Status: builtin_tools.PlanStepCompleted},
			{ID: "auto-scan-1", Status: builtin_tools.PlanStepInProgress},
		},
	}

	out := agent.buildRecoveryChildContextJSON(snapshot)
	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected non-empty recovery context")
	}

	mustContain := []string{
		"sub-A", "/abs/sub-A/final_assessment.json",
		"sub-B", "扫描认证模块的越权漏洞", "interrupted",
		"sub-C", "failed",
		filepath.Join(parentRoot, "shared", parentStep),
		filepath.Join(parentRoot, "shared", "auto-scan-1"),
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("recovery context missing %q\n--- got ---\n%s", s, out)
		}
	}

	mustNotContain := []string{
		"SECRET_FULL_ASSESSMENT", // 不内联 final_assessment.json 全文
		"transcript_blob_ref",    // 不注入转录 blob 路径
	}
	for _, s := range mustNotContain {
		if strings.Contains(out, s) {
			t.Errorf("recovery context should not contain %q\n--- got ---\n%s", s, out)
		}
	}
}

func TestBuildRecoveryChildContext_GateMiss(t *testing.T) {
	const parentStep = "recon-1"
	children := map[string]*builtin_tools.WorkspaceChildAgentPointer{
		"sub-A": {Status: "completed", ParentStepKey: parentStep, LatestFinalFile: "/abs/sub-A/final.json"},
	}
	agent := setupRecoveryParent(t, children)

	// recon-1 已有「已完成」step_outcome → 已综合，不应注入。
	snapshot := builtin_tools.StateSnapshot{
		StepOutcomes: []*builtin_tools.StepOutcome{
			{StepID: "recon-1", Status: builtin_tools.StepOutcomeCompleted},
		},
	}

	if out := agent.buildRecoveryChildContextJSON(snapshot); strings.TrimSpace(out) != "" {
		t.Fatalf("expected empty recovery context when step already synthesized, got:\n%s", out)
	}
}

func TestMaybeBuildRecoveryChildContext_NotResume(t *testing.T) {
	children := map[string]*builtin_tools.WorkspaceChildAgentPointer{
		"sub-A": {Status: "completed", ParentStepKey: "audit-sast", LatestFinalFile: "/abs/final.json"},
	}
	agent := setupRecoveryParent(t, children)
	agent.resumeChildRecovery = false

	snapshot := builtin_tools.StateSnapshot{}
	if out := agent.maybeBuildRecoveryChildContextJSON(snapshot); out != "" {
		t.Fatalf("expected empty when not a resume turn, got:\n%s", out)
	}
}

func TestMaybeBuildRecoveryChildContext_ClearedAfterUse(t *testing.T) {
	children := map[string]*builtin_tools.WorkspaceChildAgentPointer{
		"sub-A": {Status: "completed", ParentStepKey: "audit-sast", LatestFinalFile: "/abs/final.json"},
	}
	agent := setupRecoveryParent(t, children)
	agent.resumeChildRecovery = true

	// audit-sast 无已完成 step_outcome → 命中。
	snapshot := builtin_tools.StateSnapshot{}

	first := agent.maybeBuildRecoveryChildContextJSON(snapshot)
	if strings.TrimSpace(first) == "" {
		t.Fatalf("expected non-empty on first plan of resume turn")
	}
	if agent.resumeChildRecovery {
		t.Fatalf("expected resumeChildRecovery cleared after use")
	}
	if second := agent.maybeBuildRecoveryChildContextJSON(snapshot); second != "" {
		t.Fatalf("expected empty on second plan (flag consumed), got:\n%s", second)
	}
}

// 运行中的子 Agent 名必须落在 Warnings 而非 IncompleteItems：
// task_planner 原则 1.3 会把 incomplete_items 转成补齐步骤（depends_on 源 step），
// 而此路径 source_step_id 为空、名字实为子 Agent 名，若进 IncompleteItems 会被凭空规划成步骤。
func TestCheckChildAgentsCompletion_RunningGoToWarnings(t *testing.T) {
	children := map[string]*builtin_tools.WorkspaceChildAgentPointer{
		"sub-running-1": {Status: "running", ParentStepKey: "audit-sast"},
		"sub-pending-2": {Status: "", ParentStepKey: "audit-sast"},
		"sub-done-3":    {Status: "completed", ParentStepKey: "audit-sast"},
		"sub-failed-4":  {Status: "failed", ParentStepKey: "audit-sast"},
	}
	agent := setupRecoveryParent(t, children)

	rc := agent.checkChildAgentsCompletion()
	if rc == nil {
		t.Fatalf("expected non-nil ReplanContext when child agents still running")
	}
	if len(rc.IncompleteItems) != 0 {
		t.Errorf("IncompleteItems must be empty, got %v", rc.IncompleteItems)
	}
	got := make(map[string]bool, len(rc.Warnings))
	for _, w := range rc.Warnings {
		got[w] = true
	}
	if !got["sub-running-1"] || !got["sub-pending-2"] {
		t.Errorf("Warnings must contain running/pending child names, got %v", rc.Warnings)
	}
	if got["sub-done-3"] || got["sub-failed-4"] {
		t.Errorf("Warnings must not contain completed/failed child names, got %v", rc.Warnings)
	}
}
