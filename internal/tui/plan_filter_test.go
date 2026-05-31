package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

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

// Regression: sibling sub-agents spawned under the same root step share a
// parent_step_id ("2"), and each sub-agent numbers its own items locally, so an
// item id ("2") collides with that shared step id. Grouping child plans by step
// id alone made flattenPlan pull sibling sub-agents into each other, producing a
// cascade of duplicated, ever-more-indented [sub-call_…] rows. Keying on
// (parent agent, step) must keep siblings flat and isolated.
func TestSidebarTodoNoCrossAgentStepCollision(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "root"
	m.chat.agentSpawnByCallID["call_aaa1234"] = agentSpawnInfo{ParentAgent: "root", ParentStepID: "2", CallID: "call_aaa1234", SubScheme: true}
	m.chat.agentSpawnByCallID["call_bbb1234"] = agentSpawnInfo{ParentAgent: "root", ParentStepID: "2", CallID: "call_bbb1234", SubScheme: true}

	// Root item id "1" deliberately collides with each child's item id "1"; only
	// the differing text must keep it from being deduped away.
	rootPlan := &PlanPart{AgentName: "root", Items: []PlanItemView{
		{ID: "1", Step: "派发"},
		{ID: "2", Step: "执行扫描"},
	}}
	// Both siblings: ParentAgent="root", ParentStepID="2", and a colliding item id "2".
	planA := &PlanPart{AgentName: "sub-call_aaa", ParentAgent: "root", ParentStepID: "2", Items: []PlanItemView{
		{ID: "1", Step: "扫描A"},
		{ID: "2", Step: "解析A"},
	}}
	planB := &PlanPart{AgentName: "sub-call_bbb", ParentAgent: "root", ParentStepID: "2", Items: []PlanItemView{
		{ID: "1", Step: "扫描B"},
		{ID: "2", Step: "解析B"},
	}}
	m.chat.parts = []DisplayPart{
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_aaa1234", Status: "running"}},
		{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: "call_bbb1234", Status: "running"}},
		{Type: PartTypePlan, Plan: rootPlan},
		{Type: PartTypePlan, Plan: planA},
		{Type: PartTypePlan, Plan: planB},
	}

	// Main view: root(2) + A(2) + B(2) = 6, no bloat, nesting at most one level deep.
	mainSnap := m.buildSidebarSnapshot()
	if len(mainSnap.PlanItems) != 6 {
		t.Fatalf("main view PlanItems = %d, want 6 (root + A + B, no cascade)", len(mainSnap.PlanItems))
	}
	sawRootDispatch := false
	for _, it := range mainSnap.PlanItems {
		if it.Depth > 1 {
			t.Fatalf("main view item %q has Depth %d, want <=1 (no staircase)", it.Step, it.Depth)
		}
		if it.Step == "派发" {
			sawRootDispatch = true
		}
	}
	// Root step "派发" (id "1") must survive even though child items also use id "1".
	if !sawRootDispatch {
		t.Fatalf("root step 派发 was dropped — dedup must match on text, not on colliding item id")
	}

	// Drill into A: only A's own two items, flat (Depth 0), no sibling B, no dup.
	m.chat.viewingChild = "call_aaa1234"
	childSnap := m.buildSidebarSnapshot()
	if len(childSnap.PlanItems) != 2 {
		t.Fatalf("drill-in PlanItems = %d, want 2 (only A's items)", len(childSnap.PlanItems))
	}
	for _, it := range childSnap.PlanItems {
		if it.Step != "扫描A" && it.Step != "解析A" {
			t.Fatalf("drill-in leaked non-A item %q", it.Step)
		}
		if it.Depth != 0 {
			t.Fatalf("drill-in item %q has Depth %d, want 0 (no escalating indent)", it.Step, it.Depth)
		}
	}
}

