package tui

import (
	"strings"

	"aster/internal/ai"
	tuiui "aster/internal/tui/ui"
)

type ModelOption struct {
	ID      string
	OwnedBy string
}

func modelOptionsFromDescriptors(items []ai.ModelDescriptor) []ModelOption {
	out := make([]ModelOption, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		out = append(out, ModelOption{
			ID:      strings.TrimSpace(item.ID),
			OwnedBy: strings.TrimSpace(item.OwnedBy),
		})
	}
	return out
}

func buildModelSelectOptions(models []ModelOption, currentModelID string, recentModelIDs []string) []tuiui.SelectOption {
	currentModelID = strings.TrimSpace(currentModelID)
	used := make(map[string]struct{})
	var options []tuiui.SelectOption

	appendSection := func(title string, items []ModelOption) {
		if len(items) == 0 {
			return
		}
		options = append(options, tuiui.SelectOption{Label: title, Disabled: true})
		for _, m := range items {
			desc := m.OwnedBy
			if desc == "" {
				desc = "unknown owner"
			}
			options = append(options, tuiui.SelectOption{
				Label:       m.ID,
				Value:       m.ID,
				Description: desc,
			})
			used[m.ID] = struct{}{}
		}
	}

	var current []ModelOption
	for _, m := range models {
		if m.ID == currentModelID {
			current = append(current, m)
			break
		}
	}
	appendSection("Current", current)

	var recent []ModelOption
	for _, id := range recentModelIDs {
		id = strings.TrimSpace(id)
		if id == "" || id == currentModelID {
			continue
		}
		if _, exists := used[id]; exists {
			continue
		}
		for _, m := range models {
			if m.ID == id {
				recent = append(recent, m)
				break
			}
		}
	}
	appendSection("Recent", recent)

	var available []ModelOption
	for _, m := range models {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		if _, exists := used[m.ID]; exists {
			continue
		}
		available = append(available, m)
	}
	appendSection("All Models", available)

	return options
}
