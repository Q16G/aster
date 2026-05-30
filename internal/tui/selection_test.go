package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestExtractSelectedText_KeepsMarkdownTablePipe(t *testing.T) {
	// ASCII pipe used by Markdown tables must survive — only the box-drawing
	// gutter glyph is decorative.
	glyph := lipgloss.NormalBorder().Left
	content := "| CWE | 漏洞类型 |"
	line := glyph + " " + content

	got := ExtractSelectedText([]string{line}, 0, 0, 0, 9999)
	if got != content {
		t.Errorf("table pipe corrupted:\n got = %q\nwant = %q", got, content)
	}
}

func TestExtractSelectedText_MidLineSelectionUnaffected(t *testing.T) {
	// A selection that starts past the gutter has no leading glyph and must be
	// returned verbatim (no accidental trimming of real content).
	line := "report-1234-security-report.md"
	got := ExtractSelectedText([]string{line}, 0, 0, 0, 9999)
	if got != line {
		t.Errorf("non-gutter line altered:\n got = %q\nwant = %q", got, line)
	}
}
