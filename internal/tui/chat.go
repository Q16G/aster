package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aster/internal/builtin_tools"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type agentSpawnInfo struct {
	ParentAgent  string
	ParentStepID string
	CallID       string
	// SubScheme is true when the child was spawned by the sub_agent tool
	// ("sub-<callID[:8]>") and false for skill forks ("skill-<name>-<callID[:6]>").
	// It disambiguates the call_id join when two children share a callID prefix.
	SubScheme bool
}

type ChatModel struct {
	viewport viewport.Model
	// store owns the authoritative timeline state: the part slice, the
	// streaming buffers, and the sub-agent spawn metadata. ChatModel keeps
	// only view/UI concerns (cursor, expansion, dirty flag, cached render).
	store *PartsStore
	// renderer owns the per-part fragment cache that turns store contents
	// into the timeline string. refreshContent drains the store's dirty set
	// into the renderer and pulls cached fragments instead of re-rendering
	// every part each frame.
	renderer *Renderer

	// thinkingByKey 把活体 thinking buffer 按 (agentName, stepID) 复合键隔离——
	// 同一 agentName 下 main（stepID=""）和多个 peer（stepID="p1" 等）各自独立 buffer。
	// 旧设计单 agentName 单 buffer 导致 peer 的 reasoning content 渗入 root thinking
	// 流，主屏幕上 InlineStep 卡片下方仍能看到 `| Thinking: ...` 大段内容（截图症状）。
	thinkingByKey map[thinkBufKey]*thinkingState
	thinkingOrder []thinkBufKey
	width         int
	height        int
	// 展开/折叠态已迁到 ExpandableCardPart 接口字段（Part.<X>.IsExpanded）；
	// 旧的 `m.toolExpanded[idx int]` 全局表已删除——idx 在 part 列表前插
	// （concurrent peer / sub_agent reflow）时会漂移，导致展开态错位（FR3 根因）。
	cursor           int
	focused          bool
	partLineOffsets  []int
	contentDirty     bool
	autoFollowBottom bool
	fullContent      string
	rootAgentName    string
	// viewingChild is the call_id of the sub-agent whose transcript currently
	// replaces the main timeline in-place ("" = showing the main timeline).
	viewingChild string
	// viewingStepID 是 inline_step 详情态（变体 C「全屏 detail view」）下查看的
	// peer stepID。"" 表示非详情态。与 viewingChild 互斥（见 EnterStepDetail
	// 实现的 viewingChild 守卫）——sub-agent transcript 内部的并发 step 走
	// sub-agent 自己的渲染机制，不通过本字段。
	viewingStepID string

	activeStepByAgent map[string]string
}

func NewChatModel() ChatModel {
	vp := viewport.New(0, 0)
	vp.SetContent("")
	return ChatModel{
		viewport:          vp,
		store:             NewPartsStore(),
		renderer:          NewRenderer(),
		thinkingByKey:     make(map[thinkBufKey]*thinkingState),
		autoFollowBottom:  true,
		activeStepByAgent: make(map[string]string),
	}
}

// setCursor moves the selection to idx and invalidates both the old and new
// selection's cached fragments so the Renderer re-emits them with the right
// styling on the next pass. Direct field assignment to m.cursor is forbidden
// outside this helper because it would silently keep stale rendered fragments
// for the deselected/selected parts.
func (m *ChatModel) setCursor(idx int) {
	if m.cursor == idx {
		return
	}
	if m.cursor >= 0 && m.cursor < m.store.Len() {
		m.store.MarkDirty(m.store.At(m.cursor).ID)
	}
	if idx >= 0 && idx < m.store.Len() {
		m.store.MarkDirty(m.store.At(idx).ID)
	}
	m.cursor = idx
}

// setFocused toggles focus and invalidates the currently-selected part, since
// the focused flag flips its rendered styling. Same rationale as setCursor.
func (m *ChatModel) setFocused(v bool) {
	if m.focused == v {
		return
	}
	if m.cursor >= 0 && m.cursor < m.store.Len() {
		m.store.MarkDirty(m.store.At(m.cursor).ID)
	}
	m.focused = v
}

// setCardExpanded 把指定 idx 的卡片切换展开/折叠并打 dirty 让 Renderer 重画。
// 调用方负责确保 idx 指向一个 ExpandableCardPart 类型；非可展开 part 直接 no-op。
//
// 把展开态从 `m.toolExpanded[idx int]` 全局表迁到 part 实体字段后（FR3 根因
// 修复）：状态不再因 idx 漂移失效；新卡片类型只需实现 ExpandableCardPart 接口
// 即可获得 enter/space 切换、SetAll 恢复、expandAll 等所有能力——编译器兜底。
func (m *ChatModel) setCardExpanded(idx int, v bool) bool {
	if idx < 0 || idx >= m.store.Len() {
		return false
	}
	part := m.store.At(idx)
	card, ok := part.AsExpandable()
	if !ok {
		return false
	}
	if card.Expanded() == v {
		return false
	}
	card.SetExpanded(v)
	m.store.MarkDirty(part.ID)
	return true
}

func (m *ChatModel) SetSize(w, h int) {
	if m.width == w && m.height == h {
		return
	}
	m.width = w
	m.height = h
	m.viewport.Width = w
	m.viewport.Height = h
	m.refreshContent()
}

// AddPart 追加 part 并返回新 part 的 store 下标。
//
// **不再无条件 setCursor 到新 part**（方案 C，commit 后修复 P6）：旧设计每次都把
// cursor 强设到新 idx，导致不可见的 InlineStep / 子 part 也成 cursor 目标。一旦
// enter/space 在不可见 cursor 上触发，scrollToCursor 用 partLineOffsets[idx]=0
// 的 sentinel 默认值就把 viewport 拽到顶部，autoFollowBottom 反复被打断。
//
// 新设计：AddPart 是「哑 append」，cursor 不动；autoFollowBottom 维持时 GotoBottom。
// 调用方需要把 cursor 跟到最新 part 时显式调 FocusLastVisiblePart()。
//
// 自动展开（StepResult 等 shouldAutoExpandPart=true 类型）仍然在 part 实体上设置
// Expanded=true——这跟 cursor 无关，纯粹是新 part 默认渲染状态。
func (m *ChatModel) AddPart(part DisplayPart) int {
	if part.Time.IsZero() {
		part.Time = time.Now()
	}
	m.store.Append(part)
	idx := m.store.Len() - 1
	part = m.store.At(idx)
	if shouldAutoExpandPart(part.Type) {
		if card, ok := part.AsExpandable(); ok {
			card.SetExpanded(true)
		}
	}
	m.refreshContent()
	if m.autoFollowBottom {
		m.viewport.GotoBottom()
	}
	m.syncAutoFollowFromViewport()
	return idx
}

// FocusLastVisiblePart 把 cursor 移到主时间线最后一个 mainVisible 的 part。
// 调用方语义：「我刚加了一个用户可感知的 part，希望 cursor 跟随它（或它之前的可见 part）」。
// 没有可见 part 时 cursor 置 -1（无效），不影响 viewport。
//
// 与原 AddPart 自动 setCursor 的关键差异：永远跳到 **mainVisible** 的 idx——
// 即使最新 part 是 InlineStep（不进主流）也不会把 cursor 卡在那。
func (m *ChatModel) FocusLastVisiblePart() {
	for j := m.store.Len() - 1; j >= 0; j-- {
		if m.mainVisible(j) {
			m.setCursor(j)
			return
		}
	}
	m.setCursor(-1)
}

// allocPartLineOffsets 分配 partLineOffsets 切片，所有位置预填 sentinel -1。
// 渲染阶段把可见 part 的 idx 写入真实 lineCount，scrollToCursor 见 -1 直接 noop。
// 解决「idx=0 既表示首行又表示未布局」二义性（方案 C P6 根治）。
func allocPartLineOffsets(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = -1
	}
	return out
}

func (m *ChatModel) streamBuilder(agentName, stepID string) *strings.Builder {
	return m.store.StreamingBuilder(agentName, stepID)
}

func (m *ChatModel) dropStreamBuilder(agentName, stepID string) {
	m.store.StreamingDrop(agentName, stepID)
}

// StreamingAgents 返回当前活跃流 buffer 的 (agent, stepID) 键列表。
// 调用方按需筛 stepID==""（仅主路径）或 agent==某个 root agent 等。
func (m *ChatModel) StreamingAgents() []streamKey {
	return m.store.StreamingOrder()
}

func (m *ChatModel) hasStreamingContent() bool {
	return m.store.StreamingHasContent()
}

func (m *ChatModel) StreamContent(agentName, stepID string) string {
	if b, ok := m.store.StreamingLookup(agentName, stepID); ok {
		return b.String()
	}
	return ""
}

// AppendStream 把流式 token 追加到 (agentName, stepID) 对应 buffer。
// stepID="" 表示主路径流；stepID="p1" 等表示 peer 流——两者独立 buffer 互不串扰。
func (m *ChatModel) AppendStream(agentName, delta, stepID string) {
	m.streamBuilder(agentName, stepID).WriteString(delta)
	m.markDirty()
}

