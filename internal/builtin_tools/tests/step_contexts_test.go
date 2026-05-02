package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceStepContexts_AppendAndLoad_NormalizesNamespaceAndLimits(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	if err := AppendWorkspaceStepContextRecords(root, []*StepContextRecord{
		{ContextKey: "ctx-1", Namespace: "", StepID: "step-1", PlanVersion: 1, SummaryFile: "shared/step_artifacts/step-1.summary.md", ResultFile: "shared/step_artifacts/step-1.result.json", CreatedAt: now},
		{ContextKey: "ctx-2", Namespace: "agents/msg/call/agent", StepID: "step-2", PlanVersion: 1, CreatedAt: now},
		{ContextKey: "ctx-3", Namespace: "root", StepID: "step-3", PlanVersion: 1, CreatedAt: now},
	}); err != nil {
		t.Fatalf("append step contexts failed: %v", err)
	}

	all, err := LoadWorkspaceStepContextRecords(root, 0)
	if err != nil {
		t.Fatalf("load step contexts failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 records, got %d", len(all))
	}
	if got := NormalizeWorkspaceNamespace(all[0].Namespace); got != "root" {
		t.Fatalf("expected record[0] namespace normalized to root, got %q", got)
	}
	if got, want := all[0].SummaryFile, filepath.ToSlash(filepath.Join(root, "shared", "step_artifacts", "step-1.summary.md")); got != want {
		t.Fatalf("expected record[0] summary_file absolute, want=%q got=%q", want, got)
	}
	if got, want := all[0].ResultFile, filepath.ToSlash(filepath.Join(root, "shared", "step_artifacts", "step-1.result.json")); got != want {
		t.Fatalf("expected record[0] result_file absolute, want=%q got=%q", want, got)
	}

	limited, err := LoadWorkspaceStepContextRecords(root, 2)
	if err != nil {
		t.Fatalf("load step contexts (limit) failed: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("expected 2 limited records, got %d", len(limited))
	}
	if limited[0].ContextKey != "ctx-2" || limited[1].ContextKey != "ctx-3" {
		t.Fatalf("unexpected limited order: %+v", limited)
	}
}

func TestWorkspaceStepContexts_PersistsAbsoluteArtifactPaths(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	if err := AppendWorkspaceStepContextRecords(root, []*StepContextRecord{
		{
			ContextKey:  "ctx-1",
			Namespace:   "",
			StepID:      "step-1",
			PlanVersion: 1,
			SummaryFile: "shared/step_artifacts/step-1.summary.md",
			ResultFile:  "shared/step_artifacts/step-1.result.json",
			CreatedAt:   now,
		},
	}); err != nil {
		t.Fatalf("append step contexts failed: %v", err)
	}
	raw, err := os.ReadFile(WorkspaceStepContextsFileAbs(root))
	if err != nil {
		t.Fatalf("read step contexts file failed: %v", err)
	}
	wantSummary := filepath.ToSlash(filepath.Join(root, "shared", "step_artifacts", "step-1.summary.md"))
	wantResult := filepath.ToSlash(filepath.Join(root, "shared", "step_artifacts", "step-1.result.json"))
	if !containsAll(string(raw), wantSummary, wantResult) {
		t.Fatalf("expected step_contexts.jsonl to persist absolute paths; raw=%s", string(raw))
	}
}

func TestWorkspaceStepContexts_LoadDoesNotDuplicateAbsoluteArtifactPaths(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	if err := AppendWorkspaceStepContextRecords(root, []*StepContextRecord{
		{
			ContextKey:  "ctx-stable",
			Namespace:   "",
			StepID:      "step-1",
			PlanVersion: 1,
			SummaryFile: "shared/step_artifacts/step-1.summary.md",
			ResultFile:  "shared/step_artifacts/step-1.result.json",
			CreatedAt:   now,
		},
	}); err != nil {
		t.Fatalf("append step contexts failed: %v", err)
	}

	firstLoad, err := LoadWorkspaceStepContextRecords(root, 0)
	if err != nil {
		t.Fatalf("first load step contexts failed: %v", err)
	}
	secondLoad, err := LoadWorkspaceStepContextRecords(root, 0)
	if err != nil {
		t.Fatalf("second load step contexts failed: %v", err)
	}
	if len(firstLoad) != 1 || len(secondLoad) != 1 {
		t.Fatalf("expected one record on each load, got first=%d second=%d", len(firstLoad), len(secondLoad))
	}

	wantResult := filepath.ToSlash(filepath.Join(root, "shared", "step_artifacts", "step-1.result.json"))
	if got := firstLoad[0].ResultFile; got != wantResult {
		t.Fatalf("expected first load result_file %q, got %q", wantResult, got)
	}
	if got := secondLoad[0].ResultFile; got != wantResult {
		t.Fatalf("expected second load result_file %q, got %q", wantResult, got)
	}

	if got := WorkspaceArtifactPath(root, firstLoad[0].ResultFile); got != wantResult {
		t.Fatalf("expected resolving absolute result_file to stay stable, want=%q got=%q", wantResult, got)
	}
}

func containsAll(raw string, expected ...string) bool {
	for _, item := range expected {
		if !strings.Contains(raw, item) {
			return false
		}
	}
	return true
}
