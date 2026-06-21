package tui

import (
	"testing"

	"aster/internal/react"
)

func TestChildAgentCallToken(t *testing.T) {
	cases := map[string]string{
		"sub-call_aaa":           "call_aaa", // sub_agent: token = callID[:8]
		"skill-deep_scan-call_s": "call_s",   // skill fork: token after last '-'
		"skill-a-b-call_x":       "call_x",   // skill name with '-': last segment wins
		"root":                   "",         // root agent: no scheme prefix
		"my-agent":               "",         // arbitrary name: no scheme prefix
		"":                       "",         // empty
	}
	for in, want := range cases {
		if got := childAgentCallToken(in); got != want {
			t.Fatalf("childAgentCallToken(%q) = %q, want %q", in, got, want)
		}
	}
}

// lookupSpawnByChild must resolve both naming schemes against the full call_id
// captured at tool_start, using the truncated token embedded in the child name.
func TestLookupSpawnByChild(t *testing.T) {
	m := NewChatModel()
	m.store.spawnByCallID["call_aaa1234"] = agentSpawnInfo{ParentStepID: "step-A", CallID: "call_aaa1234", SubScheme: true}
	m.store.spawnByCallID["call_skill_99"] = agentSpawnInfo{ParentStepID: "step-S", CallID: "call_skill_99", SubScheme: false}

	if info, ok := m.lookupSpawnByChild("sub-call_aaa"); !ok || info.ParentStepID != "step-A" {
		t.Fatalf("sub child: got (%+v, %v), want step-A", info, ok)
	}
	if info, ok := m.lookupSpawnByChild("skill-deep_scan-call_s"); !ok || info.ParentStepID != "step-S" {
		t.Fatalf("skill child: got (%+v, %v), want step-S", info, ok)
	}
	if _, ok := m.lookupSpawnByChild("root"); ok {
		t.Fatal("root agent should not resolve to any spawn entry")
	}
}

// Two sub-agents are spawned under different parent steps and their plan events
// arrive interleaved (not LIFO order). Each plan must resolve its own parent
// step, not the most recently spawned one.
func TestConcurrentSubAgentPlanAttribution(t *testing.T) {
	m := NewModel(ModelDeps{})

	startAgent := func(parentStep, callID string) {
		m.chat.activeStepByAgent["root"] = parentStep
		m.handleAgentEvent(&react.AgentOutputEvent{
			Type:      react.EventTypeToolStart,
			AgentName: "root",
			Payload: map[string]any{
				"tool_name": "sub_agent",
				"call_id":   callID,
				"is_agent":  true,
			},
		})
	}

	emitPlan := func(agentName string) {
		m.handleAgentEvent(&react.AgentOutputEvent{
			Type:      react.EventTypeTaskPlan,
			AgentName: agentName,
			Payload: map[string]any{
				"explanation": "",
				"plan": []any{
					map[string]any{"id": "s1", "step": "do work", "status": "pending"},
				},
			},
		})
	}

	// Spawn sub-A under step-A, then sub-B under step-B (interleaved starts).
	startAgent("step-A", "call_aaa1") // -> sub-call_aaa
	startAgent("step-B", "call_bbb1") // -> sub-call_bbb

	// Plans arrive in reverse-of-spawn order, which previously broke the LIFO
	// stack-top heuristic.
	emitPlan("sub-call_bbb")
	emitPlan("sub-call_aaa")

	wantParent := map[string]string{
		"sub-call_aaa": "step-A",
		"sub-call_bbb": "step-B",
	}
	seen := map[string]bool{}
	for _, p := range m.chat.store.parts {
		if p.Type != PartTypePlan || p.Plan == nil {
			continue
		}
		want, ok := wantParent[p.Plan.AgentName]
		if !ok {
			continue
		}
		seen[p.Plan.AgentName] = true
		if p.Plan.ParentStepID != want {
			t.Fatalf("plan %q: ParentStepID = %q, want %q", p.Plan.AgentName, p.Plan.ParentStepID, want)
		}
	}
	for name := range wantParent {
		if !seen[name] {
			t.Fatalf("expected a plan part for %q", name)
		}
	}
}

