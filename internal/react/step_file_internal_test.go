package react

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aster/internal/workspacefs"
)

func newTestStepFileRuntime(t *testing.T) (WorkspaceRuntime, string) {
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
	abs := workspacefs.New(rt.RootDir(), "").StepFile("s1")
	if abs != filepath.Join(sharedDir, "step_s1.md") {
		t.Fatalf("unexpected step file abs path: %s", abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read scaffold failed: %v", err)
	}
	content := string(data)
	for _, needle := range []string{"# step_s1: 扫描目标目录", "## 未覆盖待补（实时维护）", "## 进展记录", "## 收尾产出"} {
		if !strings.Contains(content, needle) {
			t.Fatalf("scaffold missing %q, got:\n%s", needle, content)
		}
	}

	// 已存在时不覆盖：保护 resume / replan 重入同 step 的既有进展。
	custom := "# step_s1: 扫描目标目录\n\n## 未覆盖待补（实时维护）\n- [x] 已完成项\n"
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
// 防止写入工具中途短暂写空触发 readSharedStepFileForPrompt 回退到 legacy 路径读老内容。
func TestStepFileExists_ZeroBytedIsExist(t *testing.T) {
	l := workspacefs.New(t.TempDir(), "")
	if err := os.MkdirAll(l.SharedDir(), 0o755); err != nil {
		t.Fatalf("mkdir shared dir failed: %v", err)
	}
	abs := l.StepFile("s9")

	if stepFileExists(l, "s9") {
		t.Fatal("stepFileExists should be false before file creation")
	}

	if err := os.WriteFile(abs, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty file failed: %v", err)
	}
	if !stepFileExists(l, "s9") {
		t.Fatal("stepFileExists should be true for a 0-byte file (transient tool write)")
	}

	if err := os.WriteFile(abs, []byte("# step_s9\n"), 0o644); err != nil {
		t.Fatalf("write content failed: %v", err)
	}
	if !stepFileExists(l, "s9") {
		t.Fatal("stepFileExists should be true for a non-empty file")
	}
}

// TestLegacyStepFileExists_RequiresContent 锁定 legacyStepFileExists 仍依赖 size>0
// （旧布局只有有内容才值得 fallback）。
func TestLegacyStepFileExists_RequiresContent(t *testing.T) {
	l := workspacefs.New(t.TempDir(), "")
	legacy := l.LegacyStepFile("s9")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir failed: %v", err)
	}

	if err := os.WriteFile(legacy, []byte{}, 0o644); err != nil {
		t.Fatalf("write empty legacy failed: %v", err)
	}
	if legacyStepFileExists(l, "s9") {
		t.Fatal("legacyStepFileExists should be false for 0-byte legacy file")
	}

	if err := os.WriteFile(legacy, []byte("# legacy step.md\n"), 0o644); err != nil {
		t.Fatalf("write legacy content failed: %v", err)
	}
	if !legacyStepFileExists(l, "s9") {
		t.Fatal("legacyStepFileExists should be true for non-empty legacy file")
	}
}

func TestStepFilePaths(t *testing.T) {
	if got := stepFileRelPath("s2"); got != "shared/step_s2.md" {
		t.Fatalf("stepFileRelPath = %q", got)
	}
	if got := workspacefs.New("", "").StepFile("s2"); got != "" {
		t.Fatalf("StepFile with empty root should be empty, got %q", got)
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

func TestBuildThinkActPrompt_RendersOpenItemsPaths(t *testing.T) {
	manager, err := newDefaultPromptManager()
	if err != nil {
		t.Fatalf("newDefaultPromptManager failed: %v", err)
	}
	parts, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		CurrentStep:         map[string]any{"id": "s1", "step": "x"},
		HasCurrentStep:      true,
		OpenItemsLedgerPath: "/ws/shared/open_items.md",
	})
	if err != nil {
		t.Fatalf("build think_act failed: %v", err)
	}
	if !strings.Contains(parts.User, "/ws/shared/open_items.md") {
		t.Fatalf("think_act user prompt must contain ledger path, got:\n%s", parts.User)
	}
	if !strings.Contains(parts.User, "未闭环账本") {
		t.Fatalf("think_act user prompt must label the ledger pointer, got:\n%s", parts.User)
	}

	// 未注入路径时不渲染指针行。
	without, err := manager.BuildThinkActPrompt(ThinkActPromptInput{
		CurrentStep:    map[string]any{"id": "s1", "step": "x"},
		HasCurrentStep: true,
	})
	if err != nil {
		t.Fatalf("build think_act (no path) failed: %v", err)
	}
	if strings.Contains(without.User, "未闭环账本") {
		t.Fatalf("think_act user prompt must omit ledger line when path absent, got:\n%s", without.User)
	}
	if strings.Contains(without.User, "已闭环归档") {
		t.Fatalf("think_act user prompt must omit archive line when path absent, got:\n%s", without.User)
	}
}

func TestMarkdownSectionLines(t *testing.T) {
	raw := "# 标题\n\n## 输入事实\n- 目标: xx\n\n- 端口: yy\n## 执行中补充\n内容行\n"
	got := markdownSectionLines(raw, "## 输入事实")
	if len(got) != 2 || got[0] != "- 目标: xx" || got[1] != "- 端口: yy" {
		t.Fatalf("unexpected section lines: %#v", got)
	}
	if lines := markdownSectionLines(raw, "## 执行中补充"); len(lines) != 1 || lines[0] != "内容行" {
		t.Fatalf("unexpected tail section lines: %#v", lines)
	}
	if lines := markdownSectionLines(raw, "## 不存在"); lines != nil {
		t.Fatalf("missing heading must return nil, got %#v", lines)
	}
	if lines := markdownSectionLines("", "## 输入事实"); lines != nil {
		t.Fatalf("empty raw must return nil, got %#v", lines)
	}
}

func TestTaskContextInputFactsPresent(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"骨架空节", "# 贯穿全程关键事实\n\n## 输入事实\n\n## 执行中补充\n", false},
		{"有条目", "# 贯穿全程关键事实\n\n## 输入事实\n- 目标: xx\n\n## 执行中补充\n", true},
		{"非条目文本不算", "## 输入事实\n随便一句叙述\n## 执行中补充\n", false},
		{"空条目不算", "## 输入事实\n- \n## 执行中补充\n", false},
		{"文件缺失", "", false},
	}
	for _, tc := range cases {
		if got := taskContextInputFactsPresent(tc.raw); got != tc.want {
			t.Fatalf("%s: want %v got %v", tc.name, tc.want, got)
		}
	}
}

