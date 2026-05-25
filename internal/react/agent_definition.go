package react

import (
	"aster/internal/mcp"
)

// AgentDefinition is the declarative specification for an Agent.
// Different agents are expressed as different definitions, not different runtimes.
type AgentDefinition struct {
	Name            string
	Role            string
	Background      string
	Instruction     string
	ModelID         string
	ToolNames       []string
	SkillNames      []string
	PreloadSkills   []string
	MCPServers      []*mcp.MCPServerConfig
	Policies        AgentPolicies
	Context         []TaskContextEntry
}

// AgentPolicies controls runtime behavior boundaries.
type AgentPolicies struct {
	MaxIterations           int
	AllowBash               bool
	BashPermissionContext   *BashToolConfig
	ResultSource            ResultSource
	EnableHistoryCompaction bool
}

// BuildTaskContext converts Context entries into TaskContextData.
func (d *AgentDefinition) BuildTaskContext() *TaskContextData {
	if len(d.Context) == 0 {
		return nil
	}
	entries := make([]TaskContextEntry, len(d.Context))
	copy(entries, d.Context)
	return &TaskContextData{Entries: entries}
}
