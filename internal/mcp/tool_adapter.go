package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpprotocol "github.com/mark3labs/mcp-go/mcp"
)

type ToolAdapter struct {
	serverName string
	fullName   string
	tool       mcpprotocol.Tool
	client     mcpclient.MCPClient
}

func NewToolAdapter(serverName string, tool mcpprotocol.Tool, client mcpclient.MCPClient) *ToolAdapter {
	return &ToolAdapter{
		serverName: serverName,
		fullName:   tool.Name,
		tool:       tool,
		client:     client,
	}
}

func (a *ToolAdapter) Name() string         { return a.fullName }
func (a *ToolAdapter) Description() string  { return a.tool.Description }
func (a *ToolAdapter) OriginalName() string { return a.tool.Name }
func (a *ToolAdapter) ServerName() string   { return a.serverName }

func (a *ToolAdapter) Parameters() any {
	schema := a.tool.InputSchema
	result := map[string]any{
		"type": schema.Type,
	}
	if schema.Properties != nil {
		result["properties"] = schema.Properties
	} else {
		result["properties"] = map[string]any{}
	}
	if len(schema.Required) > 0 {
		result["required"] = schema.Required
	}
	return result
}

func (a *ToolAdapter) Execute(ctx context.Context, args map[string]any) (string, error) {
	req := mcpprotocol.CallToolRequest{}
	req.Params.Name = a.tool.Name
	req.Params.Arguments = args

	result, err := a.client.CallTool(ctx, req)
	if err != nil {
		return "", fmt.Errorf("mcp call %s/%s failed: %w", a.serverName, a.tool.Name, err)
	}

	return formatCallToolResult(result), nil
}

func formatCallToolResult(result *mcpprotocol.CallToolResult) string {
	if result == nil {
		return ""
	}

	var parts []string
	for _, c := range result.Content {
		switch v := c.(type) {
		case mcpprotocol.TextContent:
			parts = append(parts, v.Text)
		default:
			b, err := json.Marshal(v)
			if err == nil {
				parts = append(parts, string(b))
			}
		}
	}

	text := strings.Join(parts, "\n")
	if result.IsError {
		return fmt.Sprintf("[error] %s", text)
	}
	return text
}
