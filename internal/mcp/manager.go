package mcp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpprotocol "github.com/mark3labs/mcp-go/mcp"
	"golang.org/x/net/proxy"
)

const (
	defaultConnectTimeout  = 30 * time.Second
	defaultResponseTimeout = 30 * time.Second
	maxReconnectAttempts   = 3
	pingTimeout            = 5 * time.Second
)

// reconnectBackoffs 控制重连重试的退避节奏，长度须等于 maxReconnectAttempts。
var reconnectBackoffs = []time.Duration{
	200 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
}

type WarningEmitter interface {
	EmitWarning(message string)
}

type Manager struct {
	mu        sync.Mutex
	globalEnv map[string]string
	servers   map[string]*MCPServerEntry
	clients   map[string]mcpclient.MCPClient
	adapters  map[string][]*ToolAdapter

	// generations 记录每个 server 的连接代次，每次成功建立连接自增，
	// 供 CallTool/reconnect 做并发去重（避免对同一断链重复重启进程）。
	generations map[string]uint64
	// reconnectLocks 为每个 server 提供一把重连串行锁。
	reconnectLocks map[string]*sync.Mutex
	// newClientFn 默认指向 createClient，测试可注入以替换底层 client 构造。
	newClientFn func(cfg *MCPServerConfig, env map[string]string) (mcpclient.MCPClient, error)

	// onStatusChange 在服务器状态发生运行时迁移后（锁外）被调用，
	// 供 UI 做事件驱动刷新。在 program 启动前设置一次，无并发问题。
	onStatusChange func(name string, status MCPServerStatus)
}

func NewManager() *Manager {
	return &Manager{
		servers:        make(map[string]*MCPServerEntry),
		clients:        make(map[string]mcpclient.MCPClient),
		adapters:       make(map[string][]*ToolAdapter),
		generations:    make(map[string]uint64),
		reconnectLocks: make(map[string]*sync.Mutex),
		newClientFn:    createClient,
	}
}

// SetStatusChangeHandler 注册状态变更回调。需在任何 Connect/Disconnect 之前调用一次。
func (m *Manager) SetStatusChangeHandler(fn func(name string, status MCPServerStatus)) {
	m.onStatusChange = fn
}

// notifyStatus 必须在释放 m.mu 之后调用，避免持锁回调进 UI。
func (m *Manager) notifyStatus(name string, status MCPServerStatus) {
	if m.onStatusChange != nil {
		m.onStatusChange(name, status)
	}
}

