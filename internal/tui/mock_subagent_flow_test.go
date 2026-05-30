package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Drives the real Model.handleAgentEvent path with the mock scenario and asserts
// the end-to-end routing: sub-agent think/tool/stream collapse out of the main
// timeline (only the card and root parts remain), while the drill-in transcript
// still contains the full sub-agent event stream.
func TestMockSubAgentFlow_RoutesAndCollapses(t *testing.T) {
	const childCallID = "call_aaa1234"
	const childName = "sub-call_aaa" // sub-<callID[:8]>

	m := NewModel(ModelDeps{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = tm.(Model)

	for _, me := range MockSubAgentScenario("", childName, childCallID) {
		tm, _ := m.Update(AgentEventMsg{Event: me.Event})
		m = tm.(Model)
	}

	// Commit any still-buffered think/stream the way run completion would.
	m.chat.FlushThinking()
	m.flushAllStreamsAndPersist()
	m.chat.refreshContent()

	main := m.chat.fullContent

	// Sub-agent details must NOT leak into the main timeline.
	for _, leak := range []string{
		"先搜索事件发射的位置",         // child thinking
		"EmitterFunc",        // child rg args
		"event_bridge.go:29", // child stream
		"确认根因",               // child thinking 2
	} {
		if strings.Contains(main, leak) {
			t.Errorf("sub-agent detail leaked into main timeline: %q", leak)
		}
	}

	// Root details and the collapsed card (with its description) must be present.
	for _, want := range []string{
		"先看一下仓库结构",        // root thinking
		"我先派一个子 agent",    // root stream
		"探查 internal/tui", // card description
	} {
		if !strings.Contains(main, want) {
			t.Errorf("expected main timeline to contain %q", want)
		}
	}

	// The drill-in transcript must collect the full sub-agent stream.
	transcript, ok := m.chat.RenderAgentTranscript(childCallID, 90)
	if !ok {
		t.Fatal("expected a drill-in transcript for the sub-agent")
	}
	for _, want := range []string{
		"先搜索事件发射的位置", // thinking
		"rg",         // tool name
		"read_file",  // tool name
		"在 event_bridge.go:29 找到了发射器", // stream text
		"确认根因", // thinking 2
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("expected drill-in transcript to contain %q", want)
		}
	}

	// partsForChild must resolve the sub-agent's parts via call_id join.
	if len(m.chat.partsForChild(childCallID)) == 0 {
		t.Fatal("partsForChild returned no parts for the sub-agent")
	}
}

// Reproduces and guards the main-timeline attribution regression: root-agent
// parts carry AgentName="code-audit", and filterMainParts/isRootAgent drop them
// whenever ChatModel.rootAgentName doesn't match. When rootAgentName is left ""
// (the post-newSession state before the fix), the main agent's think/tool/stream
// all vanish even though events flowed. Once rootAgentName is restored to the
// root agent's name, the same parts render.
func TestMockMainAgentFlow_RootAttribution(t *testing.T) {
	const rootName = "code-audit"

	run := func(t *testing.T, chatRootName string) string {
		t.Helper()
		m := NewModel(ModelDeps{})
		tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		m = tm.(Model)
		m.chat.rootAgentName = chatRootName

		for _, me := range MockMainAgentScenario(rootName) {
			tm, _ := m.Update(AgentEventMsg{Event: me.Event})
			m = tm.(Model)
		}
		m.chat.FlushThinking()
		m.flushAllStreamsAndPersist()
		m.chat.refreshContent()
		return m.chat.fullContent
	}

	// Bug state: chat rootAgentName "" != event AgentName "code-audit" -> filtered.
	t.Run("mismatch hides main agent", func(t *testing.T) {
		main := run(t, "")
		for _, gone := range []string{"list_files", "read_file", "分析结论"} {
			if strings.Contains(main, gone) {
				t.Errorf("expected main timeline to drop %q when rootAgentName mismatches, but it was shown", gone)
			}
		}
	})

	// Fixed state: rootAgentName matches the root agent's emitted name -> shown.
	t.Run("match shows main agent", func(t *testing.T) {
		main := run(t, rootName)
		for _, want := range []string{"list_files", "read_file", "分析结论", "规划如何排查"} {
			if !strings.Contains(main, want) {
				t.Errorf("expected main timeline to contain %q when rootAgentName matches", want)
			}
		}
	})
}
