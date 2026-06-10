package react

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

func newMaintenanceTestAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	rootDir := t.TempDir()
	rt, err := newLocalWorkspaceRuntime("s1", rootDir, "root")
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}
	if err := rt.(interface{ EnsureSharedScaffold() error }).EnsureSharedScaffold(); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	return &Agent{workspaceRuntime: rt}, rt.SharedDir()
}

func TestExecuteMaintenanceDirectives_FullCycle(t *testing.T) {
	a, sharedDir := newMaintenanceTestAgent(t)
	ledger := filepath.Join(sharedDir, "open_items.md")

	warnings := a.executeMaintenanceDirectives("step-1", []*builtin_tools.MaintenanceDirective{
		{Type: builtin_tools.MaintenanceDirectiveLedgerAdd, Content: "模块 X 未深测", Evidence: "digest L3"},
		{Type: builtin_tools.MaintenanceDirectiveContextBake, Content: "入口 B 可达", Evidence: "scan 输出"},
	})
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	data, _ := os.ReadFile(ledger)
	content := string(data)
	if !strings.Contains(content, "- [OI-001] 模块 X 未深测（来源: step-1，证据: digest L3）") {
		t.Fatalf("ledger_add missing: %s", content)
	}
	if !strings.Contains(content, "next_id: 2") {
		t.Fatalf("next_id not incremented: %s", content)
	}
	ctxData, _ := os.ReadFile(filepath.Join(sharedDir, "task_context.md"))
	if got := extractMarkdownSection(string(ctxData), taskContextRuntimeAddHeading); !strings.Contains(got, "入口 B 可达（证据: scan 输出）") {
		t.Fatalf("context_bake missing: %s", string(ctxData))
	}

	// 更新批注 + 闭环归档。
	warnings = a.executeMaintenanceDirectives("step-2", []*builtin_tools.MaintenanceDirective{
		{Type: builtin_tools.MaintenanceDirectiveLedgerUpdate, Target: "OI-001", Content: "已定位触发点"},
		{Type: builtin_tools.MaintenanceDirectiveArchiveItem, Target: "OI-001", Evidence: "复测通过"},
	})
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	data, _ = os.ReadFile(ledger)
	if strings.Contains(string(data), "OI-001") {
		t.Fatalf("archived item should leave unresolved section: %s", string(data))
	}
	archive, _ := os.ReadFile(filepath.Join(sharedDir, "open_items_archive.md"))
	if !strings.Contains(string(archive), "OI-001") || !strings.Contains(string(archive), "闭环证据: 复测通过") || !strings.Contains(string(archive), "（更新: 已定位触发点）") {
		t.Fatalf("archive missing item or evidence: %s", string(archive))
	}
}

func TestExecuteMaintenanceDirectives_WarningsNotBlocking(t *testing.T) {
	a, _ := newMaintenanceTestAgent(t)
	warnings := a.executeMaintenanceDirectives("step-1", []*builtin_tools.MaintenanceDirective{
		{Type: builtin_tools.MaintenanceDirectiveArchiveItem, Target: "OI-999"},
		{Type: builtin_tools.MaintenanceDirectiveMergeStaging, Content: "3 条子 agent 条目待归并"},
		{Type: "bogus"},
		{Type: builtin_tools.MaintenanceDirectiveLedgerAdd, Content: "正常条目"},
	})
	if len(warnings) != 3 {
		t.Fatalf("expected 3 warnings (missing item / merge deferred / unknown type), got %v", warnings)
	}
	// 失败不阻塞：合法指令仍被执行。
	data, _ := os.ReadFile(filepath.Join(a.workspaceRuntime.SharedDir(), "open_items.md"))
	if !strings.Contains(string(data), "正常条目") {
		t.Fatalf("valid directive should still execute: %s", string(data))
	}
}

func TestAllocateLedgerID_RebuildsCounterFromExistingIDs(t *testing.T) {
	dir := t.TempDir()
	// 模型覆盖写丢掉 next_id 行：从既有 [OI-7] 推断 max+1。
	path := filepath.Join(dir, "open_items.md")
	if err := os.WriteFile(path, []byte("## 未解决\n- [OI-007] 旧条目\n\n## 不可解局限\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	id, err := allocateLedgerID(path)
	if err != nil {
		t.Fatalf("allocate: %v", err)
	}
	if id != "OI-008" {
		t.Fatalf("expected OI-008, got %s", id)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "next_id: 9") {
		t.Fatalf("counter not rebuilt: %s", string(data))
	}
}