// FlushStream 把 (agentName, stepID) 当前 buffer 落成 TextPart 后释放 buffer。
// TextPart.StepID 字段携带 stepID 让 parts_store.idxByStepID 把该 TextPart 归到
// 对应 InlineStepPart 卡片子序列。
func (m *ChatModel) FlushStream(agentName, stepID string) bool {
	flushed := false
	if b, ok := m.store.StreamingLookup(agentName, stepID); ok && b.Len() > 0 {
		m.store.Append(DisplayPart{
			Type: PartTypeText,
			Time: time.Now(),
			Text: &TextPart{Content: b.String(), AgentName: agentName, StepID: stepID},
		})
		flushed = true
	}
	m.dropStreamBuilder(agentName, stepID)
	m.markDirty()
	return flushed
}

// thinkBufKey 是 thinking buffer 的复合键。同一 agentName 下 main 路径（stepID=""）
// 与多个 peer（stepID="p1" 等）必须独立 buffer，否则 peer 的 reasoning delta 会和
// 主路径混在同一 buffer 里 → rootThinkingState 把 peer 内容也当 root 显示在主屏幕
// renderThinkingStream 里（截图症状）。与 streamKey 模式对齐。
type thinkBufKey struct {
	agent  string
	stepID string
}

// thinkingState is one (agent, stepID) bucket's in-progress thinking buffer.
// stepID 已经在 key 里，不再作字段——避免「首次冻结」陷阱（旧版 thinkingState.stepID
// 字段 once set never changes，混入后无法纠正归属）。
type thinkingState struct {
	buf     strings.Builder
	groupID string
}

func (m *ChatModel) thinkingStateFor(agentName, stepID string) *thinkingState {
	k := thinkBufKey{agent: agentName, stepID: stepID}
	s, ok := m.thinkingByKey[k]
	if !ok {
		s = &thinkingState{}
		m.thinkingByKey[k] = s
		m.thinkingOrder = append(m.thinkingOrder, k)
	}
	return s
}

func (m *ChatModel) dropThinking(agentName, stepID string) {
	k := thinkBufKey{agent: agentName, stepID: stepID}
	if _, ok := m.thinkingByKey[k]; !ok {
		return
	}
	delete(m.thinkingByKey, k)
	for i, key := range m.thinkingOrder {
		if key == k {
			m.thinkingOrder = append(m.thinkingOrder[:i], m.thinkingOrder[i+1:]...)
			break
		}
	}
}

func (m *ChatModel) anyThinking() bool {
	for _, s := range m.thinkingByKey {
		if s.buf.Len() > 0 {
			return true
		}
	}
	return false
}

func (m *ChatModel) isRootAgent(name string) bool {
	return name == m.rootAgentName || name == ""
}

// rootThinkingState returns the first **main path** (stepID="") root-agent thinking
// buffer with pending content, or nil. peer thinking（stepID!=""）的活体内容被显式
// 排除——它们应当通过 InlineStepPart 卡片展开层呈现，不应在主屏 renderThinkingStream
// 抢戏（修复主屏幕 `| Thinking: ...` 漏到卡片下方的症状）。
func (m *ChatModel) rootThinkingState() *thinkingState {
	for _, k := range m.thinkingOrder {
		if k.stepID != "" {
			continue
		}
		if !m.isRootAgent(k.agent) {
			continue
		}
		if s := m.thinkingByKey[k]; s != nil && s.buf.Len() > 0 {
			return s
		}
	}
	return nil
}

func (m *ChatModel) AppendThinking(delta string) {
	m.AppendThinkingWithGroupID(delta, "")
}

// AppendThinkingWithGroupID appends a root-agent thinking delta. Kept for
// callers/tests that don't carry an agent name.
func (m *ChatModel) AppendThinkingWithGroupID(delta string, groupID string) {
	m.AppendThinkingForAgent("", delta, groupID, "")
}

// AppendThinkingForAgent appends a thinking delta for a specific (agent, stepID)
// bucket and aggregates by group_id within that bucket's stream. group_id 是
// 主聚合键；event_id 是记录唯一不能用于分组。stepID="" 主路径，非空为 peer 桶
// 归属——不同 (agent, stepID) 独立 buffer，互不串扰。
func (m *ChatModel) AppendThinkingForAgent(agentName, delta, groupID, stepID string) {
	s := m.thinkingStateFor(agentName, stepID)

	if groupID != "" && s.groupID != "" && groupID != s.groupID {
		m.FlushThinkingForAgentStep(agentName, stepID)
		s = m.thinkingStateFor(agentName, stepID)
	}

	if groupID != "" && s.buf.Len() == 0 {
		if idx, ok := m.store.idxByThinkingGroup[thinkingKey{agentName, groupID}]; ok && idx >= 0 && idx < len(m.store.parts) {
			p := &m.store.parts[idx]
			if p.Type == PartTypeThinking && p.Thinking != nil {
				p.Thinking.Content += delta
				if p.Thinking.StepID == "" && stepID != "" {
					p.Thinking.StepID = stepID
					if m.store.idxByStepID == nil {
						m.store.idxByStepID = make(map[string][]int)
					}
					m.store.idxByStepID[stepID] = append(m.store.idxByStepID[stepID], idx)
				}
				p.Version++
				m.store.MarkDirty(p.ID)
				s.groupID = groupID
				m.markDirty()
				return
			}
		}
	}

	s.groupID = groupID
	s.buf.WriteString(delta)
	m.markDirty()
}

// FlushThinking flushes every (agent, stepID) pending thinking buffer into parts.
// Used at boundaries (stream/tool/result/run-end) where no single bucket is implied.
func (m *ChatModel) FlushThinking() bool {
	flushed := false
	for _, k := range append([]thinkBufKey(nil), m.thinkingOrder...) {
		if m.FlushThinkingForAgentStep(k.agent, k.stepID) {
			flushed = true
		}
	}
	return flushed
}

// FlushThinkingForAgent flushes 所有（agent, *）下任意 stepID 的 thinking buffer。
// 兼容旧调用方语义——agent-global boundary 事件（tool_start / result 等）想 settle
// 那个 agent 所有未完 thinking。
func (m *ChatModel) FlushThinkingForAgent(agentName string) bool {
	flushed := false
	for _, k := range append([]thinkBufKey(nil), m.thinkingOrder...) {
		if k.agent != agentName {
			continue
		}
		if m.FlushThinkingForAgentStep(k.agent, k.stepID) {
			flushed = true
		}
	}
	return flushed
}

// FlushThinkingForAgentStep flush 指定 (agent, stepID) 桶。每个桶独立 settle。
func (m *ChatModel) FlushThinkingForAgentStep(agentName, stepID string) bool {
	k := thinkBufKey{agent: agentName, stepID: stepID}
	s, ok := m.thinkingByKey[k]
	if !ok || s.buf.Len() == 0 {
		m.dropThinking(agentName, stepID)
		return false
	}
	content := s.buf.String()
	groupID := s.groupID

	m.store.AppendThinkingDelta(agentName, groupID, stepID, content, time.Now())
	m.dropThinking(agentName, stepID)
	m.markDirty()
	return true
}

func (m *ChatModel) markDirty() {
	m.contentDirty = true
}

func (m *ChatModel) IsDirty() bool {
	return m.contentDirty
}

func (m *ChatModel) FlushRender() bool {
	if !m.contentDirty {
		return false
	}
	followBottom := m.autoFollowBottom
	m.contentDirty = false
	m.refreshContent()
	if followBottom {
		m.viewport.GotoBottom()
	}
	m.syncAutoFollowFromViewport()
	return true
}

func (m *ChatModel) syncAutoFollowFromViewport() {
	m.autoFollowBottom = m.viewport.AtBottom()
}

func (m *ChatModel) UpdateLastTool(fn func(*ToolPart)) {
	for i := len(m.store.parts) - 1; i >= 0; i-- {
		if m.store.parts[i].Type == PartTypeTool && m.store.parts[i].Tool != nil {
			fn(m.store.parts[i].Tool)
			m.refreshContent()
			return
		}
	}
}

func (m *ChatModel) UpdateToolByCallID(callID string, fn func(*ToolPart)) {
	if callID == "" {
		m.UpdateLastTool(fn)
		return
	}
	if m.store.UpdateToolByCallID(callID, fn) {
		m.refreshContent()
	}
}

func (m *ChatModel) UpdateSubAgentByCallID(callID string, fn func(*SubAgentPart)) {
	idx := m.store.UpdateSubAgentByCallID(callID, fn)
	if idx < 0 {
		return
	}
	toolTime := m.partTimeByCallID(callID, "")
	m.store.parts[idx].SubAgent.Duration = time.Since(toolTime)
	m.refreshContent()
}

// UpdateInlineStepByStepID mutates the InlineStepPart for stepID via the closure.
// Duration 自动按本 part 创建时间起算，与 SubAgent 路径对称。
func (m *ChatModel) UpdateInlineStepByStepID(stepID string, fn func(*InlineStepPart)) {
	idx := m.store.UpdateInlineStepByStepID(stepID, fn)
	if idx < 0 {
		return
	}
	startedAt := m.store.parts[idx].InlineStep.StartedAt
	if !startedAt.IsZero() {
		m.store.parts[idx].InlineStep.Duration = time.Since(startedAt)
	}
	m.refreshContent()
}

