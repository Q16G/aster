package tui

type HitPanel int

const (
	PanelChat HitPanel = iota
	PanelSidebar
	PanelInput
	PanelFooter
	PanelThinking
	PanelPicker
	PanelUnknown
)

type HitResult struct {
	Panel       HitPanel
	ContentLine int
	ContentCol  int
}

func (m *Model) HitTest(screenX, screenY int) HitResult {
	chatWidth := m.layoutChatWidth
	chatHeight := m.layoutChatHeight
	mainHeight := m.layoutMainHeight
	inputHeight := m.layoutInputHeight

	if m.sidebarVisible() && screenX >= chatWidth {
		return HitResult{Panel: PanelSidebar}
	}

	if screenY >= mainHeight {
		return HitResult{Panel: PanelFooter}
	}

	panelHeight := m.thinkingPanel.Height()
	pickerHeight := m.pickerHeight(chatWidth)
	inputStart := chatHeight + panelHeight + pickerHeight

	if screenY < chatHeight {
		contentLine := m.chat.ContentYOffset() + screenY
		contentCol := screenX - 1
		if contentCol < 0 {
			contentCol = 0
		}
		return HitResult{
			Panel:       PanelChat,
			ContentLine: contentLine,
			ContentCol:  contentCol,
		}
	}

	if panelHeight > 0 && screenY < chatHeight+panelHeight {
		return HitResult{Panel: PanelThinking}
	}

	if pickerHeight > 0 && screenY < chatHeight+panelHeight+pickerHeight {
		return HitResult{Panel: PanelPicker}
	}

	if screenY < inputStart+inputHeight {
		return HitResult{Panel: PanelInput}
	}

	return HitResult{Panel: PanelUnknown}
}
