package ui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Spinner struct {
	spinner spinner.Model
	label   string
	visible bool
}

func NewSpinner(label string) *Spinner {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
	return &Spinner{
		spinner: s,
		label:   label,
	}
}

func (s *Spinner) Start() tea.Cmd {
	s.visible = true
	return s.spinner.Tick
}

func (s *Spinner) Stop() {
	s.visible = false
}

func (s *Spinner) SetLabel(label string) {
	s.label = label
}

func (s *Spinner) IsVisible() bool {
	return s.visible
}

func (s *Spinner) Update(msg tea.Msg) tea.Cmd {
	if !s.visible {
		return nil
	}
	var cmd tea.Cmd
	s.spinner, cmd = s.spinner.Update(msg)
	return cmd
}

func (s *Spinner) View() string {
	if !s.visible {
		return ""
	}
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	return s.spinner.View() + " " + labelStyle.Render(s.label)
}
