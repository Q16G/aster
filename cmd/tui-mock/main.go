package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Minimal mock TUI to verify:
//   1. alt screen mode (no frame ghosting)
//   2. thinking dedup (identical flush should not duplicate)

type thinkingPart struct {
	content string
}

type mockModel struct {
	width, height int
	parts         []thinkingPart
	thinkingBuf   strings.Builder
	isThinking    bool
	phase         int
	statusText    string
	log           []string
	sidebarSnap   int
	autoQuit      bool
}

type tickMsg struct{}
type phaseMsg struct{ phase int }

func tick(d time.Duration, phase int) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return phaseMsg{phase: phase}
	})
}

func (m *mockModel) Init() tea.Cmd {
	return tick(time.Second, 1)
}

func (m *mockModel) flushThinking() bool {
	if m.thinkingBuf.Len() == 0 {
		m.isThinking = false
		return false
	}
	content := m.thinkingBuf.String()
	if n := len(m.parts); n > 0 {
		last := m.parts[n-1]
		if last.content == content {
			m.thinkingBuf.Reset()
			m.isThinking = false
			m.log = append(m.log, fmt.Sprintf("[DEDUP] skipped duplicate thinking: %q", truncate(content, 40)))
			return false
		}
	}
	m.parts = append(m.parts, thinkingPart{content: content})
	m.thinkingBuf.Reset()
	m.isThinking = false
	m.log = append(m.log, fmt.Sprintf("[FLUSH] persisted thinking part #%d: %q", len(m.parts), truncate(content, 40)))
	return true
}

func (m *mockModel) appendThinking(delta string) {
	m.thinkingBuf.WriteString(delta)
	m.isThinking = true
}

