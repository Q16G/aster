package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aster/internal/ai"
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
	m.agentSpawnByCallID["call_aaa1234"] = agentSpawnInfo{ParentStepID: "step-A", CallID: "call_aaa1234", SubScheme: true}
	m.agentSpawnByCallID["call_skill_99"] = agentSpawnInfo{ParentStepID: "step-S", CallID: "call_skill_99", SubScheme: false}

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
	for _, p := range m.chat.parts {
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

// Concurrent sub-agent streams must stay in separate, attributed buffers and
// flush into distinct TextParts rather than merging into one blob.
func TestConcurrentSubAgentStreamingAttribution(t *testing.T) {
	m := NewChatModel()
	m.SetSize(80, 24)

	m.AppendStream("sub-A", "alpha ")
	m.AppendStream("sub-B", "beta ")
	m.AppendStream("sub-A", "alpha2")
	m.AppendStream("sub-B", "beta2")

	if !m.FlushStream("sub-A") {
		t.Fatal("expected FlushStream(sub-A) to flush content")
	}
	if !m.FlushStream("sub-B") {
		t.Fatal("expected FlushStream(sub-B) to flush content")
	}

	got := map[string]string{}
	for _, p := range m.parts {
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

func writeAsyncResult(t *testing.T, baseDir, sessionID, child string, res asyncSubAgentResult) {
	t.Helper()
	dir := filepath.Join(baseDir, sessionID, "workspace", "sub_agents", child)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "async_result.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBackfillCompletedSubAgents(t *testing.T) {
	baseDir := t.TempDir()
	sessionID := "sess-1"

	writeAsyncResult(t, baseDir, sessionID, "sub-aaa", asyncSubAgentResult{
		AgentID: "sub-aaa", Status: "completed", OK: true, Result: "done aaa",
	})
	writeAsyncResult(t, baseDir, sessionID, "sub-bbb", asyncSubAgentResult{
		AgentID: "sub-bbb", Status: "running",
	})

	history := []*ai.MsgInfo{ai.NewUserMsgInfo("original task")}
	history, injected := backfillCompletedSubAgents(baseDir, sessionID, history)
	if injected != 1 {
		t.Fatalf("expected 1 injected result (completed only), got %d", injected)
	}
	last := msgContentString(history[len(history)-1].Content)
	if !strings.Contains(last, "agent_id: sub-aaa") || !strings.Contains(last, "done aaa") {
		t.Fatalf("injected message missing expected content: %q", last)
	}

	// Idempotent: a second pass must not re-inject the already-present result.
	_, injected2 := backfillCompletedSubAgents(baseDir, sessionID, history)
	if injected2 != 0 {
		t.Fatalf("expected 0 injected on second pass (dedup), got %d", injected2)
	}
}