// Visual: renders the actual sidebar Todo panel for a realistic scenario (root
// step 2 spawns 3 sibling sub-agents, each numbering its items 1/2 — exactly the
// id-collision that produced the [sub-call_…] staircase). Prints both the main
// timeline and a drilled-in view so the rendered output can be eyeballed.
// Run with: go test ./internal/tui/ -run TestRenderTodoVisual -v
func TestRenderTodoVisual(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii) // strip ANSI so the dump is readable

	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "code-audit"

	type sib struct {
		callID, agent string
		s1, s2        string
	}
	sibs := []sib{
		{"call_aaa1234", "sub-call_aaa", "执行 semgrep 扫描命令 (A)", "解析 sast-tm.json (A)"},
		{"call_bbb1234", "sub-call_bbb", "执行 semgrep 扫描命令 (B)", "解析 sast-tm.json (B)"},
		{"call_ccc1234", "sub-call_ccc", "执行 semgrep 扫描命令 (C)", "解析 sast-tm.json (C)"},
	}

	rootPlan := &PlanPart{AgentName: "code-audit", Items: []PlanItemView{
		{ID: "1", Step: "梳理目标范围", Status: "completed"},
		{ID: "2", Step: "并行派发子 agent 扫描", Status: "in_progress"},
		{ID: "3", Step: "汇总并生成报告"},
	}}
	parts := []DisplayPart{{Type: PartTypePlan, Plan: rootPlan}}
	for _, s := range sibs {
		m.chat.agentSpawnByCallID[s.callID] = agentSpawnInfo{ParentAgent: "code-audit", ParentStepID: "2", CallID: s.callID, SubScheme: true}
		parts = append(parts,
			DisplayPart{Type: PartTypeSubAgent, SubAgent: &SubAgentPart{AgentName: "sub_agent", CallID: s.callID, Status: "running"}},
			DisplayPart{Type: PartTypePlan, Plan: &PlanPart{
				AgentName: s.agent, ParentAgent: "code-audit", ParentStepID: "2",
				Items: []PlanItemView{
					{ID: "1", Step: s.s1, Status: "completed"},
					{ID: "2", Step: s.s2, Status: "in_progress"},
				},
			}},
		)
	}
	m.chat.parts = parts

	render := func(snap SidebarSnapshot) string {
		sb := &strings.Builder{}
		sideModel := NewSidebarModel()
		sideModel.snapshot = snap
		sideModel.renderTodoSection(sb, 48)
		return strings.TrimRight(sb.String(), "\n")
	}

	t.Log("\n===== 主时间线（root + 3 个兄弟子 agent，应平铺、最多一层缩进）=====\n" +
		render(m.buildSidebarSnapshot()))

	m.chat.viewingChild = "call_aaa1234"
	t.Log("\n===== 下钻进 sub-call_aaa（应只显示 A 自己的 2 个待办，无瀑布）=====\n" +
		render(m.buildSidebarSnapshot()))
}

// Regression for the reload path: a legacy session has no parent_agent persisted
// on child plans AND a freshly loaded ChatModel has an empty agentSpawnByCallID
// (only the live tool-start handler populates it). SetParts must rebuild the
// spawn map from the loaded sub-agent ToolParts so parent resolution — and thus
// the composite-key grouping and drill-in — works on reopened sessions too.
func TestSidebarTodoRebuildsSpawnMapOnLoad(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "code-audit"

	rootPlan := &PlanPart{AgentName: "code-audit", Items: []PlanItemView{
		{ID: "1", Step: "梳理"},
		{ID: "2", Step: "并行派发"},
	}}
	// Legacy child plans: ParentAgent deliberately empty (pre-fix persisted data).
	planA := &PlanPart{AgentName: "sub-call_aaa", ParentStepID: "2", Items: []PlanItemView{
		{ID: "1", Step: "扫描A"},
		{ID: "2", Step: "解析A"},
	}}
	planB := &PlanPart{AgentName: "sub-call_bbb", ParentStepID: "2", Items: []PlanItemView{
		{ID: "1", Step: "扫描B"},
		{ID: "2", Step: "解析B"},
	}}
	// Loaded parts include the spawning ToolParts (IsAgent + parent AgentName),
	// exactly as display_parts.jsonl round-trips them.
	parts := []DisplayPart{
		{Type: PartTypePlan, Plan: rootPlan},
		{Type: PartTypeTool, Tool: &ToolPart{Name: "sub_agent", CallID: "call_aaa1234", AgentName: "code-audit", IsAgent: true, State: "completed"}},
		{Type: PartTypeTool, Tool: &ToolPart{Name: "sub_agent", CallID: "call_bbb1234", AgentName: "code-audit", IsAgent: true, State: "completed"}},
		{Type: PartTypePlan, Plan: planA},
		{Type: PartTypePlan, Plan: planB},
	}

	// Go through the real load chokepoint; do NOT pre-populate agentSpawnByCallID.
	m.chat.SetParts(parts)

	for _, id := range []string{"call_aaa1234", "call_bbb1234"} {
		info, ok := m.chat.agentSpawnByCallID[id]
		if !ok {
			t.Fatalf("SetParts did not rebuild spawn entry for %q", id)
		}
		if info.ParentAgent != "code-audit" {
			t.Fatalf("rebuilt spawn %q ParentAgent = %q, want code-audit", id, info.ParentAgent)
		}
	}

	mainSnap := m.buildSidebarSnapshot()
	if len(mainSnap.PlanItems) != 6 {
		t.Fatalf("main view PlanItems = %d, want 6 (root + A + B, no cascade)", len(mainSnap.PlanItems))
	}
	for _, it := range mainSnap.PlanItems {
		if it.Depth > 1 {
			t.Fatalf("main view item %q has Depth %d, want <=1 (no staircase on reload)", it.Step, it.Depth)
		}
	}

	m.chat.viewingChild = "call_aaa1234"
	childSnap := m.buildSidebarSnapshot()
	if len(childSnap.PlanItems) != 2 {
		t.Fatalf("drill-in PlanItems = %d, want 2 (only A's items)", len(childSnap.PlanItems))
	}
	for _, it := range childSnap.PlanItems {
		if it.Step != "扫描A" && it.Step != "解析A" {
			t.Fatalf("drill-in leaked non-A item %q", it.Step)
		}
		if it.Depth != 0 {
			t.Fatalf("drill-in item %q has Depth %d, want 0", it.Step, it.Depth)
		}
	}
}

