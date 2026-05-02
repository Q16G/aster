package react

import (
	"fmt"
	"strings"

	"aster/internal/mcp"
)

type MCPManagerForPrompt interface {
	ServerEntries() []*mcp.MCPServerEntry
}

type MCPPromptContext struct {
	Table string
}

func (c *MCPPromptContext) HasTable() bool {
	return c != nil && strings.TrimSpace(c.Table) != ""
}

func BuildMCPPromptTable(entries []*mcp.MCPServerEntry) string {
	if len(entries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("| name | description | type | tool_count | status |\n")
	sb.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, e := range entries {
		toolCount := "-"
		if e.Status == mcp.MCPStatusConnected {
			toolCount = fmt.Sprintf("%d", e.ToolCount)
		}
		desc := e.Config.Description
		if desc == "" {
			desc = "-"
		}
		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s |\n",
			e.Name, desc, e.Config.Type, toolCount, string(e.Status)))
	}
	return strings.TrimSpace(sb.String())
}
