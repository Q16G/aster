package tui

import (
	"sort"
	"strings"

	"aster/internal/ai"
	"aster/internal/provider"
	tuicontext "aster/internal/tui/context"
	tuiui "aster/internal/tui/ui"
)

type ModelOption struct {
	ID         string
	ProviderID string
	OwnedBy    string
	Status     string
}

const modelValueSep = "\x00"

func encodeModelValue(providerID, modelID string) string {
	return providerID + modelValueSep + modelID
}

func decodeModelValue(value string) (providerID, modelID string) {
	if idx := strings.Index(value, modelValueSep); idx >= 0 {
		return value[:idx], value[idx+1:]
	}
	return "", value
}

func modelOptionsFromDescriptors(providerID string, items []ai.ModelDescriptor) []ModelOption {
	out := make([]ModelOption, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			continue
		}
		out = append(out, ModelOption{
			ID:         strings.TrimSpace(item.ID),
			ProviderID: providerID,
			OwnedBy:    strings.TrimSpace(item.OwnedBy),
		})
	}
	return out
}

func registryModelsToOptions(providerID string, models []*provider.ModelInfo) []ModelOption {
	out := make([]ModelOption, 0, len(models))
	for _, m := range models {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		if !m.Capabilities.ToolCall && !m.Capabilities.Temperature {
			continue
		}
		out = append(out, ModelOption{
			ID:         m.ID,
			ProviderID: providerID,
			OwnedBy:    m.Family,
			Status:     m.Status,
		})
		for variantName := range m.Variants {
			out = append(out, ModelOption{
				ID:         m.ID + ":" + variantName,
				ProviderID: providerID,
				OwnedBy:    "↳ " + variantName,
				Status:     m.Status,
			})
		}
	}
	return out
}

