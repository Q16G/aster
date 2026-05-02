package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"aster/internal/mcp"
	"aster/internal/react"

	"gopkg.in/yaml.v3"
)

type ProfileYAML struct {
	Name        string                 `yaml:"name"`
	Role        string                 `yaml:"role,omitempty"`
	Background  string                 `yaml:"background,omitempty"`
	Instruction string                 `yaml:"instruction,omitempty"`
	ModelID     string                 `yaml:"model_id,omitempty"`
	SkillNames  []string               `yaml:"skill_names,omitempty"`
	ToolNames   []string               `yaml:"tool_names,omitempty"`
	Policies    *ProfilePoliciesYAML   `yaml:"policies,omitempty"`
	MCPServers  []ProfileMCPServerYAML `yaml:"mcp_servers,omitempty"`
}

type ProfilePoliciesYAML struct {
	MaxIterations           *int  `yaml:"max_iterations,omitempty"`
	AllowBash               *bool `yaml:"allow_bash,omitempty"`
	EnableHistoryCompaction *bool `yaml:"enable_history_compaction,omitempty"`
}

type ProfileMCPServerYAML struct {
	Name    string            `yaml:"name"`
	Type    string            `yaml:"type"`
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	URL     string            `yaml:"url,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
}

func LoadProfilesFromDir(dir string) ([]ProfileYAML, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read profiles dir %q: %w", dir, err)
	}

	var profiles []ProfileYAML
	var names []string

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var p ProfileYAML
		if err := yaml.Unmarshal(data, &p); err != nil {
			continue
		}
		if p.Name == "" {
			p.Name = strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

func (r *ProfileRegistry) MergeYAML(p ProfileYAML) {
	if p.Name == "" {
		return
	}

	existing, found := r.profiles[p.Name]
	if !found {
		existing = react.AgentDefinition{
			Name:     p.Name,
			Policies: defaultPolicies(),
		}
	}

	if p.Role != "" {
		existing.Role = p.Role
	}
	if p.Background != "" {
		existing.Background = p.Background
	}
	if p.Instruction != "" {
		existing.Instruction = p.Instruction
	}
	if p.ModelID != "" {
		existing.ModelID = p.ModelID
	}
	if len(p.SkillNames) > 0 {
		existing.SkillNames = p.SkillNames
	}
	if len(p.ToolNames) > 0 {
		existing.ToolNames = p.ToolNames
	}

	if p.Policies != nil {
		if p.Policies.MaxIterations != nil {
			existing.Policies.MaxIterations = *p.Policies.MaxIterations
		}
		if p.Policies.AllowBash != nil {
			existing.Policies.AllowBash = *p.Policies.AllowBash
		}
		if p.Policies.EnableHistoryCompaction != nil {
			existing.Policies.EnableHistoryCompaction = *p.Policies.EnableHistoryCompaction
		}
	}

	if len(p.MCPServers) > 0 {
		configs := make([]*mcp.MCPServerConfig, 0, len(p.MCPServers))
		for _, s := range p.MCPServers {
			configs = append(configs, &mcp.MCPServerConfig{
				Name:    s.Name,
				Type:    s.Type,
				Command: s.Command,
				Args:    s.Args,
				URL:     s.URL,
				Env:     s.Env,
			})
		}
		existing.MCPServers = configs
	}

	if !found {
		r.order = append(r.order, p.Name)
	}
	r.profiles[p.Name] = existing
}
