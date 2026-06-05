package tui

import "testing"

// TestCountWrappedRows 校验换行行数计算与 textarea wrap() 一致：
// 关键是"行宽正好为换行宽整数倍"和"词换行"场景要比 ceil(lw/w) 多一行，
// 否则输入框高度算少会顶掉首行。
func TestCountWrappedRows(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		width int
		want  int
	}{
		{"empty line is one row", "", 4, 1},
		{"short fits one row", "ab", 4, 1},
		{"almost full one row", "abc", 4, 1},
		{"exact width wraps to two", "abcd", 4, 2},
		{"two times width wraps to three", "abcdefgh", 4, 3},
		{"word wrap on spaces", "aa bb cc", 5, 3},
		{"cjk exact width wraps", "你好", 4, 2},
		{"cjk under width one row", "你", 4, 1},
		{"long single word over width", "abcdefghij", 4, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countWrappedRows(tc.line, tc.width); got != tc.want {
				t.Fatalf("countWrappedRows(%q, %d) = %d, want %d", tc.line, tc.width, got, tc.want)
			}
		})
	}
}

// TestCountWrappedRows_NonPositiveWidth 宽度非正时退化为 1 行，避免除零或负行数。
func TestCountWrappedRows_NonPositiveWidth(t *testing.T) {
	if got := countWrappedRows("anything", 0); got != 1 {
		t.Fatalf("countWrappedRows with width 0 = %d, want 1", got)
	}
}

// TestDesiredHeight_ClampAndWrap 验证 DesiredHeight 在多逻辑行 + 换行下累加，
// 并受 minInputLines / maxInputLines 夹取。
func TestDesiredHeight_ClampAndWrap(t *testing.T) {
	m := NewInputModel()
	m.SetWidth(10)

	w := m.textarea.Width()
	if w <= 0 {
		t.Fatalf("expected positive textarea width, got %d", w)
	}

	// 空内容取 minInputLines。
	if got := m.DesiredHeight(); got != minInputLines {
		t.Fatalf("empty DesiredHeight = %d, want %d", got, minInputLines)
	}

	// 单逻辑行恰好填满内部宽度，应换行成 2，与 countWrappedRows 一致。
	full := make([]byte, w)
	for i := range full {
		full[i] = 'x'
	}
	m.SetValue(string(full))
	if got, want := m.DesiredHeight(), countWrappedRows(string(full), w); got != want {
		t.Fatalf("full-line DesiredHeight = %d, want %d", got, want)
	}

	// 远超 maxInputLines 的内容应被夹取。
	many := ""
	for i := 0; i < maxInputLines+5; i++ {
		many += "line\n"
	}
	m.SetValue(many)
	if got := m.DesiredHeight(); got != maxInputLines {
		t.Fatalf("many-line DesiredHeight = %d, want %d (clamped)", got, maxInputLines)
	}
}
