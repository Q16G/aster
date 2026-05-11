package tui

import (
	"aster/internal/ai"
	"aster/internal/builtin_tools"
	"errors"
	"fmt"
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

func TestAgentDonePersistsHistoryAfterFailure(t *testing.T) {
	m := newViewTestModel()
	m.agentRunning = true
	m.agentCtx = &AgentExecContext{
		InitialHistory: []*ai.MsgInfo{ai.NewUserMsgInfo("old context")},
	}

	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("please continue"),
		ai.NewAIMsgInfo("partial reply"),
	}

	model, _ := m.Update(AgentDoneMsg{
		Err:     errors.New("context canceled"),
		History: history,
	})
	updated := model.(Model)
	if updated.agentCtx == nil {
		t.Fatal("expected agent context to remain available")
	}
	if got := len(updated.agentCtx.InitialHistory); got != len(history) {
		t.Fatalf("expected latest history to be preserved after failure, got %d entries", got)
	}
	if got := strings.TrimSpace(fmt.Sprintf("%v", updated.agentCtx.InitialHistory[0].Content)); got != "please continue" {
		t.Fatalf("expected failed run history to overwrite stale context, got %q", got)
	}
}

func TestAgentDoneShowsInterruptNoticeAndSkipsMachineResultAfterFinalAnswer(t *testing.T) {
	m := newViewTestModel()
	m.agentRunning = true
	m.hadFinalAnswerDuringRun = true
	m.externalInterrupt = &builtin_tools.ExternalInterrupt{
		ReasonCode:       "provider_quota",
		Retryable:        false,
		UserMessage:      "当前 provider 配额已耗尽，本次不会自动重试。",
		SuggestedActions: []string{"切换到仍有额度的 provider 或 model"},
	}

	model, _ := m.Update(AgentDoneMsg{
		Result: &builtin_tools.RunResult{
			Success: true,
			Result:  `{"raw":"machine-payload"}`,
		},
	})
	updated := model.(Model)

	var sawMachinePayload bool
	var sawInterruptNotice bool
	for _, part := range updated.chat.Parts() {
		if part.Text != nil && part.Text.Content == `{"raw":"machine-payload"}` {
			sawMachinePayload = true
		}
		if part.System != nil && strings.Contains(part.System.Content, "当前 provider 配额已耗尽，本次不会自动重试。") {
			sawInterruptNotice = true
		}
	}
	if sawMachinePayload {
		t.Fatal("did not expect machine payload to be rendered after final answer was already shown")
	}
	if !sawInterruptNotice {
		t.Fatal("expected interrupt notice to be rendered")
	}
}
