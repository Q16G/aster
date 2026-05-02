package ui

import tea "github.com/charmbracelet/bubbletea"

type Dialog interface {
	Update(msg tea.Msg) (Dialog, tea.Cmd)
	View() string
	SetSize(w, h int)
	ID() string
}

type dialogEntry struct {
	dialog  Dialog
	onClose func()
}

type DialogStack struct {
	layers []dialogEntry
	width  int
	height int
}

func NewDialogStack() *DialogStack {
	return &DialogStack{}
}

func (s *DialogStack) Push(d Dialog, onClose func()) {
	d.SetSize(s.width, s.height)
	s.layers = append(s.layers, dialogEntry{dialog: d, onClose: onClose})
}

func (s *DialogStack) Pop() (Dialog, bool) {
	if len(s.layers) == 0 {
		return nil, false
	}
	top := s.layers[len(s.layers)-1]
	s.layers = s.layers[:len(s.layers)-1]
	if top.onClose != nil {
		top.onClose()
	}
	return top.dialog, true
}

func (s *DialogStack) Replace(d Dialog, onClose func()) {
	s.Pop()
	s.Push(d, onClose)
}

func (s *DialogStack) Clear() {
	for len(s.layers) > 0 {
		s.Pop()
	}
}

func (s *DialogStack) Top() (Dialog, bool) {
	if len(s.layers) == 0 {
		return nil, false
	}
	return s.layers[len(s.layers)-1].dialog, true
}

func (s *DialogStack) IsEmpty() bool {
	return len(s.layers) == 0
}

func (s *DialogStack) Len() int {
	return len(s.layers)
}

func (s *DialogStack) SetSize(w, h int) {
	s.width = w
	s.height = h
	for _, entry := range s.layers {
		entry.dialog.SetSize(w, h)
	}
}

func (s *DialogStack) Update(msg tea.Msg) tea.Cmd {
	top, ok := s.Top()
	if !ok {
		return nil
	}
	updated, cmd := top.Update(msg)
	s.layers[len(s.layers)-1].dialog = updated
	return cmd
}

func (s *DialogStack) View() string {
	top, ok := s.Top()
	if !ok {
		return ""
	}
	return top.View()
}
