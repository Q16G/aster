package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpprotocol "github.com/mark3labs/mcp-go/mcp"
)

type WarningEmitter interface {
	EmitWarning(message string)
}

type Manager struct {
	mu       sync.Mutex
	servers  map[string]*MCPServerEntry
	clients  map[string]mcpclient.MCPClient
	adapters map[string][]*ToolAdapter
}

func NewManager() *Manager {
	return &Manager{
		servers:  make(map[string]*MCPServerEntry),
		clients:  make(map[string]mcpclient.MCPClient),
		adapters: make(map[string][]*ToolAdapter),
	}
}

func (m *Manager) LoadFromConfigWithProbe(ctx context.Context, cfg *Config, emitter WarningEmitter) {
	if cfg == nil || len(cfg.MCPServers) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for name, serverCfg := range cfg.MCPServers {
		if err := serverCfg.Validate(); err != nil {
			if emitter != nil {
				emitter.EmitWarning(fmt.Sprintf("MCP Server %q 配置无效，已跳过: %s", name, err))
			}
			continue
		}

		client, err := m.createClient(serverCfg)
		if err != nil {
			if emitter != nil {
				emitter.EmitWarning(fmt.Sprintf("MCP Server %q 不可用，已跳过: %s", name, err))
			}
			continue
		}

		initReq := mcpprotocol.InitializeRequest{}
		initReq.Params.ProtocolVersion = mcpprotocol.LATEST_PROTOCOL_VERSION
		initReq.Params.ClientInfo = mcpprotocol.Implementation{
			Name:    "aster",
			Version: "1.0.0",
		}
		if _, err := client.Initialize(ctx, initReq); err != nil {
			_ = client.Close()
			if emitter != nil {
				emitter.EmitWarning(fmt.Sprintf("MCP Server %q Initialize 失败，已跳过: %s", name, err))
			}
			continue
		}

		toolsResult, err := client.ListTools(ctx, mcpprotocol.ListToolsRequest{})
		if err != nil {
			_ = client.Close()
			if emitter != nil {
				emitter.EmitWarning(fmt.Sprintf("MCP Server %q ListTools 失败，已跳过: %s", name, err))
			}
			continue
		}

		ads := make([]*ToolAdapter, 0, len(toolsResult.Tools))
		toolNames := make([]string, 0, len(toolsResult.Tools))
		for _, t := range toolsResult.Tools {
			adapter := NewToolAdapter(name, t, client)
			ads = append(ads, adapter)
			toolNames = append(toolNames, adapter.Name())
		}

		m.clients[name] = client
		m.adapters[name] = ads
		m.servers[name] = &MCPServerEntry{
			Name:        name,
			Config:      serverCfg,
			Status:      MCPStatusConnected,
			ToolCount:   len(ads),
			ToolNames:   toolNames,
			ConnectedAt: time.Now(),
		}
	}
}

func (m *Manager) Connect(ctx context.Context, name string) ([]*ToolAdapter, error) {
	m.mu.Lock()
	entry, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("mcp server %q not found", name)
	}
	if entry.Status == MCPStatusConnected {
		ads := m.adapters[name]
		m.mu.Unlock()
		return ads, nil
	}

	entry.Status = MCPStatusConnecting
	entry.Error = ""
	m.mu.Unlock()

	client, err := m.createClient(entry.Config)
	if err != nil {
		m.setError(name, err)
		return nil, fmt.Errorf("create mcp client %q: %w", name, err)
	}

	initReq := mcpprotocol.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpprotocol.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpprotocol.Implementation{
		Name:    "aster",
		Version: "1.0.0",
	}

	if _, err := client.Initialize(ctx, initReq); err != nil {
		m.setError(name, err)
		return nil, fmt.Errorf("initialize mcp %q: %w", name, err)
	}

	toolsResult, err := client.ListTools(ctx, mcpprotocol.ListToolsRequest{})
	if err != nil {
		m.setError(name, err)
		return nil, fmt.Errorf("list tools from mcp %q: %w", name, err)
	}

	ads := make([]*ToolAdapter, 0, len(toolsResult.Tools))
	toolNames := make([]string, 0, len(toolsResult.Tools))
	for _, t := range toolsResult.Tools {
		adapter := NewToolAdapter(name, t, client)
		ads = append(ads, adapter)
		toolNames = append(toolNames, adapter.Name())
	}

	m.mu.Lock()
	m.clients[name] = client
	m.adapters[name] = ads
	entry.Status = MCPStatusConnected
	entry.ToolCount = len(ads)
	entry.ToolNames = toolNames
	entry.ConnectedAt = time.Now()
	m.mu.Unlock()

	return ads, nil
}

