package tuicontext

import (
	"embed"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

//go:embed theme/*.json
var themeFS embed.FS

type ThemeData struct {
	Name string

	// Backgrounds (3-tier)
	Background        lipgloss.Color
	BackgroundPanel   lipgloss.Color
	BackgroundElement lipgloss.Color

	// Text
	Text      lipgloss.Color
	TextMuted lipgloss.Color

	// Borders
	Border      lipgloss.Color
	BorderFocus lipgloss.Color

	// Message accent
	UserAccent      lipgloss.Color
	AssistantAccent lipgloss.Color
	ToolAccent      lipgloss.Color

	// Status
	Success lipgloss.Color
	Warning lipgloss.Color
	Error   lipgloss.Color

	// Diff
	DiffAdded   lipgloss.Color
	DiffRemoved lipgloss.Color

	// Legacy fields (populated by loader for backward compat)
	HeaderFg         lipgloss.Color
	HeaderBg         lipgloss.Color
	StatusFg         lipgloss.Color
	StatusBg         lipgloss.Color
	BorderColor      lipgloss.Color
	FocusBorderColor lipgloss.Color
}

type themeJSON struct {
	Name              string `json:"name"`
	Background        string `json:"background"`
	BackgroundPanel   string `json:"backgroundPanel"`
	BackgroundElement string `json:"backgroundElement"`
	Text              string `json:"text"`
	TextMuted         string `json:"textMuted"`
	Border            string `json:"border"`
	BorderFocus       string `json:"borderFocus"`
	UserAccent        string `json:"userAccent"`
	AssistantAccent   string `json:"assistantAccent"`
	ToolAccent        string `json:"toolAccent"`
	Success           string `json:"success"`
	Warning           string `json:"warning"`
	Error             string `json:"error"`
	DiffAdded         string `json:"diffAdded"`
	DiffRemoved       string `json:"diffRemoved"`
}

func (j themeJSON) toThemeData() ThemeData {
	td := ThemeData{
		Name:              j.Name,
		Background:        lipgloss.Color(j.Background),
		BackgroundPanel:   lipgloss.Color(j.BackgroundPanel),
		BackgroundElement: lipgloss.Color(j.BackgroundElement),
		Text:              lipgloss.Color(j.Text),
		TextMuted:         lipgloss.Color(j.TextMuted),
		Border:            lipgloss.Color(j.Border),
		BorderFocus:       lipgloss.Color(j.BorderFocus),
		UserAccent:        lipgloss.Color(j.UserAccent),
		AssistantAccent:   lipgloss.Color(j.AssistantAccent),
		ToolAccent:        lipgloss.Color(j.ToolAccent),
		Success:           lipgloss.Color(j.Success),
		Warning:           lipgloss.Color(j.Warning),
		Error:             lipgloss.Color(j.Error),
		DiffAdded:         lipgloss.Color(j.DiffAdded),
		DiffRemoved:       lipgloss.Color(j.DiffRemoved),
	}
	// Fill legacy fields
	td.HeaderFg = td.Text
	td.HeaderBg = td.BackgroundPanel
	td.StatusFg = td.TextMuted
	td.StatusBg = td.Background
	td.BorderColor = td.Border
	td.FocusBorderColor = td.BorderFocus
	return td
}

func DarkTheme() ThemeData {
	return ThemeData{
		Name:              "dark",
		Background:        lipgloss.Color("235"),
		BackgroundPanel:   lipgloss.Color("236"),
		BackgroundElement: lipgloss.Color("238"),
		Text:              lipgloss.Color("15"),
		TextMuted:         lipgloss.Color("7"),
		Border:            lipgloss.Color("240"),
		BorderFocus:       lipgloss.Color("62"),
		UserAccent:        lipgloss.Color("12"),
		AssistantAccent:   lipgloss.Color("10"),
		ToolAccent:        lipgloss.Color("11"),
		Success:           lipgloss.Color("10"),
		Warning:           lipgloss.Color("11"),
		Error:             lipgloss.Color("9"),
		DiffAdded:         lipgloss.Color("10"),
		DiffRemoved:       lipgloss.Color("9"),
		HeaderFg:          lipgloss.Color("15"),
		HeaderBg:          lipgloss.Color("62"),
		StatusFg:          lipgloss.Color("7"),
		StatusBg:          lipgloss.Color("236"),
		BorderColor:       lipgloss.Color("240"),
		FocusBorderColor:  lipgloss.Color("62"),
	}
}

func LightTheme() ThemeData {
	return ThemeData{
		Name:              "light",
		Background:        lipgloss.Color("255"),
		BackgroundPanel:   lipgloss.Color("254"),
		BackgroundElement: lipgloss.Color("252"),
		Text:              lipgloss.Color("0"),
		TextMuted:         lipgloss.Color("8"),
		Border:            lipgloss.Color("247"),
		BorderFocus:       lipgloss.Color("25"),
		UserAccent:        lipgloss.Color("25"),
		AssistantAccent:   lipgloss.Color("28"),
		ToolAccent:        lipgloss.Color("130"),
		Success:           lipgloss.Color("28"),
		Warning:           lipgloss.Color("130"),
		Error:             lipgloss.Color("124"),
		DiffAdded:         lipgloss.Color("28"),
		DiffRemoved:       lipgloss.Color("124"),
		HeaderFg:          lipgloss.Color("15"),
		HeaderBg:          lipgloss.Color("25"),
		StatusFg:          lipgloss.Color("0"),
		StatusBg:          lipgloss.Color("252"),
		BorderColor:       lipgloss.Color("247"),
		FocusBorderColor:  lipgloss.Color("25"),
	}
}

var loadedThemes []ThemeData

func init() {
	loadedThemes = loadThemesFromFS()
}

func loadThemesFromFS() []ThemeData {
	entries, err := themeFS.ReadDir("theme")
	if err != nil {
		return nil
	}
	var themes []ThemeData
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := themeFS.ReadFile(filepath.Join("theme", entry.Name()))
		if readErr != nil {
			continue
		}
		var tj themeJSON
		if jsonErr := json.Unmarshal(data, &tj); jsonErr != nil {
			continue
		}
		if tj.Name == "" {
			tj.Name = strings.TrimSuffix(entry.Name(), ".json")
		}
		themes = append(themes, tj.toThemeData())
	}
	return themes
}

func AllThemes() []ThemeData {
	all := []ThemeData{DarkTheme(), LightTheme()}
	all = append(all, loadedThemes...)
	return all
}

func FindTheme(name string) (ThemeData, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "dark" {
		return DarkTheme(), true
	}
	if name == "light" {
		return LightTheme(), true
	}
	for _, t := range loadedThemes {
		if strings.ToLower(t.Name) == name {
			return t, true
		}
	}
	return ThemeData{}, false
}

type ThemeProvider struct {
	*Provider[ThemeData]
}

func NewThemeProvider() *ThemeProvider {
	return &ThemeProvider{
		Provider: NewProvider(DarkTheme()),
	}
}

func (t *ThemeProvider) Toggle() {
	t.Update(func(td ThemeData) ThemeData {
		if td.Name == "dark" {
			return LightTheme()
		}
		return DarkTheme()
	})
}

func (t *ThemeProvider) SetByName(name string) {
	if found, ok := FindTheme(name); ok {
		t.Set(found)
	} else if name == "light" {
		t.Set(LightTheme())
	} else {
		t.Set(DarkTheme())
	}
}

func (t *ThemeProvider) List() []string {
	all := AllThemes()
	names := make([]string, len(all))
	for i, th := range all {
		names[i] = th.Name
	}
	return names
}
