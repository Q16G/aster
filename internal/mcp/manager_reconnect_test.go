package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpprotocol "github.com/mark3labs/mcp-go/mcp"
)

// shrinkBackoffs 把重连退避缩到极短，避免重试相关用例拖慢测试。
// 这些用例不调用 t.Parallel()，串行执行下改写包级变量是安全的。
func shrinkBackoffs(t *testing.T) {
	t.Helper()
	old := reconnectBackoffs
	reconnectBackoffs = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { reconnectBackoffs = old })
}

// fakeMCPClient 是一个可配置行为的 MCPClient stub，用于驱动重连/重试路径。
type fakeMCPClient struct {
	mu         sync.Mutex
	pingErr    error
	callErr    error
	callResult *mcpprotocol.CallToolResult
	callCount  int
	closed     bool
}

func (c *fakeMCPClient) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCount
}

func (c *fakeMCPClient) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *fakeMCPClient) Initialize(context.Context, mcpprotocol.InitializeRequest) (*mcpprotocol.InitializeResult, error) {
	return &mcpprotocol.InitializeResult{}, nil
}

func (c *fakeMCPClient) Ping(context.Context) error { return c.pingErr }

func (c *fakeMCPClient) ListTools(context.Context, mcpprotocol.ListToolsRequest) (*mcpprotocol.ListToolsResult, error) {
	return &mcpprotocol.ListToolsResult{Tools: []mcpprotocol.Tool{{Name: "echo"}}}, nil
}

func (c *fakeMCPClient) ListToolsByPage(context.Context, mcpprotocol.ListToolsRequest) (*mcpprotocol.ListToolsResult, error) {
	return &mcpprotocol.ListToolsResult{Tools: []mcpprotocol.Tool{{Name: "echo"}}}, nil
}

func (c *fakeMCPClient) CallTool(context.Context, mcpprotocol.CallToolRequest) (*mcpprotocol.CallToolResult, error) {
	c.mu.Lock()
	c.callCount++
	closed := c.closed
	err := c.callErr
	res := c.callResult
	c.mu.Unlock()
	if closed {
		return nil, errors.New("client closed")
	}
	if err != nil {
		return nil, err
	}
	if res != nil {
		return res, nil
	}
	return &mcpprotocol.CallToolResult{}, nil
}

func (c *fakeMCPClient) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	return nil
}

func (c *fakeMCPClient) ListResourcesByPage(context.Context, mcpprotocol.ListResourcesRequest) (*mcpprotocol.ListResourcesResult, error) {
	return nil, nil
}
func (c *fakeMCPClient) ListResources(context.Context, mcpprotocol.ListResourcesRequest) (*mcpprotocol.ListResourcesResult, error) {
	return nil, nil
}
func (c *fakeMCPClient) ListResourceTemplatesByPage(context.Context, mcpprotocol.ListResourceTemplatesRequest) (*mcpprotocol.ListResourceTemplatesResult, error) {
	return nil, nil
}
func (c *fakeMCPClient) ListResourceTemplates(context.Context, mcpprotocol.ListResourceTemplatesRequest) (*mcpprotocol.ListResourceTemplatesResult, error) {
	return nil, nil
}
func (c *fakeMCPClient) ReadResource(context.Context, mcpprotocol.ReadResourceRequest) (*mcpprotocol.ReadResourceResult, error) {
	return nil, nil
}
func (c *fakeMCPClient) Subscribe(context.Context, mcpprotocol.SubscribeRequest) error   { return nil }
func (c *fakeMCPClient) Unsubscribe(context.Context, mcpprotocol.UnsubscribeRequest) error { return nil }
func (c *fakeMCPClient) ListPromptsByPage(context.Context, mcpprotocol.ListPromptsRequest) (*mcpprotocol.ListPromptsResult, error) {
	return nil, nil
}
func (c *fakeMCPClient) ListPrompts(context.Context, mcpprotocol.ListPromptsRequest) (*mcpprotocol.ListPromptsResult, error) {
	return nil, nil
}
func (c *fakeMCPClient) GetPrompt(context.Context, mcpprotocol.GetPromptRequest) (*mcpprotocol.GetPromptResult, error) {
	return nil, nil
}
func (c *fakeMCPClient) SetLevel(context.Context, mcpprotocol.SetLevelRequest) error { return nil }
func (c *fakeMCPClient) Complete(context.Context, mcpprotocol.CompleteRequest) (*mcpprotocol.CompleteResult, error) {
	return nil, nil
}
func (c *fakeMCPClient) OnNotification(func(mcpprotocol.JSONRPCNotification)) {}