func buildModelSelectOptions(models []ModelOption, currentModelID string, favoriteModelIDs, recentModelIDs []string) []tuiui.SelectOption {
	currentModelID = strings.TrimSpace(currentModelID)
	used := make(map[string]struct{})
	favoriteSet := make(map[string]bool, len(favoriteModelIDs))
	for _, id := range favoriteModelIDs {
		if id = strings.TrimSpace(id); id != "" {
			favoriteSet[id] = true
		}
	}
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
			if m.Status != "" && m.Status != "active" {
				desc += " [" + m.Status + "]"
			}
			options = append(options, tuiui.SelectOption{
				Label:       m.ID,
				Value:       m.ID,
				Description: desc,
				IsFavorite:  favoriteSet[m.ID],
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

	var favorites []ModelOption
	for _, id := range favoriteModelIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := used[id]; exists {
			continue
		}
		for _, m := range models {
			if m.ID == id {
				favorites = append(favorites, m)
				break
			}
		}
	}
	appendSection("★ Favorites", favorites)

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
	sort.SliceStable(available, func(i, j int) bool {
		return modelStatusRank(available[i].Status) < modelStatusRank(available[j].Status)
	})
	appendSection("All Models", available)

	return options
}

func buildMultiProviderModelSelectOptions(
	modelsByProvider map[string][]ModelOption,
	providerOrder []string,
	currentProviderID, currentModelID string,
	favorites, recents []tuicontext.ProviderModelRef,
) []tuiui.SelectOption {
	currentModelID = strings.TrimSpace(currentModelID)

	type modelKey struct{ provider, model string }
	used := make(map[modelKey]struct{})
	favoriteSet := make(map[modelKey]bool, len(favorites))
	for _, f := range favorites {
		if f.ModelID != "" {
			favoriteSet[modelKey{f.ProviderID, f.ModelID}] = true
		}
	}

	allModels := make(map[modelKey]ModelOption)
	for _, models := range modelsByProvider {
		for _, m := range models {
			allModels[modelKey{m.ProviderID, m.ID}] = m
		}
	}

	multiProvider := len(providerOrder) > 1

	var options []tuiui.SelectOption

	makeOption := func(m ModelOption) tuiui.SelectOption {
		desc := m.OwnedBy
		if desc == "" {
			desc = m.ProviderID
		} else if multiProvider {
			desc = m.ProviderID + " / " + desc
		}
		if m.Status != "" && m.Status != "active" {
			desc += " [" + m.Status + "]"
		}
		return tuiui.SelectOption{
			Label:       m.ID,
			Value:       encodeModelValue(m.ProviderID, m.ID),
			Description: desc,
			IsFavorite:  favoriteSet[modelKey{m.ProviderID, m.ID}],
		}
	}

	// Current
	if currentModelID != "" && currentProviderID != "" {
		key := modelKey{currentProviderID, currentModelID}
		if m, ok := allModels[key]; ok {
			options = append(options, tuiui.SelectOption{Label: "Current", Disabled: true})
			options = append(options, makeOption(m))
			used[key] = struct{}{}
		}
	}

	// Favorites
	var favOpts []tuiui.SelectOption
	for _, f := range favorites {
		key := modelKey{f.ProviderID, f.ModelID}
		if _, ok := used[key]; ok {
			continue
		}
		if m, ok := allModels[key]; ok {
			favOpts = append(favOpts, makeOption(m))
			used[key] = struct{}{}
		}
	}
	if len(favOpts) > 0 {
		options = append(options, tuiui.SelectOption{Label: "★ Favorites", Disabled: true})
		options = append(options, favOpts...)
	}

	// Recent
	var recentOpts []tuiui.SelectOption
	for _, r := range recents {
		key := modelKey{r.ProviderID, r.ModelID}
		if _, ok := used[key]; ok {
			continue
		}
		if m, ok := allModels[key]; ok {
			recentOpts = append(recentOpts, makeOption(m))
			used[key] = struct{}{}
		}
	}
	if len(recentOpts) > 0 {
		options = append(options, tuiui.SelectOption{Label: "Recent", Disabled: true})
		options = append(options, recentOpts...)
	}

	// All Models — grouped by provider
	for _, pid := range providerOrder {
		models := modelsByProvider[pid]
		var available []ModelOption
		for _, m := range models {
			key := modelKey{m.ProviderID, m.ID}
			if _, ok := used[key]; ok {
				continue
			}
			available = append(available, m)
		}
		if len(available) == 0 {
			continue
		}
		sort.SliceStable(available, func(i, j int) bool {
			return modelStatusRank(available[i].Status) < modelStatusRank(available[j].Status)
		})
		if multiProvider {
			options = append(options, tuiui.SelectOption{Label: "── " + pid + " ──", Disabled: true})
		} else {
			options = append(options, tuiui.SelectOption{Label: "All Models", Disabled: true})
		}
		for _, m := range available {
			options = append(options, makeOption(m))
		}
	}

	return options
}

func modelStatusRank(s string) int {
	switch s {
	case "", "active":
		return 0
	case "beta":
		return 1
	case "alpha":
		return 2
	case "deprecated":
		return 3
	default:
		return 4
	}
}

func ParseModelVariant(modelID string, reg *provider.Registry, providerID string, configVariants map[string]map[string]any) (baseModel, variant string, opts map[string]any) {
	if idx := strings.Index(modelID, ":"); idx > 0 {
		baseModel = modelID[:idx]
		variant = modelID[idx+1:]
	} else {
		baseModel = modelID
		return baseModel, "", nil
	}

	if vo, ok := configVariants[variant]; ok {
		return baseModel, variant, vo
	}

	if reg == nil {
		return baseModel, variant, nil
	}
	mi, ok := reg.GetModel(providerID, baseModel)
	if !ok || mi.Variants == nil {
		return baseModel, variant, nil
	}
	if vo, ok := mi.Variants[variant]; ok {
		return baseModel, variant, vo
	}
	return baseModel, variant, nil
}
