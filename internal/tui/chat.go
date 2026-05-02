package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ChatMessage struct {
	Role    string // "user" / "assistant" / "tool" / "system"
	Content string
	Time    time.Time
}

type ChatModel struct {
	viewport     viewport.Model
	messages     []ChatMessage
	streaming    strings.Builder
	isStreaming  bool
	width        int
	height       int
	toolVerbose  bool
	toolExpanded   map[int]bool
	cursor         int
	focused        bool
	msgLineOffsets []int
}

func NewChatModel() ChatModel {
	vp := viewport.New(0, 0)
	vp.SetContent("")
	return ChatModel{
		viewport:     vp,
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

func (m *ChatModel) AddMessage(msg ChatMessage) {
	if msg.Time.IsZero() {
		msg.Time = time.Now()
	}
	m.messages = append(m.messages, msg)
	m.cursor = len(m.messages) - 1
	m.refreshContent()
	m.viewport.GotoBottom()
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
		m.messages = append(m.messages, ChatMessage{
			Role:    "assistant",
			Content: m.streaming.String(),
			Time:    time.Now(),
		})
		m.streaming.Reset()
		flushed = true
	}
	m.isStreaming = false
	m.refreshContent()
	m.viewport.GotoBottom()
	return flushed
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
			if m.cursor < len(m.messages)-1 {
				m.cursor++
			}
			m.refreshContent()
			m.scrollToCursor()
			return m, nil
		case "enter", " ":
			if m.cursor >= 0 && m.cursor < len(m.messages) && m.messages[m.cursor].Role == "tool" {
				m.toolExpanded[m.cursor] = !m.toolExpanded[m.cursor]
				m.refreshContent()
				m.scrollToCursor()
				return m, nil
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
	m.msgLineOffsets = make([]int, 0, len(m.messages))
	lineCount := 0

	for i, msg := range m.messages {
		m.msgLineOffsets = append(m.msgLineOffsets, lineCount)
		rendered := m.renderMessage(i, msg)
		sb.WriteString(rendered)
		sb.WriteString("\n")
		lineCount += strings.Count(rendered, "\n") + 1
	}
	if m.isStreaming {
		sb.WriteString(m.renderStreamingMessage())
		sb.WriteString("\n")
	}
	if len(m.messages) == 0 && !m.isStreaming {
		sb.WriteString(lipgloss.NewStyle().Faint(true).Render("(empty)"))
	}
	m.viewport.SetContent(sb.String())
}

func (m *ChatModel) scrollToCursor() {
	if len(m.msgLineOffsets) == 0 || m.cursor < 0 || m.cursor >= len(m.msgLineOffsets) {
		return
	}
	targetLine := m.msgLineOffsets[m.cursor]
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

func (m *ChatModel) SetMessages(msgs []ChatMessage) {
	m.messages = msgs
	m.toolExpanded = make(map[int]bool)
	m.refreshContent()
	m.viewport.GotoBottom()
}

func (m *ChatModel) Messages() []ChatMessage {
	return m.messages
}

func (m *ChatModel) HasContent() bool {
	return len(m.messages) > 0 || m.isStreaming
}

func (m *ChatModel) SetFocused(f bool) {
	m.focused = f
	m.refreshContent()
}

// --- Panel-style rendering ---

var (
	userBorderColor      = lipgloss.Color("12")
	assistantBorderColor = lipgloss.Color("10")
	toolBorderColor      = lipgloss.Color("11")
)

func (m *ChatModel) renderMessage(idx int, msg ChatMessage) string {
	maxWidth := m.width - 4
	if maxWidth < 10 {
		maxWidth = 10
	}

	switch msg.Role {
	case "user":
		return m.renderUserMessage(msg, maxWidth)
	case "assistant":
		return m.renderAssistantMessage(msg, maxWidth)
	case "tool":
		return m.renderToolMessage(idx, msg, maxWidth)
	case "plan":
		return renderPlanMessage(msg.Content)
	case "system":
		return m.renderSystemMessage(msg)
	default:
		return msg.Content
	}
}

func (m *ChatModel) renderUserMessage(msg ChatMessage, maxWidth int) string {
	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderLeft(true).
		BorderForeground(userBorderColor).
		PaddingLeft(1).
		Width(maxWidth)

	content := wrapText(msg.Content, maxWidth-4)
	return style.Render(content)
}

func (m *ChatModel) renderAssistantMessage(msg ChatMessage, maxWidth int) string {
	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(assistantBorderColor).
		PaddingLeft(1).
		Width(maxWidth)

	content := wrapText(msg.Content, maxWidth-4)
	return style.Render(content)
}

func (m *ChatModel) renderStreamingMessage() string {
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

func (m *ChatModel) renderToolMessage(idx int, msg ChatMessage, maxWidth int) string {
	expanded := m.toolExpanded[idx]
	selected := m.focused && idx == m.cursor

	if !expanded {
		icon := "▸"
		if selected {
			icon = "▶"
		}
		summary := compactToolMessage(msg.Content, maxWidth-4)
		style := lipgloss.NewStyle().Foreground(toolBorderColor)
		if selected {
			style = style.Bold(true)
		}
		return style.Render(icon + " " + summary)
	}

	borderColor := toolBorderColor
	if selected {
		borderColor = lipgloss.Color("15")
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderLeft(true).
		BorderForeground(borderColor).
		PaddingLeft(1).
		Width(maxWidth)

	content := msg.Content
	if !m.toolVerbose {
		lines := strings.Split(content, "\n")
		if len(lines) > 20 {
			content = strings.Join(lines[:20], "\n") + fmt.Sprintf("\n... (%d more lines)", len(lines)-20)
		}
	}
	icon := "▾"
	if selected {
		icon = "▼"
	}
	return icon + " " + style.Render(wrapText(content, maxWidth-4))
}

func (m *ChatModel) renderSystemMessage(msg ChatMessage) string {
	return lipgloss.NewStyle().Faint(true).Italic(true).Render(msg.Content)
}

// --- Helpers ---

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

func compactToolMessage(content string, maxWidth int) string {
	idx := strings.Index(content, " → ")
	if idx < 0 {
		if len(content) > maxWidth {
			return content[:maxWidth-1] + "…"
		}
		return content
	}
	header := content[:idx]
	rest := content[idx+len(" → "):]
	if len(rest) > 60 {
		rest = rest[:57] + "..."
	}
	line := header + " → " + rest
	if len(line) > maxWidth {
		line = line[:maxWidth-1] + "…"
	}
	return line
}

func toolMessageContent(toolName string, args string, result string) string {
	summary := result
	if len(summary) > 200 {
		summary = summary[:200] + "…"
	}
	if args != "" {
		return fmt.Sprintf("[%s](%s) → %s", toolName, truncateLines(args, 80), summary)
	}
	return fmt.Sprintf("[%s] → %s", toolName, summary)
}

func truncateLines(s string, maxWidth int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if len(line) > maxWidth {
			lines[i] = line[:maxWidth-1] + "…"
		}
	}
	return strings.Join(lines, "\n")
}

var (
	planPendingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	planActiveStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	planCompleteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	planFailedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	systemStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Italic(true)
)

func renderPlanMessage(content string) string {
	lines := strings.Split(content, "\n")
	var rendered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "[completed]"):
			rendered = append(rendered, planCompleteStyle.Render("  ✓ "+strings.TrimPrefix(trimmed, "[completed] ")))
		case strings.HasPrefix(trimmed, "[in_progress]"):
			rendered = append(rendered, planActiveStyle.Render("  ▸ "+strings.TrimPrefix(trimmed, "[in_progress] ")))
		case strings.HasPrefix(trimmed, "[failed]"):
			rendered = append(rendered, planFailedStyle.Render("  ✗ "+strings.TrimPrefix(trimmed, "[failed] ")))
		case strings.HasPrefix(trimmed, "[pending]"):
			rendered = append(rendered, planPendingStyle.Render("  ○ "+strings.TrimPrefix(trimmed, "[pending] ")))
		default:
			rendered = append(rendered, systemStyle.Render(line))
		}
	}
	return strings.Join(rendered, "\n")
}