// Orphan child plans (whose ParentStepID matches no root item) are appended after
// the root tree. Their order must be stable (by agent name) regardless of the
// random map iteration order, so the sidebar doesn't reshuffle between renders.
func TestSidebarTodoOrphanOrderDeterministic(t *testing.T) {
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "code-audit"

	rootPlan := &PlanPart{AgentName: "code-audit", Items: []PlanItemView{
		{ID: "1", Step: "根步骤"},
	}}
	// ParentStepID "99" matches no root item → both stay unattached → orphans.
	orphanB := &PlanPart{AgentName: "sub-call_bbb", ParentAgent: "code-audit", ParentStepID: "99", Items: []PlanItemView{
		{ID: "1", Step: "孤儿B"},
	}}
	orphanA := &PlanPart{AgentName: "sub-call_aaa", ParentAgent: "code-audit", ParentStepID: "99", Items: []PlanItemView{
		{ID: "1", Step: "孤儿A"},
	}}
	m.chat.parts = []DisplayPart{
		{Type: PartTypePlan, Plan: rootPlan},
		{Type: PartTypePlan, Plan: orphanB},
		{Type: PartTypePlan, Plan: orphanA},
	}

	// Run several times: map order is randomized per iteration, so a flaky
	// (unsorted) implementation would eventually reorder the orphans.
	for i := 0; i < 20; i++ {
		snap := m.buildSidebarSnapshot()
		var orphanSteps []string
		for _, it := range snap.PlanItems {
			if it.Step == "孤儿A" || it.Step == "孤儿B" {
				orphanSteps = append(orphanSteps, it.Step)
			}
		}
		if len(orphanSteps) != 2 || orphanSteps[0] != "孤儿A" || orphanSteps[1] != "孤儿B" {
			t.Fatalf("orphan order = %v, want [孤儿A 孤儿B] (sorted by agent name)", orphanSteps)
		}
	}
}