// clientFactory 按顺序分发预置的 fake client，记录构造次数（即建链/重连次数）。
// count 统计 next 被调用的总次数（含失败尝试）。当 errAfter>0 且已分发次数达到
// 该阈值时，next 返回错误以模拟重连握手失败；超出预置列表时新建的 client 继承 extraErr。
type clientFactory struct {
	mu       sync.Mutex
	clients  []*fakeMCPClient
	count    int
	extraErr error
	errAfter int
}

func (f *clientFactory) next(*MCPServerConfig, map[string]string) (mcpclient.MCPClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := f.count
	f.count++
	if f.errAfter > 0 && idx >= f.errAfter {
		return nil, errors.New("spawn failed")
	}
	if idx < len(f.clients) {
		return f.clients[idx], nil
	}
	extra := &fakeMCPClient{callErr: f.extraErr}
	f.clients = append(f.clients, extra)
	return extra, nil
}

func (f *clientFactory) created() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

func newFakeManager(t *testing.T, f *clientFactory) *Manager {
	t.Helper()
	mgr := NewManager()
	mgr.newClientFn = f.next
	mgr.RegisterServer("svc", &MCPServerConfig{Name: "svc", Type: "stdio", Command: "x"})
	if _, err := mgr.Connect(context.Background(), "svc"); err != nil {
		t.Fatalf("initial connect: %v", err)
	}
	return mgr
}

// Ping 探活失败时，CallTool 应先重连再调用，最终成功。
func TestCallTool_ReconnectsOnPingFailure(t *testing.T) {
	c1 := &fakeMCPClient{pingErr: errors.New("dead pipe")}
	c2 := &fakeMCPClient{}
	f := &clientFactory{clients: []*fakeMCPClient{c1, c2}}
	mgr := newFakeManager(t, f)

	if _, err := mgr.CallTool(context.Background(), "svc", mcpprotocol.CallToolRequest{}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if !c1.isClosed() {
		t.Fatal("expected stale client closed after reconnect")
	}
	if c2.calls() != 1 {
		t.Fatalf("expected reconnected client to serve 1 call, got %d", c2.calls())
	}
	if f.created() != 2 {
		t.Fatalf("expected 2 clients created (connect + reconnect), got %d", f.created())
	}
}

// 并发多次失败应只触发一次重连（代次去重），其余复用新连接。
func TestCallTool_ConcurrentReconnectDedup(t *testing.T) {
	shrinkBackoffs(t)
	c1 := &fakeMCPClient{callErr: errors.New("connection reset")}
	c2 := &fakeMCPClient{}
	f := &clientFactory{clients: []*fakeMCPClient{c1, c2}}
	mgr := newFakeManager(t, f)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = mgr.CallTool(context.Background(), "svc", mcpprotocol.CallToolRequest{})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
	}
	if f.created() != 2 {
		t.Fatalf("expected exactly one reconnect (2 clients total), got %d", f.created())
	}
}

// ctx 已取消时不应重连（避免在用户取消/超时下反复重启进程）。
func TestCallTool_CanceledCtxSkipsReconnect(t *testing.T) {
	c1 := &fakeMCPClient{callErr: errors.New("connection reset")}
	f := &clientFactory{clients: []*fakeMCPClient{c1}}
	mgr := newFakeManager(t, f)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := mgr.CallTool(ctx, "svc", mcpprotocol.CallToolRequest{}); err == nil {
		t.Fatal("expected error from canceled call")
	}
	if f.created() != 1 {
		t.Fatalf("expected no reconnect under canceled ctx (1 client), got %d", f.created())
	}
}

