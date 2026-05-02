package mcp

import "time"

type MCPServerStatus string

const (
	MCPStatusLoaded       MCPServerStatus = "loaded"
	MCPStatusConnecting   MCPServerStatus = "connecting"
	MCPStatusConnected    MCPServerStatus = "connected"
	MCPStatusError        MCPServerStatus = "error"
	MCPStatusDisconnected MCPServerStatus = "disconnected"
)

type MCPServerEntry struct {
	Name        string
	Config      *MCPServerConfig
	Status      MCPServerStatus
	ToolCount   int
	ToolNames   []string
	Error       string
	ConnectedAt time.Time
}
