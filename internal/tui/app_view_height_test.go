package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestComposeToastView_NeverOverflows 锁定不变式：无论 toast 占几行，叠加 toast 后
// 渲染出的 View 高度都恰好等于终端高度。一旦超高，终端会把顶部聊天行顶出可视区，
// 使绝对鼠标坐标与 HitTest 布局错位（拖选复制起点偏上一行）——本测试守护该根因。
func TestComposeToastView_NeverOverflows(t *testing.T) {
	const (
		width      = 80
		height     = 24
		footerH    = 1
		mainHeight = height - footerH
	)

	mkLines := func(n int) string {
		rows := make([]string, n)
		for i := range rows {
			rows[i] = "x"
		}
		return strings.Join(rows, "\n")
	}

	mainArea := mkLines(mainHeight)
	footerView := mkLines(footerH)

	for _, toastRows := range []int{1, 2, 3, 5} {
		toastView := mkLines(toastRows)
		got := composeToastView(mainArea, toastView, footerView, width, height, mainHeight)
		if h := lipgloss.Height(got); h != height {
			t.Fatalf("toastRows=%d: composed view height = %d, want %d (overflow shifts chat and breaks copy mapping)", toastRows, h, height)
		}
	}
}

// TestComposeToastView_PreservesChatTop main 区顶部（聊天区）必须完整保留，被截断的
// 只能是底部行，这样聊天首行仍在物理第 0 行、HitTest 映射不偏。
func TestComposeToastView_PreservesChatTop(t *testing.T) {
	const (
		width      = 40
		height     = 10
		mainHeight = height - 1
	)
	// main 区每行带唯一标记，便于检查顶部是否保留。
	rows := make([]string, mainHeight)
	for i := range rows {
		rows[i] = "row" + string(rune('A'+i))
	}
	mainArea := strings.Join(rows, "\n")
	footerView := "footer"
	toastView := "toast"

	got := composeToastView(mainArea, toastView, footerView, width, height, mainHeight)
	if !strings.Contains(got, "rowA") {
		t.Fatalf("chat top row was truncated; composed view must keep the top of main area:\n%s", got)
	}
	if lipgloss.Height(got) != height {
		t.Fatalf("composed view height = %d, want %d", lipgloss.Height(got), height)
	}
}
