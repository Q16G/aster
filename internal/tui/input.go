package tui

import (
	"strings"
	"unicode"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type pasteResultMsg struct {
	text string
}

func readClipboardCmd() tea.Cmd {
	return func() tea.Msg {
		text, err := clipboard.ReadAll()
		if err != nil || text == "" {
			return pasteResultMsg{}
		}
		return pasteResultMsg{text: text}
	}
}

const (
	maxInputHistory = 50
	minInputLines   = 1
	maxInputLines   = 10
)

type InputModel struct {
	textarea     textarea.Model
	enabled      bool
	history      []string
	historyIdx   int
	historyDraft string
}

func NewInputModel() InputModel {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.ShowLineNumbers = false
	ta.SetHeight(minInputLines)
	ta.MaxHeight = maxInputLines
	ta.CharLimit = 0
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter"))
	ta.Focus()

	return InputModel{
		textarea: ta,
		enabled:  true,
	}
}

func (m *InputModel) SetWidth(w int) {
	m.textarea.SetWidth(w)
}

func (m *InputModel) SetHeight(h int) {
	m.textarea.SetHeight(h)
}

func (m InputModel) Height() int {
	return m.textarea.Height()
}

func (m InputModel) DesiredHeight() int {
	w := m.textarea.Width()
	if w <= 0 {
		return minInputLines
	}
	val := m.textarea.Value()
	if val == "" {
		return minInputLines
	}
	lines := strings.Split(val, "\n")
	visual := 0
	for _, line := range lines {
		visual += countWrappedRows(line, w)
	}
	if visual < minInputLines {
		visual = minInputLines
	}
	if visual > maxInputLines {
		visual = maxInputLines
	}
	return visual
}

// countWrappedRows 忠实复刻 bubbles textarea v1.0.0 的内部换行 wrap()，返回单个逻辑
// 行（不含 '\n'）在给定换行宽度下占用的可视行数。textarea 按词换行，且当末段宽度
// >= width 时会额外补一行；早先用 ceil(lw/w) 估算会在"整数倍宽"和词换行场景下少算
// 一行，导致输入框自带 viewport 下滚、首行被顶掉。这里与 wrap() 同算法只计行数。
func countWrappedRows(line string, width int) int {
	if width <= 0 {
		return 1
	}
	rows := 1
	curWidth := 0 // 当前行已占宽度
	curLen := 0   // 当前行已含 rune 数（对应 wrap 里 len(lines[row]) > 0 判定）
	var wordWidth, wordLen, spaces int
	for _, r := range line {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			wordWidth += runewidth.RuneWidth(r)
			wordLen++
		}
		if spaces > 0 {
			if curWidth+wordWidth+spaces > width {
				rows++
				curWidth = wordWidth + spaces
				curLen = wordLen + spaces
			} else {
				curWidth += wordWidth + spaces
				curLen += wordLen + spaces
			}
			spaces, wordWidth, wordLen = 0, 0, 0
		} else if wordWidth+runewidth.RuneWidth(r) > width {
			// 单个词已填满整行：若当前行已有内容则换行，再把词放到（新）行上。
			if curLen > 0 {
				rows++
				curWidth, curLen = 0, 0
			}
			curWidth += wordWidth
			curLen += wordLen
			wordWidth, wordLen = 0, 0
		}
	}
	if curWidth+wordWidth+spaces >= width {
		rows++
	}
	return rows
}

func (m *InputModel) recalcHeight() {
	m.textarea.SetHeight(m.DesiredHeight())
}

func (m *InputModel) SetEnabled(v bool) {
	m.enabled = v
	if v {
		m.textarea.Focus()
	} else {
		m.textarea.Blur()
	}
}

func (m InputModel) Update(msg tea.Msg) (InputModel, tea.Cmd) {
	if !m.enabled {
		return m, nil
	}

	if paste, ok := msg.(pasteResultMsg); ok {
		if paste.text != "" {
			m.textarea.InsertString(paste.text)
		}
		m.recalcHeight()
		return m, nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "ctrl+v" {
			return m, readClipboardCmd()
		}
		switch keyMsg.Type {
		case tea.KeyEnter:
			if keyMsg.Alt {
				break
			}
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return m, nil
			}
			m.pushHistory(text)
			m.textarea.Reset()
			m.recalcHeight()
			if strings.HasPrefix(text, "/") {
				return m, func() tea.Msg {
					return SlashCommandMsg{Command: text}
				}
			}
			return m, func() tea.Msg {
				return UserSubmitMsg{Text: text}
			}
		case tea.KeyUp:
			if m.textarea.Line() == 0 {
				if strings.TrimSpace(m.textarea.Value()) == "" || m.historyIdx > 0 {
					if m.recallHistory(-1) {
						m.recalcHeight()
						return m, nil
					}
				}
			}
		case tea.KeyDown:
			if m.textarea.Line() >= m.textarea.LineCount()-1 {
				if m.historyIdx < len(m.history) {
					if m.recallHistory(1) {
						m.recalcHeight()
						return m, nil
					}
				}
			}
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	m.recalcHeight()

	val := m.textarea.Value()
	if val == "/" {
		return m, tea.Batch(cmd, func() tea.Msg {
			return CommandPickerRequestMsg{}
		})
	}
	if val == "@" {
		return m, tea.Batch(cmd, func() tea.Msg {
			return FilePickerRequestMsg{}
		})
	}

	return m, cmd
}

func (m InputModel) View() string {
	return m.textarea.View()
}

func (m *InputModel) Focus() tea.Cmd {
	return m.textarea.Focus()
}

func (m InputModel) Value() string {
	return m.textarea.Value()
}

func (m *InputModel) Clear() {
	m.textarea.Reset()
	m.recalcHeight()
}

func (m *InputModel) SetValue(s string) {
	m.textarea.SetValue(s)
	m.recalcHeight()
}

func (m *InputModel) pushHistory(text string) {
	if len(m.history) > 0 && m.history[len(m.history)-1] == text {
		m.historyIdx = len(m.history)
		return
	}
	m.history = append(m.history, text)
	if len(m.history) > maxInputHistory {
		m.history = m.history[len(m.history)-maxInputHistory:]
	}
	m.historyIdx = len(m.history)
}

func (m *InputModel) recallHistory(delta int) bool {
	if len(m.history) == 0 {
		return false
	}
	if m.historyIdx == len(m.history) {
		m.historyDraft = m.textarea.Value()
	}
	next := m.historyIdx + delta
	if next < 0 || next > len(m.history) {
		return false
	}
	m.historyIdx = next
	if next == len(m.history) {
		m.textarea.SetValue(m.historyDraft)
	} else {
		m.textarea.SetValue(m.history[next])
	}
	return true
}
