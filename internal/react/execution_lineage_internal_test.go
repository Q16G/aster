package react

import (
	"os"
	"path/filepath"
	"testing"

	"aster/internal/builtin_tools"
)

func setupParentWorkspace(t *testing.T, childName string, parentStepKey string, contextKey string) string {
	t.Helper()
	parentRoot := t.TempDir()

	runtime, err := newLocalWorkspaceRuntime("test-session", parentRoot, "root")
	if err != nil {
		t.Fatalf("create parent runtime: %v", err)
	}

	state := &builtin_tools.WorkspaceState{
		SessionID:          "test-session",
		LatestStepOutcomes: make(map[string]*builtin_tools.WorkspaceStepOutcomePointer),
		ChildAgents:        make(map[string]*builtin_tools.WorkspaceChildAgentPointer),
	}

	if childName != "" {
		state.ChildAgents[childName] = &builtin_tools.WorkspaceChildAgentPointer{
			Status:          "running",
			ParentStepKey:   parentStepKey,
			ArtifactRootDir: filepath.Join(parentRoot, "sub_agents", childName),
		}
	}

	if parentStepKey != "" && contextKey != "" {
		state.LatestStepOutcomes[parentStepKey] = &builtin_tools.WorkspaceStepOutcomePointer{
			StepKey:    parentStepKey,
			ContextKey: contextKey,
		}
	}

	if err := runtime.SaveWorkspaceState(state); err != nil {
		t.Fatalf("save parent state: %v", err)
	}
	return parentRoot
}

func writeStepContextRecord(t *testing.T, rootDir string, rec *builtin_tools.StepContextRecord) {
	t.Helper()
	if err := builtin_tools.AppendWorkspaceStepContextRecords(rootDir, []*builtin_tools.StepContextRecord{rec}); err != nil {
		t.Fatalf("append step context record: %v", err)
	}
}

