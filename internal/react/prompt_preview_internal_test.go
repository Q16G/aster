package react

import (
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
		{"零值退默认窗口", 0, int(float64(defaultContextWindowTokens) * promptPreviewRatio)},
		{"负值退默认窗口", -1, int(float64(defaultContextWindowTokens) * promptPreviewRatio)},
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
	if got := previewStepFileForPrompt("  \n", "/ws/shared/steps/s1.md", 100); got != "" {
		t.Errorf("空内容应保持空串 gate，got %q", got)
	}
	if got := previewStepFileForPrompt("step 结论", "/ws/shared/steps/s1.md", 100); got != "step 结论" {
		t.Errorf("limit 内应原样返回，got %q", got)
	}
}

func TestIsTruncatedForPromptPlaceholders(t *testing.T) {
	for _, s := range []string{"(文件尚不存在)", "(文件为空)", "(共享区不可用)", "正常内容"} {
		if isTruncatedForPrompt(s) {
			t.Errorf("%q 不应命中截断判定", s)
		}
	}
}
