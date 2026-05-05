package tui

import (
	"strings"
	"testing"
	"time"

	tuicontext "aster/internal/tui/context"
	tuiui "aster/internal/tui/ui"
)

func newViewTestModel() Model {
	m := NewModel(nil, nil, nil, nil, nil, nil, nil, nil, tuicontext.NewSyncStore())
	m.width = 80
	m.height = 24
	m.footer.SetWorkdir("/tmp")
	return m
}

func TestViewInlineLoadingRendersSingleSpinner(t *testing.T) {
	m := newViewTestModel()
	m.agentRunning = true
	m.statusText = "thinking..."
	m.spinner.Start()
	m.chat.AddPart(DisplayPart{
		Type: PartTypeSystem,
		Time: time.Now(),
		System: &SystemPart{
			Content: "hello",
		},
	})
	m.updateLayout()

	view := m.View()

	if count := strings.Count(view, "thinking..."); count != 1 {
		t.Fatalf("expected one thinking label, got %d in view %q", count, view)
	}
	if strings.Contains(view, "simple reply path started") {
		t.Fatalf("view should not include runtime log text, got %q", view)
	}
}

func TestViewInlineLoadingPrefersRetryLabel(t *testing.T) {
	m := newViewTestModel()
	m.agentRunning = true
	m.statusText = "正在规划"
	m.retryState = &retryState{
		Message: "Too Many Requests",
		Attempt: 1,
		Next:    time.Now().Add(2 * time.Second),
	}
	m.spinner.Start()
	m.chat.AddPart(DisplayPart{
		Type: PartTypeSystem,
		Time: time.Now(),
		System: &SystemPart{
			Content: "hello",
		},
	})
	m.updateLayout()

	view := m.View()

	if !strings.Contains(view, "Too Many Requests [retrying") {
		t.Fatalf("expected retry label in view, got %q", view)
	}
	if strings.Contains(view, "正在规划") {
		t.Fatalf("retry label should override normal loading text, got %q", view)
	}
}

func TestViewPickerFitsWindowAndClearsAfterClose(t *testing.T) {
	m := newViewTestModel()
	m.commandPicker = tuiui.NewCommandPickerModel([]tuiui.CommandEntry{
		{Name: "/agent", Description: "switch agent"},
		{Name: "/theme", Description: "change theme"},
	}, m.width)
	m.chat.AddPart(DisplayPart{
		Type: PartTypeSystem,
		Time: time.Now(),
		System: &SystemPart{
			Content: "hello",
		},
	})
	m.updateLayout()

	withPicker := m.View()
	if lines := strings.Count(withPicker, "\n") + 1; lines > m.height {
		t.Fatalf("picker view should fit window: got %d lines for height %d", lines, m.height)
	}

	m.commandPicker = nil
	m.updateLayout()
	withoutPicker := m.View()
	if strings.Contains(withoutPicker, "/agent") || strings.Contains(withoutPicker, "╭") || strings.Contains(withoutPicker, "╰") {
		t.Fatalf("picker artifacts should be cleared after close, got %q", withoutPicker)
	}
}

func TestAgentDoneClearsRetryState(t *testing.T) {
	m := newViewTestModel()
	m.agentRunning = true
	m.retryState = &retryState{
		Message: "Too Many Requests",
		Attempt: 1,
		Next:    time.Now().Add(2 * time.Second),
	}

	model, _ := m.Update(AgentDoneMsg{})
	updated := model.(Model)
	if updated.retryState != nil {
		t.Fatalf("expected retry state cleared after agent done, got %#v", updated.retryState)
	}
}
