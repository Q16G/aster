package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type mockDialog struct {
	id      string
	updated bool
	w, h    int
}

func (m *mockDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	m.updated = true
	return m, nil
}

func (m *mockDialog) View() string           { return "dialog:" + m.id }
func (m *mockDialog) SetSize(w, h int)       { m.w = w; m.h = h }
func (m *mockDialog) ID() string             { return m.id }

func TestDialogStack_PushPop(t *testing.T) {
	s := NewDialogStack()
	if !s.IsEmpty() {
		t.Fatal("new stack should be empty")
	}

	d1 := &mockDialog{id: "d1"}
	d2 := &mockDialog{id: "d2"}

	s.Push(d1, nil)
	s.Push(d2, nil)

	if s.Len() != 2 {
		t.Fatalf("expected len 2, got %d", s.Len())
	}

	top, ok := s.Top()
	if !ok || top.ID() != "d2" {
		t.Fatal("expected d2 on top")
	}

	popped, ok := s.Pop()
	if !ok || popped.ID() != "d2" {
		t.Fatal("expected to pop d2")
	}

	top, ok = s.Top()
	if !ok || top.ID() != "d1" {
		t.Fatal("expected d1 on top after pop")
	}

	s.Pop()
	if !s.IsEmpty() {
		t.Fatal("expected empty after popping all")
	}
}

func TestDialogStack_OnClose(t *testing.T) {
	s := NewDialogStack()
	var closed []string

	s.Push(&mockDialog{id: "a"}, func() { closed = append(closed, "a") })
	s.Push(&mockDialog{id: "b"}, func() { closed = append(closed, "b") })

	s.Pop()
	if len(closed) != 1 || closed[0] != "b" {
		t.Fatalf("expected [b], got %v", closed)
	}

	s.Pop()
	if len(closed) != 2 || closed[1] != "a" {
		t.Fatalf("expected [b, a], got %v", closed)
	}
}

func TestDialogStack_Clear(t *testing.T) {
	s := NewDialogStack()
	var closed []string

	s.Push(&mockDialog{id: "x"}, func() { closed = append(closed, "x") })
	s.Push(&mockDialog{id: "y"}, func() { closed = append(closed, "y") })
	s.Push(&mockDialog{id: "z"}, func() { closed = append(closed, "z") })

	s.Clear()

	if !s.IsEmpty() {
		t.Fatal("expected empty after clear")
	}
	if len(closed) != 3 {
		t.Fatalf("expected 3 onClose calls, got %d", len(closed))
	}
	// Clear pops from top: z, y, x
	if closed[0] != "z" || closed[1] != "y" || closed[2] != "x" {
		t.Fatalf("unexpected close order: %v", closed)
	}
}

func TestDialogStack_Replace(t *testing.T) {
	s := NewDialogStack()
	var closed string

	s.Push(&mockDialog{id: "old"}, func() { closed = "old" })
	s.Replace(&mockDialog{id: "new"}, nil)

	if closed != "old" {
		t.Fatal("expected old dialog's onClose to be called")
	}

	top, ok := s.Top()
	if !ok || top.ID() != "new" {
		t.Fatal("expected new dialog on top")
	}
	if s.Len() != 1 {
		t.Fatalf("expected len 1, got %d", s.Len())
	}
}

func TestDialogStack_SetSize(t *testing.T) {
	s := NewDialogStack()
	d1 := &mockDialog{id: "d1"}
	d2 := &mockDialog{id: "d2"}

	s.Push(d1, nil)
	s.Push(d2, nil)
	s.SetSize(80, 24)

	if d1.w != 80 || d1.h != 24 {
		t.Fatalf("d1 size: %dx%d", d1.w, d1.h)
	}
	if d2.w != 80 || d2.h != 24 {
		t.Fatalf("d2 size: %dx%d", d2.w, d2.h)
	}
}

func TestDialogStack_Update(t *testing.T) {
	s := NewDialogStack()
	d := &mockDialog{id: "d"}
	s.Push(d, nil)

	s.Update(tea.KeyMsg{})
	if !d.updated {
		t.Fatal("expected top dialog to receive Update")
	}
}

func TestDialogStack_View(t *testing.T) {
	s := NewDialogStack()
	if v := s.View(); v != "" {
		t.Fatalf("empty stack should return empty view, got %q", v)
	}

	s.Push(&mockDialog{id: "vis"}, nil)
	if v := s.View(); v != "dialog:vis" {
		t.Fatalf("expected 'dialog:vis', got %q", v)
	}
}

func TestDialogStack_PopEmpty(t *testing.T) {
	s := NewDialogStack()
	_, ok := s.Pop()
	if ok {
		t.Fatal("popping empty stack should return false")
	}
}

func TestDialogStack_PushSetsSizeFromStack(t *testing.T) {
	s := NewDialogStack()
	s.SetSize(120, 40)

	d := &mockDialog{id: "sized"}
	s.Push(d, nil)

	if d.w != 120 || d.h != 40 {
		t.Fatalf("pushed dialog should inherit stack size: %dx%d", d.w, d.h)
	}
}
