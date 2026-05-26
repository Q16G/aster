package tui

import (
	"strings"
	"testing"

	"aster/internal/mcp"
)

func TestMCPEntryDescriptionIncludesErrorDetail(t *testing.T) {
	desc := mcpEntryDescription(&mcp.MCPServerEntry{
		Status: mcp.MCPStatusError,
		Error:  `exec: "yak": executable file not found in $PATH`,
	})

	if !strings.Contains(desc, "error: exec: \"yak\"") {
		t.Fatalf("expected description to include MCP error detail, got %q", desc)
	}
}
