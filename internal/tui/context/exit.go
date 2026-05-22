package tuicontext

import "time"

type ExitState struct {
	QuitPending bool
	QuitKey     string
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

func (e *ExitProvider) RequestQuit(key string) bool {
	s := e.Get()
	if !s.QuitPending || s.QuitKey != key || time.Since(s.QuitTimer) > 3*time.Second {
		e.Set(ExitState{QuitPending: true, QuitKey: key, QuitTimer: time.Now()})
		return false
	}
	return true
}

func (e *ExitProvider) Reset() {
	e.Set(ExitState{})
}
