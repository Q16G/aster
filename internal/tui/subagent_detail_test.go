package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// partsForChild must group parts by the spawning call_id and never cross-talk
// between concurrent sub-agents, while excluding root/unattributed parts.
func TestPartsForChildGroupsByCallID(t *testing.T) {
	m := NewChatModel()
	m.agentSpawnByCallID["call_aaa1234"] = agentSpawnInfo{CallID: "call_aaa1234", SubScheme: true}
	m.agentSpawnByCallID["call_bbb9999"] = agentSpawnInfo{CallID: "call_bbb9999", SubScheme: true}

	m.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_aaa1234", Status: "running"}},
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_bbb9999", Status: "running"}},
		{Type: PartTypeText, Text: &TextPart{Content: "from A", AgentName: "sub-call_aaa"}},
		{Type: PartTypeText, Text: &TextPart{Content: "from B", AgentName: "sub-call_bbb"}},
		{Type: PartTypePlan, Plan: &PlanPart{AgentName: "sub-call_aaa"}},
		{Type: PartTypeText, Text: &TextPart{Content: "root text", AgentName: "root"}},
	}

	if got := m.partsForChild("call_aaa1234"); !reflect.DeepEqual(got, []int{0, 2, 4}) {
		t.Fatalf("child A parts = %v, want [0 2 4]", got)
	}
	if got := m.partsForChild("call_bbb9999"); !reflect.DeepEqual(got, []int{1, 3}) {
		t.Fatalf("child B parts = %v, want [1 3]", got)
	}
	if got := m.partsForChild(""); got != nil {
		t.Fatalf("empty callID should return nil, got %v", got)
	}
}

// RenderAgentTranscript renders the filtered child parts and must restore the
// transient width/expand mutations so the main view is unaffected.
func TestRenderAgentTranscriptRestoresState(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)
	m.agentSpawnByCallID["call_aaa1234"] = agentSpawnInfo{CallID: "call_aaa1234", SubScheme: true}
	m.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_aaa1234", Status: "running"}},
		{Type: PartTypeText, Text: &TextPart{Content: "child output", AgentName: "sub-call_aaa"}},
	}
	wantWidth := m.width

	content, ok := m.RenderAgentTranscript("call_aaa1234", 70)
	if !ok {
		t.Fatal("expected transcript content")
	}
	if content == "" {
		t.Fatal("transcript content is empty")
	}
	if m.width != wantWidth {
		t.Fatalf("width not restored: got %d want %d", m.width, wantWidth)
	}
	if m.toolExpanded[0] || m.toolExpanded[1] {
		t.Fatalf("toolExpanded not restored: %v", m.toolExpanded)
	}

	if _, ok := m.RenderAgentTranscript("call_missing", 70); ok {
		t.Fatal("unknown callID should return ok=false")
	}
}

// Enter on a focused sub-agent card emits OpenSubAgentDetailMsg; Space keeps the
// inline toggle (no message).
func TestEnterOnSubAgentEmitsOpenDetail(t *testing.T) {
	m := NewChatModel()
	m.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_xyz", Status: "running"}},
	}
	m.cursor = 0

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command from Enter on sub-agent card")
	}
	open, ok := cmd().(OpenSubAgentDetailMsg)
	if !ok {
		t.Fatalf("expected OpenSubAgentDetailMsg, got %T", cmd())
	}
	if open.CallID != "call_xyz" {
		t.Fatalf("CallID = %q, want call_xyz", open.CallID)
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd != nil {
		t.Fatal("Space should toggle inline (no command), not drill in")
	}
}
