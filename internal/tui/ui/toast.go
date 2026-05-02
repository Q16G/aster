package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ToastLevel int

const (
	ToastInfo ToastLevel = iota
	ToastSuccess
	ToastWarning
	ToastError
)

type Toast struct {
	Message   string
	Level     ToastLevel
	CreatedAt time.Time
	id        int
}

type ToastExpiredMsg struct {
	ID int
}

type ToastManager struct {
	toasts     []Toast
	maxVisible int
	width      int
	nextID     int
}

func NewToastManager(maxVisible int) *ToastManager {
	return &ToastManager{
		maxVisible: maxVisible,
	}
}

func (t *ToastManager) Push(msg string, level ToastLevel, duration time.Duration) tea.Cmd {
	id := t.nextID
	t.nextID++
	t.toasts = append(t.toasts, Toast{
		Message:   msg,
		Level:     level,
		CreatedAt: time.Now(),
		id:        id,
	})
	if len(t.toasts) > t.maxVisible {
		t.toasts = t.toasts[len(t.toasts)-t.maxVisible:]
	}
	return tea.Tick(duration, func(time.Time) tea.Msg {
		return ToastExpiredMsg{ID: id}
	})
}

func (t *ToastManager) Remove(id int) {
	for i, toast := range t.toasts {
		if toast.id == id {
			t.toasts = append(t.toasts[:i], t.toasts[i+1:]...)
			return
		}
	}
}

func (t *ToastManager) SetWidth(w int) {
	t.width = w
}

func (t *ToastManager) IsEmpty() bool {
	return len(t.toasts) == 0
}

func (t *ToastManager) View() string {
	if len(t.toasts) == 0 {
		return ""
	}

	toastWidth := min(t.width/3, 40)
	if toastWidth < 15 {
		toastWidth = 15
	}

	var rendered []string
	for _, toast := range t.toasts {
		var icon string
		var color lipgloss.Color
		switch toast.Level {
		case ToastSuccess:
			icon = "[ok]"
			color = lipgloss.Color("42")
		case ToastWarning:
			icon = "[!]"
			color = lipgloss.Color("214")
		case ToastError:
			icon = "[x]"
			color = lipgloss.Color("196")
		default:
			icon = "[i]"
			color = lipgloss.Color("75")
		}

		style := lipgloss.NewStyle().
			Foreground(color).
			Width(toastWidth).
			Padding(0, 1)

		rendered = append(rendered, style.Render(icon+" "+toast.Message))
	}

	return lipgloss.JoinVertical(lipgloss.Right, rendered...)
}