func (m *ChatModel) partTimeByCallID(callID, toolName string) time.Time {
	for i := len(m.store.parts) - 1; i >= 0; i-- {
		if m.store.parts[i].Type == PartTypeTool && m.store.parts[i].Tool != nil {
			t := m.store.parts[i].Tool
			if callID != "" && t.CallID == callID {
				return m.store.parts[i].Time
			}
			if callID == "" && t.Name == toolName {
				return m.store.parts[i].Time
			}
		}
	}
	// Background sub-agent cards have no backing tool part (their launcher
	// tool call already ended); fall back to the sub-agent part's own time so
	// elapsed is computed from when the card was created.
	if callID != "" {
		for i := len(m.store.parts) - 1; i >= 0; i-- {
			if m.store.parts[i].Type == PartTypeSubAgent && m.store.parts[i].SubAgent != nil && m.store.parts[i].SubAgent.CallID == callID {
				return m.store.parts[i].Time
			}
		}
	}
	return time.Now()
}

func (m *ChatModel) isRootAgentPlan(p *PlanPart) bool {
	return p.AgentName == m.rootAgentName || p.AgentName == ""
}

// lookupSpawnByChild resolves the spawn context for a child agent name by
// joining on the truncated call_id token its name embeds (see
// childAgentCallToken). The full call_id captured at tool_start has the token
// as a prefix, so a prefix match recovers the spawn entry. The match is gated on
// the naming scheme (sub_agent vs skill fork) because a sub child's token
// (callID[:8]) and a skill child's token (callID[:6]) can otherwise prefix the
// same call_id — gating ensures a "skill-" child only binds to a skill spawn and
// a "sub-" child only to a sub_agent spawn.
func (m *ChatModel) lookupSpawnByChild(agentName string) (agentSpawnInfo, bool) {
	return m.store.LookupSpawnByChild(agentName)
}

// partAgentName returns the producing agent's name for parts that carry one.
func partAgentName(p DisplayPart) string {
	switch p.Type {
	case PartTypeText:
		if p.Text != nil {
			return p.Text.AgentName
		}
	case PartTypeTool:
		if p.Tool != nil {
			return p.Tool.AgentName
		}
	case PartTypePlan:
		if p.Plan != nil {
			return p.Plan.AgentName
		}
	case PartTypeSummary:
		if p.Summary != nil {
			return p.Summary.AgentName
		}
	case PartTypeThinking:
		if p.Thinking != nil {
			return p.Thinking.AgentName
		}
	case PartTypeStepReplan:
		if p.StepReplan != nil {
			return p.StepReplan.AgentName
		}
	case PartTypeStepTriage:
		if p.StepTriage != nil {
			return p.StepTriage.AgentName
		}
	case PartTypeStepSummary:
		if p.StepSummary != nil {
			return p.StepSummary.AgentName
		}
	case PartTypeStepResult:
		if p.StepResult != nil {
			return p.StepResult.AgentName
		}
	case PartTypeFinalAnswer:
		if p.FinalAnswer != nil {
			return p.FinalAnswer.AgentName
		}
	case PartTypePhaseBanner:
		if p.PhaseBanner != nil {
			return p.PhaseBanner.AgentName
		}
	}
	return ""
}

// HasRunningSubAgents reports whether any sub-agent is still running. The
// right-side panel only lists running sub-agents, so it hides once they finish.
func (m *ChatModel) HasRunningSubAgents() bool {
	for _, p := range m.store.parts {
		if p.Type == PartTypeSubAgent && p.SubAgent != nil && p.SubAgent.Status == "running" {
			return true
		}
	}
	return false
}

// SubAgentSummaries returns the running sub-agent cards in timeline order, for
// the right-side panel. Finished sub-agents drop out of the panel (they remain
// reachable via their collapsed card in the main timeline).
func (m *ChatModel) SubAgentSummaries() []SubAgentPart {
	var out []SubAgentPart
	for _, p := range m.store.parts {
		if p.Type == PartTypeSubAgent && p.SubAgent != nil && p.SubAgent.Status == "running" {
			out = append(out, *p.SubAgent)
		}
	}
	return out
}

// lastUserPartIndex returns the index of the most recent user message part, or
// -1 when none exists. It marks the start of the current turn: everything after
// it belongs to the turn the user just kicked off. Backed by the store's
// incrementally maintained lastUserIdx (O(1)).
func (m *ChatModel) lastUserPartIndex() int {
	return m.store.LastUserIndex()
}

// HasSubAgentsThisTurn reports whether the current turn spawned any sub-agent,
// regardless of whether it is still running. Unlike HasRunningSubAgents this
// keeps the panel visible after the children settle, matching the Todo nesting
// which also survives termination. A new user turn naturally drops the previous
// turn's cards because lastUserIdx advances past them.
func (m *ChatModel) HasSubAgentsThisTurn() bool {
	return len(m.store.subAgentIdxThisTurn) > 0
}

// SubAgentCardsThisTurn returns every sub-agent card spawned in the current turn
// (running and terminal) in timeline order, for the right-side panel. Backed by
// the store's incrementally maintained subAgentIdxThisTurn (O(M)).
func (m *ChatModel) SubAgentCardsThisTurn() []SubAgentPart {
	return m.store.SubAgentsThisTurn()
}

// childTitle returns the display name of the sub-agent spawned by callID.
func (m *ChatModel) childTitle(callID string) string {
	for _, p := range m.store.parts {
		if p.Type == PartTypeSubAgent && p.SubAgent != nil && p.SubAgent.CallID == callID {
			if p.SubAgent.AgentName != "" {
				return p.SubAgent.AgentName
			}
			return "sub_agent"
		}
	}
	return "sub_agent"
}

// partsForChild returns the indices of parts belonging to the sub-agent spawned
// by callID: the spawning SubAgent card itself plus every attributed part whose
// producing agent resolves back to that callID. Backed by the store's
// incrementally maintained childPartsByCallID (O(K), K = parts owned by child).
func (m *ChatModel) partsForChild(callID string) []int {
	return m.store.PartsForChild(callID)
}

// PlanForChild returns the latest PlanPart owned by the sub-agent spawned by
// callID (matched via lookupSpawnByChild), or nil when it has no plan yet.
func (m *ChatModel) PlanForChild(callID string) *PlanPart {
	if callID == "" {
		return nil
	}
	var found *PlanPart
	for _, p := range m.store.parts {
		if p.Type == PartTypePlan && p.Plan != nil && !m.isRootAgent(p.Plan.AgentName) {
			if info, ok := m.lookupSpawnByChild(p.Plan.AgentName); ok && info.CallID == callID {
				found = p.Plan
			}
		}
	}
	return found
}

// EnterChild switches the chat area to the in-place transcript of the sub-agent
// spawned by callID. Returns false (and stays on the main timeline) when callID
// does not name a known sub-agent.
func (m *ChatModel) EnterChild(callID string) bool {
	if callID == "" {
		return false
	}
	if _, ok := m.store.spawnByCallID[callID]; !ok {
		return false
	}
	m.viewingChild = callID
	m.refreshContent()
	m.viewport.GotoTop()
	return true
}

// ExitChild returns the chat area to the main timeline.
func (m *ChatModel) ExitChild() {
	if m.viewingChild == "" {
		return
	}
	m.viewingChild = ""
	m.refreshContent()
}

func (m *ChatModel) ViewingChild() string { return m.viewingChild }

// EnterStepDetail 切到 inline_step 详情态（变体 C 的「全屏 detail view」）。
// 返回 false 表示拒绝：
//   - stepID 为空
//   - 当前已在 viewingChild（sub-agent transcript）——子 agent 内部的并发 step
//     不通过本机制呈现，避免嵌套渲染歧义（plan 文档 [v2-fix P5]）
//   - store 里找不到对应 InlineStepPart
func (m *ChatModel) EnterStepDetail(stepID string) bool {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" {
		return false
	}
	if m.viewingChild != "" {
		return false
	}
	if m.store.CardForStep(stepID) < 0 {
		return false
	}
	m.viewingStepID = stepID
	m.refreshContent()
	m.viewport.GotoTop()
	return true
}

// ExitStepDetail 退回主时间线（split 模式）。已是非详情态时 no-op。
func (m *ChatModel) ExitStepDetail() {
	if m.viewingStepID == "" {
		return
	}
	m.viewingStepID = ""
	m.refreshContent()
}

// ViewingStepID 返回当前 inline_step 详情态的 stepID（""=非详情态）。
func (m *ChatModel) ViewingStepID() string { return m.viewingStepID }

// SetViewingStepID 给 detail 模式下 h/l 切 peer 用——viewingStepID 已知改变时
// 同步触发 refreshContent。stepID="" 等价于 ExitStepDetail。
func (m *ChatModel) SetViewingStepID(stepID string) {
	stepID = strings.TrimSpace(stepID)
	if stepID == m.viewingStepID {
		return
	}
	if stepID == "" {
		m.ExitStepDetail()
		return
	}
	if m.viewingChild != "" {
		return
	}
	if m.store.CardForStep(stepID) < 0 {
		return
	}
	m.viewingStepID = stepID
	m.refreshContent()
	m.viewport.GotoTop()
}

