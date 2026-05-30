package tuicontext

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type ProviderModelRef struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
}

type LocalPreferences struct {
	SidebarMode             string             `json:"sidebar_mode"`
	HeaderVisible           bool               `json:"header_visible"`
	TipsHidden              bool               `json:"tips_hidden"`
	DismissedGettingStarted bool               `json:"dismissed_getting_started"`
	SidebarWidth            int                `json:"sidebar_width"`
	ThemeName               string             `json:"theme_name"`
	FavoriteModels          []ProviderModelRef `json:"favorite_models,omitempty"`
	RecentModels            []ProviderModelRef `json:"recent_models,omitempty"`

	PreferredAgent          string            `json:"preferred_agent,omitempty"`
	PreferredPermissionMode string            `json:"preferred_permission_mode,omitempty"`
	PreferredModels         map[string]string `json:"preferred_models,omitempty"`

	// Deprecated: migrated to RecentModels on first load.
	RecentModelIDs []string `json:"recent_model_ids,omitempty"`
}

type LocalProvider struct {
	*Provider[LocalPreferences]
}

func NewLocalProvider() *LocalProvider {
	return &LocalProvider{
		Provider: NewProvider(LocalPreferences{
			SidebarWidth:  24,
			SidebarMode:   "auto",
			HeaderVisible: true,
		}),
	}
}

func NewLocalProviderFromFile(path string) *LocalProvider {
	p := NewLocalProvider()
	if prefs, err := LoadLocalPreferences(path); err == nil {
		p.Set(prefs)
	}
	p.Subscribe(func(v LocalPreferences) {
		_ = SaveLocalPreferences(path, v)
	})
	return p
}

func (l *LocalProvider) MigrateRecentModels(defaultProviderID string) {
	l.Update(func(p LocalPreferences) LocalPreferences {
		if len(p.RecentModelIDs) == 0 || len(p.RecentModels) > 0 {
			return p
		}
		for _, id := range p.RecentModelIDs {
			if id == "" {
				continue
			}
			p.RecentModels = append(p.RecentModels, ProviderModelRef{
				ProviderID: defaultProviderID,
				ModelID:    id,
			})
		}
		p.RecentModelIDs = nil
		return p
	})
}

func (l *LocalProvider) RememberModel(providerID, modelID string) {
	l.Update(func(p LocalPreferences) LocalPreferences {
		ref := ProviderModelRef{ProviderID: providerID, ModelID: modelID}
		filtered := make([]ProviderModelRef, 0, len(p.RecentModels))
		for _, r := range p.RecentModels {
			if r.ProviderID == ref.ProviderID && r.ModelID == ref.ModelID {
				continue
			}
			filtered = append(filtered, r)
		}
		p.RecentModels = append([]ProviderModelRef{ref}, filtered...)
		if len(p.RecentModels) > 10 {
			p.RecentModels = p.RecentModels[:10]
		}
		return p
	})
}

func (l *LocalProvider) ToggleFavoriteModel(providerID, modelID string) {
	l.Update(func(p LocalPreferences) LocalPreferences {
		ref := ProviderModelRef{ProviderID: providerID, ModelID: modelID}
		for i, r := range p.FavoriteModels {
			if r.ProviderID == ref.ProviderID && r.ModelID == ref.ModelID {
				p.FavoriteModels = append(p.FavoriteModels[:i], p.FavoriteModels[i+1:]...)
				return p
			}
		}
		p.FavoriteModels = append(p.FavoriteModels, ref)
		return p
	})
}

func LoadLocalPreferences(path string) (LocalPreferences, error) {
	var p LocalPreferences
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewLocalProvider().Get(), nil
		}
		return p, err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	return p, nil
}

func SaveLocalPreferences(path string, p LocalPreferences) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}

func RecentModelIDs(models []ProviderModelRef) []string {
	ids := make([]string, 0, len(models))
	for _, r := range models {
		ids = append(ids, r.ModelID)
	}
	return ids
}

func (l *LocalProvider) SetPreferredAgent(name string) {
	l.Update(func(p LocalPreferences) LocalPreferences {
		p.PreferredAgent = name
		return p
	})
}

func (l *LocalProvider) SetPreferredPermissionMode(mode string) {
	l.Update(func(p LocalPreferences) LocalPreferences {
		p.PreferredPermissionMode = mode
		return p
	})
}

func (l *LocalProvider) SetPreferredModel(providerID, modelID string) {
	l.Update(func(p LocalPreferences) LocalPreferences {
		if p.PreferredModels == nil {
			p.PreferredModels = make(map[string]string)
		}
		p.PreferredModels[providerID] = modelID
		return p
	})
}

func (l *LocalProvider) SetThemeName(name string) {
	l.Update(func(p LocalPreferences) LocalPreferences {
		p.ThemeName = name
		return p
	})
}

func (l *LocalProvider) PreferredModelForProvider(providerID string) string {
	prefs := l.Get()
	if prefs.PreferredModels == nil {
		return ""
	}
	return prefs.PreferredModels[providerID]
}
