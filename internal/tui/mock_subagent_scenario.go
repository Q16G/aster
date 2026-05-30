package tui

import (
	"encoding/json"

	"aster/internal/builtin_tools"
	"aster/internal/react"
)

// MockEvent pairs a synthetic agent event with a human-readable note. The mock
// harness (cmd/mock-subagent) and the integration test both replay the same
// scenario so the manual demo and the automated assertion stay in lockstep.
type MockEvent struct {
	Note  string
	Event *react.AgentOutputEvent
}

// MockSubAgentScenario returns a scripted run that exercises the sub-agent
// event-routing feature: a root agent thinks, streams, then spawns one
// sub-agent. The sub-agent produces its own thinking / tool / stream events
// (which must collapse behind the card in the main timeline) before finishing.
//
// rootName is the root agent's name ("" is treated as root); childName must be
// the name the sub-agent emits its events under. For the sub_agent tool the
// runtime names children "sub-<callID[:8]>", so callers pass that here.
func MockSubAgentScenario(rootName, childName, childCallID string) []MockEvent {
	root := func(t react.EventType) *react.AgentOutputEvent {
		return &react.AgentOutputEvent{Type: t, AgentName: rootName, Payload: map[string]any{}}
	}
	child := func(t react.EventType) *react.AgentOutputEvent {
		return &react.AgentOutputEvent{Type: t, AgentName: childName, Payload: map[string]any{}}
	}
	think := func(ev *react.AgentOutputEvent, group, text string) *react.AgentOutputEvent {
		ev.GroupID = group
		ev.Payload["think_content"] = text
		return ev
	}
	stream := func(ev *react.AgentOutputEvent, text string) *react.AgentOutputEvent {
		ev.Content = text
		return ev
	}
	toolStart := func(ev *react.AgentOutputEvent, name, callID string, isAgent bool, args map[string]any) *react.AgentOutputEvent {
		ev.Payload["tool_name"] = name
		ev.Payload["call_id"] = callID
		ev.Payload["is_agent"] = isAgent
		ev.Payload["arguments"] = args
		return ev
	}
	toolEnd := func(ev *react.AgentOutputEvent, name, callID string, isAgent bool, result string) *react.AgentOutputEvent {
		ev.Payload["tool_name"] = name
		ev.Payload["call_id"] = callID
		ev.Payload["is_agent"] = isAgent
		ev.Payload["result"] = result
		return ev
	}

	desc := "探查 internal/tui 的事件分流路径"
	return []MockEvent{
		{"root 思考（主时间线实时显示）", think(root(react.EventTypeThink), "g-root-1", "先看一下仓库结构，规划如何排查事件分流。")},
		{"root 流式文本", stream(root(react.EventTypeStream), "我先派一个子 agent 去探查代码库。")},
		{"root 调用 sub_agent（卡片出现）", toolStart(root(react.EventTypeToolStart), builtin_tools.SubAgentToolName, childCallID, true,
			map[string]any{"description": desc})},

		{"子 agent 思考（应折叠，不内联）", think(child(react.EventTypeThink), "g-sub-1", "我先搜索事件发射的位置。")},
		{"子 agent 调用 rg（应折叠）", toolStart(child(react.EventTypeToolStart), "rg", "call_rg_1", false,
			map[string]any{"pattern": "EmitterFunc"})},
		{"子 agent rg 结果", toolEnd(child(react.EventTypeToolEnd), "rg", "call_rg_1", false, "3 matches in event_bridge.go")},
		{"子 agent 调用 read_file（应折叠）", toolStart(child(react.EventTypeToolStart), "read_file", "call_rf_1", false,
			map[string]any{"path": "internal/tui/event_bridge.go"})},
		{"子 agent read_file 结果", toolEnd(child(react.EventTypeToolEnd), "read_file", "call_rf_1", false, "func (b *EventBridge) EmitterFunc() ...")},
		{"子 agent 流式文本（应折叠）", stream(child(react.EventTypeStream), "在 event_bridge.go:29 找到了发射器。")},
		{"子 agent 再次思考（应折叠）", think(child(react.EventTypeThink), "g-sub-2", "确认根因：事件没有按 agent 过滤。")},

		{"sub_agent 工具结束（卡片变完成态）", toolEnd(root(react.EventTypeToolEnd), builtin_tools.SubAgentToolName, childCallID, true,
			"探查完成：事件经 EmitterFunc 全量下发，缺少按 agent 的过滤。")},
		{"root 总结流式文本", stream(root(react.EventTypeStream), "子 agent 已定位问题，下面是修复方案……")},
		{"root 结果", func() *react.AgentOutputEvent {
			ev := root(react.EventTypeResult)
			ev.Payload["result"] = "完成。"
			return ev
		}()},
	}
}