func TestParentContextKeyFromParentWorkspace_EmptyParentRoot(t *testing.T) {
	agent := &Agent{parentWorkspaceRoot: ""}
	got := agent.parentContextKeyFromParentWorkspace()
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestParentContextKeyFromParentWorkspace_EmptyAgentName(t *testing.T) {
	parentRoot := setupParentWorkspace(t, "", "", "")
	writeStepContextRecord(t, parentRoot, &builtin_tools.StepContextRecord{
		ContextKey:  "root:1:step-1",
		Namespace:   "root",
		StepID:      "step-1",
		PlanVersion: 1,
	})

	agent := &Agent{
		agentName:           "",
		parentWorkspaceRoot: parentRoot,
		workspaceSessionID:  "test-session",
	}
	got := agent.parentContextKeyFromParentWorkspace()
	if got != "root:1:step-1" {
		t.Fatalf("expected fallback to latestRootContextKey, got %q", got)
	}
}

func TestParentContextKeyFromParentWorkspace_FindsExactParentStepContext(t *testing.T) {
	parentRoot := setupParentWorkspace(t, "sub-abc123", "step-2", "root:1:step-2")

	agent := &Agent{
		agentName:           "sub-abc123",
		parentWorkspaceRoot: parentRoot,
		workspaceSessionID:  "test-session",
	}
	got := agent.parentContextKeyFromParentWorkspace()
	if got != "root:1:step-2" {
		t.Fatalf("expected %q, got %q", "root:1:step-2", got)
	}
}

func TestParentContextKeyFromParentWorkspace_ChildNotRegistered(t *testing.T) {
	parentRoot := setupParentWorkspace(t, "sub-other", "step-1", "root:1:step-1")
	writeStepContextRecord(t, parentRoot, &builtin_tools.StepContextRecord{
		ContextKey:  "root:1:step-1",
		Namespace:   "root",
		StepID:      "step-1",
		PlanVersion: 1,
	})

	agent := &Agent{
		agentName:           "sub-not-registered",
		parentWorkspaceRoot: parentRoot,
		workspaceSessionID:  "test-session",
	}
	got := agent.parentContextKeyFromParentWorkspace()
	if got != "root:1:step-1" {
		t.Fatalf("expected fallback to latestRootContextKey %q, got %q", "root:1:step-1", got)
	}
}

func TestParentContextKeyFromParentWorkspace_ParentStepInProgress(t *testing.T) {
	parentRoot := setupParentWorkspace(t, "sub-abc123", "step-3", "")
	writeStepContextRecord(t, parentRoot, &builtin_tools.StepContextRecord{
		ContextKey:  "root:1:step-2",
		Namespace:   "root",
		StepID:      "step-2",
		PlanVersion: 1,
	})

	agent := &Agent{
		agentName:           "sub-abc123",
		parentWorkspaceRoot: parentRoot,
		workspaceSessionID:  "test-session",
	}
	got := agent.parentContextKeyFromParentWorkspace()
	if got != "root:1:step-2" {
		t.Fatalf("expected fallback to latestRootContextKey %q, got %q", "root:1:step-2", got)
	}
}

func TestParentContextKeyFromParentWorkspace_NoParentWorkspaceDir(t *testing.T) {
	agent := &Agent{
		agentName:           "sub-abc123",
		parentWorkspaceRoot: filepath.Join(os.TempDir(), "nonexistent-ws-"+generateRandomString(8)),
		workspaceSessionID:  "test-session",
	}
	got := agent.parentContextKeyFromParentWorkspace()
	if got != "" {
		t.Fatalf("expected empty for nonexistent parent workspace, got %q", got)
	}
}

func TestSelectDirectInheritedContextKeys_ChildInheritsParentContext(t *testing.T) {
	parentRoot := setupParentWorkspace(t, "sub-child1", "step-2", "root:1:step-2")

	agent := &Agent{
		agentName:           "sub-child1",
		parentWorkspaceRoot: parentRoot,
		workspaceSessionID:  "test-session",
		workspaceRootDir:    filepath.Join(parentRoot, "sub_agents", "sub-child1"),
	}

	currentStep := &builtin_tools.PlanItem{ID: "step-1"}
	got := selectDirectInheritedContextKeys(nil, "root", currentStep, agent)

	if len(got) != 1 || got[0] != "root:1:step-2" {
		t.Fatalf("expected [root:1:step-2], got %v", got)
	}
}

func TestSelectDirectInheritedContextKeys_LocalContextPreferred(t *testing.T) {
	parentRoot := setupParentWorkspace(t, "sub-child1", "step-2", "root:1:step-2")
	childRoot := filepath.Join(parentRoot, "sub_agents", "sub-child1")

	agent := &Agent{
		agentName:           "sub-child1",
		parentWorkspaceRoot: parentRoot,
		workspaceSessionID:  "test-session",
		workspaceRootDir:    childRoot,
	}

	localRecord := &builtin_tools.StepContextRecord{
		ContextKey:  "root:1:child-step-1",
		Namespace:   "root",
		StepID:      "child-step-1",
		PlanVersion: 1,
	}

	currentStep := &builtin_tools.PlanItem{ID: "step-2"}
	got := selectDirectInheritedContextKeys([]*builtin_tools.StepContextRecord{localRecord}, "root", currentStep, agent)

	if len(got) != 1 || got[0] != "root:1:child-step-1" {
		t.Fatalf("expected local context preferred [root:1:child-step-1], got %v", got)
	}
}

func TestSelectDirectInheritedContextKeys_NoParentWorkspace(t *testing.T) {
	agent := &Agent{
		agentName:           "top-level",
		parentWorkspaceRoot: "",
		workspaceSessionID:  "test-session",
	}

	currentStep := &builtin_tools.PlanItem{ID: "step-1"}
	got := selectDirectInheritedContextKeys(nil, "root", currentStep, agent)

	if got != nil {
		t.Fatalf("expected nil for top-level agent with no records, got %v", got)
	}
}
