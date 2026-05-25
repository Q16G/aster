package mcp

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type MCPServerConfig struct {
	Name        string            `yaml:"-"`
	Description string            `yaml:"description"`
	Type        string            `yaml:"type"`
	Command     string            `yaml:"command,omitempty"`
	Args        []string          `yaml:"args,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
	URL         string            `yaml:"url,omitempty"`
	Headers     map[string]string `yaml:"headers,omitempty"`
	Resident    bool              `yaml:"resident,omitempty"`
	Timeout     *int              `yaml:"timeout,omitempty"`
}

type Config struct {
	GlobalEnv  map[string]string           `yaml:"-"`
	MCPServers map[string]*MCPServerConfig `yaml:"mcp_servers"`
}

func MergeEnv(global, perServer map[string]string) map[string]string {
	merged := make(map[string]string, len(global)+len(perServer))
	for k, v := range global {
		merged[k] = v
	}
	for k, v := range perServer {
		merged[k] = v
	}
	return merged
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read mcp config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse mcp config %q: %w", path, err)
	}

	for name, sc := range cfg.MCPServers {
		if sc == nil {
			delete(cfg.MCPServers, name)
			continue
		}
		sc.Name = name
		expandEnvInHeaders(sc)
	}

	return &cfg, nil
}

func expandEnvInHeaders(sc *MCPServerConfig) {
	for k, v := range sc.Headers {
		sc.Headers[k] = os.Expand(v, func(key string) string {
			if val, ok := os.LookupEnv(key); ok {
				return val
			}
			return "${" + key + "}"
		})
	}
}

func (c *MCPServerConfig) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("mcp server %q: type is required", c.Name)
	}
	switch strings.ToLower(c.Type) {
	case "stdio":
		if c.Command == "" {
			return fmt.Errorf("mcp server %q: command is required for stdio type", c.Name)
		}
	case "sse", "streamable-http":
		if c.URL == "" {
			return fmt.Errorf("mcp server %q: url is required for %s type", c.Name, c.Type)
		}
	default:
		return fmt.Errorf("mcp server %q: unsupported type %q", c.Name, c.Type)
	}
	return nil
}