func (m *Manager) GetAdapters(name string) []*ToolAdapter {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.adapters[name]
}

func (m *Manager) Disconnect(name string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.servers[name]
	if !ok {
		return nil, fmt.Errorf("mcp server %q not found", name)
	}

	removedNames := make([]string, len(entry.ToolNames))
	copy(removedNames, entry.ToolNames)

	if client, ok := m.clients[name]; ok {
		_ = client.Close()
		delete(m.clients, name)
	}
	delete(m.adapters, name)

	entry.Status = MCPStatusDisconnected
	entry.ToolCount = 0
	entry.ToolNames = nil
	entry.Error = ""

	return removedNames, nil
}

func (m *Manager) LookupConfigs(names []string) []*MCPServerConfig {
	m.mu.Lock()
	defer m.mu.Unlock()

	var configs []*MCPServerConfig
	for _, name := range names {
		if entry, ok := m.servers[name]; ok {
			configs = append(configs, entry.Config)
		}
	}
	return configs
}

func (m *Manager) ServerEntries() []*MCPServerEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries := make([]*MCPServerEntry, 0, len(m.servers))
	for _, entry := range m.servers {
		cp := *entry
		entries = append(entries, &cp)
	}
	return entries
}

func (m *Manager) ResidentServers() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var names []string
	for name, entry := range m.servers {
		if entry.Config.Resident {
			names = append(names, name)
		}
	}
	return names
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, client := range m.clients {
		_ = client.Close()
		delete(m.clients, name)
	}
	for name, entry := range m.servers {
		entry.Status = MCPStatusDisconnected
		entry.ToolCount = 0
		entry.ToolNames = nil
		delete(m.adapters, name)
	}
}

func (m *Manager) setError(name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if entry, ok := m.servers[name]; ok {
		entry.Status = MCPStatusError
		entry.Error = err.Error()
	}
}

func (m *Manager) createClient(cfg *MCPServerConfig) (mcpclient.MCPClient, error) {
	switch strings.ToLower(cfg.Type) {
	case "stdio":
		env := os.Environ()
		for k, v := range cfg.Env {
			env = append(env, k+"="+v)
		}
		return mcpclient.NewStdioMCPClient(cfg.Command, env, cfg.Args...)
	case "sse":
		opts := buildHTTPOptions(cfg)
		return mcpclient.NewSSEMCPClient(cfg.URL, opts...)
	case "streamable-http":
		opts := buildStreamableHTTPOptions(cfg)
		return mcpclient.NewStreamableHttpClient(cfg.URL, opts...)
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", cfg.Type)
	}
}

func buildHTTPOptions(cfg *MCPServerConfig) []transport.ClientOption {
	var opts []transport.ClientOption
	if len(cfg.Headers) > 0 {
		opts = append(opts, transport.WithHeaders(cfg.Headers))
	}
	return opts
}

func buildStreamableHTTPOptions(cfg *MCPServerConfig) []transport.StreamableHTTPCOption {
	var opts []transport.StreamableHTTPCOption
	if len(cfg.Headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(cfg.Headers))
	}
	return opts
}
