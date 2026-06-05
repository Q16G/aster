package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestExtractSelectedText_CJKEndBoundary 选区末 cell 落在全角字符的首格时，整个
// 字符应被完整复制，而不是被 cellToRuneIndex(endCellCol+1) 整体丢掉。
func TestExtractSelectedText_CJKEndBoundary(t *testing.T) {
	line := "hello 世界" // cells: h0 e1 l2 l3 o4 sp5 世6-7 界8-9
	// 末列 6 = 世 的首格。
	got := ExtractSelectedText([]string{line}, 0, 0, 0, 6)
	if got != "hello 世" {
		t.Fatalf("CJK end boundary dropped char:\n got = %q\nwant = %q", got, "hello 世")
	}

	// 末列 7 = 世 的次格，结果不变（不重复、不丢）。
	got = ExtractSelectedText([]string{line}, 0, 0, 0, 7)
	if got != "hello 世" {
		t.Fatalf("CJK second cell selection wrong:\n got = %q\nwant = %q", got, "hello 世")
	}

	// 末列 8 = 界 的首格，应连界一起。
	got = ExtractSelectedText([]string{line}, 0, 0, 0, 8)
	if got != "hello 世界" {
		t.Fatalf("CJK full selection wrong:\n got = %q\nwant = %q", got, "hello 世界")
	}
}

// TestSelectLine_StripsGutter 三击整行复制与拖选一致地剥掉左边框 gutter。
func TestSelectLine_StripsGutter(t *testing.T) {
	glyph := lipgloss.NormalBorder().Left
	content := "some report content"
	line := glyph + " " + content

	s := &SelectionModel{}
	s.SelectLine(0, []string{line})
	if s.text != content {
		t.Fatalf("SelectLine gutter not stripped:\n got = %q\nwant = %q", s.text, content)
	}

	// 与拖选整行（ExtractSelectedText）结果一致。
	drag := ExtractSelectedText([]string{line}, 0, 0, 0, 9999)
	if drag != s.text {
		t.Fatalf("SelectLine and drag disagree:\n select = %q\n drag   = %q", s.text, drag)
	}
}

// TestStripLeftGutter_PreservesIndentation 仅剥除 glyph 后的单个 padding 空格，
// 正文自身的缩进必须保留。
func TestStripLeftGutter_PreservesIndentation(t *testing.T) {
	glyph := lipgloss.NormalBorder().Left
	indented := "    if err != nil {" // 4 空格缩进
	line := glyph + " " + indented

	got := ExtractSelectedText([]string{line}, 0, 0, 0, 9999)
	if got != indented {
		t.Fatalf("indentation not preserved:\n got = %q\nwant = %q", got, indented)
	}
}
