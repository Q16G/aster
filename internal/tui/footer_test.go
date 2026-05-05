package tui

import (
	"strings"
	"testing"

	tuicontext "aster/internal/tui/context"
)

func TestFooterViewStaysSingleLineAndTruncates(t *testing.T) {
	var f FooterModel
	f.SetWidth(40)
	f.SetWorkdir("/Users/qinchenkai/projects/sastx/very/long/path")
	f.SetStatus("simple reply path started with a very long status", "", "")

	view := f.View(tuicontext.NewThemeProvider().Get())

	if strings.Contains(view, "\n") {
		t.Fatalf("footer should stay single line, got %q", view)
	}
	if !strings.Contains(view, "…") {
		t.Fatalf("expected truncated footer to contain ellipsis, got %q", view)
	}
}
