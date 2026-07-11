package react

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestPromptPreviewTokens 验证 preview 上限 = usableInputTokens × ratio，线性无 clamp；
// 非法输入退回 defaultContextWindowTokens 兜底。
func TestPromptPreviewTokens(t *testing.T) {
	cases := []struct {
		name string
		uit  int
		want int
	}{
		{"大预算线性放大", 1_000_000, int(1_000_000 * promptPreviewRatio)},
		{"小预算线性收窄", 10_000, int(10_000 * promptPreviewRatio)},
		{"零值退默认可用预算", 0, int(float64(defaultContextWindowTokens-DefaultOutputReserveTokens) * promptPreviewRatio)},
		{"负值退默认可用预算", -1, int(float64(defaultContextWindowTokens-DefaultOutputReserveTokens) * promptPreviewRatio)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := promptPreviewTokens(tc.uit); got != tc.want {
				t.Errorf("promptPreviewTokens(%d) = %d, want %d", tc.uit, got, tc.want)
			}
		})
	}
	// 无 clamp：两倍预算严格得到两倍上限。
	if promptPreviewTokens(200_000) != 2*promptPreviewTokens(100_000) {
		t.Errorf("promptPreviewTokens 应为纯线性，无 floor/ceil clamp")
	}
}

func TestPreviewForPromptWithinLimit(t *testing.T) {
	content := "第一行事实\n第二行事实"
	got := previewForPrompt("  "+content+"\n", "/ws/shared/task_context.md", 1000)
	if got != content {
		t.Errorf("limit 内应原样返回（仅 TrimSpace），got %q", got)
	}
	if isTruncatedForPrompt(got) {
		t.Errorf("未截断内容不应命中截断判定")
	}
}

func TestPreviewForPromptEmptyAndNoLimit(t *testing.T) {
	if got := previewForPrompt("", "/ws/x.md", 100); got != "(文件为空)" {
		t.Errorf("空串应返回占位，got %q", got)
	}
	if got := previewForPrompt("  \n\t ", "/ws/x.md", 100); got != "(文件为空)" {
		t.Errorf("纯空白应返回占位，got %q", got)
	}
	long := strings.Repeat("行内容\n", 5000)
	if got := previewForPrompt(long, "/ws/x.md", 0); got != strings.TrimSpace(long) {
		t.Errorf("limitTokens <= 0 应不截断")
	}
}

// TestPreviewForPromptTruncates 验证超限截断：正文 token ≤ limit、UTF-8 合法、
// 指针文案含绝对路径与 read_file 实名、命中 isTruncatedForPrompt。
func TestPreviewForPromptTruncates(t *testing.T) {
	const absPath = "/ws/shared/open_items.md"
	const limit = 50
	long := strings.Repeat("OI-001 中文账本条目，含多字节字符与证据标注。\n", 500)
	got := previewForPrompt(long, absPath, limit)

	if !isTruncatedForPrompt(got) {
		t.Fatalf("超限内容应命中截断判定")
	}
	if !strings.Contains(got, absPath) {
		t.Errorf("截断提示应含绝对路径 %s", absPath)
	}
	if !strings.Contains(got, "read_file") {
		t.Errorf("截断提示应用 builtin 实名 read_file")
	}
	if !utf8.ValidString(got) {
		t.Errorf("截断结果必须是合法 UTF-8")
	}
	markerAt := strings.Index(got, promptTruncatedMarker)
	if markerAt < 0 {
		t.Fatalf("截断结果应含标记串 %q", promptTruncatedMarker)
	}
	body := strings.TrimSpace(got[:markerAt])
	if body == "" {
		t.Fatalf("limit=50 足够放正文，不应全指针化")
	}
	if n := countTokens(body); n > limit {
		t.Errorf("截断正文 token 数 %d 超出 limit %d", n, limit)
	}
}

func TestPointerOnlyForPrompt(t *testing.T) {
	const absPath = "/ws/shared/task_context.md"
	got := pointerOnlyForPrompt(absPath)
	if !isTruncatedForPrompt(got) {
		t.Errorf("纯指针文案应命中截断判定")
	}
	if !strings.Contains(got, absPath) || !strings.Contains(got, "read_file") {
		t.Errorf("纯指针文案应含绝对路径与 read_file 实名，got %q", got)
	}
}

