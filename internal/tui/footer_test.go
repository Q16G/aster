package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

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

func TestClassifyStatus(t *testing.T) {
	tests := []struct {
		text     string
		expected StatusCategory
	}{
		{"ready", StatusIdle},
		{"", StatusIdle},
		{"error", StatusError},
		{"failed", StatusError},
		{"calling bash...", StatusTool},
		{"calling semgrep_scan...", StatusTool},
		{"thinking...", StatusRunning},
		{"cancelling...", StatusRunning},
		{"iteration 2/5", StatusIteration},
		{"agent: sub_agent", StatusAgent},
		{"retrying in 3s attempt #2", StatusRetry},
		{"theme: dark", StatusUserAction},
		{"model: gpt-4", StatusUserAction},
		{"new session: abc123", StatusUserAction},
	}

	for _, tt := range tests {
		got := classifyStatus(tt.text)
		if got != tt.expected {
			t.Errorf("classifyStatus(%q) = %d, want %d", tt.text, got, tt.expected)
		}
	}
}

func TestStatusColor(t *testing.T) {
	th := tuicontext.DarkTheme()

	tests := []struct {
		status   string
		expected lipgloss.Color
	}{
		{"ready", th.Success},
		{"error", th.Error},
		{"failed", th.Error},
		{"calling bash...", th.ToolAccent},
		{"thinking...", th.Warning},
		{"iteration 1/3", th.AssistantAccent},
		{"agent: sub", th.AssistantAccent},
		{"retrying in 2s", th.Warning},
		{"theme: nord", th.UserAccent},
	}

	for _, tt := range tests {
		var f FooterModel
		f.statusText = tt.status
		got := f.statusColor(th)
		if got != tt.expected {
			t.Errorf("statusColor for %q = %v, want %v", tt.status, got, tt.expected)
		}
	}
}

func TestModeBadgeRendering(t *testing.T) {
	th := tuicontext.DarkTheme()

	var f FooterModel
	f.SetModeIndicator("yolo")

	badge := f.renderModeBadge(th, false)
	if !strings.Contains(badge, "YOLO") {
		t.Errorf("expected badge to contain YOLO, got %q", badge)
	}

	badgeCompact := f.renderModeBadge(th, true)
	if !strings.Contains(badgeCompact, "Y") {
		t.Errorf("expected compact badge to contain Y, got %q", badgeCompact)
	}
	if strings.Contains(badgeCompact, "YOLO") {
		t.Errorf("compact badge should not contain full YOLO, got %q", badgeCompact)
	}
}

func TestFooterNarrowTerminal(t *testing.T) {
	th := tuicontext.DarkTheme()

	var f FooterModel
	f.SetWidth(30)
	f.SetWorkdir("/very/long/path/to/project")
	f.SetStatus("calling bash...", "⣟", "[chat]")
	f.SetModeIndicator("ai")
	f.SetMCPStatus(3, 2)

	view := f.View(th)
	if strings.Contains(view, "\n") {
		t.Fatalf("narrow footer should stay single line")
	}
}

func TestFooterExtremeNarrow(t *testing.T) {
	th := tuicontext.DarkTheme()

	var f FooterModel
	f.SetWidth(20)
	f.SetWorkdir("/path")
	f.SetStatus("thinking...", "⣟", "[chat]")
	f.SetModeIndicator("manual")

	view := f.View(th)
	if strings.Contains(view, "\n") {
		t.Fatalf("extreme narrow footer should stay single line")
	}
}

func TestFooterWideTerminal(t *testing.T) {
	th := tuicontext.DarkTheme()

	var f FooterModel
	f.SetWidth(100)
	f.SetWorkdir("/Users/qinchenkai/go/sastx/internal/tui")
	f.SetStatus("ready", "", "")
	f.SetModeIndicator("manual")
	f.SetMCPStatus(3, 3)

	view := f.View(th)
	if strings.Contains(view, "\n") {
		t.Fatalf("wide footer should stay single line")
	}
}

func TestFooterMCPDegradedColor(t *testing.T) {
	th := tuicontext.DarkTheme()

	var f FooterModel
	f.SetWidth(80)
	f.SetWorkdir("/path")
	f.SetStatus("ready", "", "")
	f.SetModeIndicator("ai")
	f.SetMCPStatus(3, 2)

	meta := f.renderMeta(th, true, true)
	if !strings.Contains(meta, "MCP:2/3") {
		t.Errorf("expected degraded MCP to show X/Y format, got %q", meta)
	}
}

func TestFooterMCPHealthy(t *testing.T) {
	th := tuicontext.DarkTheme()

	var f FooterModel
	f.SetWidth(80)
	f.SetMCPStatus(3, 3)

	meta := f.renderMeta(th, true, false)
	if !strings.Contains(meta, "MCP:3") {
		t.Errorf("expected healthy MCP to show MCP:N format, got %q", meta)
	}
	if strings.Contains(meta, "MCP:3/3") {
		t.Errorf("healthy MCP should not show X/Y format, got %q", meta)
	}
}

func TestFooterEmptyWidth(t *testing.T) {
	th := tuicontext.DarkTheme()

	var f FooterModel
	f.SetWidth(0)

	view := f.View(th)
	if view != "" {
		t.Errorf("zero width should return empty, got %q", view)
	}
}