// RenderAgentTranscript builds the drill-in transcript for the sub-agent spawned
// by callID: its filtered parts rendered at the given width with cards
// force-expanded. The expand/width mutations are restored before returning, so
// the main view is unaffected. Returns ok=false when no parts belong to the child.
func (m *ChatModel) RenderAgentTranscript(callID string, width int) (string, bool) {
	return m.renderChildTranscript(callID, width)
}

func (m *ChatModel) renderChildTranscript(callID string, width int) (string, bool) {
	idxs := m.partsForChild(callID)
	if len(idxs) == 0 {
		return "", false
	}
	savedWidth := m.width
	savedFocused := m.focused
	m.width = width
	// The transcript is a standalone view: no cursor selection should be drawn.
	m.focused = false
	// Force 所有相关卡片展开来产出 transcript 文本，渲染完恢复——展开态现在落在
	// part 实体的 IsExpanded 字段（B-1 改造），直接操作字段即可。
	type savedExpanded struct {
		card ExpandableCardPart
		prev bool
	}
	saved := make([]savedExpanded, 0, len(idxs))
	indexed := make([]IndexedPart, 0, len(idxs))
	for _, i := range idxs {
		p := m.store.parts[i]
		if card, ok := p.AsExpandable(); ok {
			saved = append(saved, savedExpanded{card: card, prev: card.Expanded()})
			card.SetExpanded(true)
		}
		indexed = append(indexed, IndexedPart{Index: i, Part: p})
	}

	var sb strings.Builder
	rendered := 0
	for _, turn := range groupIndexedPartsIntoTurns(indexed) {
		if rendered > 0 {
			sb.WriteString(m.renderTurnSeparator())
			sb.WriteString("\n")
		}
		rendered++
		switch turn.Type {
		case TurnTypeUser:
			for _, ip := range turn.Parts {
				if r := m.renderPart(ip.Index, ip.Part); r != "" {
					sb.WriteString(r)
					sb.WriteString("\n")
				}
			}
		case TurnTypeAssistant:
			m.renderTranscriptAssistantTurn(&sb, turn.Parts)
		}
	}

	m.width = savedWidth
	m.focused = savedFocused
	for _, s := range saved {
		s.card.SetExpanded(s.prev)
	}
	return strings.TrimRight(sb.String(), "\n"), true
}

// renderTranscriptAssistantTurn renders an assistant turn for the drill-in
// transcript, merging contiguous same-agent text runs exactly like the main
// timeline but without cursor/offset bookkeeping.
func (m *ChatModel) renderTranscriptAssistantTurn(sb *strings.Builder, parts []IndexedPart) {
	maxWidth := m.width - 4
	if maxWidth < 10 {
		maxWidth = 10
	}
	i := 0
	for i < len(parts) {
		ip := parts[i]
		if ip.Part.Type == PartTypeText && ip.Part.Text != nil {
			mergedContent, count := mergeTextRun(parts, i)
			if rendered := m.renderMergedTextBlock(mergedContent, maxWidth); rendered != "" {
				sb.WriteString(rendered)
				sb.WriteString("\n")
			}
			i += count
			continue
		}
		if rendered := m.renderPart(ip.Index, ip.Part); rendered != "" {
			sb.WriteString(rendered)
			sb.WriteString("\n")
		}
		i++
	}
}

func (m *ChatModel) UpdateLastPlanForAgent(agentName string, fn func(*PlanPart)) {
	matchRoot := agentName == m.rootAgentName
	for i := len(m.store.parts) - 1; i >= 0; i-- {
		if m.store.parts[i].Type == PartTypePlan && m.store.parts[i].Plan != nil {
			p := m.store.parts[i].Plan
			if p.AgentName == agentName || (matchRoot && p.AgentName == "") {
				fn(p)
				m.refreshContent()
				return
			}
		}
	}
}

// cancelOpenItems flips a plan's still-open (pending/in_progress) items to
// cancelled and reports whether anything changed.
func cancelOpenItems(p *PlanPart) bool {
	changed := false
	for i := range p.Items {
		switch p.Items[i].Status {
		case "pending", "in_progress":
			p.Items[i].Status = "cancelled"
			changed = true
		}
	}
	return changed
}

// CancelPendingItemsForCallID marks any still-open items in the plan(s) spawned
// by the given tool call_id as cancelled. Keyed by call_id (not the result
// payload), so it fires reliably even when a sub-agent returns non-JSON output
// or is interrupted without emitting a terminal report. A sub-agent whose card
// is still running is left untouched: a background sub-agent's launcher tool
// ends the instant it dispatches, long before the child actually finishes.
func (m *ChatModel) CancelPendingItemsForCallID(callID string) {
	if callID == "" {
		return
	}
	changed := false
	for i := range m.store.parts {
		if m.store.parts[i].Type != PartTypePlan || m.store.parts[i].Plan == nil {
			continue
		}
		p := m.store.parts[i].Plan
		if m.isRootAgentPlan(p) {
			continue
		}
		if info, ok := m.lookupSpawnByChild(p.AgentName); !ok || info.CallID != callID {
			continue
		}
		if m.isSubAgentRunning(callID) {
			continue
		}
		if cancelOpenItems(p) {
			changed = true
		}
	}
	if changed {
		m.refreshContent()
	}
}

// isSubAgentRunning reports whether the sub-agent card for callID exists and has
// not yet reached a terminal status. Used to avoid reconciling (cancelling) a
// still-running background sub-agent's plan items.
func (m *ChatModel) isSubAgentRunning(callID string) bool {
	if callID == "" {
		return false
	}
	for i := range m.store.parts {
		if m.store.parts[i].Type == PartTypeSubAgent && m.store.parts[i].SubAgent != nil && m.store.parts[i].SubAgent.CallID == callID {
			return !isTerminalSubAgentStatus(m.store.parts[i].SubAgent.Status)
		}
	}
	return false
}

// CancelSubAgentItemsForParentStep marks open items in any sub-agent plan whose
// parent step just completed as cancelled, so a finished parent step never
// leaves dangling pending children visible in the sidebar. A sub-agent that is
// still running (e.g. a background agent the parent dispatched and moved past)
// is left untouched: its own progress events keep its items authoritative.
func (m *ChatModel) CancelSubAgentItemsForParentStep(parentAgent, stepID string) {
	if stepID == "" {
		return
	}
	matchRoot := parentAgent == m.rootAgentName
	changed := false
	for i := range m.store.parts {
		if m.store.parts[i].Type != PartTypePlan || m.store.parts[i].Plan == nil {
			continue
		}
		p := m.store.parts[i].Plan
		if p.ParentStepID != stepID {
			continue
		}
		if p.ParentAgent != parentAgent && !(matchRoot && p.ParentAgent == "") {
			continue
		}
		if info, ok := m.lookupSpawnByChild(p.AgentName); ok && m.isSubAgentRunning(info.CallID) {
			continue
		}
		if cancelOpenItems(p) {
			changed = true
		}
	}
	if changed {
		m.refreshContent()
	}
}

func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		// In the in-place sub-agent transcript, navigation keys scroll the
		// drill-in view. Exit (left/esc) is handled at the Model level.
		if m.viewingChild != "" {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(key)
			m.syncAutoFollowFromViewport()
			return m, cmd
		}
		switch key.String() {
		case "up", "k":
			for j := m.cursor - 1; j >= 0; j-- {
				if m.mainVisible(j) {
					m.setCursor(j)
					break
				}
			}
			m.refreshContent()
			m.scrollToCursor()
			m.syncAutoFollowFromViewport()
			return m, nil
		case "down", "j":
			// 一键滑到最底部：把光标对齐到最后一个主时间线可见 part，
			// 使后续 up/enter/space 仍连贯，并重新开启自动跟随底部。
			for j := len(m.store.parts) - 1; j >= 0; j-- {
				if m.mainVisible(j) {
					m.setCursor(j)
					break
				}
			}
			m.refreshContent()
			m.viewport.GotoBottom()
			m.autoFollowBottom = true
			return m, nil
		case "enter", " ":
			if m.cursor >= 0 && m.cursor < len(m.store.parts) {
				part := m.store.parts[m.cursor]
				t := part.Type
				// Enter on a sub-agent card drills into its in-place transcript;
				// Space keeps the lightweight inline expand.
				if key.String() == "enter" && t == PartTypeSubAgent && part.SubAgent != nil {
					sa := part.SubAgent
					return m, func() tea.Msg {
						return EnterSubAgentMsg{CallID: sa.CallID}
					}
				}
				// 任何实现 ExpandableCardPart 的 part 都接入 enter/space 展开切换。
				// 新增卡片类型只需 DisplayPart.AsExpandable 加 case 即可，编译器
				// 保证完备性，不再需要在这里维护类型白名单（FR3 的根因）。
				if card, ok := part.AsExpandable(); ok {
					card.SetExpanded(!card.Expanded())
					m.store.MarkDirty(part.ID)
					m.refreshContent()
					m.scrollToCursor()
					m.syncAutoFollowFromViewport()
					return m, nil
				}
				_ = t
			}
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	m.syncAutoFollowFromViewport()
	return m, cmd
}