func (m *Manager) LoadFromConfigWithProbe(ctx context.Context, cfg *Config, emitter WarningEmitter) {
	if cfg == nil || len(cfg.MCPServers) == 0 {
		return
	}

	m.mu.Lock()
	m.globalEnv = cfg.GlobalEnv
	m.mu.Unlock()

	type probeResult struct {
		name    string
		cfg     *MCPServerConfig
		client  mcpclient.MCPClient
		ads     []*ToolAdapter
		names   []string
		connAt  time.Time
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []probeResult
		warns   []string
	)

	for name, serverCfg := range cfg.MCPServers {
		if err := serverCfg.Validate(); err != nil {
			mu.Lock()
			warns = append(warns, fmt.Sprintf("MCP Server %q 配置无效，已跳过: %s", name, err))
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(name string, serverCfg *MCPServerConfig) {
			defer wg.Done()

			mergedEnv := MergeEnv(cfg.GlobalEnv, serverCfg.Env)

			probeCtx, cancel := context.WithTimeout(ctx, serverTimeout(serverCfg))
			defer cancel()

			client, ads, toolNames, err := m.establish(probeCtx, name, serverCfg, mergedEnv)
			if err != nil {
				mu.Lock()
				warns = append(warns, fmt.Sprintf("MCP Server %q 不可用，已跳过: %s", name, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			results = append(results, probeResult{
				name:   name,
				cfg:    serverCfg,
				client: client,
				ads:    ads,
				names:  toolNames,
				connAt: time.Now(),
			})
			mu.Unlock()
		}(name, serverCfg)
	}

	wg.Wait()

	if emitter != nil {
		for _, w := range warns {
			emitter.EmitWarning(w)
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range results {
		m.clients[r.name] = r.client
		m.adapters[r.name] = r.ads
		m.generations[r.name]++
		m.servers[r.name] = &MCPServerEntry{
			Name:        r.name,
			Config:      r.cfg,
			Status:      MCPStatusConnected,
			ToolCount:   len(r.ads),
			ToolNames:   r.names,
			ConnectedAt: r.connAt,
		}
	}
}

func (m *Manager) RegisterServer(name string, cfg *MCPServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.servers[name]; exists {
		return
	}
	m.servers[name] = &MCPServerEntry{
		Name:   name,
		Config: cfg,
		Status: MCPStatusDisconnected,
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
	globalEnv := m.globalEnv
	m.mu.Unlock()
	m.notifyStatus(name, MCPStatusConnecting)

	mergedEnv := MergeEnv(globalEnv, entry.Config.Env)
	client, ads, toolNames, err := m.establish(ctx, name, entry.Config, mergedEnv)
	if err != nil {
		m.setError(name, err)
		return nil, fmt.Errorf("connect mcp %q: %w", name, err)
	}

	m.mu.Lock()
	m.clients[name] = client
	m.adapters[name] = ads
	m.generations[name]++
	entry.Status = MCPStatusConnected
	entry.ToolCount = len(ads)
	entry.ToolNames = toolNames
	entry.ConnectedAt = time.Now()
	m.mu.Unlock()
	m.notifyStatus(name, MCPStatusConnected)

	return ads, nil
}

// establish 执行一次完整握手：构造 client -> Initialize -> ListTools -> 构建 adapters。
// 仅负责建立连接与生成 adapter，不写入 Manager 的任何状态 map，由调用方决定如何存储。
// 失败时内部已关闭 client。adapter 以 m 作为 toolCaller，断线重连后透明切换底层连接。
func (m *Manager) establish(ctx context.Context, name string, cfg *MCPServerConfig, env map[string]string) (mcpclient.MCPClient, []*ToolAdapter, []string, error) {
	client, err := m.newClientFn(cfg, env)
	if err != nil {
		return nil, nil, nil, err
	}

	initReq := mcpprotocol.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpprotocol.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpprotocol.Implementation{
		Name:    "aster",
		Version: "1.0.0",
	}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		_ = client.Close()
		return nil, nil, nil, fmt.Errorf("initialize: %w", err)
	}

	toolsResult, err := client.ListTools(ctx, mcpprotocol.ListToolsRequest{})
	if err != nil {
		_ = client.Close()
		return nil, nil, nil, fmt.Errorf("list tools: %w", err)
	}

	ads := make([]*ToolAdapter, 0, len(toolsResult.Tools))
	toolNames := make([]string, 0, len(toolsResult.Tools))
	for _, t := range toolsResult.Tools {
		adapter := NewToolAdapter(name, t, m)
		ads = append(ads, adapter)
		toolNames = append(toolNames, adapter.Name())
	}
	return client, ads, toolNames, nil
}

// CallTool 在调用时解析实时连接并代发 CallTool，是 ToolAdapter 的 toolCaller 实现。
// 调用前先 Ping 探活，断链时按退避策略自动重连并重试，对上层透明。
func (m *Manager) CallTool(ctx context.Context, serverName string, req mcpprotocol.CallToolRequest) (*mcpprotocol.CallToolResult, error) {
	client, gen, ok := m.snapshotClient(serverName)
	if !ok {
		if _, err := m.Connect(ctx, serverName); err != nil {
			return nil, err
		}
		client, gen, ok = m.snapshotClient(serverName)
		if !ok {
			return nil, fmt.Errorf("mcp server %q not connected", serverName)
		}
	}

	// 调用前健康探测：Ping 失败且非用户取消时先重连，拿到可用连接再调用。
	if err := m.pingClient(ctx, client); err != nil && ctx.Err() == nil {
		if nc, ng, rerr := m.reconnect(ctx, serverName, gen); rerr == nil {
			client, gen = nc, ng
		}
	}

	result, err := client.CallTool(ctx, req)
	if err == nil {
		return result, nil
	}

	// 传输/协议层失败（业务错误经 result.IsError 返回，不会到这里）且非用户取消/超时：
	// 退避重连并重试；ctx 被取消则立即中止，避免反复重启进程。
	for attempt := 0; attempt < maxReconnectAttempts && ctx.Err() == nil; attempt++ {
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(reconnectBackoffs[attempt]):
		}

		nc, ng, rerr := m.reconnect(ctx, serverName, gen)
		if rerr != nil {
			continue
		}
		client, gen = nc, ng

		result, err = client.CallTool(ctx, req)
		if err == nil {
			return result, nil
		}
	}
	return nil, err
}

func (m *Manager) pingClient(ctx context.Context, client mcpclient.MCPClient) error {
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return client.Ping(pingCtx)
}

func (m *Manager) snapshotClient(name string) (mcpclient.MCPClient, uint64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	client, ok := m.clients[name]
	if !ok {
		return nil, 0, false
	}
	return client, m.generations[name], true
}

func (m *Manager) reconnectLock(name string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk, ok := m.reconnectLocks[name]
	if !ok {
		lk = &sync.Mutex{}
		m.reconnectLocks[name] = lk
	}
	return lk
}

// reconnect 串行重建指定 server 的连接并返回新 client 与代次。
// 进锁后若发现代次已变化（其他并发者已重连），直接复用其结果以去重。
func (m *Manager) reconnect(ctx context.Context, name string, observedGen uint64) (mcpclient.MCPClient, uint64, error) {
	lk := m.reconnectLock(name)
	lk.Lock()
	defer lk.Unlock()

	if client, gen, ok := m.snapshotClient(name); ok && gen != observedGen {
		return client, gen, nil
	}

	m.mu.Lock()
	entry, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
		return nil, 0, fmt.Errorf("mcp server %q not found", name)
	}
	cfg := entry.Config
	globalEnv := m.globalEnv
	if old, ok := m.clients[name]; ok {
		_ = old.Close()
		delete(m.clients, name)
	}
	m.mu.Unlock()

	mergedEnv := MergeEnv(globalEnv, cfg.Env)
	rctx, cancel := context.WithTimeout(ctx, serverTimeout(cfg))
	defer cancel()

	client, ads, toolNames, err := m.establish(rctx, name, cfg, mergedEnv)
	if err != nil {
		m.setError(name, err)
		return nil, 0, fmt.Errorf("reconnect mcp %q: %w", name, err)
	}

	m.mu.Lock()
	m.clients[name] = client
	m.adapters[name] = ads
	m.generations[name]++
	gen := m.generations[name]
	entry.Status = MCPStatusConnected
	entry.ToolCount = len(ads)
	entry.ToolNames = toolNames
	entry.ConnectedAt = time.Now()
	m.mu.Unlock()
	m.notifyStatus(name, MCPStatusConnected)

	return client, gen, nil
}

func (m *Manager) GetAdapters(name string) []*ToolAdapter {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.adapters[name]
}

func (m *Manager) Disconnect(name string) ([]string, error) {
	m.mu.Lock()

	entry, ok := m.servers[name]
	if !ok {
		m.mu.Unlock()
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

	m.mu.Unlock()
	m.notifyStatus(name, MCPStatusDisconnected)

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
	// Stable order: m.servers is a map (random iteration) filled by concurrent
	// probes, so without this the selector/sidebar reshuffle on every refresh.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
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
	sort.Strings(names)
	return names
}

func (m *Manager) CloseAll() {
	m.mu.Lock()

	for name, client := range m.clients {
		_ = client.Close()
		delete(m.clients, name)
	}
	disconnected := make([]string, 0, len(m.servers))
	for name, entry := range m.servers {
		entry.Status = MCPStatusDisconnected
		entry.ToolCount = 0
		entry.ToolNames = nil
		delete(m.adapters, name)
		disconnected = append(disconnected, name)
	}
	m.mu.Unlock()

	for _, name := range disconnected {
		m.notifyStatus(name, MCPStatusDisconnected)
	}
}

func (m *Manager) setError(name string, err error) {
	m.mu.Lock()
	_, ok := m.servers[name]
	if ok {
		entry := m.servers[name]
		entry.Status = MCPStatusError
		entry.Error = err.Error()
	}
	m.mu.Unlock()
	if ok {
		m.notifyStatus(name, MCPStatusError)
	}
}

func createClient(cfg *MCPServerConfig, env map[string]string) (mcpclient.MCPClient, error) {
	switch strings.ToLower(cfg.Type) {
	case "stdio":
		osEnv := os.Environ()
		for k, v := range env {
			osEnv = append(osEnv, k+"="+v)
		}
		return mcpclient.NewStdioMCPClient(cfg.Command, osEnv, cfg.Args...)
	case "sse":
		opts := buildSSEOptions(cfg, env)
		return mcpclient.NewSSEMCPClient(cfg.URL, opts...)
	case "streamable-http":
		opts := buildStreamableHTTPOptions(cfg, env)
		return mcpclient.NewStreamableHttpClient(cfg.URL, opts...)
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", cfg.Type)
	}
}

func buildSSEOptions(cfg *MCPServerConfig, env map[string]string) []transport.ClientOption {
	var opts []transport.ClientOption
	if len(cfg.Headers) > 0 {
		opts = append(opts, transport.WithHeaders(cfg.Headers))
	}
	if httpClient := buildProxyHTTPClient(env); httpClient != nil {
		opts = append(opts, transport.WithHTTPClient(httpClient))
	}
	endpointTimeout, responseTimeout := resolveTimeouts(cfg)
	opts = append(opts, transport.WithEndpointTimeout(endpointTimeout))
	opts = append(opts, transport.WithResponseTimeout(responseTimeout))
	return opts
}

func buildStreamableHTTPOptions(cfg *MCPServerConfig, env map[string]string) []transport.StreamableHTTPCOption {
	var opts []transport.StreamableHTTPCOption
	if len(cfg.Headers) > 0 {
		opts = append(opts, transport.WithHTTPHeaders(cfg.Headers))
	}
	if httpClient := buildProxyHTTPClient(env); httpClient != nil {
		opts = append(opts, transport.WithHTTPBasicClient(httpClient))
	}
	_, responseTimeout := resolveTimeouts(cfg)
	opts = append(opts, transport.WithHTTPTimeout(responseTimeout))
	return opts
}

func resolveTimeouts(cfg *MCPServerConfig) (endpoint, response time.Duration) {
	endpoint = defaultConnectTimeout
	response = defaultResponseTimeout
	if cfg.Timeout != nil && *cfg.Timeout > 0 {
		d := time.Duration(*cfg.Timeout) * time.Second
		endpoint = d
		response = d
	}
	return
}

func serverTimeout(cfg *MCPServerConfig) time.Duration {
	if cfg.Timeout != nil && *cfg.Timeout > 0 {
		return time.Duration(*cfg.Timeout) * time.Second
	}
	return defaultConnectTimeout
}

func buildProxyHTTPClient(env map[string]string) *http.Client {
	proxyStr := proxyFromEnv(env)
	if proxyStr == "" {
		return nil
	}
	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		return nil
	}
	tr := &http.Transport{}
	switch proxyURL.Scheme {
	case "http", "https":
		tr.Proxy = http.ProxyURL(proxyURL)
	case "socks5":
		dialer, err := proxy.SOCKS5("tcp", proxyURL.Host, nil, proxy.Direct)
		if err != nil {
			return nil
		}
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	default:
		return nil
	}
	return &http.Client{Transport: tr}
}

func proxyFromEnv(env map[string]string) string {
	for _, key := range []string{
		"HTTPS_PROXY", "https_proxy",
		"HTTP_PROXY", "http_proxy",
		"ALL_PROXY", "all_proxy",
	} {
		if v := strings.TrimSpace(env[key]); v != "" {
			return v
		}
	}
	return ""
}