// MockTwoSubAgentScenario scripts two overlapping sub-agents to exercise the
// "running-only" right-side panel: agents A and B run concurrently (panel shows
// both), then A finishes and drops out of the panel (panel shows only B), while
// B is intentionally left running so the panel stays visible for manual review.
// Finished agent A remains reachable via its collapsed card in the main timeline.
//
// Returns the events plus the two children's (name, callID) so the harness can
// drive drill-in by call_id.
func MockTwoSubAgentScenario() (events []MockEvent, aName, aCallID, bName, bCallID string) {
	aCallID, aName = "call_aaa11111", "sub-call_aaa" // sub-<callID[:8]>
	bCallID, bName = "call_bbb22222", "sub-call_bbb"

	root := func(t react.EventType) *react.AgentOutputEvent {
		return &react.AgentOutputEvent{Type: t, AgentName: "", Payload: map[string]any{}}
	}
	agent := func(name string, t react.EventType) *react.AgentOutputEvent {
		return &react.AgentOutputEvent{Type: t, AgentName: name, Payload: map[string]any{}}
	}
	think := func(ev *react.AgentOutputEvent, group, text string) *react.AgentOutputEvent {
		ev.GroupID = group
		ev.Payload["think_content"] = text
		return ev
	}
	stream := func(ev *react.AgentOutputEvent, text string) *react.AgentOutputEvent {
		ev.Content = text
		return ev
	}
	toolStart := func(ev *react.AgentOutputEvent, name, callID string, isAgent bool, args map[string]any) *react.AgentOutputEvent {
		ev.Payload["tool_name"] = name
		ev.Payload["call_id"] = callID
		ev.Payload["is_agent"] = isAgent
		ev.Payload["arguments"] = args
		return ev
	}
	toolEnd := func(ev *react.AgentOutputEvent, name, callID string, isAgent bool, result string) *react.AgentOutputEvent {
		ev.Payload["tool_name"] = name
		ev.Payload["call_id"] = callID
		ev.Payload["is_agent"] = isAgent
		ev.Payload["result"] = result
		return ev
	}
	pi := func(id, step, status string) *builtin_tools.PlanItem {
		return &builtin_tools.PlanItem{ID: id, Step: step, Status: builtin_tools.PlanStepStatus(status)}
	}
	taskPlan := func(ev *react.AgentOutputEvent, explanation string, items ...*builtin_tools.PlanItem) *react.AgentOutputEvent {
		ev.Payload["explanation"] = explanation
		ev.Payload["plan"] = items
		return ev
	}
	taskItem := func(ev *react.AgentOutputEvent, id, step, status string) *react.AgentOutputEvent {
		ev.Payload["id"] = id
		ev.Payload["step"] = step
		ev.Payload["status"] = status
		return ev
	}

	// Root plan items; their IDs become the ParentStepID of A / B (captured at
	// spawn time from activeStepByAgent[root]), so A nests under root-1 and B
	// under root-2 in the sidebar Todo tree.
	events = []MockEvent{
		{"root 思考", think(root(react.EventTypeThink), "g-root-1", "我先派两个子 agent 并行探查。")},
		{"root 流式", stream(root(react.EventTypeStream), "派出 A、B 两个子 agent。")},
		{"root 计划（2 步）", taskPlan(root(react.EventTypeTaskPlan), "并行探查事件分流与连接状态机",
			pi("root-1", "派 A 扫描事件分流路径", "pending"),
			pi("root-2", "派 B 审计连接状态机", "pending"))},
		{"root-1 进行中（A 将挂到此步）", taskItem(root(react.EventTypeTaskItem), "root-1", "派 A 扫描事件分流路径", "in_progress")},

		{"派生 A（面板出现 A）", toolStart(root(react.EventTypeToolStart), builtin_tools.SubAgentToolName, aCallID, true,
			map[string]any{"description": "扫描事件分流路径"})},
		{"A 计划（3 步，挂到 root-1）", taskPlan(agent(aName, react.EventTypeTaskPlan), "定位并确认事件未按 agent 过滤",
			pi("a-1", "定位事件发射器", "pending"),
			pi("a-2", "读取 event_bridge.go", "pending"),
			pi("a-3", "确认根因", "pending"))},
		{"A-1 进行中", taskItem(agent(aName, react.EventTypeTaskItem), "a-1", "定位事件发射器", "in_progress")},
		{"A 思考（折叠）", think(agent(aName, react.EventTypeThink), "g-a-1", "我先定位事件发射器。")},
		{"A 调 rg（折叠）", toolStart(agent(aName, react.EventTypeToolStart), "rg", "call_rg_a", false,
			map[string]any{"pattern": "EmitterFunc"})},

		{"root-1 完成、root-2 进行中（B 将挂到此步）", taskItem(root(react.EventTypeTaskItem), "root-2", "派 B 审计连接状态机", "in_progress")},
		{"派生 B（面板出现 A、B 两项）", toolStart(root(react.EventTypeToolStart), builtin_tools.SubAgentToolName, bCallID, true,
			map[string]any{"description": "审计连接状态机"})},
		{"B 计划（2 步，挂到 root-2）", taskPlan(agent(bName, react.EventTypeTaskPlan), "审计状态机迁移完整性",
			pi("b-1", "查看 manager 状态迁移", "pending"),
			pi("b-2", "比对缺失的迁移边", "pending"))},
		{"B-1 进行中", taskItem(agent(bName, react.EventTypeTaskItem), "b-1", "查看 manager 状态迁移", "in_progress")},
		{"B 思考（折叠）", think(agent(bName, react.EventTypeThink), "g-b-1", "我先看 manager 的状态迁移。")},

		{"A rg 结果（折叠）", toolEnd(agent(aName, react.EventTypeToolEnd), "rg", "call_rg_a", false, "3 matches in event_bridge.go")},
		{"A-1 完成、A-2 进行中", taskItem(agent(aName, react.EventTypeTaskItem), "a-2", "读取 event_bridge.go", "in_progress")},
		{"A 调 read_file（折叠）", toolStart(agent(aName, react.EventTypeToolStart), "read_file", "call_rf_a", false,
			map[string]any{"path": "event_bridge.go"})},
		{"B 调 rg（折叠）", toolStart(agent(bName, react.EventTypeToolStart), "rg", "call_rg_b", false,
			map[string]any{"pattern": "StatusTransition"})},
		{"A read_file 结果（折叠）", toolEnd(agent(aName, react.EventTypeToolEnd), "read_file", "call_rf_a", false, "func EmitterFunc() ...")},
		{"A 流式（折叠）", stream(agent(aName, react.EventTypeStream), "在 event_bridge.go:29 找到发射器。")},
		{"A-2 完成、A-3 进行中", taskItem(agent(aName, react.EventTypeTaskItem), "a-3", "确认根因", "in_progress")},
		{"A 思考2（折叠）", think(agent(aName, react.EventTypeThink), "g-a-2", "确认根因：事件未按 agent 过滤。")},
		{"A-3 完成", taskItem(agent(aName, react.EventTypeTaskItem), "a-3", "确认根因", "completed")},

		{"A 结束（A 从面板消失，仅剩 B）", toolEnd(root(react.EventTypeToolEnd), builtin_tools.SubAgentToolName, aCallID, true,
			"探查完成：事件缺少按 agent 的过滤。")},

		{"B rg 结果（折叠）", toolEnd(agent(bName, react.EventTypeToolEnd), "rg", "call_rg_b", false, "5 matches in manager.go")},
		{"B-1 完成、B-2 进行中", taskItem(agent(bName, react.EventTypeTaskItem), "b-2", "比对缺失的迁移边", "in_progress")},
		{"B 思考2（折叠）", think(agent(bName, react.EventTypeThink), "g-b-2", "状态机缺少 error→reconnect 边。")},
		{"B 流式（折叠，B 持续运行）", stream(agent(bName, react.EventTypeStream), "正在比对 manager.go 的状态迁移……")},
		// B 故意不发 tool_end：保持 running，面板常驻供 review。
	}
	return events, aName, aCallID, bName, bCallID
}