// legacyBuildTodo faithfully replicates the PRE-FIX buildSidebarSnapshot Todo
// algorithm so the demo test below can render old-vs-new on identical input:
//   - child plans grouped by ParentStepID ALONE (no parent-agent dimension)
//   - dedup drops a childless item if its id OR text matches any child item
//   - the drilled-in plan is NOT pre-marked visited
// These are exactly the three behaviors the fix changed.
func legacyBuildTodo(m *Model, viewingChild string) []PlanItemView {
	latestPlans := map[string]*PlanPart{}
	for _, p := range m.chat.Parts() {
		if p.Type == PartTypePlan && p.Plan != nil {
			latestPlans[p.Plan.AgentName] = p.Plan
		}
	}
	childrenByParentStep := map[string][]*PlanPart{}
	var rootPlan *PlanPart
	for _, plan := range latestPlans {
		if m.chat.isRootAgentPlan(plan) {
			rootPlan = plan
			continue
		}
		childrenByParentStep[plan.ParentStepID] = append(childrenByParentStep[plan.ParentStepID], plan)
	}
	childItemIDs := map[string]bool{}
	childStepNorm := map[string]bool{}
	for _, children := range childrenByParentStep {
		for _, cp := range children {
			for _, it := range cp.Items {
				if it.ID != "" {
					childItemIDs[it.ID] = true
				}
				childStepNorm[normalizeStepText(it.Step)] = true
			}
		}
	}
	var out []PlanItemView
	visited := map[string]bool{}
	var flatten func(plan *PlanPart, depth int, dedup bool)
	flatten = func(plan *PlanPart, depth int, dedup bool) {
		label := ""
		if depth > 0 {
			label = plan.AgentName
		}
		for _, item := range plan.Items {
			children := childrenByParentStep[item.ID]
			if dedup && len(children) == 0 {
				if childItemIDs[item.ID] || childStepNorm[normalizeStepText(item.Step)] {
					continue
				}
			}
			item.Depth = depth
			item.AgentName = label
			out = append(out, item)
			for _, cp := range children {
				if !visited[cp.AgentName] {
					visited[cp.AgentName] = true
					flatten(cp, depth+1, false)
				}
			}
		}
	}
	if viewingChild != "" {
		if cp := m.chat.PlanForChild(viewingChild); cp != nil {
			flatten(cp, 0, false) // old: did NOT mark visited first
		}
	} else if rootPlan != nil {
		flatten(rootPlan, 0, true)
		for _, plan := range childrenByParentStep[""] {
			if !visited[plan.AgentName] {
				visited[plan.AgentName] = true
				flatten(plan, 1, false)
			}
		}
	}
	return out
}

