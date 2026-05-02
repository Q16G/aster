package tuicontext

type LocalPreferences struct {
	ToolVerbose    bool
	RecentModelIDs []string
	SidebarWidth   int
	ThemeName      string
}

type LocalProvider struct {
	*Provider[LocalPreferences]
}

func NewLocalProvider() *LocalProvider {
	return &LocalProvider{
		Provider: NewProvider(LocalPreferences{
			SidebarWidth: 24,
		}),
	}
}

func (l *LocalProvider) ToggleToolVerbose() {
	l.Update(func(p LocalPreferences) LocalPreferences {
		p.ToolVerbose = !p.ToolVerbose
		return p
	})
}

func (l *LocalProvider) RememberModel(modelID string) {
	l.Update(func(p LocalPreferences) LocalPreferences {
		filtered := make([]string, 0, len(p.RecentModelIDs))
		for _, id := range p.RecentModelIDs {
			if id != modelID {
				filtered = append(filtered, id)
			}
		}
		p.RecentModelIDs = append([]string{modelID}, filtered...)
		if len(p.RecentModelIDs) > 10 {
			p.RecentModelIDs = p.RecentModelIDs[:10]
		}
		return p
	})
}
