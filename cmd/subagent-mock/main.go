// Command subagent-mock launches the REAL Aster TUI model and replays a scripted
// run that spawns sub-agents, so the sub-agent event-routing UI can be inspected
// by hand without a live provider.
//
// By default it runs the two-agent scenario: agents A and B run concurrently
// (the right-side panel lists both), then A finishes and drops out of the panel,
// while B is left running so the panel stays visible for review. Finished agent A
// is still reachable via its collapsed card in the main timeline.
//
// Controls:
//
//	Tab          cycle focus (input -> sub-agent panel -> chat -> input)
//	↑/↓ or k/j    on the sub-agent panel: move selection; in the chat: scroll
//	Enter         on the sub-agent panel: drill into the selected (running) sub-agent
//	             on a sub-agent card in the timeline: drill into that sub-agent
//	←/Esc        exit the drill-in, back to the main timeline
//	Space         on a sub-agent card: inline expand/collapse
//	Ctrl+C        quit
//
// Run:  go run ./cmd/subagent-mock                 (two-agent demo)
//
//	go run ./cmd/subagent-mock -real          (replay real session: step_result-leak + panel-persistence fixes)
//	go run ./cmd/subagent-mock -single        (single sub-agent that finishes)
//	go run ./cmd/subagent-mock -delay 1.5s    (slow the replay down)
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aster/internal/provider"
	"aster/internal/tui"
)

func main() {
	delay := flag.Duration("delay", 900*time.Millisecond, "delay between scripted events")
	headless := flag.Bool("headless", false, "drive the model without a TTY and exit (panic smoke check)")
	single := flag.Bool("single", false, "use the single-sub-agent scenario instead of the two-agent demo")
	real := flag.Bool("real", false, "replay the real on-disk session 3f9f4a89 (two background sub-agents + step_results) to review the step_result-leak and panel-persistence fixes")
	mainAgent := flag.Bool("main", false, "use the pure root-agent scenario (no sub-agents) to inspect main-timeline attribution")
	rootName := flag.String("root-name", "code-audit", "AgentName the root agent emits under in -main mode")
	flag.Parse()

	// A non-nil registry + provider config keeps the empty-state home view from
	// dereferencing nil deps before the first scripted event arrives.
	model := tui.NewModel(tui.ModelDeps{
		Registry: provider.NewRegistry(""),
		ProviderCfg: &tui.ProviderState{
			Name:    "mock",
			ModelID: "mock-model",
			APIKey:  "mock",
		},
	})

	var events []tui.MockEvent
	var drillCallID string
	switch {
	case *real:
		// Replay of the real session: two background sub-agents emit their own
		// step_results (must fold, not leak) and finish (panel must persist).
		// Drill into A to inspect its folded step_results.
		events, _, drillCallID, _, _ = tui.MockRealSessionScenario()
	case *mainAgent:
		// Pure root-agent run; no sub-agent to drill into. The harness leaves
		// rootAgentName == "" (no AgentCtx), so passing -root-name=code-audit
		// reproduces the attribution bug (content filtered out), while
		// -root-name="" makes the root parts show.
		events = tui.MockMainAgentScenario(*rootName)
	case *single:
		const childCallID = "call_aaa1234"
		const childName = "sub-call_aaa" // sub-<callID[:8]>; must match runtime naming
		events = tui.MockSubAgentScenario("", childName, childCallID)
		drillCallID = childCallID
	default:
		var aCallID string
		events, _, aCallID, _, _ = tui.MockTwoSubAgentScenario()
		drillCallID = aCallID
	}

	if *headless {
		runHeadless(model, events, drillCallID)
		return
	}

	p := tea.NewProgram(&model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	go func() {
		// Let the first frame render before driving events.
		time.Sleep(600 * time.Millisecond)
		for _, me := range events {
			p.Send(tui.AgentEventMsg{Event: me.Event})
			time.Sleep(*delay)
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// runHeadless drives the real model through the scenario without a terminal,
// rendering View() at the empty-state and after every event to catch any
// render-path panics (and to eyeball that sub-agent details stay collapsed).
func runHeadless(model tui.Model, events []tui.MockEvent, drillCallID string) {
	var tm tea.Model = &model
	step := func(msg tea.Msg) {
		var m2 tea.Model
		m2, _ = tm.Update(msg)
		tm = m2
		_ = tm.View() // panics here if a render path touches a nil dep
	}

	step(tea.WindowSizeMsg{Width: 120, Height: 40}) // empty-state home view
	for _, me := range events {
		step(tui.AgentEventMsg{Event: me.Event})
	}

	if drillCallID == "" {
		// Pure root-agent scenario: no sub-agent to drill into. Settle the timeline
		// the way real run completion does (flush thinking/stream buffers), then
		// report whether the main agent's tools/text made it into the timeline.
		step(tui.AgentDoneMsg{})
		view := tm.View()
		report := func(label, needle string) {
			mark := "MISSING"
			if strings.Contains(view, needle) {
				mark = "shown"
			}
			fmt.Printf("  %-12s %q -> %s\n", label, needle, mark)
		}
		fmt.Println("headless OK: drove main-agent scenario, no panic. Main-timeline contents:")
		report("tool", "list_files")
		report("tool", "read_file")
		report("text", "分析结论")
		return
	}

	// Main-timeline content check: the root's own step_results must show, while
	// the background sub-agents' step_results must be folded out (Bug 1 fix).
	mainView := tm.View()
	reportMain := func(label, needle string, wantShown bool) {
		shown := strings.Contains(mainView, needle)
		mark := "shown"
		if !shown {
			mark = "MISSING"
		}
		ok := "OK"
		if shown != wantShown {
			ok = "FAIL"
		}
		fmt.Printf("  [%s] %-22s %q -> %s\n", ok, label, needle, mark)
	}
	fmt.Println("main-timeline contents (root step_results shown, sub-agent step_results folded):")
	reportMain("root step recon", "项目框架与攻击面侦察", true)
	reportMain("root step analysis", "确定审计方向与优先级", true)
	reportMain("sub-A step scan-scm", "对 scm 模块执行 SAST 扫描", false)
	reportMain("sub-A step summarize", "汇总三个模块的扫描结果", false)
	reportMain("sub-B step step-3", "检查当前系统资源并确定并发度", false)

	// Exercise the in-place drill-in render path: enter a sub-agent transcript,
	// render, then exit back to the main timeline and render again.
	step(tui.EnterSubAgentMsg{CallID: drillCallID})
	step(tea.KeyMsg{Type: tea.KeyLeft}) // ← exits the drill-in

	fmt.Println("headless OK: drove scenario + drill-in/exit and rendered View() with no panic")
}
