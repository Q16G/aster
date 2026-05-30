package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	viewport         viewport.Model
	parts            []DisplayPart
	streamingByAgent map[string]*strings.Builder
	streamingOrder   []string
	thinkingByAgent  map[string]*thinkingState
	thinkingOrder    []string
	width            int
	height           int
	toolExpanded     map[int]bool
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

	activeStepByAgent map[string]string
	// agentSpawnByCallID maps a child agent's spawning tool call_id to the spawn
	// context captured at its tool_start. call_id is the one stable identifier
	// shared by tool_start and the later child name (both sub-<callID[:8]> and
	// skill-<name>-<callID[:6]> embed a truncation of it), so it is the correct
	// join key — independent of the child's naming scheme.
	agentSpawnByCallID map[string]agentSpawnInfo
	agentParent        map[string]agentSpawnInfo
}

func NewChatModel() ChatModel {
	vp := viewport.New(0, 0)
	vp.SetContent("")
	return ChatModel{
		viewport:           vp,
		streamingByAgent:   make(map[string]*strings.Builder),
		thinkingByAgent:    make(map[string]*thinkingState),
		toolExpanded:       make(map[int]bool),
		autoFollowBottom:   true,
		activeStepByAgent:  make(map[string]string),
		agentSpawnByCallID: make(map[string]agentSpawnInfo),
		agentParent:        make(map[string]agentSpawnInfo),
	}
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

func (m *ChatModel) AddPart(part DisplayPart) {
	if part.Time.IsZero() {
		part.Time = time.Now()
	}
	m.parts = append(m.parts, part)
	idx := len(m.parts) - 1
	m.cursor = idx
	if shouldAutoExpandPart(part.Type) {
		m.toolExpanded[idx] = true
	}
	m.refreshContent()
	if m.autoFollowBottom {
		m.viewport.GotoBottom()
	}
	m.syncAutoFollowFromViewport()
}

func (m *ChatModel) streamBuilder(agentName string) *strings.Builder {
	b, ok := m.streamingByAgent[agentName]
	if !ok {
		b = &strings.Builder{}
		m.streamingByAgent[agentName] = b
		m.streamingOrder = append(m.streamingOrder, agentName)
	}
	return b
}

func (m *ChatModel) dropStreamBuilder(agentName string) {
	if _, ok := m.streamingByAgent[agentName]; !ok {
		return
	}
	delete(m.streamingByAgent, agentName)
	for i, name := range m.streamingOrder {
		if name == agentName {
			m.streamingOrder = append(m.streamingOrder[:i], m.streamingOrder[i+1:]...)
			break
		}
	}
}

func (m *ChatModel) StreamingAgents() []string {
	return append([]string(nil), m.streamingOrder...)
}

func (m *ChatModel) hasStreamingContent() bool {
	for _, b := range m.streamingByAgent {
		if b.Len() > 0 {
			return true
		}
	}
	return false
}

func (m *ChatModel) StreamContent(agentName string) string {
	if b, ok := m.streamingByAgent[agentName]; ok {
		return b.String()
	}
	return ""
}

func (m *ChatModel) AppendStream(agentName, delta string) {
	m.streamBuilder(agentName).WriteString(delta)
	m.markDirty()
}

func (m *ChatModel) FlushStream(agentName string) bool {
	flushed := false
	if b, ok := m.streamingByAgent[agentName]; ok && b.Len() > 0 {
		m.parts = append(m.parts, DisplayPart{
			Type: PartTypeText,
			Time: time.Now(),
			Text: &TextPart{Content: b.String(), AgentName: agentName},
		})
		flushed = true
	}
	m.dropStreamBuilder(agentName)
	m.markDirty()
	return flushed
}

// thinkingState is one agent's in-progress thinking buffer. Per-agent buffers
// keep concurrent sub-agents' thinking from cross-contaminating each other,
// mirroring streamingByAgent.
type thinkingState struct {
	buf     strings.Builder
	groupID string
}

func (m *ChatModel) thinkingStateFor(agentName string) *thinkingState {
	s, ok := m.thinkingByAgent[agentName]
	if !ok {
		s = &thinkingState{}
		m.thinkingByAgent[agentName] = s
		m.thinkingOrder = append(m.thinkingOrder, agentName)
	}
	return s
}

func (m *ChatModel) dropThinking(agentName string) {
	if _, ok := m.thinkingByAgent[agentName]; !ok {
		return
	}
	delete(m.thinkingByAgent, agentName)
	for i, n := range m.thinkingOrder {
		if n == agentName {
			m.thinkingOrder = append(m.thinkingOrder[:i], m.thinkingOrder[i+1:]...)
			break
		}
	}
}

func (m *ChatModel) anyThinking() bool {
	for _, s := range m.thinkingByAgent {
		if s.buf.Len() > 0 {
			return true
		}
	}
	return false
}

func (m *ChatModel) isRootAgent(name string) bool {
	return name == m.rootAgentName || name == ""
}

// rootThinkingState returns the first root-agent thinking buffer with pending
// content, or nil. Only root thinking is shown live in the main timeline.
func (m *ChatModel) rootThinkingState() *thinkingState {
	for _, name := range m.thinkingOrder {
		if m.isRootAgent(name) {
			if s := m.thinkingByAgent[name]; s != nil && s.buf.Len() > 0 {
				return s
			}
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
	m.AppendThinkingForAgent("", delta, groupID)
}

// AppendThinkingForAgent appends a thinking delta for a specific agent and
// aggregates by group_id within that agent's stream. group_id is the primary
// aggregation key; event_id is record-unique and should not be used for grouping.
func (m *ChatModel) AppendThinkingForAgent(agentName, delta, groupID string) {
	s := m.thinkingStateFor(agentName)

	if groupID != "" && s.groupID != "" && groupID != s.groupID {
		m.FlushThinkingForAgent(agentName)
		s = m.thinkingStateFor(agentName)
	}

	if groupID != "" && s.buf.Len() == 0 {
		for i := len(m.parts) - 1; i >= 0; i-- {
			if m.parts[i].Type == PartTypeThinking && m.parts[i].Thinking != nil &&
				m.parts[i].Thinking.GroupID == groupID && m.parts[i].Thinking.AgentName == agentName {
				m.parts[i].Thinking.Content += delta
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

// FlushThinking flushes every agent's pending thinking buffer into parts. Used
// at boundaries (stream/tool/result/run-end) where no single agent is implied.
func (m *ChatModel) FlushThinking() bool {
	flushed := false
	for _, name := range append([]string(nil), m.thinkingOrder...) {
		if m.FlushThinkingForAgent(name) {
			flushed = true
		}
	}
	return flushed
}

func (m *ChatModel) FlushThinkingForAgent(agentName string) bool {
	s, ok := m.thinkingByAgent[agentName]
	if !ok || s.buf.Len() == 0 {
		m.dropThinking(agentName)
		return false
	}
	content := s.buf.String()
	groupID := s.groupID

	if groupID != "" {
		for i := len(m.parts) - 1; i >= 0; i-- {
			if m.parts[i].Type == PartTypeThinking && m.parts[i].Thinking != nil &&
				m.parts[i].Thinking.GroupID == groupID && m.parts[i].Thinking.AgentName == agentName {
				m.parts[i].Thinking.Content += content
				m.dropThinking(agentName)
				m.markDirty()
				return true
			}
		}
	}

	m.parts = append(m.parts, DisplayPart{
		Type:     PartTypeThinking,
		Time:     time.Now(),
		Thinking: &ThinkingPart{Content: content, GroupID: groupID, AgentName: agentName},
	})
	m.dropThinking(agentName)
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
	for i := len(m.parts) - 1; i >= 0; i-- {
		if m.parts[i].Type == PartTypeTool && m.parts[i].Tool != nil {
			fn(m.parts[i].Tool)
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
	for i := len(m.parts) - 1; i >= 0; i-- {
		if m.parts[i].Type == PartTypeTool && m.parts[i].Tool != nil && m.parts[i].Tool.CallID == callID {
			fn(m.parts[i].Tool)
			m.refreshContent()
			return
		}
	}
}

func (m *ChatModel) UpdateSubAgentByCallID(callID string, fn func(*SubAgentPart)) {
	for i := len(m.parts) - 1; i >= 0; i-- {
		if m.parts[i].Type == PartTypeSubAgent && m.parts[i].SubAgent != nil && m.parts[i].SubAgent.CallID == callID {
			fn(m.parts[i].SubAgent)
			toolTime := m.partTimeByCallID(callID, "")
			m.parts[i].SubAgent.Duration = time.Since(toolTime)
			m.refreshContent()
			return
		}
	}
}

func (m *ChatModel) partTimeByCallID(callID, toolName string) time.Time {
	for i := len(m.parts) - 1; i >= 0; i-- {
		if m.parts[i].Type == PartTypeTool && m.parts[i].Tool != nil {
			t := m.parts[i].Tool
			if callID != "" && t.CallID == callID {
				return m.parts[i].Time
			}
			if callID == "" && t.Name == toolName {
				return m.parts[i].Time
			}
		}
	}
	// Background sub-agent cards have no backing tool part (their launcher
	// tool call already ended); fall back to the sub-agent part's own time so
	// elapsed is computed from when the card was created.
	if callID != "" {
		for i := len(m.parts) - 1; i >= 0; i-- {
			if m.parts[i].Type == PartTypeSubAgent && m.parts[i].SubAgent != nil && m.parts[i].SubAgent.CallID == callID {
				return m.parts[i].Time
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
	if info, ok := m.agentSpawnByCallID[agentName]; ok {
		return info, true
	}
	token := childAgentCallToken(agentName)
	if token == "" {
		return agentSpawnInfo{}, false
	}
	wantSub := strings.HasPrefix(agentName, "sub-")
	for callID, info := range m.agentSpawnByCallID {
		if info.SubScheme != wantSub {
			continue
		}
		if strings.HasPrefix(callID, token) {
			return info, true
		}
	}
	return agentSpawnInfo{}, false
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
	}
	return ""
}

// HasRunningSubAgents reports whether any sub-agent is still running. The
// right-side panel only lists running sub-agents, so it hides once they finish.
func (m *ChatModel) HasRunningSubAgents() bool {
	for _, p := range m.parts {
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
	for _, p := range m.parts {
		if p.Type == PartTypeSubAgent && p.SubAgent != nil && p.SubAgent.Status == "running" {
			out = append(out, *p.SubAgent)
		}
	}
	return out
}

// childTitle returns the display name of the sub-agent spawned by callID.
func (m *ChatModel) childTitle(callID string) string {
	for _, p := range m.parts {
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
// producing agent resolves (via lookupSpawnByChild) back to that callID.
func (m *ChatModel) partsForChild(callID string) []int {
	if callID == "" {
		return nil
	}
	var idxs []int
	for i, p := range m.parts {
		if p.Type == PartTypeSubAgent && p.SubAgent != nil && p.SubAgent.CallID == callID {
			idxs = append(idxs, i)
			continue
		}
		name := partAgentName(p)
		if name == "" {
			continue
		}
		if info, ok := m.lookupSpawnByChild(name); ok && info.CallID == callID {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

// PlanForChild returns the latest PlanPart owned by the sub-agent spawned by
// callID (matched via lookupSpawnByChild), or nil when it has no plan yet.
func (m *ChatModel) PlanForChild(callID string) *PlanPart {
	if callID == "" {
		return nil
	}
	var found *PlanPart
	for _, p := range m.parts {
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
	if _, ok := m.agentSpawnByCallID[callID]; !ok {
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
	saved := make(map[int]bool, len(idxs))
	indexed := make([]IndexedPart, 0, len(idxs))
	for _, i := range idxs {
		saved[i] = m.toolExpanded[i]
		m.toolExpanded[i] = true
		indexed = append(indexed, IndexedPart{Index: i, Part: m.parts[i]})
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
	for i, v := range saved {
		m.toolExpanded[i] = v
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
	for i := len(m.parts) - 1; i >= 0; i-- {
		if m.parts[i].Type == PartTypePlan && m.parts[i].Plan != nil {
			p := m.parts[i].Plan
			if p.AgentName == agentName || (matchRoot && p.AgentName == "") {
				fn(p)
				m.refreshContent()
				return
			}
		}
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
					m.cursor = j
					break
				}
			}
			m.refreshContent()
			m.scrollToCursor()
			m.syncAutoFollowFromViewport()
			return m, nil
		case "down", "j":
			for j := m.cursor + 1; j < len(m.parts); j++ {
				if m.mainVisible(j) {
					m.cursor = j
					break
				}
			}
			m.refreshContent()
			m.scrollToCursor()
			m.syncAutoFollowFromViewport()
			return m, nil
		case "enter", " ":
			if m.cursor >= 0 && m.cursor < len(m.parts) {
				part := m.parts[m.cursor]
				t := part.Type
				// Enter on a sub-agent card drills into its in-place transcript;
				// Space keeps the lightweight inline expand.
				if key.String() == "enter" && t == PartTypeSubAgent && part.SubAgent != nil {
					sa := part.SubAgent
					return m, func() tea.Msg {
						return EnterSubAgentMsg{CallID: sa.CallID}
					}
				}
				if t == PartTypeStepResult || t == PartTypeStepSummary || t == PartTypeFinalAnswer || t == PartTypePlan || t == PartTypeSubAgent {
					m.toolExpanded[m.cursor] = !m.toolExpanded[m.cursor]
					m.refreshContent()
					m.scrollToCursor()
					m.syncAutoFollowFromViewport()
					return m, nil
				}
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

// filterMainParts keeps only parts that belong in the main timeline: root-agent
// parts and SubAgent cards. Non-root details collapse behind their card.
func (m *ChatModel) filterMainParts(parts []IndexedPart) []IndexedPart {
	out := make([]IndexedPart, 0, len(parts))
	for _, ip := range parts {
		if ip.Part.Type == PartTypeSubAgent || m.isRootAgent(partAgentName(ip.Part)) {
			out = append(out, ip)
		}
	}
	return out
}

// mainVisible reports whether the part at index i is shown in the main timeline
// (and thus a valid cursor target).
func (m *ChatModel) mainVisible(i int) bool {
	if i < 0 || i >= len(m.parts) {
		return false
	}
	p := m.parts[i]
	return p.Type == PartTypeSubAgent || m.isRootAgent(partAgentName(p))
}

// hasRootStreamingContent reports whether any root agent has pending live stream
// content. Non-root live streams are not shown inline in the main timeline.
func (m *ChatModel) hasRootStreamingContent() bool {
	for name, b := range m.streamingByAgent {
		if m.isRootAgent(name) && b.Len() > 0 {
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

	var sb strings.Builder
	m.partLineOffsets = make([]int, len(m.parts))
	lineCount := 0

	turns := groupPartsIntoTurns(m.parts)

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
				m.partLineOffsets[ip.Index] = lineCount
				rendered := m.renderPart(ip.Index, ip.Part)
				if rendered == "" {
					continue
				}
				sb.WriteString(rendered)
				sb.WriteString("\n")
				lineCount += strings.Count(rendered, "\n") + 1
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
	if len(m.parts) == 0 && !m.hasStreamingContent() && !m.anyThinking() {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("(empty)"))
	}
	m.fullContent = sb.String()
	m.viewport.SetContent(m.fullContent)
}

// refreshChildContent renders the in-place sub-agent transcript (drill-in view)
// into the chat viewport, with a header pointing back to the main timeline.
func (m *ChatModel) refreshChildContent() {
	m.partLineOffsets = make([]int, len(m.parts))

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

		m.partLineOffsets[ip.Index] = *lineCount
		rendered := m.renderPart(ip.Index, ip.Part)
		if rendered != "" {
			sb.WriteString(rendered)
			sb.WriteString("\n")
			*lineCount += strings.Count(rendered, "\n") + 1
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
	viewTop := m.viewport.YOffset
	viewBottom := viewTop + m.viewport.Height - 1

	if targetLine < viewTop {
		m.viewport.SetYOffset(targetLine)
	} else if targetLine > viewBottom {
		m.viewport.SetYOffset(targetLine - m.viewport.Height + 1)
	}
}

func (m *ChatModel) SetParts(parts []DisplayPart) {
	m.parts = parts
	m.toolExpanded = make(map[int]bool)
	for i, part := range parts {
		if shouldAutoExpandPart(part.Type) {
			m.toolExpanded[i] = true
		}
	}
	m.refreshContent()
	m.viewport.GotoBottom()
	m.autoFollowBottom = true
}

func (m *ChatModel) Parts() []DisplayPart {
	return m.parts
}

func (m *ChatModel) AllContentLines() []string {
	return strings.Split(m.fullContent, "\n")
}

func (m *ChatModel) ContentYOffset() int {
	return m.viewport.YOffset
}

func (m *ChatModel) HasContent() bool {
	return len(m.parts) > 0 || m.hasStreamingContent()
}

func (m *ChatModel) SetFocused(f bool) {
	m.focused = f
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
	case PartTypeFinalAnswer:
		return m.renderFinalAnswerPart(idx, part, maxWidth)
	case PartTypeSubAgent:
		return m.renderSubAgentPart(idx, part, maxWidth)
	default:
		return ""
	}
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
	for _, name := range m.streamingOrder {
		// Only root live streams render inline; sub-agent streams stay collapsed.
		if !m.isRootAgent(name) {
			continue
		}
		b, ok := m.streamingByAgent[name]
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
	expanded := m.toolExpanded[idx]
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
	expanded := m.toolExpanded[idx]
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
	expanded := m.toolExpanded[idx]
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
	expanded := m.toolExpanded[idx]
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
	if len(r.MissingItems) > 0 {
		body.WriteString("\n\nMissing Items:")
		for _, item := range r.MissingItems {
			body.WriteString("\n  • " + item)
		}
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

func (m *ChatModel) renderPlanPart(idx int, part DisplayPart, maxWidth int) string {
	p := part.Plan
	if p == nil {
		return ""
	}
	expanded := m.toolExpanded[idx]
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
	for _, item := range p.Items {
		switch item.Status {
		case "completed":
			body.WriteString(planCompleteStyle.Render("  ✓ "+item.Step) + "\n")
		case "in_progress":
			body.WriteString(planActiveStyle.Render("  ▸ "+item.Step) + "\n")
		case "failed":
			body.WriteString(planFailedStyle.Render("  ✗ "+item.Step) + "\n")
		default:
			body.WriteString(planPendingStyle.Render("  ○ "+item.Step) + "\n")
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
