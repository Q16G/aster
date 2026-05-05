package tui

import (
	"aster/internal/builtin_tools"
	"aster/internal/react"
)

type ProfileRegistry struct {
	profiles map[string]react.AgentDefinition
	order    []string
}

func NewProfileRegistry() *ProfileRegistry {
	return &ProfileRegistry{
		profiles: make(map[string]react.AgentDefinition),
	}
}

func (r *ProfileRegistry) Register(def react.AgentDefinition) {
	if _, exists := r.profiles[def.Name]; !exists {
		r.order = append(r.order, def.Name)
	}
	r.profiles[def.Name] = def
}

func (r *ProfileRegistry) Get(name string) (react.AgentDefinition, bool) {
	def, ok := r.profiles[name]
	return def, ok
}

func (r *ProfileRegistry) List() []react.AgentDefinition {
	result := make([]react.AgentDefinition, 0, len(r.order))
	for _, name := range r.order {
		if def, ok := r.profiles[name]; ok {
			result = append(result, def)
		}
	}
	return result
}

func (r *ProfileRegistry) Names() []string {
	names := make([]string, len(r.order))
	copy(names, r.order)
	return names
}

func defaultPolicies() react.AgentPolicies {
	return react.AgentPolicies{
		MaxIterations: 1000,
		AllowBash:     true,
		BashPermissionContext: &react.BashToolConfig{
			PermCtx: &builtin_tools.BashPermissionContext{
				Mode: builtin_tools.PermissionModeManual,
			},
			SessionAL: builtin_tools.NewSessionAllowlist(),
		},
		EnableHistoryCompaction: true,
	}
}

func DefaultProfiles() []react.AgentDefinition {
	return nil
}
