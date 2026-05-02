package tui

import (
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"aster/internal/react"
	tuicontext "aster/internal/tui/context"
)

type EventBridge struct {
	program   atomic.Pointer[tea.Program]
	syncStore atomic.Pointer[tuicontext.SyncStore]
}

func NewEventBridge() *EventBridge {
	return &EventBridge{}
}

func (b *EventBridge) Bind(p *tea.Program) {
	b.program.Store(p)
}

func (b *EventBridge) BindSyncStore(store *tuicontext.SyncStore) {
	b.syncStore.Store(store)
}

func (b *EventBridge) EmitterFunc() react.BaseEmitterFunc {
	return func(e *react.AgentOutputEvent) error {
		if p := b.program.Load(); p != nil {
			p.Send(AgentEventMsg{Event: e})
		}
		if store := b.syncStore.Load(); store != nil {
			store.Enqueue(MapReactEvent(e))
		}
		return nil
	}
}