// TestTodoFixesBeforeAfter prints a side-by-side of the OLD (legacyBuildTodo) and
// NEW (real buildSidebarSnapshot) Todo panels on identical input, so a human can
// see exactly what each fix changed. Run with:
//   go test ./internal/tui/ -run TestTodoFixesBeforeAfter -v
func TestTodoFixesBeforeAfter(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii) // strip ANSI so the dump is readable

	renderItems := func(items []PlanItemView) string {
		sb := &strings.Builder{}
		sm := NewSidebarModel()
		sm.snapshot = SidebarSnapshot{PlanItems: items}
		sm.renderTodoSection(sb, 56)
		s := strings.TrimRight(sb.String(), "\n")
		if s == "" {
			return "  (空)"
		}
		return s
	}
	sideBySide := func(title, why, before, after string) {
		t.Logf("\n"+
			"╔════════════════════════════════════════════════════╗\n"+
			"  %s\n"+
			"  说明：%s\n"+
			"╚════════════════════════════════════════════════════╝\n"+
			"── 修复前（旧逻辑）──\n%s\n\n"+
			"── 修复后（当前代码）──\n%s",
			title, why, before, after)
	}

	// ===== 场景一：兄弟子 agent 同步骤撞号 =====
	// 根第 2 步派生 3 个子 agent，每个子 agent 自己的 item 又编号 1/2；
	// 根第 1 步 id 也是 "1"，但文本与子 item 不同。
	m := NewModel(ModelDeps{})
	m.chat.rootAgentName = "code-audit"
	sibs := []struct {
		callID, agent, s1, s2 string
	}{
		{"call_aaa1234", "sub-call_aaa", "执行 semgrep (A)", "解析 json (A)"},
		{"call_bbb1234", "sub-call_bbb", "执行 semgrep (B)", "解析 json (B)"},
		{"call_ccc1234", "sub-call_ccc", "执行 semgrep (C)", "解析 json (C)"},
	}
	rootPlan := &PlanPart{AgentName: "code-audit", Items: []PlanItemView{
		{ID: "1", Step: "梳理目标范围", Status: "completed"}, // id 撞子 item，但文本不同
		{ID: "2", Step: "并行派发子 agent", Status: "in_progress"},
		{ID: "3", Step: "汇总生成报告"},
	}}
	parts := []DisplayPart{{Type: PartTypePlan, Plan: rootPlan}}
	for _, s := range sibs {
		m.chat.agentSpawnByCallID[s.callID] = agentSpawnInfo{ParentAgent: "code-audit", ParentStepID: "2", CallID: s.callID, SubScheme: true}
		parts = append(parts, DisplayPart{Type: PartTypePlan, Plan: &PlanPart{
			AgentName: s.agent, ParentAgent: "code-audit", ParentStepID: "2",
			Items: []PlanItemView{
				{ID: "1", Step: s.s1, Status: "completed"},
				{ID: "2", Step: s.s2, Status: "in_progress"},
			},
		}})
	}
	m.chat.parts = parts

	oldMain := legacyBuildTodo(&m, "")
	newMain := m.buildSidebarSnapshot().PlanItems
	sideBySide(
		"场景一·主视图：根步骤被去重误删",
		"旧逻辑按 item id 去重，根第1步「梳理目标范围」(id=1) 撞上子 agent 的 item id 1 → 被误删；新逻辑只按文本去重，保留。",
		renderItems(oldMain), renderItems(newMain),
	)

	oldDrill := legacyBuildTodo(&m, "call_aaa1234")
	m.chat.viewingChild = "call_aaa1234"
	newDrill := m.buildSidebarSnapshot().PlanItems
	sideBySide(
		"场景一·下钻 sub-call_aaa：[sub-call_…] 嵌套瀑布",
		"旧逻辑仅按步骤号 \"2\" 分组，3 个兄弟撞号 → 互相递归出逐级缩进+重复；新逻辑用 (父agent,步骤) 复合键 → 只剩 A 自己 2 条。",
		renderItems(oldDrill), renderItems(newDrill),
	)
	t.Logf("\n[计数] 主视图  旧=%d 条 / 新=%d 条 ；下钻  旧=%d 条 / 新=%d 条",
		len(oldMain), len(newMain), len(oldDrill), len(newDrill))

	// ===== 场景二：重开旧会话（spawn map 重建）=====
	// 旧会话持久化里子 plan 没有 parent_agent；加载时 agentSpawnByCallID 为空。
	m2 := NewModel(ModelDeps{})
	m2.chat.rootAgentName = "code-audit"
	root2 := &PlanPart{AgentName: "code-audit", Items: []PlanItemView{{ID: "1", Step: "梳理"}, {ID: "2", Step: "派发"}}}
	legacyParts := []DisplayPart{
		{Type: PartTypePlan, Plan: root2},
		{Type: PartTypeTool, Tool: &ToolPart{Name: "sub_agent", CallID: "call_aaa1234", AgentName: "code-audit", IsAgent: true, State: "completed"}},
		{Type: PartTypeTool, Tool: &ToolPart{Name: "sub_agent", CallID: "call_bbb1234", AgentName: "code-audit", IsAgent: true, State: "completed"}},
		{Type: PartTypePlan, Plan: &PlanPart{AgentName: "sub-call_aaa", ParentStepID: "2", Items: []PlanItemView{{ID: "1", Step: "扫描A"}, {ID: "2", Step: "解析A"}}}},
		{Type: PartTypePlan, Plan: &PlanPart{AgentName: "sub-call_bbb", ParentStepID: "2", Items: []PlanItemView{{ID: "1", Step: "扫描B"}, {ID: "2", Step: "解析B"}}}},
	}

	// 修复前：直接塞 parts（绕过重建）→ spawn map 空 → 无法下钻（PlanForChild 返回 nil）。
	m2.chat.parts = legacyParts
	beforeDrill := "  (下钻失败：PlanForChild 解析不到子 agent，面板为空)"
	if cp := m2.chat.PlanForChild("call_aaa1234"); cp != nil {
		beforeDrill = renderItems(legacyBuildTodo(&m2, "call_aaa1234"))
	}

	// 修复后：走 SetParts → 从 ToolPart 重建 spawn map → 能下钻且分组正确。
	m3 := NewModel(ModelDeps{})
	m3.chat.rootAgentName = "code-audit"
	m3.chat.SetParts(legacyParts)
	m3.chat.viewingChild = "call_aaa1234"
	afterDrill := renderItems(m3.buildSidebarSnapshot().PlanItems)
	_, rebuilt := m3.chat.agentSpawnByCallID["call_aaa1234"]
	sideBySide(
		"场景二·重开旧会话后下钻 sub-call_aaa",
		"加载时 agentSpawnByCallID 为空；旧逻辑下根本无法下钻。SetParts 现在从子 agent 的 ToolPart 重建 spawn map，下钻恢复且 Todo 正确。",
		beforeDrill, afterDrill,
	)
	t.Logf("\n[校验] SetParts 重建 spawn 条目 call_aaa1234 = %v", rebuilt)
}
