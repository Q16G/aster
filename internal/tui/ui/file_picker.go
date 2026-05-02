package ui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type FileEntry struct {
	Name    string
	RelPath string
	IsDir   bool
}

type FilePickerResultMsg struct {
	Path      string
	Cancelled bool
}

type FilePickerModel struct {
	files    []FileEntry
	filtered []int
	cursor   int
	filter   string
	width    int
}

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".next": true,
	"vendor": true, "__pycache__": true, ".venv": true,
	"dist": true, "build": true, ".cache": true,
}

const filePickerMaxVisible = 10

func NewFilePickerModel(root string, width int) *FilePickerModel {
	var files []FileEntry
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil || rel == "." {
			return nil
		}

		depth := strings.Count(rel, string(filepath.Separator))
		if depth > 2 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		name := d.Name()
		if strings.HasPrefix(name, ".") && d.IsDir() {
			return filepath.SkipDir
		}
		if skipDirs[name] && d.IsDir() {
			return filepath.SkipDir
		}

		files = append(files, FileEntry{
			Name:    name,
			RelPath: rel,
			IsDir:   d.IsDir(),
		})
		return nil
	})

	filtered := make([]int, len(files))
	for i := range files {
		filtered[i] = i
	}

	return &FilePickerModel{
		files:    files,
		filtered: filtered,
		width:    width,
	}
}

func (p *FilePickerModel) SetFilter(query string) {
	p.filter = query
	p.applyFilter()
}

func (p *FilePickerModel) SetWidth(w int) {
	p.width = w
}

func (p *FilePickerModel) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	switch key.String() {
	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}
		return nil
	case "down", "ctrl+n":
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
		return nil
	case "enter", "tab":
		if len(p.filtered) > 0 && p.cursor < len(p.filtered) {
			idx := p.filtered[p.cursor]
			return func() tea.Msg {
				return FilePickerResultMsg{Path: p.files[idx].RelPath}
			}
		}
		return nil
	case "esc":
		return func() tea.Msg {
			return FilePickerResultMsg{Cancelled: true}
		}
	}
	return nil
}

func (p *FilePickerModel) View() string {
	if len(p.filtered) == 0 {
		style := lipgloss.NewStyle().Faint(true).Padding(0, 1)
		return style.Render("No matching files")
	}

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("62"))
	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("7"))
	dirStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12"))

	visible := len(p.filtered)
	if visible > filePickerMaxVisible {
		visible = filePickerMaxVisible
	}

	start := 0
	if p.cursor >= visible {
		start = p.cursor - visible + 1
	}
	end := start + visible
	if end > len(p.filtered) {
		end = len(p.filtered)
	}

	var lines []string
	for i := start; i < end; i++ {
		idx := p.filtered[i]
		entry := p.files[idx]
		prefix := "  "
		style := normalStyle
		if i == p.cursor {
			prefix = "> "
			style = selectedStyle
		}

		label := entry.RelPath
		if entry.IsDir {
			label += "/"
			if i != p.cursor {
				style = dirStyle
			}
		}
		lines = append(lines, style.Render(prefix+label))
	}

	content := strings.Join(lines, "\n")

	boxStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		Width(p.width - 2)

	return boxStyle.Render(content)
}

func (p *FilePickerModel) applyFilter() {
	query := strings.ToLower(strings.TrimPrefix(p.filter, "@"))
	if query == "" {
		p.filtered = make([]int, len(p.files))
		for i := range p.files {
			p.filtered[i] = i
		}
	} else {
		p.filtered = p.filtered[:0]
		for i, f := range p.files {
			if strings.Contains(strings.ToLower(f.Name), query) ||
				strings.Contains(strings.ToLower(f.RelPath), query) {
				p.filtered = append(p.filtered, i)
			}
		}
	}
	if p.cursor >= len(p.filtered) {
		if len(p.filtered) > 0 {
			p.cursor = len(p.filtered) - 1
		} else {
			p.cursor = 0
		}
	}
}
