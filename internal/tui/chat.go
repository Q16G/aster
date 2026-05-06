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

type ChatModel struct {
	viewport        viewport.Model
	parts           []DisplayPart
	streaming       *strings.Builder
	isStreaming     bool
	thinkingBuf     *strings.Builder
	isThinking      bool
	width           int
	height          int
	toolVerbose     bool
	toolExpanded    map[int]bool
	cursor          int
	focused         bool
	partLineOffsets []int
}

func NewChatModel() ChatModel {
	vp := viewport.New(0, 0)
	vp.SetContent("")
	return ChatModel{
		viewport:     vp,
		streaming:    &strings.Builder{},
		thinkingBuf:  &strings.Builder{},
		toolExpanded: make(map[int]bool),
	}
}

func (m *ChatModel) SetSize(w, h int) {
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
	m.viewport.GotoBottom()
}

func (m *ChatModel) StreamContent() string {
	return m.streaming.String()
}

func (m *ChatModel) AppendStream(delta string) {
	m.streaming.WriteString(delta)
	m.isStreaming = true
	m.refreshContent()
	m.viewport.GotoBottom()
}

func (m *ChatModel) FlushStream() bool {
	flushed := false
	if m.streaming.Len() > 0 {
		m.parts = append(m.parts, DisplayPart{
			Type: PartTypeText,
			Time: time.Now(),
			Text: &TextPart{Content: m.streaming.String()},
		})
		m.streaming.Reset()
		flushed = true
	}
	m.isStreaming = false
	m.refreshContent()
	m.viewport.GotoBottom()
	return flushed
}

func (m *ChatModel) AppendThinking(delta string) {
	m.thinkingBuf.WriteString(delta)
	m.isThinking = true
	m.refreshContent()
	m.viewport.GotoBottom()
}

func (m *ChatModel) FlushThinking() bool {
	if m.thinkingBuf.Len() == 0 {
		m.isThinking = false
		return false
	}
	m.parts = append(m.parts, DisplayPart{
		Type:     PartTypeThinking,
		Time:     time.Now(),
		Thinking: &ThinkingPart{Content: m.thinkingBuf.String()},
	})
	m.thinkingBuf.Reset()
	m.isThinking = false
	m.refreshContent()
	m.viewport.GotoBottom()
	return true
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
	return time.Now()
}

func (m *ChatModel) UpdateLastPlan(fn func(*PlanPart)) {
	for i := len(m.parts) - 1; i >= 0; i-- {
		if m.parts[i].Type == PartTypePlan && m.parts[i].Plan != nil {
			fn(m.parts[i].Plan)
			m.refreshContent()
			return
		}
	}
}

func (m ChatModel) Update(msg tea.Msg) (ChatModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			m.refreshContent()
			m.scrollToCursor()
			return m, nil
		case "down", "j":
			if m.cursor < len(m.parts)-1 {
				m.cursor++
			}
			m.refreshContent()
			m.scrollToCursor()
			return m, nil
		case "enter", " ":
			if m.cursor >= 0 && m.cursor < len(m.parts) {
				t := m.parts[m.cursor].Type
				if t == PartTypeTool || t == PartTypeStepResult || t == PartTypeStepSummary || t == PartTypeFinalAnswer || t == PartTypePlan {
					m.toolExpanded[m.cursor] = !m.toolExpanded[m.cursor]
					m.refreshContent()
					m.scrollToCursor()
					return m, nil
				}
			}
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m ChatModel) View() string {
	return m.viewport.View()
}

func (m *ChatModel) refreshContent() {
	var sb strings.Builder
	m.partLineOffsets = make([]int, 0, len(m.parts))
	lineCount := 0

	for i, part := range m.parts {
		m.partLineOffsets = append(m.partLineOffsets, lineCount)
		rendered := m.renderPart(i, part)
		if rendered == "" {
			continue
		}
		sb.WriteString(rendered)
		sb.WriteString("\n")
		lineCount += strings.Count(rendered, "\n") + 1
	}
	if m.isThinking {
		sb.WriteString(m.renderThinkingStream())
		sb.WriteString("\n")
	}
	if m.isStreaming {
		sb.WriteString(m.renderStreamingContent())
		sb.WriteString("\n")
	}
	if len(m.parts) == 0 && !m.isStreaming && !m.isThinking {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("(empty)"))
	}
	m.viewport.SetContent(sb.String())
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