// MockMainAgentScenario scripts a pure root-agent run (no sub-agents) that mirrors
// the shape of a real deep-analysis session: the root agent thinks, calls a couple
// of plain tools (read_file / list_files), streams a reply, then returns a result.
//
// rootName is the AgentName the root agent emits its events under (e.g.
// "code-audit"). It exists to reproduce the main-timeline attribution bug: when
// the ChatModel's rootAgentName does not equal rootName, filterMainParts/isRootAgent
// drops every root part and the main agent shows nothing — even though events flow.
func MockMainAgentScenario(rootName string) []MockEvent {
	root := func(t react.EventType) *react.AgentOutputEvent {
		return &react.AgentOutputEvent{Type: t, AgentName: rootName, Payload: map[string]any{}}
	}
	think := func(ev *react.AgentOutputEvent, group, text string) *react.AgentOutputEvent {
		ev.GroupID = group
		ev.Payload["think_content"] = text
		return ev
	}
	stream := func(ev *react.AgentOutputEvent, text string) *react.AgentOutputEvent {
		ev.Content = text
		return ev
	}
	toolStart := func(ev *react.AgentOutputEvent, name, callID string, args map[string]any) *react.AgentOutputEvent {
		ev.Payload["tool_name"] = name
		ev.Payload["call_id"] = callID
		ev.Payload["is_agent"] = false
		ev.Payload["arguments"] = args
		return ev
	}
	toolEnd := func(ev *react.AgentOutputEvent, name, callID, result string) *react.AgentOutputEvent {
		ev.Payload["tool_name"] = name
		ev.Payload["call_id"] = callID
		ev.Payload["is_agent"] = false
		ev.Payload["result"] = result
		return ev
	}

	return []MockEvent{
		{"主 agent 思考（应在主时间线显示）", think(root(react.EventTypeThink), "g-main-1", "先了解项目结构与技术栈，规划如何排查。")},
		{"主 agent 调用 list_files（应显示）", toolStart(root(react.EventTypeToolStart), "list_files", "call_lf_1",
			map[string]any{"path": "/repo", "max_depth": 2})},
		{"list_files 结果", toolEnd(root(react.EventTypeToolEnd), "list_files", "call_lf_1", "code/  pom.xml  README.md")},
		{"主 agent 调用 read_file（应显示）", toolStart(root(react.EventTypeToolStart), "read_file", "call_rf_1",
			map[string]any{"path": "/repo/README.md"})},
		{"read_file 结果", toolEnd(root(react.EventTypeToolEnd), "read_file", "call_rf_1", "# 项目说明\nJava 企业级应用……")},
		{"主 agent 流式返回（应显示）", stream(root(react.EventTypeStream), "项目是一个 Java 企业级应用，下面给出分析结论……")},
		{"主 agent 结果", func() *react.AgentOutputEvent {
			ev := root(react.EventTypeResult)
			ev.Payload["result"] = "分析完成。"
			return ev
		}()},
	}
}

// MockScenarioJSON is a tiny convenience for debugging the scenario payloads.
func MockScenarioJSON(events []MockEvent) string {
	b, _ := json.MarshalIndent(events, "", "  ")
	return string(b)
}
