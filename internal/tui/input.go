package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxInputHistory = 50

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
	ta.SetHeight(1)
	ta.CharLimit = 0
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.Focus()

	return InputModel{
		textarea: ta,
		enabled:  true,
	}
}

func (m *InputModel) SetWidth(w int) {
	m.textarea.SetWidth(w)
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

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyEnter:
			text := strings.TrimSpace(m.textarea.Value())
			if text == "" {
				return m, nil
			}
			m.pushHistory(text)
			m.textarea.Reset()
			if strings.HasPrefix(text, "/") {
				return m, func() tea.Msg {
					return SlashCommandMsg{Command: text}
				}
			}
			return m, func() tea.Msg {
				return UserSubmitMsg{Text: text}
			}
		case tea.KeyUp:
			if strings.TrimSpace(m.textarea.Value()) == "" || m.historyIdx > 0 {
				if m.recallHistory(-1) {
					return m, nil
				}
			}
		case tea.KeyDown:
			if m.historyIdx < len(m.history) {
				if m.recallHistory(1) {
					return m, nil
				}
			}
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)

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
}

func (m *InputModel) SetValue(s string) {
	m.textarea.SetValue(s)
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
