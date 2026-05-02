package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"aster/internal/mcp"

	"gopkg.in/yaml.v3"
)

type ProviderConfig struct {
	BaseURL      string `yaml:"base_url"`
	APIKey       string `yaml:"api_key"`
	DefaultModel string `yaml:"default_model"`
}

type AppConfig struct {
	Providers       map[string]*ProviderConfig      `yaml:"providers"`
	DefaultProvider string                          `yaml:"default_provider"`
	MCPServers      map[string]*mcp.MCPServerConfig `yaml:"mcp_servers"`
}

type ProviderState struct {
	BaseURL string
	APIKey  string
	ModelID string
}

const (
	AppName    = "ASTER"
	AppCLIName = "aster"
	AppDirName = ".aster"
)

func DefaultAppDir() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return AppDirName
	}
	return filepath.Join(home, AppDirName)
}

func DefaultConfigPath() string {
	return filepath.Join(DefaultAppDir(), "config.yaml")
}

func LoadConfig(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &AppConfig{}, nil
		}
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	expandProviderEnv(&cfg)
	populateMCPNames(&cfg)
	return &cfg, nil
}

func (c *AppConfig) ResolveProvider(cliProvider, cliModel, cliBaseURL, cliAPIKey string) (baseURL, apiKey, model string) {
	providerName := firstNonEmpty(cliProvider, os.Getenv("ASTER_PROVIDER"), c.DefaultProvider, "openai")

	var p *ProviderConfig
	if c.Providers != nil {
		p = c.Providers[providerName]
	}
	if p == nil {
		p = &ProviderConfig{}
	}

	baseURL = firstNonEmpty(cliBaseURL, os.Getenv("ASTER_BASE_URL"), p.BaseURL, "https://api.openai.com/v1")
	apiKey = firstNonEmpty(cliAPIKey, os.Getenv("ASTER_API_KEY"), p.APIKey)
	model = firstNonEmpty(cliModel, os.Getenv("ASTER_MODEL"), p.DefaultModel, "gpt-4o")
	return
}

func (c *AppConfig) ToMCPConfig() *mcp.Config {
	if len(c.MCPServers) == 0 {
		return nil
	}
	return &mcp.Config{MCPServers: c.MCPServers}
}

func expandProviderEnv(cfg *AppConfig) {
	expand := func(s string) string {
		return os.Expand(s, os.Getenv)
	}
	for _, p := range cfg.Providers {
		if p == nil {
			continue
		}
		p.BaseURL = expand(p.BaseURL)
		p.APIKey = expand(p.APIKey)
	}
}

func populateMCPNames(cfg *AppConfig) {
	for name, sc := range cfg.MCPServers {
		if sc == nil {
			delete(cfg.MCPServers, name)
			continue
		}
		sc.Name = name
		expandMCPHeaders(sc)
	}
}

func expandMCPHeaders(sc *mcp.MCPServerConfig) {
	for k, v := range sc.Headers {
		sc.Headers[k] = os.Expand(v, func(key string) string {
			if val, ok := os.LookupEnv(key); ok {
				return val
			}
			return "${" + key + "}"
		})
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