func (m ChatModel) View() string {
	return m.viewport.View()
}

func (m ChatModel) ViewWithSelection(sel *SelectionModel) string {
	raw := m.viewport.View()
	if sel == nil || sel.state == SelectionNone {
		return raw
	}
	lines := strings.Split(raw, "\n")
	highlighted := ApplySelectionHighlight(lines, sel)
	return strings.Join(highlighted, "\n")
}

// filterMainParts keeps only parts that belong in the main timeline:
//   - root-agent parts
//   - SubAgent cards (sub-agent 本体仍 inline 主流——子 agent 是独立 agent 实例)
//
// **InlineStep 本体不再进主流**：变体 C 移植后 InlineStepPart 由下区 tile bar
// 负责呈现，主流不再 inline 卡片（plan v2 §Step 3）。partStepID(p) != "" 的
// 子 part（ThinkingPart / ToolPart / TextPart 等）仍归集到对应 InlineStep 子
// 序列——通过 InlineStep detail view（chat.viewingStepID）显示。
func (m *ChatModel) filterMainParts(parts []IndexedPart) []IndexedPart {
	out := make([]IndexedPart, 0, len(parts))
	for _, ip := range parts {
		if partStepID(ip.Part) != "" {
			continue
		}
		if ip.Part.Type == PartTypeInlineStep {
			continue
		}
		if ip.Part.Type == PartTypeSubAgent || m.isRootAgent(partAgentName(ip.Part)) {
			out = append(out, ip)
		}
	}
	return out
}

// mainVisible reports whether the part at index i is shown in the main timeline
// (and thus a valid cursor target).
func (m *ChatModel) mainVisible(i int) bool {
	if i < 0 || i >= len(m.store.parts) {
		return false
	}
	p := m.store.parts[i]
	if partStepID(p) != "" {
		return false
	}
	if p.Type == PartTypeInlineStep {
		return false
	}
	return p.Type == PartTypeSubAgent || m.isRootAgent(partAgentName(p))
}

// hasRootStreamingContent reports whether any root agent has pending live stream
// content. Non-root live streams are not shown inline in the main timeline.
// 仅主路径 stream（stepID==""）参与主流活体显示；peer 流（stepID!=""）属于
// InlineStepPart 卡片内部内容，不在主流抢戏。
func (m *ChatModel) hasRootStreamingContent() bool {
	for _, k := range m.store.streamingOrder {
		if k.stepID != "" {
			continue
		}
		if !m.isRootAgent(k.agent) {
			continue
		}
		if b, ok := m.store.streamingByKey[k]; ok && b.Len() > 0 {
			return true
		}
	}
	return false
}

func (m *ChatModel) refreshContent() {
	if m.viewingChild != "" {
		m.refreshChildContent()
		return
	}
	if m.viewingStepID != "" {
		m.refreshStepDetailContent()
		return
	}

	// Drain the store's dirty set into the renderer cache so this pass sees
	// fresh fragments for any part that was appended, mutated, or whose
	// selection state flipped via setCursor/setFocused.
	m.renderer.drainStore(m.store)

	var sb strings.Builder
	m.partLineOffsets = allocPartLineOffsets(len(m.store.parts))
	lineCount := 0

	turns := groupPartsIntoTurns(m.store.parts)

	renderedTurns := 0
	for _, turn := range turns {
		// Sub-agent details (think/tool/text/plan) are collapsed behind their
		// SubAgent card in the main timeline; only root parts and the cards
		// themselves render inline. Indices on IndexedPart are preserved, so
		// filtering here keeps partLineOffsets correct.
		parts := m.filterMainParts(turn.Parts)
		if len(parts) == 0 {
			continue
		}
		if renderedTurns > 0 {
			sep := m.renderTurnSeparator()
			sb.WriteString(sep)
			sb.WriteString("\n")
			lineCount += strings.Count(sep, "\n") + 1
		}
		renderedTurns++

		switch turn.Type {
		case TurnTypeUser:
			for _, ip := range parts {
				ip := ip
				m.partLineOffsets[ip.Index] = lineCount
				rendered, lc := m.renderer.fragmentFor(ip.Part, m.width, func() string {
					return m.renderPart(ip.Index, ip.Part)
				})
				if rendered == "" {
					continue
				}
				sb.WriteString(rendered)
				sb.WriteString("\n")
				lineCount += lc
			}
		case TurnTypeAssistant:
			m.renderAssistantTurn(&sb, parts, &lineCount)
		}
	}

	if m.rootThinkingState() != nil {
		sb.WriteString(m.renderThinkingStream())
		sb.WriteString("\n")
	}
	if m.hasRootStreamingContent() {
		sb.WriteString(m.renderStreamingContent())
		sb.WriteString("\n")
	}
	if len(m.store.parts) == 0 && !m.hasStreamingContent() && !m.anyThinking() {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("(empty)"))
	}
	m.fullContent = sb.String()
	m.viewport.SetContent(m.fullContent)
}

// refreshStepDetailContent 渲染 inline_step 详情态（变体 C 的「全屏 detail view」）。
// 与 refreshChildContent 平行：用 store.CardForStep + ChildrenForStep 拉子序列、
// 复用 renderPart 渲染每条 ToolPart/ThinkingPart/TextPart。
//
// 头部提示 "‹ Peer: <stepID> <description>   （Esc 返回 / h-l 切 peer）"。
func (m *ChatModel) refreshStepDetailContent() {
	m.partLineOffsets = allocPartLineOffsets(len(m.store.parts))

	stepID := m.viewingStepID
	cardIdx := m.store.CardForStep(stepID)
	var card *InlineStepPart
	if cardIdx >= 0 && cardIdx < m.store.Len() {
		if p := m.store.At(cardIdx); p.InlineStep != nil {
			card = p.InlineStep
		}
	}

	desc := ""
	statusText := ""
	if card != nil {
		desc = strings.TrimSpace(card.Description)
		statusText = strings.TrimSpace(card.Status)
	}

	headTitle := "‹ Peer: " + stepID
	if desc != "" {
		headTitle += "  " + desc
	}
	if statusText != "" {
		headTitle += " [" + statusText + "]"
	}
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true).Render(headTitle) +
		lipgloss.NewStyle().Faint(true).Render("   （Esc 返回 · h/l 切 peer）")

	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n\n")

	children := m.store.ChildrenForStep(stepID)
	if len(children) == 0 {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("（该 peer 暂无 think/tool）"))
	} else {
		for _, idx := range children {
			if idx < 0 || idx >= m.store.Len() {
				continue
			}
			part := m.store.At(idx)
			rendered, _ := m.renderer.fragmentFor(part, m.width, func() string {
				return m.renderPart(idx, part)
			})
			if rendered == "" {
				continue
			}
			sb.WriteString(rendered)
			if !strings.HasSuffix(rendered, "\n") {
				sb.WriteString("\n")
			}
		}
	}

	m.fullContent = sb.String()
	m.viewport.SetContent(m.fullContent)
}

// refreshChildContent renders the in-place sub-agent transcript (drill-in view)
// into the chat viewport, with a header pointing back to the main timeline.
func (m *ChatModel) refreshChildContent() {
	m.partLineOffsets = allocPartLineOffsets(len(m.store.parts))

	title := m.childTitle(m.viewingChild)
	header := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).
		Render("‹ 子 Agent: "+title) +
		lipgloss.NewStyle().Faint(true).Render("   （← 返回）")

	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("\n\n")
	if body, ok := m.renderChildTranscript(m.viewingChild, m.width); ok {
		sb.WriteString(body)
	} else {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("（子 agent 暂无事件）"))
	}

	m.fullContent = sb.String()
	m.viewport.SetContent(m.fullContent)
}

func (m *ChatModel) renderAssistantTurn(sb *strings.Builder, parts []IndexedPart, lineCount *int) {
	maxWidth := m.width - 4
	if maxWidth < 10 {
		maxWidth = 10
	}

	i := 0
	for i < len(parts) {
		ip := parts[i]

		if ip.Part.Type == PartTypeText && ip.Part.Text != nil {
			mergedContent, count := mergeTextRun(parts, i)
			for j := 0; j < count; j++ {
				m.partLineOffsets[parts[i+j].Index] = *lineCount
			}
			rendered := m.renderMergedTextBlock(mergedContent, maxWidth)
			if rendered != "" {
				sb.WriteString(rendered)
				sb.WriteString("\n")
				*lineCount += strings.Count(rendered, "\n") + 1
			}
			i += count
			continue
		}

		idx, part := ip.Index, ip.Part
		m.partLineOffsets[idx] = *lineCount
		rendered, lc := m.renderer.fragmentFor(part, m.width, func() string {
			return m.renderPart(idx, part)
		})
		if rendered != "" {
			sb.WriteString(rendered)
			sb.WriteString("\n")
			*lineCount += lc
		}
		i++
	}
}

func (m *ChatModel) renderMergedTextBlock(content string, maxWidth int) string {
	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(assistantBorderColor).
		PaddingLeft(1).
		Width(maxWidth)
	return style.Render(wrapText(content, maxWidth-4))
}