// 健康连接（ping ok + call ok）不应重连，原 client 直接服务一次。
func TestCallTool_HealthyNoReconnect(t *testing.T) {
	c1 := &fakeMCPClient{}
	f := &clientFactory{clients: []*fakeMCPClient{c1}}
	mgr := newFakeManager(t, f)

	if _, err := mgr.CallTool(context.Background(), "svc", mcpprotocol.CallToolRequest{}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if f.created() != 1 {
		t.Fatalf("expected no reconnect, got %d clients", f.created())
	}
	if c1.calls() != 1 {
		t.Fatalf("expected 1 call on original client, got %d", c1.calls())
	}
}

// ping 正常但调用本身传输失败：应重连并重试成功（响应式主路径，显式断言）。
func TestCallTool_ReactiveReconnectAfterCallFailure(t *testing.T) {
	shrinkBackoffs(t)
	c1 := &fakeMCPClient{callErr: errors.New("broken pipe")}
	c2 := &fakeMCPClient{}
	f := &clientFactory{clients: []*fakeMCPClient{c1, c2}}
	mgr := newFakeManager(t, f)

	if _, err := mgr.CallTool(context.Background(), "svc", mcpprotocol.CallToolRequest{}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if c2.calls() != 1 {
		t.Fatalf("expected reconnected client to serve the retry, got %d", c2.calls())
	}
	if f.created() != 2 {
		t.Fatalf("expected 2 clients (connect + reconnect), got %d", f.created())
	}
}

// 业务错误（result.IsError=true 且无 Go error）必须原样透传，绝不触发重连。
func TestCallTool_BusinessErrorDoesNotReconnect(t *testing.T) {
	c1 := &fakeMCPClient{callResult: &mcpprotocol.CallToolResult{
		IsError: true,
		Content: []mcpprotocol.Content{mcpprotocol.TextContent{Type: "text", Text: "bad input"}},
	}}
	f := &clientFactory{clients: []*fakeMCPClient{c1}}
	mgr := newFakeManager(t, f)

	res, err := mgr.CallTool(context.Background(), "svc", mcpprotocol.CallToolRequest{})
	if err != nil {
		t.Fatalf("business error must not surface as transport error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected business error result passed through, got %+v", res)
	}
	if f.created() != 1 {
		t.Fatalf("business error must not trigger reconnect, got %d clients", f.created())
	}
}

// 持续传输失败：退避重试有界，耗尽 maxReconnectAttempts 后返回错误。
func TestCallTool_RetryExhaustionReturnsError(t *testing.T) {
	shrinkBackoffs(t)
	f := &clientFactory{extraErr: errors.New("server down")}
	mgr := newFakeManager(t, f)

	if _, err := mgr.CallTool(context.Background(), "svc", mcpprotocol.CallToolRequest{}); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if want := 1 + maxReconnectAttempts; f.created() != want {
		t.Fatalf("expected %d clients (connect + %d reconnects), got %d", want, maxReconnectAttempts, f.created())
	}
}

// 重连握手本身失败：CallTool 返回错误，server 状态置为 error，后续调用可自愈。
func TestCallTool_ReconnectHandshakeFailureSetsError(t *testing.T) {
	shrinkBackoffs(t)
	c1 := &fakeMCPClient{pingErr: errors.New("dead pipe")}
	f := &clientFactory{clients: []*fakeMCPClient{c1}, errAfter: 1}
	mgr := newFakeManager(t, f)

	if _, err := mgr.CallTool(context.Background(), "svc", mcpprotocol.CallToolRequest{}); err == nil {
		t.Fatal("expected error when reconnect handshake fails")
	}
	entries := mgr.ServerEntries()
	if len(entries) != 1 || entries[0].Status != MCPStatusError {
		t.Fatalf("expected server status error after failed reconnect, got %+v", entries)
	}
}

// server 已注册但尚未连接时，CallTool 应自动建立连接并成功。
func TestCallTool_AutoConnectsWhenNotConnected(t *testing.T) {
	c1 := &fakeMCPClient{}
	f := &clientFactory{clients: []*fakeMCPClient{c1}}
	mgr := NewManager()
	mgr.newClientFn = f.next
	mgr.RegisterServer("svc", &MCPServerConfig{Name: "svc", Type: "stdio", Command: "x"})

	if _, err := mgr.CallTool(context.Background(), "svc", mcpprotocol.CallToolRequest{}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if f.created() != 1 {
		t.Fatalf("expected exactly one connect, got %d", f.created())
	}
	if c1.calls() != 1 {
		t.Fatalf("expected 1 call after auto-connect, got %d", c1.calls())
	}
}

// 对未知 server 调用应返回错误。
func TestCallTool_UnknownServer(t *testing.T) {
	mgr := NewManager()
	if _, err := mgr.CallTool(context.Background(), "ghost", mcpprotocol.CallToolRequest{}); err == nil {
		t.Fatal("expected error for unknown server")
	}
}

// 端到端验证 ToolAdapter.Execute 经 Manager.CallTool 路由，并在断链时透明重连。
func TestToolAdapter_ExecuteRoutesThroughManager(t *testing.T) {
	shrinkBackoffs(t)
	c1 := &fakeMCPClient{callErr: errors.New("broken pipe")}
	c2 := &fakeMCPClient{}
	f := &clientFactory{clients: []*fakeMCPClient{c1, c2}}
	mgr := newFakeManager(t, f)

	adapters := mgr.GetAdapters("svc")
	if len(adapters) != 1 {
		t.Fatalf("expected 1 adapter, got %d", len(adapters))
	}
	if _, err := adapters[0].Execute(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("adapter Execute: %v", err)
	}
	if c2.calls() != 1 {
		t.Fatalf("expected reconnected client to serve adapter call, got %d", c2.calls())
	}
	if f.created() != 2 {
		t.Fatalf("expected reconnect via adapter path, got %d clients", f.created())
	}
}