func seedStepTimelineToolCalls(t *testing.T, l workspacefs.Layout, stepID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := appendStepTimeline(l, stepID, &TimelineEvent{
			Type: "tool_call",
			Tool: "bash",
			Key:  fmt.Sprintf("call-%d", i),
		}); err != nil {
			t.Fatalf("appendStepTimeline failed: %v", err)
		}
	}
}

func TestCheckStepFileProgress(t *testing.T) {
	rt, _ := newTestStepFileRuntime(t)
	l := workspacefs.New(rt.RootDir(), "")
	a := &Agent{workspaceRuntime: rt, state: NewStateTracker()}

	// 文件不存在：放行（不把基础设施异常转嫁给模型）。
	if err := a.checkStepFileProgress("s9"); err != nil {
		t.Fatalf("missing step file must pass, got: %v", err)
	}

	// 空骨架 + 执行量未达阈值：短步豁免放行。
	if err := ensureStepFileScaffold(rt, "s9", "示例步骤"); err != nil {
		t.Fatalf("ensureStepFileScaffold failed: %v", err)
	}
	seedStepTimelineToolCalls(t, l, "s9", stepFileGateMinToolCalls-1)
	if err := a.checkStepFileProgress("s9"); err != nil {
		t.Fatalf("low-volume step must be exempt, got: %v", err)
	}

	// 空骨架 + 执行量达阈值：首次拒绝。
	seedStepTimelineToolCalls(t, l, "s9", 1)
	if err := a.checkStepFileProgress("s9"); err == nil {
		t.Fatal("empty scaffold with enough tool calls must be rejected")
	}
	// 仍为空：超过有界拒绝次数后降级放行。
	if err := a.checkStepFileProgress("s9"); err != nil {
		t.Fatalf("gate must degrade-accept after bounded rejections, got: %v", err)
	}
	// 有界拒绝按 step 独立计数：另一个空骨架 step 仍会被拒绝一次。
	if err := ensureStepFileScaffold(rt, "s10", "另一步骤"); err != nil {
		t.Fatalf("ensureStepFileScaffold failed: %v", err)
	}
	seedStepTimelineToolCalls(t, l, "s10", stepFileGateMinToolCalls)
	if err := a.checkStepFileProgress("s10"); err == nil {
		t.Fatal("another empty step must still be rejected once")
	}

	// 进展记录有内容：放行。
	abs := l.StepFile("s9")
	content := "# step_s9: 示例步骤\n\n## 未覆盖待补（实时维护）\n\n## 进展记录\n- 已完成 xx\n\n## 收尾产出\n"
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write step file failed: %v", err)
	}
	if err := a.checkStepFileProgress("s9"); err != nil {
		t.Fatalf("progress-present step file must pass, got: %v", err)
	}

	// runtime 缺位：放行。
	if err := (&Agent{}).checkStepFileProgress("s9"); err != nil {
		t.Fatalf("nil runtime must pass, got: %v", err)
	}
}
