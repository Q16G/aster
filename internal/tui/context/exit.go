package tuicontext

import "time"

type ExitState struct {
	QuitPending bool
	QuitTimer   time.Time
}

type ExitProvider struct {
	*Provider[ExitState]
}

func NewExitProvider() *ExitProvider {
	return &ExitProvider{
		Provider: NewProvider(ExitState{}),
	}
}

func (e *ExitProvider) RequestQuit() bool {
	s := e.Get()
	if !s.QuitPending || time.Since(s.QuitTimer) > 3*time.Second {
		e.Set(ExitState{QuitPending: true, QuitTimer: time.Now()})
		return false
	}
	return true
}

func (e *ExitProvider) Reset() {
	e.Set(ExitState{})
}
