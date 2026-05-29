package react_test

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	. "aster/internal/react"
)

// TestConcurrentSubAgentEventFlow_Live 用真实 LLM 跑一遍「父 agent 并发派发子 agent」，
// 通过 factory 的 per-agent emitter 捕获真机 event 流，验证父/子事件各自带正确 AgentName、
// 不串台。这补上单元测试（模拟事件）覆盖不到的「生产端事件产出 + 归属」链路。
//
// 启用方式：SASTPRO_REACT_LIVE_TEST=1 go test ./internal/react/tests/... -run TestConcurrentSubAgentEventFlow -v
func TestConcurrentSubAgentEventFlow_Live(t *testing.T) {
	if os.Getenv("SASTPRO_REACT_LIVE_TEST") != "1" {
		t.Skip("live test disabled; set SASTPRO_REACT_LIVE_TEST=1")
	}

	client := newOpenCodeGoClient(t)

	// 线程安全捕获：父与子并发运行，sink 会被多 goroutine 调用。
	var mu sync.Mutex
	type evtKey struct{ agent, typ string }
	counts := map[evtKey]int{}
	agentsSeen := map[string]bool{}
	// 记录有 streaming/result 但 AgentName 为空的事件（串台/丢归属的征兆）。
	var blankAttribution int
	var totalEvents int

	sink := func(e *AgentOutputEvent) error {
		if e == nil {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		totalEvents++
		name := strings.TrimSpace(e.AgentName)
		typ := string(e.Type)
		if name == "" {
			if e.Type == EventTypeStream || e.Type == EventTypeResult {
				blankAttribution++
			}
		} else {
			agentsSeen[name] = true
		}
		counts[evtKey{agent: name, typ: typ}]++
		return nil
	}

	factory := NewAgentFactory(
		WithFactoryDefaultAIClient(client),
		WithFactoryToolRegistry(NewDefaultToolRegistry()),
		WithFactoryEmitterFunc(sink),
	)

	parent, err := factory.Build(AgentDefinition{
		Name: "root",
		Instruction: `你是任务编排 agent。你必须使用 sub_agent 工具在同一轮内并发派发两个相互独立的子任务，` +
			`不要自己直接回答这两个子任务。等两个子 agent 都返回后，把它们的结果合并成一句话作为最终答案。`,
		Policies: AgentPolicies{MaxIterations: 10},
	})
	if err != nil {
		t.Fatalf("factory.Build(root) failed: %v", err)
	}

	input := `请并发派发两个子 agent：
- 子任务A：只计算 2+2 并返回数字
- 子任务B：只计算 10*3 并返回数字
两个都返回后，输出形如 "A=<结果>, B=<结果>" 的汇总。`

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	t.Log("Running parent agent with concurrent sub_agent spawning (live)...")
	runResult, err := parent.Execute(ctx, input)
	if err != nil {
		t.Fatalf("parent.Execute failed: %v", err)
	}
	if runResult == nil || !runResult.Success {
		t.Fatalf("run not successful: %+v", runResult)
	}
	t.Logf("=== FINAL RESULT ===\n%s", runResult.Result)

	// 打印按 agent 分组的事件统计，便于人工 review。
	mu.Lock()
	defer mu.Unlock()

	names := make([]string, 0, len(agentsSeen))
	for n := range agentsSeen {
		names = append(names, n)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString("=== EVENT FLOW BY AGENT ===\n")
	for _, n := range names {
		sb.WriteString("[" + n + "]\n")
		typs := make([]string, 0)
		for k, c := range counts {
			if k.agent == n {
				typs = append(typs, k.typ+"="+strconv.Itoa(c))
			}
		}
		sort.Strings(typs)
		sb.WriteString("  " + strings.Join(typs, " ") + "\n")
	}
	report := sb.String()
	t.Log(report)
	os.WriteFile("/tmp/concurrent_subagent_eventflow.txt", []byte(report+"\nFINAL:\n"+runResult.Result), 0o644)

	// 子 agent 名（非 root 且非空）。
	childNames := make([]string, 0)
	for _, n := range names {
		if n != "root" {
			childNames = append(childNames, n)
		}
	}

	// 断言：
	// 1) 没有「有内容但 AgentName 为空」的事件（串台/丢归属）。
	if blankAttribution > 0 {
		t.Errorf("found %d stream/result events with empty AgentName (归属丢失/串台)", blankAttribution)
	}
	// 2) root 自身有事件。
	if !agentsSeen["root"] {
		t.Errorf("no events attributed to root agent; agents seen=%v", names)
	}
	// 3) 至少派发出 1 个子 agent，且其名与 root 不同（理想是 2 个并发）。
	if len(childNames) == 0 {
		t.Errorf("REVIEW: 模型未派发任何子 agent，无法观测父/子事件流；请查看 /tmp/concurrent_subagent_eventflow.txt 与最终结果")
	} else {
		t.Logf("child agents observed (%d): %v", len(childNames), childNames)
		if len(childNames) < 2 {
			t.Logf("NOTE: 仅观测到 1 个子 agent（模型可能未并发派发 2 个），父/子归属仍可验证")
		}
	}
	t.Logf("total events=%d distinct agents=%d", totalEvents, len(names))
}
