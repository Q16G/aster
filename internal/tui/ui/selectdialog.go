package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type SelectResultMsg struct {
	DialogID  string
	Index     int
	Value     string
	Cancelled bool
}

type SelectOption struct {
	Label       string
	Value       string
	Description string
	Disabled    bool
	IsFavorite  bool
}

type SelectDialog struct {
	id               string
	title            string
	options          []SelectOption
	filtered         []int
	cursor           int
	filter           textinput.Model
	width            int
	height           int
	FullWidth        bool
	OnFavoriteToggle func(value string)
}

func NewSelectDialog(id, title string, options []SelectOption) *SelectDialog {
	ti := textinput.New()
	ti.Placeholder = "Filter..."
	ti.Focus()

	filtered := make([]int, len(options))
	for i := range options {
		filtered[i] = i
	}

	cursor := 0
	for i, idx := range filtered {
		if !options[idx].Disabled {
			cursor = i
			break
		}
	}

	return &SelectDialog{
		id:       id,
		title:    title,
		options:  options,
		filtered: filtered,
		cursor:   cursor,
		filter:   ti,
	}
}

func (s *SelectDialog) ID() string { return s.id }

func (s *SelectDialog) Update(msg tea.Msg) (Dialog, tea.Cmd) {
	if mouse, ok := msg.(tea.MouseMsg); ok {
		me := tea.MouseEvent(mouse)
		if me.IsWheel() {
			switch me.Button {
			case tea.MouseButtonWheelUp:
				for next := s.cursor - 1; next >= 0; next-- {
					if !s.options[s.filtered[next]].Disabled {
						s.cursor = next
						break
					}
				}
			case tea.MouseButtonWheelDown:
				for next := s.cursor + 1; next < len(s.filtered); next++ {
					if !s.options[s.filtered[next]].Disabled {
						s.cursor = next
						break
					}
				}
			}
			return s, nil
		}
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			for next := s.cursor - 1; next >= 0; next-- {
				if !s.options[s.filtered[next]].Disabled {
					s.cursor = next
					break
				}
			}
			return s, nil
		case "down", "j":
			for next := s.cursor + 1; next < len(s.filtered); next++ {
				if !s.options[s.filtered[next]].Disabled {
					s.cursor = next
					break
				}
			}
			return s, nil
		case "pgup", "ctrl+u":
			step := max(1, (s.height-10)/2)
			for i := 0; i < step; i++ {
				moved := false
				for next := s.cursor - 1; next >= 0; next-- {
					if !s.options[s.filtered[next]].Disabled {
						s.cursor = next
						moved = true
						break
					}
				}
				if !moved {
					break
				}
			}
			return s, nil
		case "pgdown", "ctrl+d":
			step := max(1, (s.height-10)/2)
			for i := 0; i < step; i++ {
				moved := false
				for next := s.cursor + 1; next < len(s.filtered); next++ {
					if !s.options[s.filtered[next]].Disabled {
						s.cursor = next
						moved = true
						break
					}
				}
				if !moved {
					break
				}
			}
			return s, nil
		case "enter":
			if len(s.filtered) > 0 && s.cursor < len(s.filtered) {
				idx := s.filtered[s.cursor]
				if s.options[idx].Disabled {
					return s, nil
				}
				return s, func() tea.Msg {
					return SelectResultMsg{
						DialogID: s.id,
						Index:    idx,
						Value:    s.options[idx].Value,
					}
				}
			}
			return s, nil
		case "ctrl+f":
			if s.OnFavoriteToggle != nil && len(s.filtered) > 0 && s.cursor < len(s.filtered) {
				idx := s.filtered[s.cursor]
				opt := s.options[idx]
				if !opt.Disabled {
					s.options[idx].IsFavorite = !s.options[idx].IsFavorite
					s.OnFavoriteToggle(opt.Value)
				}
			}
			return s, nil
		case "esc":
			return s, func() tea.Msg {
				return SelectResultMsg{DialogID: s.id, Cancelled: true}
			}
		}
	}

	var cmd tea.Cmd
	s.filter, cmd = s.filter.Update(msg)
	s.applyFilter()
	return s, cmd
}

