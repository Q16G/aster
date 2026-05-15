package ai

type PromptCacheConfig struct {
	Enabled bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	TTL     string `yaml:"ttl,omitempty" json:"ttl,omitempty"`
}

func (c *PromptCacheConfig) Clone() *PromptCacheConfig {
	if c == nil {
		return nil
	}
	cloned := *c
	return &cloned
}
