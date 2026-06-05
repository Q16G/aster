package tui

import (
	"encoding/base64"
	"io"
	"os"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type clipboardCopiedMsg struct {
	text string
}

// focusInput points keyboard focus at the input and re-focuses the textarea so
// typing works right after a mouse click. Setting m.focus alone only fixes key
// routing; a textarea blurred by a prior setFocus (Tab/sub-agent/esc) would still
// drop every keystroke, so we go through setFocus to re-enable and re-focus it.
// No-op only while the agent is running or a modal dialog is open — the two
// states where the main input is intentionally disabled — so a click never
// re-enables typing then. Returns the cursor-blink cmd when focus was taken.
func (m *Model) focusInput() tea.Cmd {
	if m.agentRunning || !m.dialogStack.IsEmpty() {
		return nil
	}
	m.setFocus(FocusInput)
	return m.input.Focus()
}

func (m Model) handleLeftClick(me tea.MouseEvent) (tea.Model, tea.Cmd) {
	hit := m.HitTest(me.X, me.Y)

	switch me.Action {
	case tea.MouseActionPress:
		if hit.Panel == PanelChat {
			focusCmd := m.focusInput()
			m.selection.startYOffset = m.chat.ContentYOffset()
			m.selection.DetectMultiClick(me.X, me.Y)
			switch m.selection.clickCount {
			case 1:
				m.selection.Start(me.X, me.Y)
			case 2:
				lines := m.chat.AllContentLines()
				m.selection.SelectWord(hit.ContentLine, hit.ContentCol, lines)
				if m.selection.HasSelection() {
					return m, tea.Batch(focusCmd, m.copySelectionCmd())
				}
			case 3:
				lines := m.chat.AllContentLines()
				m.selection.SelectLine(hit.ContentLine, lines)
				if m.selection.HasSelection() {
					return m, tea.Batch(focusCmd, m.copySelectionCmd())
				}
			}
			return m, focusCmd
		}
		m.selection.Clear()
		switch hit.Panel {
		case PanelInput:
			return m, m.focusInput()
		case PanelSidebar:
			m.focus = FocusSidebar
		}

	case tea.MouseActionMotion:
		if m.selection.state == SelectionInProgress {
			m.selection.Update(me.X, me.Y)
		}

	case tea.MouseActionRelease:
		if m.selection.state == SelectionInProgress {
			m.selection.Finish(me.X, me.Y)
			if m.selection.state == SelectionDone {
				m.extractAndSetSelectionText()
				if m.selection.HasSelection() {
					return m, m.copySelectionCmd()
				}
			}
		}
	}

	return m, nil
}

func (m Model) handleWheel(me tea.MouseEvent, raw tea.Msg) (tea.Model, tea.Cmd) {
	m.selection.Clear()

	if !m.dialogStack.IsEmpty() {
		cmd := m.dialogStack.Update(raw)
		return m, cmd
	}

	hit := m.HitTest(me.X, me.Y)
	switch hit.Panel {
	case PanelSidebar:
		var cmd tea.Cmd
		m.sidebar, cmd = m.sidebar.Update(raw)
		return m, cmd
	default:
		var cmd tea.Cmd
		m.chat, cmd = m.chat.Update(raw)
		return m, cmd
	}
}

func (m *Model) extractAndSetSelectionText() {
	start, end := m.selection.NormalizedRange()
	startLine := m.selection.startYOffset + start.Y
	endLine := m.selection.startYOffset + end.Y
	startCol := start.X - 1
	endCol := end.X - 1
	if startCol < 0 {
		startCol = 0
	}
	if endCol < 0 {
		endCol = 0
	}

	allLines := m.chat.AllContentLines()
	m.selection.text = ExtractSelectedText(allLines, startLine, startCol, endLine, endCol)
}

func (m *Model) copySelectionCmd() tea.Cmd {
	text := m.selection.text
	if text == "" {
		return nil
	}
	return func() tea.Msg {
		// OSC52 让终端把内容写入系统剪贴板，跨平台且能透传到 SSH / 容器外的本地
		// 终端（Windows Terminal、iTerm2、kitty 等），不再受本地剪贴板库的平台限制。
		b64 := base64.StdEncoding.EncodeToString([]byte(text))
		seq := ansi.SetClipboard(ansi.SystemClipboard, b64)
		_, _ = io.WriteString(os.Stdout, seq)
		// 本地剪贴板库作为 best-effort 回退，兼顾不支持 OSC52 的终端；失败忽略。
		_ = clipboard.WriteAll(text)
		return clipboardCopiedMsg{text: text}
	}
}