func (s *SelectDialog) applyFilter() {
	query := strings.TrimSpace(s.filter.Value())
	if query == "" {
		s.filtered = make([]int, len(s.options))
		for i := range s.options {
			s.filtered[i] = i
		}
	} else {
		matched := FuzzyFilter(query, s.options)
		valueToOrigIdx := make(map[string]int, len(s.options))
		for i, opt := range s.options {
			if !opt.Disabled && opt.Value != "" {
				valueToOrigIdx[opt.Value] = i
			}
		}
		s.filtered = s.filtered[:0]
		for _, opt := range matched {
			if idx, ok := valueToOrigIdx[opt.Value]; ok {
				s.filtered = append(s.filtered, idx)
			}
		}
	}
	if s.cursor >= len(s.filtered) {
		s.cursor = max(0, len(s.filtered)-1)
	}
	if len(s.filtered) > 0 && s.options[s.filtered[s.cursor]].Disabled {
		for i := s.cursor; i < len(s.filtered); i++ {
			if !s.options[s.filtered[i]].Disabled {
				s.cursor = i
				return
			}
		}
		for i := s.cursor; i >= 0; i-- {
			if !s.options[s.filtered[i]].Disabled {
				s.cursor = i
				return
			}
		}
	}
}

func (s *SelectDialog) View() string {
	contentWidth := min(s.width-4, 60)
	if contentWidth < 20 {
		contentWidth = 20
	}
	if s.FullWidth {
		contentWidth = s.width - 4
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	descStyle := lipgloss.NewStyle().Faint(true)
	headingStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))

	var sb strings.Builder
	sb.WriteString(titleStyle.Render(s.title))
	sb.WriteString("\n\n")
	sb.WriteString(s.filter.View())
	sb.WriteString("\n\n")

	maxVisible := min(len(s.filtered), (s.height-10))
	if maxVisible < 1 {
		maxVisible = 1
	}
	start := 0
	if s.cursor > maxVisible/2 {
		start = s.cursor - maxVisible/2
	}
	if start+maxVisible > len(s.filtered) {
		start = max(0, len(s.filtered)-maxVisible)
	}
	end := min(start+maxVisible, len(s.filtered))

	lineWidth := contentWidth - 6
	if s.FullWidth {
		lineWidth = contentWidth - 2
	}

	for i := start; i < end; i++ {
		idx := s.filtered[i]
		opt := s.options[idx]
		if opt.Disabled {
			sb.WriteString(headingStyle.Render(opt.Label))
			sb.WriteString("\n")
			continue
		}
		prefix := "  "
		style := normalStyle
		if i == s.cursor {
			prefix = "> "
			style = selectedStyle
		}
		star := ""
		if opt.IsFavorite {
			star = "★ "
		}
		label := opt.Label
		if s.FullWidth && lineWidth > 0 {
			maxLabel := lineWidth - runewidth.StringWidth(prefix) - runewidth.StringWidth(star)
			if opt.Description != "" {
				maxLabel -= runewidth.StringWidth(opt.Description) + 1
			}
			if maxLabel > 0 && runewidth.StringWidth(label) > maxLabel {
				label = runewidth.Truncate(label, maxLabel-1, "…")
			}
		}
		line := fmt.Sprintf("%s%s%s", prefix, star, label)
		sb.WriteString(style.Render(line))
		if opt.Description != "" {
			sb.WriteString(" " + descStyle.Render(opt.Description))
		}
		sb.WriteString("\n")
	}

	if len(s.filtered) == 0 {
		sb.WriteString(descStyle.Render("  No matches"))
	}

	if s.FullWidth {
		wrapper := lipgloss.NewStyle().
			Padding(1, 1).
			Width(s.width)
		return lipgloss.Place(s.width, s.height, lipgloss.Left, lipgloss.Top, wrapper.Render(sb.String()))
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(contentWidth)

	dialog := border.Render(sb.String())
	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (s *SelectDialog) SetSize(w, h int) {
	s.width = w
	s.height = h
	if s.FullWidth {
		s.filter.Width = max(20, w-10)
	} else {
		s.filter.Width = min(w-10, 50)
	}
}

