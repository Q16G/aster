package tui

import (
	"reflect"
	"strings"
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

// Sub-agent think/tool/text must be collapsed out of the main timeline (only the
// SubAgent card and root parts render inline) while remaining collectable by
// partsForChild for the drill-in view.
func TestMainTimelineExcludesNonRootDetails(t *testing.T) {
	m := NewChatModel()
	m.rootAgentName = "root"
	m.SetSize(100, 40)
	m.agentSpawnByCallID["call_aaa1234"] = agentSpawnInfo{CallID: "call_aaa1234", SubScheme: true}

	m.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_aaa1234", Status: "running", Description: "CARD_DESC"}},
		{Type: PartTypeThinking, Thinking: &ThinkingPart{Content: "SECRET_THINKING", AgentName: "sub-call_aaa"}},
		{Type: PartTypeTool, Tool: &ToolPart{Name: "rg", Arguments: "SECRET_TOOL_ARGS", State: "completed", AgentName: "sub-call_aaa"}},
		{Type: PartTypeText, Text: &TextPart{Content: "ROOT_VISIBLE", AgentName: "root"}},
		{Type: PartTypeThinking, Thinking: &ThinkingPart{Content: "ROOT_THINKING", AgentName: "root"}},
	}
	m.refreshContent()

	out := m.fullContent
	if strings.Contains(out, "SECRET_THINKING") {
		t.Error("sub-agent thinking leaked into main timeline")
	}
	if strings.Contains(out, "SECRET_TOOL_ARGS") {
		t.Error("sub-agent tool leaked into main timeline")
	}
	if !strings.Contains(out, "ROOT_VISIBLE") {
		t.Error("root text missing from main timeline")
	}
	if !strings.Contains(out, "ROOT_THINKING") {
		t.Error("root thinking missing from main timeline")
	}
	if !strings.Contains(out, "CARD_DESC") {
		t.Error("sub-agent card (with description) missing from main timeline")
	}

	if got := m.partsForChild("call_aaa1234"); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Fatalf("partsForChild = %v, want [0 1 2] (card + thinking + tool)", got)
	}
}

// step_result must be attributed to its producing agent like step_summary:
// root step_results stay in the main timeline, sub-agent step_results are
// collapsed out of main and surface only in their child's drill-in.
//
// Fixture replays the real session ~/.aster/sessions/3f9f4a89-…: 5 step_result
// events — recon/analysis (root code-audit) stay; scan-scm/summarize (child
// 4SEEm) and step-3 (child WLXEc) leak before the fix and must be folded after.
func TestStepResultAttributedByAgent(t *testing.T) {
	const rootName = "code-audit"
	const childA = "sub-call_4SEEm" // owns scan-scm + summarize
	const childB = "sub-call_WLXEc" // owns step-3

	m := NewChatModel()
	m.rootAgentName = rootName
	m.SetSize(120, 60)
	m.agentSpawnByCallID[childA] = agentSpawnInfo{CallID: childA, SubScheme: true}
	m.agentSpawnByCallID[childB] = agentSpawnInfo{CallID: childB, SubScheme: true}

	sr := func(agent, stepID, stepName string) DisplayPart {
		return DisplayPart{Type: PartTypeStepResult, StepResult: &StepResultPart{
			AgentName: agent, StepID: stepID, StepName: stepName,
			Status: "completed", DisplayResult: "RESULT_" + stepID,
		}}
	}
	m.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: childA, Status: "running"}},
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: childB, Status: "running"}},
		sr(rootName, "recon", "STEP_RECON"),     // root → main
		sr(rootName, "analysis", "STEP_ANALYS"), // root → main
		sr(childA, "scan-scm", "STEP_SCANSCM"),  // child A → folded
		sr(childA, "summarize", "STEP_SUMM"),    // child A → folded
		sr(childB, "step-3", "STEP_STEP3"),      // child B → folded
	}
	m.refreshContent()

	out := m.fullContent
	for _, keep := range []string{"STEP_RECON", "STEP_ANALYS"} {
		if !strings.Contains(out, keep) {
			t.Errorf("root step_result %q missing from main timeline", keep)
		}
	}
	for _, leak := range []string{"STEP_SCANSCM", "STEP_SUMM", "STEP_STEP3"} {
		if strings.Contains(out, leak) {
			t.Errorf("sub-agent step_result %q leaked into main timeline", leak)
		}
	}

	// The folded step_results must be collectable in their child's drill-in.
	if got := m.partsForChild(childA); !reflect.DeepEqual(got, []int{0, 4, 5}) {
		t.Fatalf("child A parts = %v, want [0 4 5] (card + scan-scm + summarize)", got)
	}
	if got := m.partsForChild(childB); !reflect.DeepEqual(got, []int{1, 6}) {
		t.Fatalf("child B parts = %v, want [1 6] (card + step-3)", got)
	}
}