// When two peer steps run concurrently, the sub_agent spawned by one peer must
// be attributed to THAT peer's step, not to whichever peer last flipped to
// in_progress. The pre-fix code read activeStepByAgent[agentName] — a single
// slot per agent that concurrent peers overwrite — so P1's sub-agent leaked
// onto P2. The fix prefers the tool_start event's own step_id (=
// effectiveStepID(runCtx)), which precisely identifies the spawning peer.
func TestConcurrentSubAgentStepIDAttribution(t *testing.T) {
	m := NewModel(ModelDeps{})

	// Both peers flip to in_progress; the single activeStepByAgent slot ends up
	// holding the last writer (p2), modelling the race that mis-attributed P1.
	flipInProgress := func(stepID string) {
		m.handleAgentEvent(&react.AgentOutputEvent{
			Type:      react.EventTypeTaskItem,
			AgentName: "root",
			Payload:   map[string]any{"id": stepID, "status": "in_progress"},
		})
	}
	flipInProgress("p1")
	flipInProgress("p2")
	if got := m.chat.activeStepByAgent["root"]; got != "p2" {
		t.Fatalf("precondition: activeStepByAgent[root] = %q, want p2 (last writer)", got)
	}

	// Peer p1 spawns a sync sub_agent. Its tool_start carries step_id=p1.
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeToolStart,
		AgentName: "root",
		Payload: map[string]any{
			"tool_name": "sub_agent",
			"call_id":   "call_p1aaaa",
			"is_agent":  true,
			"step_id":   "p1",
		},
	})

	// Spawn attribution must honour the event's step_id, not the raced slot.
	if info := m.chat.store.spawnByCallID["call_p1aaaa"]; info.ParentStepID != "p1" {
		t.Fatalf("spawn ParentStepID = %q, want p1 (not the raced activeStepByAgent=p2)", info.ParentStepID)
	}

	// The sync sub-agent card must carry StepID=p1 and index under p1 only.
	var card *SubAgentPart
	for _, p := range m.chat.store.parts {
		if p.Type == PartTypeSubAgent && p.SubAgent != nil && p.SubAgent.CallID == "call_p1aaaa" {
			card = p.SubAgent
		}
	}
	if card == nil {
		t.Fatal("expected a sub-agent card for call_p1aaaa")
	}
	if card.StepID != "p1" {
		t.Fatalf("sub-agent card StepID = %q, want p1", card.StepID)
	}
	if len(m.chat.store.PartsForStep("p1")) == 0 {
		t.Fatal("sub-agent card should be indexed under step p1")
	}
	for _, idx := range m.chat.store.PartsForStep("p2") {
		if p := m.chat.store.parts[idx]; p.Type == PartTypeSubAgent {
			t.Fatal("sub-agent card leaked into step p2's index")
		}
	}
}

// The background sub_agent card (driven by the BgStart bridge, not the collapsed
// tool_start/end) must also carry its spawning peer's step_id so concurrent
// peers' panel cards don't cross-attribute.
func TestBackgroundSubAgentCardCarriesStepID(t *testing.T) {
	m := NewModel(ModelDeps{})

	// p1 launches a background sub_agent: tool_start (run_in_background) records
	// the spawn but creates no sync card; the durable card comes from BgStart.
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeToolStart,
		AgentName: "root",
		Payload: map[string]any{
			"tool_name": "sub_agent",
			"call_id":   "call_p1bbbb",
			"is_agent":  true,
			"step_id":   "p1",
			"arguments": map[string]any{"run_in_background": true},
		},
	})
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeSubAgentBgStart,
		AgentName: "root",
		Payload: map[string]any{
			"agent_id":  "sub-call_p1bbbb",
			"tool_name": "sub_agent",
			"step_id":   "p1",
		},
	})

	var card *SubAgentPart
	for _, p := range m.chat.store.parts {
		if p.Type == PartTypeSubAgent && p.SubAgent != nil {
			card = p.SubAgent
		}
	}
	if card == nil {
		t.Fatal("expected a background sub-agent card")
	}
	if card.StepID != "p1" {
		t.Fatalf("background sub-agent card StepID = %q, want p1", card.StepID)
	}
}

