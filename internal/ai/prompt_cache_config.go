package ai

type PromptCacheConfig struct {
	Enabled   bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Mode      string   `yaml:"mode,omitempty" json:"mode,omitempty"`
	Retention string   `yaml:"retention,omitempty" json:"retention,omitempty"`
	TTL       string   `yaml:"ttl,omitempty" json:"ttl,omitempty"`
	Families  []string `yaml:"families,omitempty" json:"families,omitempty"`
}

func (c *PromptCacheConfig) Clone() *PromptCacheConfig {
	if c == nil {
		return nil
	}
	cloned := *c
	if len(c.Families) > 0 {
		cloned.Families = append([]string(nil), c.Families...)
	}
	return &cloned
}
