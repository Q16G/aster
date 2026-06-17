package tui

import (
	"fmt"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// buildBenchModel constructs a ChatModel pre-populated with n alternating
// user/text/tool parts (one user every 4 parts). Designed to exercise the
// renderer at representative timeline sizes without depending on streaming.
func buildBenchModel(n int) ChatModel {
	m := NewChatModel()
	m.SetSize(80, 24)
	parts := make([]DisplayPart, 0, n)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		t := base.Add(time.Duration(i) * time.Second)
		switch i % 4 {
		case 0:
			parts = append(parts, DisplayPart{
				Type: PartTypeUser, Time: t,
				User: &UserPart{Content: fmt.Sprintf("question %d", i)},
			})
		case 1:
			parts = append(parts, DisplayPart{
				Type: PartTypeThinking, Time: t,
				Thinking: &ThinkingPart{Content: fmt.Sprintf("thought %d ...", i), GroupID: fmt.Sprintf("g%d", i)},
			})
		case 2:
			parts = append(parts, DisplayPart{
				Type: PartTypeText, Time: t,
				Text: &TextPart{Content: fmt.Sprintf("answer fragment %d", i)},
			})
		case 3:
			parts = append(parts, DisplayPart{
				Type: PartTypeTool, Time: t,
				Tool: &ToolPart{
					Name: "rg", CallID: fmt.Sprintf("call_%d", i),
					Arguments: `"pattern"`, Result: "match.go:42:hit\n",
					State: "completed", Duration: 50 * time.Millisecond,
				},
			})
		}
	}
	m.store.parts = parts
	m.store.RebuildIndex()
	m.refreshContent()
	return m
}

// BenchmarkRender_NParts measures refreshContent under two regimes per N:
//   - "cold": every iteration starts with an empty cache so all N parts miss.
//   - "steady": cache is fully warm and exactly one part is marked dirty per
//     iteration (the common case during streaming or a single tool update).
//
// Numbers should be compared against the pre-Stage-4 baseline: the steady-state
// path is where the cache buys its win and should drop into the < 100µs/op
// range for N=2000 at width 80.
func BenchmarkRender_NParts(b *testing.B) {
	lipgloss.SetColorProfile(termenv.Ascii)

	for _, n := range []int{100, 500, 2000} {
		n := n
		b.Run(fmt.Sprintf("N=%d/cold", n), func(b *testing.B) {
			m := buildBenchModel(n)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m.renderer = NewRenderer()
				for _, p := range m.store.parts {
					m.store.MarkDirty(p.ID)
				}
				m.refreshContent()
			}
		})
		b.Run(fmt.Sprintf("N=%d/steady", n), func(b *testing.B) {
			m := buildBenchModel(n)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if len(m.store.parts) > 0 {
					m.store.MarkDirty(m.store.parts[i%len(m.store.parts)].ID)
				}
				m.refreshContent()
			}
		})
	}
}
