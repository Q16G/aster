package react

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureStepFileScaffold_CreateAndIdempotent(t *testing.T) {
	sharedDir := t.TempDir()

	if err := ensureStepFileScaffold(sharedDir, "s1", "扫描目标目录"); err != nil {
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
	if err := ensureStepFileScaffold(sharedDir, "s1", "扫描目标目录"); err != nil {
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
	if err := ensureStepFileScaffold("", "s1", "x"); err != nil {
		t.Fatalf("empty sharedDir should be a no-op, got: %v", err)
	}
	if err := ensureStepFileScaffold(t.TempDir(), "", "x"); err != nil {
		t.Fatalf("empty stepID should be a no-op, got: %v", err)
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
