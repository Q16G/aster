package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// fixtureBaseTime is the deterministic anchor for every part Time in golden
// fixtures. Tests must never call time.Now(); the renderer reads Time only
// inside lookup helpers that golden fixtures do not exercise, but we set it
// explicitly so that any future renderer change that prints timestamps is
// caught by the golden diff rather than producing flaky output.
var fixtureBaseTime = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func fixtureTime(offsetSeconds int) time.Time {
	return fixtureBaseTime.Add(time.Duration(offsetSeconds) * time.Second)
}

func fixtureEmpty() ChatModel {
	m := NewChatModel()
	m.SetSize(80, 24)
	return m
}

func fixtureSingleUser() ChatModel {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.parts = []DisplayPart{
		{Type: PartTypeUser, Time: fixtureTime(0), User: &UserPart{Content: "Hello, agent."}},
	}
	m.refreshContent()
	return m
}

func fixtureUserAssistantTool() ChatModel {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.parts = []DisplayPart{
		{Type: PartTypeUser, Time: fixtureTime(0), User: &UserPart{Content: "List files in current dir."}},
		{Type: PartTypeThinking, Time: fixtureTime(1), Thinking: &ThinkingPart{
			Content: "I'll call list_files to enumerate.", GroupID: "g1",
		}},
		{Type: PartTypeText, Time: fixtureTime(2), Text: &TextPart{Content: "Listing files now."}},
		{Type: PartTypeTool, Time: fixtureTime(3), Tool: &ToolPart{
			Name: "list_files", CallID: "call_aaa",
			Arguments: `{"path":"."}`,
			Result:    "main.go\nREADME.md\ngo.mod\n",
			State:     "completed",
			Duration:  250 * time.Millisecond,
		}},
	}
	m.refreshContent()
	return m
}

func fixtureThinkingStream() ChatModel {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.parts = []DisplayPart{
		{Type: PartTypeUser, Time: fixtureTime(0), User: &UserPart{Content: "Think out loud."}},
		{Type: PartTypeThinking, Time: fixtureTime(1), Thinking: &ThinkingPart{
			Content: "Step 1: clarify request.\nStep 2: outline.\nStep 3: respond.",
			GroupID: "g1",
		}},
		{Type: PartTypeThinking, Time: fixtureTime(2), Thinking: &ThinkingPart{
			Content: "On reflection, simpler approach is enough.",
			GroupID: "g2",
		}},
	}
	m.refreshContent()
	return m
}

func fixtureMultiSubAgent() ChatModel {
	m := NewChatModel()
	m.rootAgentName = "root"
	m.SetSize(100, 30)
	m.agentSpawnByCallID["call_aaa1234"] = agentSpawnInfo{CallID: "call_aaa1234", SubScheme: true}
	m.agentSpawnByCallID["call_bbb9999"] = agentSpawnInfo{CallID: "call_bbb9999", SubScheme: true}
	m.parts = []DisplayPart{
		{Type: PartTypeUser, Time: fixtureTime(0), User: &UserPart{Content: "Split into two sub-tasks."}},
		{Type: PartTypeSubAgent, Time: fixtureTime(1), SubAgent: &SubAgentPart{
			AgentName:   "sub_agent",
			CallID:      "call_aaa1234",
			Status:      "completed",
			Description: "scan repo for TODOs",
			Summary:     "Found 3 TODOs.",
			Duration:    5 * time.Second,
		}},
		{Type: PartTypeTool, Time: fixtureTime(2), Tool: &ToolPart{
			Name:      "rg",
			CallID:    "call_aaa_tool1",
			AgentName: "sub-call_aaa",
			Arguments: `"TODO"`,
			Result:    "main.go:10:TODO: refactor\n",
			State:     "completed",
			Duration:  80 * time.Millisecond,
		}},
		{Type: PartTypeSubAgent, Time: fixtureTime(3), SubAgent: &SubAgentPart{
			AgentName:   "sub_agent",
			CallID:      "call_bbb9999",
			Status:      "running",
			Description: "summarize README",
		}},
		{Type: PartTypeText, Time: fixtureTime(4), Text: &TextPart{
			Content: "Both sub-tasks dispatched.", AgentName: "root",
		}},
	}
	m.refreshContent()
	return m
}

func fixtureLongToolAndTimeline() ChatModel {
	m := NewChatModel()
	m.SetSize(80, 30)
	var resultLines strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&resultLines, "line %03d: lorem ipsum dolor sit amet consectetur.\n", i)
	}
	m.parts = []DisplayPart{
		{Type: PartTypeUser, Time: fixtureTime(0), User: &UserPart{Content: "Run a long command."}},
		{Type: PartTypeTool, Time: fixtureTime(1), Tool: &ToolPart{
			Name:      "bash",
			CallID:    "call_xxx",
			Arguments: `{"cmd":"cat huge.log"}`,
			Result:    resultLines.String(),
			State:     "completed",
			Duration:  1200 * time.Millisecond,
		}},
	}
	m.refreshContent()
	return m
}

func formatOffsets(offsets []int) string {
	var sb strings.Builder
	for _, o := range offsets {
		fmt.Fprintf(&sb, "%d\n", o)
	}
	return sb.String()
}

// TestGoldenChatRender locks the current refreshContent() output for a set of
// representative fixtures. The 6 fixtures exercise the empty state, a single
// user message, a complete assistant turn (user+thinking+text+tool), back-to-
// back thinking parts, a multi sub-agent turn with both running and completed
// children, and a long tool result.
//
// Re-generate goldens with: go test ./internal/tui/ -run TestGoldenChatRender -update-golden
func TestGoldenChatRender(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)

	cases := []struct {
		name  string
		build func() ChatModel
	}{
		{"chat_empty", fixtureEmpty},
		{"chat_single_user", fixtureSingleUser},
		{"chat_user_assistant_tool", fixtureUserAssistantTool},
		{"chat_thinking_stream", fixtureThinkingStream},
		{"chat_multi_subagent", fixtureMultiSubAgent},
		{"chat_long_tool", fixtureLongToolAndTimeline},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			m := c.build()
			diffGolden(t, c.name+".txt", m.fullContent)
			diffGolden(t, c.name+".offsets.txt", formatOffsets(m.partLineOffsets))
		})
	}
}