// Concurrent sub-agent streams must stay in separate, attributed buffers and
// flush into distinct TextParts rather than merging into one blob.
func TestConcurrentSubAgentStreamingAttribution(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendStream("sub-A", "alpha ", "")
	m.AppendStream("sub-B", "beta ", "")
	m.AppendStream("sub-A", "alpha2", "")
	m.AppendStream("sub-B", "beta2", "")

	if !m.FlushStream("sub-A", "") {
		t.Fatal("expected FlushStream(sub-A) to flush content")
	}
	if !m.FlushStream("sub-B", "") {
		t.Fatal("expected FlushStream(sub-B) to flush content")
	}

	got := map[string]string{}
	for _, p := range m.store.parts {
		if p.Type == PartTypeText && p.Text != nil {
			got[p.Text.AgentName] = p.Text.Content
		}
	}
	if got["sub-A"] != "alpha alpha2" {
		t.Fatalf("sub-A content = %q, want %q", got["sub-A"], "alpha alpha2")
	}
	if got["sub-B"] != "beta beta2" {
		t.Fatalf("sub-B content = %q, want %q", got["sub-B"], "beta beta2")
	}
}

// A sub-agent that streamed must not suppress the root agent's result fallback.
// Previously a run-level hadStreamDuringRun flag was set by any streaming agent,
// so the root's empty-stream result was wrongly dropped. The per-agent flag fixes
// this, and the fallback TextPart must carry the producing agent's name.
func TestResultFallbackPerAgentIsolation(t *testing.T) {
	m := NewModel(ModelDeps{})

	// A sub-agent streams (sets only its own hadStream flag).
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeStream,
		AgentName: "sub-call_aaa",
		Content:   "sub streamed text",
	})
	m.flushStreamAndPersist("sub-call_aaa", "")

	// Root emits a result with no prior streaming of its own. Its fallback must fire.
	m.handleAgentEvent(&react.AgentOutputEvent{
		Type:      react.EventTypeResult,
		AgentName: "root",
		Payload:   map[string]any{"result": "root final answer"},
	})

	var rootText *TextPart
	for _, p := range m.chat.store.parts {
		if p.Type == PartTypeText && p.Text != nil && p.Text.Content == "root final answer" {
			rootText = p.Text
		}
	}
	if rootText == nil {
		t.Fatal("root result fallback was suppressed by sub-agent streaming")
	}
	if rootText.AgentName != "root" {
		t.Fatalf("root fallback TextPart AgentName = %q, want %q", rootText.AgentName, "root")
	}
}

// Same-agent consecutive text merges; different-agent text does not.
func TestMergeTextRunRespectsAgent(t *testing.T) {
	parts := []IndexedPart{
		{Index: 0, Part: DisplayPart{Type: PartTypeText, Text: &TextPart{Content: "a1", AgentName: "sub-A"}}},
		{Index: 1, Part: DisplayPart{Type: PartTypeText, Text: &TextPart{Content: "a2", AgentName: "sub-A"}}},
		{Index: 2, Part: DisplayPart{Type: PartTypeText, Text: &TextPart{Content: "b1", AgentName: "sub-B"}}},
	}
	content, count := mergeTextRun(parts, 0)
	if count != 2 {
		t.Fatalf("expected merge count 2 (same agent), got %d", count)
	}
	if content != "a1\n\na2" {
		t.Fatalf("unexpected merged content %q", content)
	}
	content, count = mergeTextRun(parts, 2)
	if count != 1 || content != "b1" {
		t.Fatalf("expected single-part run for sub-B, got count=%d content=%q", count, content)
	}
}