func (m *ChatModel) renderTurnSeparator() string {
	maxWidth := m.width - 4
	if maxWidth < 10 {
		maxWidth = 10
	}
	w := maxWidth
	if w > 60 {
		w = 60
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Faint(true).Render(strings.Repeat("─", w))
}

func (m *ChatModel) scrollToCursor() {
	if len(m.partLineOffsets) == 0 || m.cursor < 0 || m.cursor >= len(m.partLineOffsets) {
		return
	}
	targetLine := m.partLineOffsets[m.cursor]
	// sentinel -1 = part 未在主时间线渲染（filterMainParts 过滤掉的 InlineStep /
	// 子 part 等）。viewport 不动——避免把视口拽到顶部（P6 用户感知最强的 bug
	// 根因：旧设计用 0 既表示首行又表示未布局，二义性）。
	if targetLine < 0 {
		return
	}
	viewTop := m.viewport.YOffset
	viewBottom := viewTop + m.viewport.Height - 1

	if targetLine < viewTop {
		m.viewport.SetYOffset(targetLine)
	} else if targetLine > viewBottom {
		m.viewport.SetYOffset(targetLine - m.viewport.Height + 1)
	}
}

func (m *ChatModel) SetParts(parts []DisplayPart) {
	// Back-fill identities for historical session files written before the
	// ID/Version fields existed (ID==0 sentinel). Dense back-fill from 1 keeps
	// identities stable across loads of the same file; live parts written by a
	// running session already carry IDs from PartsStore.Append and are left
	// intact. PartsStore.SetAll picks up the resulting max ID to seed nextID
	// for future allocations.
	for i := range parts {
		if parts[i].ID == 0 {
			parts[i].ID = uint64(i + 1)
		}
		if parts[i].Version == 0 {
			parts[i].Version = 1
		}
	}
	m.store.SetAll(parts)
	// Switching sessions dense-back-fills IDs 1..N; the Renderer's per-part
	// fragment cache is keyed by (id, version, width) so the previous
	// session's id=1 entry would otherwise be served on the next refresh
	// (first-frame stale text from the wrong session). Drop the entire cache
	// at the session boundary; subsequent refreshContent passes repopulate
	// it from the freshly loaded parts.
	m.renderer = NewRenderer()
	// 展开态恢复：旧设计 m.toolExpanded 在 SetAll 时按 idx 重建——idx 跨 session
	// 没意义、且不持久化。新设计展开态直接落在 part 实体的 IsExpanded 字段（json:"-"
	// 不参与持久化），跨 session 默认全折叠；shouldAutoExpandPart 的类型仍触发自动
	// 展开（如 StepResult 默认展开），通过 AsExpandable 接口写入。
	for _, part := range parts {
		if shouldAutoExpandPart(part.Type) {
			if card, ok := part.AsExpandable(); ok {
				card.SetExpanded(true)
			}
		}
		// Rebuild the spawn map from loaded parts: the live EventTypeToolStart
		// handler is the only other writer, so a freshly loaded session would
		// otherwise have an empty map — breaking sub-agent drill-in (EnterChild,
		// PlanForChild) and the sidebar's parent-agent resolution. The spawning
		// tool's ToolPart round-trips with IsAgent + the parent's AgentName, which
		// is all we need (ParentStepID isn't persisted on the tool, but the render
		// path reads it from the plan, not from here). Don't clobber richer live
		// entries that may already carry ParentStepID.
		if part.Type == PartTypeTool && part.Tool != nil && part.Tool.IsAgent && part.Tool.CallID != "" {
			if _, ok := m.store.spawnByCallID[part.Tool.CallID]; !ok {
				m.store.spawnByCallID[part.Tool.CallID] = agentSpawnInfo{
					ParentAgent: part.Tool.AgentName,
					CallID:      part.Tool.CallID,
					SubScheme:   part.Tool.Name == builtin_tools.SubAgentToolName,
				}
			}
		}
	}
	m.refreshContent()
	m.viewport.GotoBottom()
	m.autoFollowBottom = true
}

func (m *ChatModel) Parts() []DisplayPart {
	return m.store.parts
}

func (m *ChatModel) AllContentLines() []string {
	return strings.Split(m.fullContent, "\n")
}

func (m *ChatModel) ContentYOffset() int {
	return m.viewport.YOffset
}

func (m *ChatModel) HasContent() bool {
	return len(m.store.parts) > 0 || m.hasStreamingContent()
}

func (m *ChatModel) SetFocused(f bool) {
	m.setFocused(f)
	m.refreshContent()
}

// --- Rendering ---

var (
	userBorderColor      = lipgloss.Color("12")
	assistantBorderColor = lipgloss.Color("10")
	toolBorderColor      = lipgloss.Color("11")
	toolErrorColor       = lipgloss.Color("9")
	toolCompletedColor   = lipgloss.Color("8")
)

func (m *ChatModel) renderPart(idx int, part DisplayPart) string {
	maxWidth := m.width - 4
	if maxWidth < 10 {
		maxWidth = 10
	}

	switch part.Type {
	case PartTypeUser:
		return m.renderUserPart(part, maxWidth)
	case PartTypeText:
		return m.renderTextPart(part, maxWidth)
	case PartTypeTool:
		return m.renderToolPart(idx, part, maxWidth)
	case PartTypePlan:
		return m.renderPlanPart(idx, part, maxWidth)
	case PartTypeSystem:
		return m.renderSystemPart(part)
	case PartTypeThinking:
		return m.renderThinkingPart(part, maxWidth)
	case PartTypeSummary:
		return m.renderSummaryPart(part)
	case PartTypeStepResult:
		return m.renderStepResultPart(idx, part, maxWidth)
	case PartTypeStepSummary:
		return m.renderStepSummaryPart(idx, part, maxWidth)
	case PartTypeStepReplan:
		return m.renderStepReplanPart(idx, part, maxWidth)
	case PartTypeStepTriage:
		return m.renderStepTriagePart(idx, part, maxWidth)
	case PartTypeFinalAnswer:
		return m.renderFinalAnswerPart(idx, part, maxWidth)
	case PartTypeSubAgent:
		return m.renderSubAgentPart(idx, part, maxWidth)
	case PartTypeInlineStep:
		// 变体 C 移植后 InlineStep 由下区 tile bar 负责呈现；filterMainParts
		// 也已不让本 part 进主流。这里保留 no-op return 防止意外路径（renderer
		// 直接调用 renderPart 而非走 filterMainParts）渲染出空行——返回 "" 即可。
		return ""
	case PartTypePhaseBanner:
		return m.renderPhaseBannerPart(part, maxWidth)
	default:
		return ""
	}
}

func (m *ChatModel) renderPhaseBannerPart(part DisplayPart, maxWidth int) string {
	b := part.PhaseBanner
	if b == nil {
		return ""
	}
	label := b.Label
	if label == "" {
		label = phaseLabel(b.Phase)
	}
	text := "▸ " + label
	if b.Iteration > 0 {
		text += fmt.Sprintf(" · iter %d", b.Iteration)
	}
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Bold(true)
	return style.Render(truncateDisplayWidth(text, maxWidth))
}

func (m *ChatModel) renderUserPart(part DisplayPart, maxWidth int) string {
	if part.User == nil {
		return ""
	}
	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(userBorderColor).
		PaddingLeft(1).
		Width(maxWidth)

	content := wrapText(part.User.Content, maxWidth-4)
	return style.Render(content)
}

func (m *ChatModel) renderTextPart(part DisplayPart, maxWidth int) string {
	if part.Text == nil {
		return ""
	}
	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(assistantBorderColor).
		PaddingLeft(1).
		Width(maxWidth)

	content := wrapText(part.Text.Content, maxWidth-4)
	return style.Render(content)
}

func (m *ChatModel) renderStreamingContent() string {
	maxWidth := m.width - 4
	if maxWidth < 10 {
		maxWidth = 10
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(assistantBorderColor).
		PaddingLeft(1).
		Width(maxWidth)
	var sb strings.Builder
	first := true
	for _, k := range m.store.streamingOrder {
		// Only root + main-path (stepID="") live streams render inline; peer/sub-agent
		// streams stay collapsed under their card.
		if k.stepID != "" {
			continue
		}
		if !m.isRootAgent(k.agent) {
			continue
		}
		b, ok := m.store.streamingByKey[k]
		if !ok || b.Len() == 0 {
			continue
		}
		if !first {
			sb.WriteString("\n")
		}
		first = false
		content := wrapText(b.String(), maxWidth-4) + "▌"
		sb.WriteString(style.Render(content))
	}
	return sb.String()
}

func (m *ChatModel) renderThinkingStream() string {
	maxWidth := m.width - 4
	if maxWidth < 10 {
		maxWidth = 10
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color("8")).
		PaddingLeft(1).
		Width(maxWidth).
		Foreground(lipgloss.Color("8"))

	var raw string
	if s := m.rootThinkingState(); s != nil {
		raw = s.buf.String()
	}
	content := wrapText("Thinking: "+raw, maxWidth-4) + "▌"
	return style.Render(content)
}

func (m *ChatModel) renderSubAgentPart(idx int, part DisplayPart, maxWidth int) string {
	expanded := false
	if part.SubAgent != nil {
		expanded = part.SubAgent.Expanded()
	}
	selected := m.focused && idx == m.cursor
	return renderSubAgentCard(part.SubAgent, maxWidth, expanded, selected)
}

func (m *ChatModel) renderToolPart(idx int, part DisplayPart, maxWidth int) string {
	t := part.Tool
	if t == nil {
		return ""
	}
	selected := m.focused && idx == m.cursor
	icon := ToolIcon(t.Name)

	summary := t.Name
	if t.Arguments != "" {
		args := truncateOneLine(t.Arguments, 40)
		summary += " " + args
	}
	if t.State == "completed" && t.Duration > 0 {
		summary += " · " + formatDuration(t.Duration)
	}
	if t.State == "error" && t.Error != "" {
		summary += " · " + truncateDisplayWidth(t.Error, 50)
	} else if t.State == "running" {
		summary += " · running..."
	}

	var style lipgloss.Style
	switch t.State {
	case "running":
		style = lipgloss.NewStyle().Foreground(toolBorderColor)
	case "error":
		style = lipgloss.NewStyle().Foreground(toolErrorColor)
	default:
		style = lipgloss.NewStyle().Foreground(toolCompletedColor)
	}
	if selected {
		style = style.Bold(true)
	}
	line := truncateDisplayWidth(icon+" "+summary, maxWidth)
	return style.Render(line)
}

func (m *ChatModel) renderStepResultPart(idx int, part DisplayPart, maxWidth int) string {
	sr := part.StepResult
	if sr == nil {
		return ""
	}
	expanded := sr.Expanded()
	selected := m.focused && idx == m.cursor

	icon := "▣"
	color := assistantBorderColor
	if strings.EqualFold(strings.TrimSpace(sr.Status), "failed") {
		color = toolErrorColor
	}

	title := "step result"
	if sr.StepName != "" {
		title += ": " + sr.StepName
	}
	content := strings.TrimSpace(sr.DisplayResult)
	if content == "" {
		content = strings.TrimSpace(sr.Summary)
	}
	if content == "" {
		content = strings.TrimSpace(sr.Error)
	}

	if !expanded {
		summary := title
		if content != "" {
			summary += " — " + truncateDisplayWidth(content, 60)
		}
		style := lipgloss.NewStyle().Foreground(color)
		if selected {
			style = style.Bold(true)
		}
		line := truncateDisplayWidth(icon+" "+summary, maxWidth)
		return style.Render(line)
	}

	borderColor := color
	if selected {
		borderColor = lipgloss.Color("15")
	}
	headerStyle := lipgloss.NewStyle().Foreground(borderColor).Bold(true)
	header := icon + " " + title
	if sr.Status != "" {
		header += " (" + sr.Status + ")"
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(maxWidth)
	return headerStyle.Render(header) + "\n" + style.Render(wrapText(content, maxWidth-4))
}

func (m *ChatModel) renderStepSummaryPart(idx int, part DisplayPart, maxWidth int) string {
	s := part.StepSummary
	if s == nil {
		return ""
	}
	expanded := s.Expanded()
	selected := m.focused && idx == m.cursor

	icon := "◆"
	color := toolCompletedColor

	if !expanded {
		summary := "step_summary"
		if s.StepName != "" {
			summary += ": " + s.StepName
		}
		if s.ShortSummary != "" {
			summary += " — " + truncateDisplayWidth(s.ShortSummary, 60)
		}
		style := lipgloss.NewStyle().Foreground(color)
		if selected {
			style = style.Bold(true)
		}
		line := truncateDisplayWidth(icon+" "+summary, maxWidth)
		return style.Render(line)
	}

	borderColor := color
	if selected {
		borderColor = lipgloss.Color("15")
	}
	headerStyle := lipgloss.NewStyle().Foreground(borderColor).Bold(true)
	header := icon + " step_summary"
	if s.StepName != "" {
		header += ": " + s.StepName
	}

	var body strings.Builder
	if s.LongSummary != "" {
		body.WriteString(s.LongSummary)
	} else if s.ShortSummary != "" {
		body.WriteString(s.ShortSummary)
	}
	if len(s.KeyFacts) > 0 {
		body.WriteString("\n\nKey Facts:")
		for _, f := range s.KeyFacts {
			body.WriteString("\n  • " + f)
		}
	}
	if len(s.OpenQuestions) > 0 {
		body.WriteString("\n\nOpen Questions:")
		for _, q := range s.OpenQuestions {
			body.WriteString("\n  ? " + q)
		}
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(maxWidth)
	return headerStyle.Render(header) + "\n" + style.Render(wrapText(body.String(), maxWidth-4))
}

func (m *ChatModel) renderStepReplanPart(idx int, part DisplayPart, maxWidth int) string {
	r := part.StepReplan
	if r == nil {
		return ""
	}
	expanded := r.Expanded()
	selected := m.focused && idx == m.cursor

	icon := "↻"
	color := toolCompletedColor
	if r.ShouldReplan {
		color = lipgloss.Color("11")
	}

	summaryText := strings.TrimSpace(r.ReplanReason)
	if summaryText == "" {
		if r.ShouldReplan {
			summaryText = "需要重规划"
		} else {
			summaryText = "继续当前计划"
		}
	}

	if !expanded {
		summary := "step_replan"
		if r.StepName != "" {
			summary += ": " + r.StepName
		}
		if r.TopicCompleted > 0 || r.TopicBlocked > 0 {
			summary += fmt.Sprintf(" [lane +%d done", r.TopicCompleted)
			if r.TopicBlocked > 0 {
				summary += fmt.Sprintf(", %d blocked", r.TopicBlocked)
			}
			summary += "]"
		}
		summary += " — " + truncateDisplayWidth(summaryText, 60)
		style := lipgloss.NewStyle().Foreground(color)
		if selected {
			style = style.Bold(true)
		}
		line := truncateDisplayWidth(icon+" "+summary, maxWidth)
		return style.Render(line)
	}

	borderColor := color
	if selected {
		borderColor = lipgloss.Color("15")
	}
	headerStyle := lipgloss.NewStyle().Foreground(borderColor).Bold(true)
	header := icon + " step_replan"
	if r.StepName != "" {
		header += ": " + r.StepName
	}

	var body strings.Builder
	if r.ShouldReplan {
		body.WriteString("Decision: replan required")
	} else {
		body.WriteString("Decision: continue current plan")
	}
	if r.ReplanReason != "" {
		body.WriteString("\n\nReason:\n" + r.ReplanReason)
	}
	if r.NextGoal != "" {
		body.WriteString("\n\nNext Goal:\n" + r.NextGoal)
	}
	if r.TopicAssessments > 0 {
		body.WriteString(fmt.Sprintf("\n\nPhase assessments: %d (continue %d / completed %d / blocked %d)",
			r.TopicAssessments, r.TopicContinue, r.TopicCompleted, r.TopicBlocked))
	}
	if len(r.Warnings) > 0 {
		body.WriteString("\n\nWarnings:")
		for _, warning := range r.Warnings {
			body.WriteString("\n  • " + warning)
		}
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(maxWidth)
	return headerStyle.Render(header) + "\n" + style.Render(wrapText(body.String(), maxWidth-4))
}

// renderStepTriagePart 渲染 Triage 廉价决策的 UI 卡片。结构参考 renderStepReplanPart 但更瘦:
// Triage 不涉及 plan / next_goal / surfaces 等重产出字段。
func (m *ChatModel) renderStepTriagePart(idx int, part DisplayPart, maxWidth int) string {
	r := part.StepTriage
	if r == nil {
		return ""
	}
	expanded := r.Expanded()
	selected := m.focused && idx == m.cursor

	// continue 路径用 ✓(通过/确认),replan 路径用 ↻(重新)
	// 视觉上与 StepReplan 的 icon (always ↻) + 颜色变化保持区分度
	icon := "✓"
	color := toolCompletedColor
	if r.Suggestion == "replan" {
		icon = "↻"
		color = lipgloss.Color("11")
	}

	summaryText := strings.TrimSpace(r.Reason)
	if summaryText == "" {
		switch r.Suggestion {
		case "replan":
			summaryText = "需要重规划"
		case "continue":
			summaryText = "继续当前计划"
		default:
			summaryText = "triage: " + r.Suggestion
		}
	}

	if !expanded {
		summary := "step_triage"
		if r.StepName != "" {
			summary += ": " + r.StepName
		}
		summary += " — " + truncateDisplayWidth(summaryText, 60)
		style := lipgloss.NewStyle().Foreground(color)
		if selected {
			style = style.Bold(true)
		}
		line := truncateDisplayWidth(icon+" "+summary, maxWidth)
		return style.Render(line)
	}

	borderColor := color
	if selected {
		borderColor = lipgloss.Color("15")
	}
	headerStyle := lipgloss.NewStyle().Foreground(borderColor).Bold(true)
	header := icon + " step_triage"
	if r.StepName != "" {
		header += ": " + r.StepName
	}

	var body strings.Builder
	body.WriteString("Decision: ")
	body.WriteString(r.Suggestion)
	if r.Reason != "" {
		body.WriteString("\n\nReason:\n" + r.Reason)
	}
	if r.DurationMs > 0 {
		body.WriteString(fmt.Sprintf("\n\nDuration: %d ms", r.DurationMs))
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(maxWidth)
	return headerStyle.Render(header) + "\n" + style.Render(wrapText(body.String(), maxWidth-4))
}

func (m *ChatModel) renderFinalAnswerPart(idx int, part DisplayPart, maxWidth int) string {
	fa := part.FinalAnswer
	if fa == nil {
		return ""
	}

	displayContent := fa.Content
	if fa.Source == "step_result" {
		displayContent = prettyPrintJSON(displayContent)
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(assistantBorderColor).
		PaddingLeft(1).
		Width(maxWidth)

	return style.Render(wrapText(displayContent, maxWidth-4))
}

func shouldAutoExpandPart(partType PartType) bool {
	switch partType {
	case PartTypeStepResult:
		return true
	default:
		return false
	}
}

type planTopicGroup struct {
	name   string
	status string
	items  []PlanItemView
}

// groupPlanItemsByTopic 按 phase 把 plan items 分组，保持 phases 声明顺序。
// 返回 nil 表示不分组渲染（无 phases、或只有一个 synthetic phase——旧观感零变化）。
// 未挂靠任何已知 phase 的 item 归到末尾「其他」组。
func groupPlanItemsByTopic(p *PlanPart) []planTopicGroup {
	if p == nil || len(p.Topics) == 0 {
		return nil
	}
	// 单个 synthetic phase：不渲染组头，退化为平铺。
	if len(p.Topics) == 1 && p.Topics[0].ID == builtin_tools.SyntheticTopicID {
		return nil
	}
	order := make([]string, 0, len(p.Topics))
	idx := make(map[string]int, len(p.Topics))
	groups := make([]planTopicGroup, 0, len(p.Topics)+1)
	for _, phase := range p.Topics {
		name := strings.TrimSpace(phase.Name)
		if name == "" {
			name = phase.ID
		}
		idx[phase.ID] = len(groups)
		groups = append(groups, planTopicGroup{name: name, status: phase.Status})
		order = append(order, phase.ID)
	}
	_ = order
	var orphan []PlanItemView
	for _, item := range p.Items {
		if gi, ok := idx[item.TopicID]; ok {
			groups[gi].items = append(groups[gi].items, item)
		} else {
			orphan = append(orphan, item)
		}
	}
	if len(orphan) > 0 {
		groups = append(groups, planTopicGroup{name: "其他", items: orphan})
	}
	// 过滤空组（无 step 的 lane 不占行）。
	out := groups[:0]
	for _, g := range groups {
		if len(g.items) > 0 {
			out = append(out, g)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *ChatModel) renderPlanPart(idx int, part DisplayPart, maxWidth int) string {
	p := part.Plan
	if p == nil {
		return ""
	}
	expanded := p.Expanded()
	selected := m.focused && idx == m.cursor

	total := len(p.Items)
	var done, failed, active int
	for _, item := range p.Items {
		switch item.Status {
		case "completed":
			done++
		case "failed":
			failed++
		case "in_progress":
			active++
		}
	}

	icon := "▤"
	agentTag := ""
	if p.AgentName != "" && p.AgentName != m.rootAgentName {
		agentTag = " (" + p.AgentName + ")"
	}
	color := lipgloss.Color("11")
	if total > 0 && done == total {
		color = lipgloss.Color("10")
	} else if failed > 0 {
		color = lipgloss.Color("9")
	}

	if !expanded {
		summary := fmt.Sprintf("plan%s [%d/%d", agentTag, done, total)
		if failed > 0 {
			summary += fmt.Sprintf(", %d failed", failed)
		}
		if active > 0 {
			summary += fmt.Sprintf(", %d active", active)
		}
		summary += "]"
		if p.Explanation != "" {
			prefix := icon + " " + summary + " — "
			remaining := maxWidth - runewidth.StringWidth(prefix)
			if remaining > 10 {
				summary += " — " + truncateDisplayWidth(p.Explanation, remaining)
			}
		}
		style := lipgloss.NewStyle().Foreground(color)
		if selected {
			style = style.Bold(true)
		}
		line := truncateDisplayWidth(icon+" "+summary, maxWidth)
		return style.Render(line)
	}

	borderColor := color
	if selected {
		borderColor = lipgloss.Color("15")
	}
	headerStyle := lipgloss.NewStyle().Foreground(borderColor).Bold(true)
	header := fmt.Sprintf("%s plan%s [%d/%d]", icon, agentTag, done, total)

	var body strings.Builder
	if p.Explanation != "" {
		body.WriteString(planExplanationStyle.Render(p.Explanation))
		body.WriteString("\n")
	}
	renderPlanItemLine := func(item PlanItemView) {
		switch item.Status {
		case "completed":
			body.WriteString(planCompleteStyle.Render("  ✓ "+item.Step) + "\n")
		case "in_progress":
			body.WriteString(planActiveStyle.Render("  ▸ "+item.Step) + "\n")
		case "failed":
			body.WriteString(planFailedStyle.Render("  ✗ "+item.Step) + "\n")
		case "skipped":
			body.WriteString(planPendingStyle.Render("  ⊘ "+item.Step) + "\n")
		default:
			body.WriteString(planPendingStyle.Render("  ○ "+item.Step) + "\n")
		}
	}
	// 多 lane 时按 phase 分组加组头；单 synthetic phase 或无 phase 时平铺（旧观感不变）。
	if groups := groupPlanItemsByTopic(p); len(groups) > 0 {
		for _, g := range groups {
			header := "[" + g.name + "]"
			if g.status != "" && g.status != "pending" {
				header += " (" + g.status + ")"
			}
			body.WriteString(planExplanationStyle.Render(header) + "\n")
			for _, item := range g.items {
				renderPlanItemLine(item)
			}
		}
	} else {
		for _, item := range p.Items {
			renderPlanItemLine(item)
		}
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(maxWidth)
	return headerStyle.Render(header) + "\n" + style.Render(strings.TrimRight(body.String(), "\n"))
}

func (m *ChatModel) renderSystemPart(part DisplayPart) string {
	if part.System == nil {
		return ""
	}
	return lipgloss.NewStyle().Faint(true).Italic(true).Render(part.System.Content)
}

func (m *ChatModel) renderThinkingPart(part DisplayPart, maxWidth int) string {
	if part.Thinking == nil || part.Thinking.Content == "" {
		return ""
	}
	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(lipgloss.Color("8")).
		PaddingLeft(1).
		Width(maxWidth).
		Foreground(lipgloss.Color("8"))

	return style.Render(wrapText("Thinking: "+part.Thinking.Content, maxWidth-4))
}

func (m *ChatModel) renderSummaryPart(part DisplayPart) string {
	s := part.Summary
	if s == nil {
		return ""
	}
	var iconStyle lipgloss.Style
	if s.Success {
		iconStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	} else {
		iconStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	}
	infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	info := fmt.Sprintf(" %s · %s · %s", s.AgentName, s.ModelID, formatDuration(s.Duration))
	if s.TokenCount != "" && s.TokenCount != "--" {
		info += fmt.Sprintf(" · %s tokens", s.TokenCount)
	}
	if s.CostEstimate != "" && s.CostEstimate != "--" {
		info += fmt.Sprintf(" · %s", s.CostEstimate)
	}
	return iconStyle.Render("▣") + infoStyle.Render(info)
}

// --- Helpers ---

var (
	planPendingStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	planActiveStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	planCompleteStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	planFailedStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	planExplanationStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Italic(true)
)

func wrapText(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	var result []string
	for _, line := range lines {
		if runewidth.StringWidth(line) <= maxWidth {
			result = append(result, line)
			continue
		}
		var current strings.Builder
		currentWidth := 0
		for _, r := range line {
			rw := runewidth.RuneWidth(r)
			if currentWidth+rw > maxWidth {
				result = append(result, current.String())
				current.Reset()
				currentWidth = 0
			}
			current.WriteRune(r)
			currentWidth += rw
		}
		if current.Len() > 0 {
			result = append(result, current.String())
		}
	}
	return strings.Join(result, "\n")
}

func truncateOneLine(s string, maxWidth int) string {
	s = strings.Split(s, "\n")[0]
	return truncateDisplayWidth(s, maxWidth)
}

func summarizeStepResultForCollapsed(content string, maxWidth int) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "(empty)"
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return truncateDisplayWidth(content, maxWidth)
	}
	var parts []string
	if total, ok := parsed["total_findings"]; ok {
		parts = append(parts, fmt.Sprintf("%v findings", total))
	}
	if sc, ok := parsed["severity_counts"]; ok {
		if m, ok := sc.(map[string]any); ok {
			for k, v := range m {
				parts = append(parts, fmt.Sprintf("%v %s", v, k))
			}
		}
	}
	if len(parts) > 0 {
		return truncateDisplayWidth(strings.Join(parts, ", "), maxWidth)
	}
	return truncateDisplayWidth(content, maxWidth)
}

func prettyPrintJSON(s string) string {
	s = strings.TrimSpace(s)
	var parsed any
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		return s
	}
	pretty, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return s
	}
	return string(pretty)
}