// Enter on a focused sub-agent card emits EnterSubAgentMsg (in-place drill-in);
// Space keeps the inline toggle (no message).
func TestEnterOnSubAgentEmitsEnterDetail(t *testing.T) {
	m := NewChatModel()
	m.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_xyz", Status: "running"}},
	}
	m.cursor = 0

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command from Enter on sub-agent card")
	}
	open, ok := cmd().(EnterSubAgentMsg)
	if !ok {
		t.Fatalf("expected EnterSubAgentMsg, got %T", cmd())
	}
	if open.CallID != "call_xyz" {
		t.Fatalf("CallID = %q, want call_xyz", open.CallID)
	}

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd != nil {
		t.Fatal("Space should toggle inline (no command), not drill in")
	}
}

// EnterChild swaps the chat area to the sub-agent transcript in-place; ExitChild
// restores the main timeline. Verifies the child's collapsed details surface in
// the drill-in content and disappear again on exit.
func TestEnterExitChildSwapsMainContent(t *testing.T) {
	m := NewChatModel()
	m.rootAgentName = "root"
	m.SetSize(100, 40)
	m.agentSpawnByCallID["call_aaa1234"] = agentSpawnInfo{CallID: "call_aaa1234", SubScheme: true}
	m.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_aaa1234", Status: "running", Description: "CARD_DESC"}},
		{Type: PartTypeThinking, Thinking: &ThinkingPart{Content: "CHILD_THINKING", AgentName: "sub-call_aaa"}},
		{Type: PartTypeText, Text: &TextPart{Content: "ROOT_VISIBLE", AgentName: "root"}},
	}
	m.refreshContent()

	if strings.Contains(m.fullContent, "CHILD_THINKING") {
		t.Fatal("child thinking should not be inline in the main timeline")
	}

	if !m.EnterChild("call_aaa1234") {
		t.Fatal("EnterChild should succeed for a known sub-agent")
	}
	if m.ViewingChild() != "call_aaa1234" {
		t.Fatalf("ViewingChild = %q, want call_aaa1234", m.ViewingChild())
	}
	if !strings.Contains(m.fullContent, "CHILD_THINKING") {
		t.Error("drill-in content should contain the child's thinking")
	}
	if strings.Contains(m.fullContent, "ROOT_VISIBLE") {
		t.Error("drill-in content must not contain root parts")
	}

	m.ExitChild()
	if m.ViewingChild() != "" {
		t.Fatalf("ViewingChild after exit = %q, want empty", m.ViewingChild())
	}
	if !strings.Contains(m.fullContent, "ROOT_VISIBLE") {
		t.Error("main timeline should be restored after exit")
	}
	if strings.Contains(m.fullContent, "CHILD_THINKING") {
		t.Error("child thinking should be collapsed again after exit")
	}

	if m.EnterChild("call_missing") {
		t.Error("EnterChild should fail for an unknown call_id")
	}
}