func (m *ChatModel) SetToolVerbose(v bool) {
	m.toolVerbose = v
	m.refreshContent()
}

func (m *ChatModel) ToggleToolVerbose() {
	m.toolVerbose = !m.toolVerbose
	m.refreshContent()
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
}

func (m *ChatModel) Parts() []DisplayPart {
	return m.parts
}

func (m *ChatModel) HasContent() bool {
	return len(m.parts) > 0 || m.isStreaming
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
	case PartTypeFinalAnswer:
		return m.renderFinalAnswerPart(idx, part, maxWidth)
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

	content := wrapText(m.streaming.String(), maxWidth-4) + "▌"
	return style.Render(content)
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

	content := wrapText("Thinking: "+m.thinkingBuf.String(), maxWidth-4) + "▌"
	return style.Render(content)
}

func (m *ChatModel) renderToolPart(idx int, part DisplayPart, maxWidth int) string {
	t := part.Tool
	if t == nil {
		return ""
	}
	expanded := m.toolExpanded[idx]
	selected := m.focused && idx == m.cursor
	icon := ToolIcon(t.Name)

	if !expanded {
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

	// Expanded mode
	borderColor := toolBorderColor
	switch t.State {
	case "error":
		borderColor = toolErrorColor
	case "completed":
		borderColor = toolCompletedColor
	}
	if selected {
		borderColor = lipgloss.Color("15")
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(maxWidth)

	var content string
	if t.Error != "" {
		content = t.Error
	} else if t.Result != "" {
		content = t.Result
	}
	if !m.toolVerbose {
		lines := strings.Split(content, "\n")
		if len(lines) > 20 {
			content = strings.Join(lines[:20], "\n") + fmt.Sprintf("\n... (%d more lines)", len(lines)-20)
		}
	}

	header := icon + " " + t.Name
	if t.Duration > 0 {
		header += " · " + formatDuration(t.Duration)
	}
	headerStyle := lipgloss.NewStyle().Foreground(borderColor).Bold(true)

	result := headerStyle.Render(header) + "\n" + style.Render(wrapText(content, maxWidth-4))
	return result
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

func (m *ChatModel) renderFinalAnswerPart(idx int, part DisplayPart, maxWidth int) string {
	fa := part.FinalAnswer
	if fa == nil {
		return ""
	}
	expanded := m.toolExpanded[idx]
	selected := m.focused && idx == m.cursor

	icon := "★"
	color := assistantBorderColor

	if !expanded {
		summary := "answer"
		if fa.Source == "step_result" {
			summary += ": " + summarizeStepResultForCollapsed(fa.Content, 60)
		} else {
			summary += ": " + truncateDisplayWidth(fa.Content, 60)
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
	header := icon + " answer"
	if fa.Source != "" {
		header += " (" + fa.Source + ")"
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(maxWidth)

	displayContent := fa.Content
	if fa.Source == "step_result" {
		displayContent = prettyPrintJSON(displayContent)
	}
	return headerStyle.Render(header) + "\n" + style.Render(wrapText(displayContent, maxWidth-4))
}

func shouldAutoExpandPart(partType PartType) bool {
	switch partType {
	case PartTypeStepResult, PartTypeFinalAnswer:
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
	color := lipgloss.Color("11")
	if total > 0 && done == total {
		color = lipgloss.Color("10")
	} else if failed > 0 {
		color = lipgloss.Color("9")
	}

	if !expanded {
		summary := fmt.Sprintf("plan [%d/%d", done, total)
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
	header := fmt.Sprintf("%s plan [%d/%d]", icon, done, total)

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
	return iconStyle.Render("▣") + infoStyle.Render(fmt.Sprintf(" %s · %s · %s", s.AgentName, s.ModelID, formatDuration(s.Duration)))
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
		if len(line) <= maxWidth {
			result = append(result, line)
			continue
		}
		for len(line) > maxWidth {
			result = append(result, line[:maxWidth])
			line = line[maxWidth:]
		}
		if line != "" {
			result = append(result, line)
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
