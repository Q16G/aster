package tui

import "testing"

// PlanForChild must resolve a sub-agent's own plan by the spawning call_id and
// never return a root plan or a stranger's plan.
func TestPlanForChildResolvesByCallID(t *testing.T) {
	m := NewChatModel()
	m.rootAgentName = "root"
	m.agentSpawnByCallID["call_aaa1234"] = agentSpawnInfo{CallID: "call_aaa1234", SubScheme: true}

	rootPlan := &PlanPart{AgentName: "root", Items: []PlanItemView{{ID: "root-1", Step: "root step"}}}
	childPlan := &PlanPart{AgentName: "sub-call_aaa", ParentStepID: "root-1", Items: []PlanItemView{{ID: "a-1", Step: "child step"}}}
	m.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_aaa1234", Status: "running"}},
		{Type: PartTypePlan, Plan: rootPlan},
		{Type: PartTypePlan, Plan: childPlan},
	}

	if got := m.PlanForChild("call_aaa1234"); got != childPlan {
		t.Fatalf("PlanForChild = %v, want child plan", got)
	}
	if got := m.PlanForChild("call_missing"); got != nil {
		t.Fatalf("unknown callID should return nil, got %v", got)
	}
	if got := m.PlanForChild(""); got != nil {
		t.Fatalf("empty callID should return nil, got %v", got)
	}
}

// The sidebar Todo list shows root + all children on the main timeline, but only
// the drilled-in sub-agent's subtree once viewingChild is set.
func TestSidebarTodoFiltersByViewingChild(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "root"
	m.chat.agentSpawnByCallID["call_aaa1234"] = agentSpawnInfo{ParentStepID: "root-1", CallID: "call_aaa1234", SubScheme: true}

	rootPlan := &PlanPart{AgentName: "root", Items: []PlanItemView{
		{ID: "root-1", Step: "派 A"},
		{ID: "root-2", Step: "收尾"},
	}}
	childPlan := &PlanPart{AgentName: "sub-call_aaa", ParentStepID: "root-1", Items: []PlanItemView{
		{ID: "a-1", Step: "定位"},
		{ID: "a-2", Step: "确认"},
	}}
	m.chat.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_aaa1234", Status: "running"}},
		{Type: PartTypePlan, Plan: rootPlan},
		{Type: PartTypePlan, Plan: childPlan},
	}

	mainSnap := m.buildSidebarSnapshot()
	if len(mainSnap.PlanItems) != 4 {
		t.Fatalf("main view PlanItems = %d, want 4 (root + child)", len(mainSnap.PlanItems))
	}

	m.chat.viewingChild = "call_aaa1234"
	childSnap := m.buildSidebarSnapshot()
	if len(childSnap.PlanItems) != 2 {
		t.Fatalf("drill-in PlanItems = %d, want 2 (only child subtree)", len(childSnap.PlanItems))
	}
	for _, it := range childSnap.PlanItems {
		if it.ID != "a-1" && it.ID != "a-2" {
			t.Fatalf("drill-in leaked non-child item %q", it.ID)
		}
	}
}
