package react

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aster/internal/builtin_tools"
)

func newTestStepFileRuntime(t *testing.T) (builtin_tools.WorkspaceRuntime, string) {
	t.Helper()
	root := t.TempDir()
	r, err := newLocalWorkspaceRuntime("sess-test", root, "")
	if err != nil {
		t.Fatalf("newLocalWorkspaceRuntime: %v", err)
	}
	return r, r.SharedDir()
}

func TestEnsureStepFileScaffold_CreateAndIdempotent(t *testing.T) {
	rt, sharedDir := newTestStepFileRuntime(t)

	if err := ensureStepFileScaffold(rt, "s1", "扫描目标目录"); err != nil {
		t.Fatalf("ensureStepFileScaffold failed: %v", err)
	}
	abs := stepFileAbs(sharedDir, "s1")
	if abs != filepath.Join(sharedDir, "step_s1.md") {
		t.Fatalf("unexpected step file abs path: %s", abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read scaffold failed: %v", err)
	}
	content := string(data)
	for _, needle := range []string{"# step_s1: 扫描目标目录", "## 子步骤清单", "## 进展记录", "## 收尾产出"} {
		if !strings.Contains(content, needle) {
			t.Fatalf("scaffold missing %q, got:\n%s", needle, content)
		}
	}

	// 已存在时不覆盖：保护 resume / replan 重入同 step 的既有进展。
	custom := "# step_s1: 扫描目标目录\n\n## 子步骤清单\n- [x] 已完成项\n"
	if err := os.WriteFile(abs, []byte(custom), 0o644); err != nil {
		t.Fatalf("write custom content failed: %v", err)
	}
	if err := ensureStepFileScaffold(rt, "s1", "扫描目标目录"); err != nil {
		t.Fatalf("ensureStepFileScaffold (existing) failed: %v", err)
	}
	after, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("re-read failed: %v", err)
	}
	if string(after) != custom {
		t.Fatalf("scaffold must not overwrite existing file, got:\n%s", string(after))
	}
}

func TestEnsureStepFileScaffold_EmptyInputsNoop(t *testing.T) {
	if err := ensureStepFileScaffold(nil, "s1", "x"); err != nil {
		t.Fatalf("nil runtime should be a no-op, got: %v", err)
	}
	rt, _ := newTestStepFileRuntime(t)
	if err := ensureStepFileScaffold(rt, "", "x"); err != nil {
		t.Fatalf("empty stepID should be a no-op, got: %v", err)
	}
}

// TestEnsureStepFileScaffold_RootEscapeRejected 验证 WriteFileRel 的根逃逸防护：
// stepID 足够多 "../" 跳出 rootDir 时 WriteFileRel 拒绝写出。stepID 通常受
// NormalizePlanItems 约束，但 scaffold 走 WriteFileRel 而非裸 os.WriteFile，
// 保证未来 plan id 校验放宽时本层仍有兜底。
func TestEnsureStepFileScaffold_RootEscapeRejected(t *testing.T) {
	rt, sharedDir := newTestStepFileRuntime(t)
	// "../../../../escape" → stepFileRelPath = shared/step_../../../../escape.md，
	// filepath.Clean → ../escape.md（跳出 rootDir），resolveAbsPath 拒绝。
	if err := ensureStepFileScaffold(rt, "../../../../escape", "x"); err == nil {
		t.Fatal("expected WriteFileRel to reject step id escaping rootDir")
	}
	// 确认 sharedDir 父目录不会出现非法骨架文件。
	parent := filepath.Dir(filepath.Dir(sharedDir))
	matches, _ := filepath.Glob(filepath.Join(parent, "**", "escape.md"))
	if len(matches) > 0 {
		t.Fatalf("scaffold leaked outside sharedDir: %v", matches)
	}
}

// TestStepFileExists_ZeroBytedIsExist 锁定 stepFileExists 在文件为 0 字节时仍判存在——
// 防止 AI bash 中途短暂写空触发 readSharedStepFileForPrompt 回退到 legacy 路径读老内容。
func TestStepFileExists_ZeroBytedIsExist(t *testing.T) {
	sharedDir := t.TempDir()
	abs := stepFileAbs(sharedDir, "s9")

	if stepFileExists(sharedDir, "s9") {
		t.Fatal("stepFileExists should be false before file creation")
	}

	if err := os.WriteFile(abs, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty file failed: %v", err)
	}
	if !stepFileExists(sharedDir, "s9") {
		t.Fatal("stepFileExists should be true for a 0-byte file (transient AI bash write)")
	}

	if err := os.WriteFile(abs, []byte("# step_s9\n"), 0o644); err != nil {
		t.Fatalf("write content failed: %v", err)
	}
	if !stepFileExists(sharedDir, "s9") {
		t.Fatal("stepFileExists should be true for a non-empty file")
	}
}

// TestLegacyStepFileExists_RequiresContent 锁定 legacyStepFileExists 仍依赖 size>0
// （旧布局只有有内容才值得 fallback）。
func TestLegacyStepFileExists_RequiresContent(t *testing.T) {
	sharedDir := t.TempDir()
	legacyDir := filepath.Join(sharedDir, "s9")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir failed: %v", err)
	}
	legacy := filepath.Join(legacyDir, "step.md")

	if err := os.WriteFile(legacy, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty legacy failed: %v", err)
	}
	if legacyStepFileExists(sharedDir, "s9") {
		t.Fatal("legacyStepFileExists should be false for 0-byte legacy file")
	}

	if err := os.WriteFile(legacy, []byte("# legacy step.md\n"), 0o644); err != nil {
		t.Fatalf("write legacy content failed: %v", err)
	}
	if !legacyStepFileExists(sharedDir, "s9") {
		t.Fatal("legacyStepFileExists should be true for non-empty legacy file")
	}
}

func TestStepFilePaths(t *testing.T) {
	if got := stepFileRelPath("s2"); got != "shared/step_s2.md" {
		t.Fatalf("stepFileRelPath = %q", got)
	}
	if got := stepFileAbs("", "s2"); got != "" {
		t.Fatalf("stepFileAbs with empty sharedDir should be empty, got %q", got)
	}
}

func TestBuildThinkActPrompt_RendersStepFilePath(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		CurrentStep:         map[string]any{"id": "s1", "step": "x"},
		HasCurrentStep:      true,
		CurrentStepFilePath: "/ws/shared/step_s1.md",
	})
	if err != nil {
		t.Fatalf("build think_act failed: %v", err)
	}
	if !strings.Contains(parts.User, "/ws/shared/step_s1.md") {
		t.Fatalf("think_act user prompt must contain injected step file path, got:\n%s", parts.User)
	}
	if !strings.Contains(parts.User, "本 step 过程文件") {
		t.Fatalf("think_act user prompt must label the step file pointer, got:\n%s", parts.User)
	}

	// 未注入路径时不渲染指针行。
	without, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		CurrentStep:    map[string]any{"id": "s1", "step": "x"},
		HasCurrentStep: true,
	})
	if err != nil {
		t.Fatalf("build think_act (no path) failed: %v", err)
	}
	if strings.Contains(without.User, "本 step 过程文件") {
		t.Fatalf("think_act user prompt must omit step file line when path absent, got:\n%s", without.User)
	}
}