func (m *mockModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil

	case phaseMsg:
		switch msg.phase {
		case 1:
			m.statusText = "Phase 1: Streaming thinking deltas..."
			m.log = append(m.log, "--- Phase 1: simulate streaming think deltas ---")
			m.appendThinking("Let me analyze ")
			return m, tick(300*time.Millisecond, 2)
		case 2:
			m.appendThinking("the code structure ")
			return m, tick(300*time.Millisecond, 3)
		case 3:
			m.appendThinking("and find the bug.")
			return m, tick(300*time.Millisecond, 4)
		case 4:
			m.statusText = "Phase 2: Flush thinking (first time)..."
			m.log = append(m.log, "--- Phase 2: flush thinking ---")
			m.flushThinking()
			return m, tick(time.Second, 5)
		case 5:
			m.statusText = "Phase 3: Simulate duplicate summary emit..."
			m.log = append(m.log, "--- Phase 3: simulate summary think with SAME content ---")
			m.appendThinking("Let me analyze the code structure and find the bug.")
			return m, tick(500*time.Millisecond, 6)
		case 6:
			m.statusText = "Phase 4: Flush duplicate thinking..."
			m.log = append(m.log, "--- Phase 4: flush duplicate (should be deduped) ---")
			m.flushThinking()
			return m, tick(time.Second, 7)
		case 7:
			m.statusText = "Phase 5: New different thinking..."
			m.log = append(m.log, "--- Phase 5: simulate NEW different thinking ---")
			m.appendThinking("Now I'll check the sidebar rendering logic.")
			return m, tick(500*time.Millisecond, 8)
		case 8:
			m.log = append(m.log, "--- Phase 6: flush different thinking (should persist) ---")
			m.flushThinking()
			return m, tick(time.Second, 9)
		case 9:
			m.statusText = "Phase 7: Sidebar refresh cycle..."
			m.log = append(m.log, "--- Phase 7: rapid sidebar refresh (alt screen test) ---")
			m.sidebarSnap++
			return m, tick(200*time.Millisecond, 10)
		case 10:
			m.sidebarSnap++
			return m, tick(200*time.Millisecond, 11)
		case 11:
			m.sidebarSnap++
			return m, tick(200*time.Millisecond, 12)
		case 12:
			m.sidebarSnap++
			m.statusText = "DONE — press q to quit"
			m.log = append(m.log, "--- DONE ---")
			m.log = append(m.log, fmt.Sprintf("Total thinking parts: %d (expected: 2)", len(m.parts)))
			if len(m.parts) == 2 {
				m.log = append(m.log, "PASS: dedup working correctly")
			} else {
				m.log = append(m.log, fmt.Sprintf("FAIL: expected 2 parts, got %d", len(m.parts)))
			}
			if m.autoQuit {
				return m, tea.Quit
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *mockModel) View() string {
	if m.width == 0 {
		return "initializing..."
	}

	sidebarWidth := 30
	chatWidth := m.width - sidebarWidth - 3

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	dimStyle := lipgloss.NewStyle().Faint(true)
	thinkStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color("8")).
		PaddingLeft(1).
		Foreground(lipgloss.Color("8"))

	// Chat area
	var chatLines []string
	chatLines = append(chatLines, headerStyle.Render("Chat Area"))
	chatLines = append(chatLines, "")

	for i, p := range m.parts {
		chatLines = append(chatLines, thinkStyle.Render(fmt.Sprintf("Thinking #%d: %s", i+1, p.content)))
		chatLines = append(chatLines, "")
	}

	if m.isThinking {
		chatLines = append(chatLines, lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(
			fmt.Sprintf("[ streaming thinking... ] %s", m.thinkingBuf.String()),
		))
		chatLines = append(chatLines, "")
	}

	chatLines = append(chatLines, dimStyle.Render("─── Event Log ───"))
	for _, l := range m.log {
		chatLines = append(chatLines, dimStyle.Render(l))
	}

	chatContent := strings.Join(chatLines, "\n")

	// Sidebar
	var sidebarLines []string
	sidebarLines = append(sidebarLines, headerStyle.Render("Session"))
	sidebarLines = append(sidebarLines, fmt.Sprintf("  Agent: mock-agent"))
	sidebarLines = append(sidebarLines, fmt.Sprintf("  Provider: mock"))
	sidebarLines = append(sidebarLines, fmt.Sprintf("  Model: gpt-mock"))
	sidebarLines = append(sidebarLines, "")
	sidebarLines = append(sidebarLines, headerStyle.Render("Status"))
	sidebarLines = append(sidebarLines, fmt.Sprintf("  Refresh: #%d", m.sidebarSnap))
	sidebarLines = append(sidebarLines, fmt.Sprintf("  Parts: %d", len(m.parts)))

	sidebarContent := strings.Join(sidebarLines, "\n")

	chatBox := lipgloss.NewStyle().Width(chatWidth).Height(m.height - 3).Render(chatContent)
	sidebarBox := lipgloss.NewStyle().
		Width(sidebarWidth).
		Height(m.height - 3).
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color("8")).
		PaddingLeft(1).
		Render(sidebarContent)

	main := lipgloss.JoinHorizontal(lipgloss.Top, chatBox, sidebarBox)

	footer := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("10")).
		Render(fmt.Sprintf("  [%s]  press q to quit", m.statusText))

	return main + "\n" + footer
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func main() {
	headless := len(os.Args) > 1 && os.Args[1] == "--headless"
	m := &mockModel{statusText: "starting...", autoQuit: headless}
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if headless {
		opts = append(opts, tea.WithInput(nil), tea.WithoutRenderer())
	}
	p := tea.NewProgram(m, opts...)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if headless {
		fmt.Println("=== Mock TUI Test Results ===")
		for _, l := range m.log {
			fmt.Println(l)
		}
		fmt.Printf("\nThinking parts: %d\n", len(m.parts))
		for i, p := range m.parts {
			fmt.Printf("  Part #%d: %q\n", i+1, p.content)
		}
		if len(m.parts) == 2 {
			fmt.Println("\nRESULT: PASS")
		} else {
			fmt.Printf("\nRESULT: FAIL (expected 2 parts, got %d)\n", len(m.parts))
			os.Exit(1)
		}
	}
}