// TestPreviewForPromptPointerOnlyWhenLimitTiny 验证 limit 连指针文案都放不下时全指针化：
// 不再输出任何正文（显式判据 limit < 指针 token 成本）。
func TestPreviewForPromptPointerOnlyWhenLimitTiny(t *testing.T) {
	const absPath = "/ws/shared/open_items.md"
	long := strings.Repeat("账本条目内容。\n", 200)
	pointerCost := countTokens(pointerOnlyForPrompt(absPath))
	got := previewForPrompt(long, absPath, pointerCost-1)
	if got != pointerOnlyForPrompt(absPath) {
		t.Errorf("limit=%d（< 指针成本 %d）应全指针化，got %q", pointerCost-1, pointerCost, got)
	}
}

// TestTruncateToTokenBudgetSingleLongLine 覆盖无换行单长行：换行对齐分支失效，
// 走纯比例估算 + 10%% 收缩路径，结果仍须 token ≤ limit 且 UTF-8 合法。
func TestTruncateToTokenBudgetSingleLongLine(t *testing.T) {
	long := strings.Repeat("无换行的超长中文单行内容持续堆积", 200)
	for _, limit := range []int{5, 50, 500} {
		got := truncateToTokenBudget(long, limit)
		if n := countTokens(got); n > limit {
			t.Errorf("limit=%d: 结果 token 数 %d 超限", limit, n)
		}
		if !utf8.ValidString(got) {
			t.Errorf("limit=%d: 结果非合法 UTF-8", limit)
		}
		if got == "" {
			t.Errorf("limit=%d: 不应截成空串", limit)
		}
	}
}

// TestPreviewPlannerJournalRealFile 真实文件组合用例：limit 截断 + 路径指针、
// limit=0 不截断、文件缺失返回空串（HAS_PLANNER_JOURNAL gate 语义）。
func TestPreviewPlannerJournalRealFile(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.Join(dir, "workspace", "planner.jsonl")
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat(`{"kind":"step","item":{"id":"s1","step":"真实文件截断用例内容行"}}`+"\n", 300)
	if err := os.WriteFile(absPath, []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	got := previewPlannerJournalForPrompt(dir, 60)
	if !got.Truncated || !strings.Contains(got.Text, absPath) {
		t.Errorf("超限真实文件应截断并含路径指针，got 前 80 字符 %q", got.Text[:min(80, len(got.Text))])
	}
	if got.PointerPath() != absPath {
		t.Errorf("截断字段 PointerPath 应为真相源 %s，got %q", absPath, got.PointerPath())
	}
	if full := previewPlannerJournalForPrompt(dir, 0); full.Text != strings.TrimSpace(long) {
		t.Errorf("limit=0 应返回全文不截断")
	}
	if missing := previewPlannerJournalForPrompt(t.TempDir(), 60); missing.Has() {
		t.Errorf("缺失 journal 应返回空 PreviewField（gate 语义），got %q", missing.Text)
	}
}

func TestTruncateToTokenBudget(t *testing.T) {
	long := strings.Repeat("多字节内容行，用于验证边界。\n", 300)
	for _, limit := range []int{10, 100, 1000} {
		got := truncateToTokenBudget(long, limit)
		if n := countTokens(got); n > limit {
			t.Errorf("limit=%d: 结果 token 数 %d 超限", limit, n)
		}
		if !utf8.ValidString(got) {
			t.Errorf("limit=%d: 结果非合法 UTF-8", limit)
		}
	}
	short := "短内容"
	if got := truncateToTokenBudget(short, 1000); got != short {
		t.Errorf("limit 内应原样返回，got %q", got)
	}
}

// TestPreviewStepFileForPrompt 验证 step 文件 preview 保持空串 gate 语义：
// 空内容返回空串（而非「(文件为空)」占位），非空走统一 preview。
func TestPreviewStepFileForPrompt(t *testing.T) {
	if got := previewStepFileForPrompt("  \n", "/ws/shared/steps/s1.md", 100); got.Has() {
		t.Errorf("空内容应保持空 gate，got %q", got.Text)
	}
	if got := previewStepFileForPrompt("step 结论", "/ws/shared/steps/s1.md", 100); got.Text != "step 结论" {
		t.Errorf("limit 内应原样返回，got %q", got.Text)
	}
}

func TestIsTruncatedForPromptPlaceholders(t *testing.T) {
	for _, s := range []string{"(文件尚不存在)", "(文件为空)", "(共享区不可用)", "正常内容"} {
		if isTruncatedForPrompt(s) {
			t.Errorf("%q 不应命中截断判定", s)
		}
	}
}
