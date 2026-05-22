package tui

import (
	"runtime"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

var copyOnSelect = runtime.GOOS != "windows"

type clipboardCopiedMsg struct {
	text string
}

func (m Model) handleLeftClick(me tea.MouseEvent) (tea.Model, tea.Cmd) {
	hit := m.HitTest(me.X, me.Y)

	switch me.Action {
	case tea.MouseActionPress:
		if hit.Panel == PanelChat {
			m.focus = FocusChat
			m.selection.startYOffset = m.chat.ContentYOffset()
			m.selection.DetectMultiClick(me.X, me.Y)
			switch m.selection.clickCount {
			case 1:
				m.selection.Start(me.X, me.Y)
			case 2:
				lines := m.chat.AllContentLines()
				m.selection.SelectWord(hit.ContentLine, hit.ContentCol, lines)
				if copyOnSelect && m.selection.HasSelection() {
					return m, m.copySelectionCmd()
				}
			case 3:
				lines := m.chat.AllContentLines()
				m.selection.SelectLine(hit.ContentLine, lines)
				if copyOnSelect && m.selection.HasSelection() {
					return m, m.copySelectionCmd()
				}
			}
		} else {
			m.selection.Clear()
			switch hit.Panel {
			case PanelInput:
				m.focus = FocusInput
			case PanelSidebar:
				m.focus = FocusSidebar
			}
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
				if copyOnSelect && m.selection.HasSelection() {
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
		if err := clipboard.WriteAll(text); err != nil {
			return clipboardCopiedMsg{}
		}
		return clipboardCopiedMsg{text: text}
	}
}
