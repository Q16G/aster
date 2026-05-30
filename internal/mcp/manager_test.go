package mcp

import (
	"context"
	"sync"
	"testing"
)

type stubEmitter struct {
	warnings []string
}

func (e *stubEmitter) EmitWarning(msg string) {
	e.warnings = append(e.warnings, msg)
}

func TestLoadFromConfigWithProbe_SkipsUnavailable(t *testing.T) {
	emitter := &stubEmitter{}
	mgr := NewManager()

	cfg := &Config{
		MCPServers: map[string]*MCPServerConfig{
			"missing-cmd": {
				Name:    "missing-cmd",
				Type:    "stdio",
				Command: "nonexistent-binary-that-does-not-exist-12345",
			},
		},
	}

	mgr.LoadFromConfigWithProbe(context.Background(), cfg, emitter)

	entries := mgr.ServerEntries()
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries (unavailable should be discarded), got %d", len(entries))
	}
	if len(emitter.warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(emitter.warnings))
	}
}

func TestLoadFromConfigWithProbe_SkipsInvalidConfig(t *testing.T) {
	emitter := &stubEmitter{}
	mgr := NewManager()

	cfg := &Config{
		MCPServers: map[string]*MCPServerConfig{
			"bad": {
				Name: "bad",
				Type: "stdio",
				// missing command
			},
		},
	}

	mgr.LoadFromConfigWithProbe(context.Background(), cfg, emitter)

	if len(mgr.ServerEntries()) != 0 {
		t.Fatal("expected invalid config to be skipped")
	}
	if len(emitter.warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(emitter.warnings))
	}
}

func TestResidentServers(t *testing.T) {
	mgr := NewManager()
	mgr.servers["a"] = &MCPServerEntry{
		Name:   "a",
		Config: &MCPServerConfig{Resident: true},
		Status: MCPStatusConnected,
	}
	mgr.servers["b"] = &MCPServerEntry{
		Name:   "b",
		Config: &MCPServerConfig{Resident: false},
		Status: MCPStatusConnected,
	}

	residents := mgr.ResidentServers()
	if len(residents) != 1 || residents[0] != "a" {
		t.Fatalf("expected [a], got %v", residents)
	}
}

func TestServerEntriesStableOrder(t *testing.T) {
	mgr := NewManager()
	for _, name := range []string{"delta", "alpha", "charlie", "bravo"} {
		mgr.servers[name] = &MCPServerEntry{
			Name:   name,
			Config: &MCPServerConfig{},
			Status: MCPStatusConnected,
		}
	}

	want := []string{"alpha", "bravo", "charlie", "delta"}
	for call := 0; call < 5; call++ {
		entries := mgr.ServerEntries()
		if len(entries) != len(want) {
			t.Fatalf("call %d: got %d entries, want %d", call, len(entries), len(want))
		}
		for i, e := range entries {
			if e.Name != want[i] {
				t.Fatalf("call %d: entries[%d]=%q, want %q", call, i, e.Name, want[i])
			}
		}
	}
}

func TestResidentServersStableOrder(t *testing.T) {
	mgr := NewManager()
	for _, name := range []string{"delta", "alpha", "charlie", "bravo"} {
		mgr.servers[name] = &MCPServerEntry{
			Name:   name,
			Config: &MCPServerConfig{Resident: true},
			Status: MCPStatusConnected,
		}
	}

	want := []string{"alpha", "bravo", "charlie", "delta"}
	for call := 0; call < 5; call++ {
		residents := mgr.ResidentServers()
		if len(residents) != len(want) {
			t.Fatalf("call %d: got %d residents, want %d", call, len(residents), len(want))
		}
		for i, n := range residents {
			if n != want[i] {
				t.Fatalf("call %d: residents[%d]=%q, want %q", call, i, n, want[i])
			}
		}
	}
}

func TestGetAdapters_Empty(t *testing.T) {
	mgr := NewManager()
	adapters := mgr.GetAdapters("nonexistent")
	if len(adapters) != 0 {
		t.Fatalf("expected nil adapters for unknown server, got %d", len(adapters))
	}
}

func TestDisconnect_UnknownServer(t *testing.T) {
	mgr := NewManager()
	_, err := mgr.Disconnect("unknown")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}

func TestConnect_UnknownServer(t *testing.T) {
	mgr := NewManager()
	_, err := mgr.Connect(context.Background(), "unknown")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}

// statusRecorder 是一个 mock 状态变更回调，按顺序记录收到的 (name, status)。
type statusRecorder struct {
	mu     sync.Mutex
	events []statusEvent
}

type statusEvent struct {
	name   string
	status MCPServerStatus
}

func (r *statusRecorder) handler(name string, status MCPServerStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, statusEvent{name: name, status: status})
}

func (r *statusRecorder) snapshot() []statusEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]statusEvent(nil), r.events...)
}

func (r *statusRecorder) statuses() []MCPServerStatus {
	out := make([]MCPServerStatus, 0)
	for _, e := range r.snapshot() {
		out = append(out, e.status)
	}
	return out
}

func eqStatuses(got []MCPServerStatus, want ...MCPServerStatus) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// Connect 一个 transport type 不支持的 server：应同步走 connecting -> error，
// 且每次迁移都触发一次回调（这是事件驱动刷新侧边栏的基础）。
func TestStatusChangeHandler_ConnectingThenError(t *testing.T) {
	mgr := NewManager()
	rec := &statusRecorder{}
	mgr.SetStatusChangeHandler(rec.handler)

	mgr.RegisterServer("bad", &MCPServerConfig{Name: "bad", Type: "nope"})

	if _, err := mgr.Connect(context.Background(), "bad"); err == nil {
		t.Fatal("expected error connecting to server with unsupported transport")
	}

	if got := rec.statuses(); !eqStatuses(got, MCPStatusConnecting, MCPStatusError) {
		t.Fatalf("expected [connecting error], got %v", got)
	}
	for _, e := range rec.snapshot() {
		if e.name != "bad" {
			t.Fatalf("unexpected event name %q", e.name)
		}
	}

	entries := mgr.ServerEntries()
	if len(entries) != 1 || entries[0].Status != MCPStatusError {
		t.Fatalf("expected entry status error, got %+v", entries)
	}
}

func TestStatusChangeHandler_Disconnect(t *testing.T) {
	mgr := NewManager()
	rec := &statusRecorder{}
	mgr.SetStatusChangeHandler(rec.handler)

	mgr.RegisterServer("svc", &MCPServerConfig{Name: "svc", Type: "stdio"})

	if _, err := mgr.Disconnect("svc"); err != nil {
		t.Fatalf("disconnect failed: %v", err)
	}

	if got := rec.statuses(); !eqStatuses(got, MCPStatusDisconnected) {
		t.Fatalf("expected [disconnected], got %v", got)
	}
}

func TestStatusChangeHandler_CloseAll(t *testing.T) {
	mgr := NewManager()
	rec := &statusRecorder{}
	mgr.SetStatusChangeHandler(rec.handler)

	mgr.RegisterServer("a", &MCPServerConfig{Name: "a", Type: "stdio"})
	mgr.RegisterServer("b", &MCPServerConfig{Name: "b", Type: "stdio"})

	mgr.CloseAll()

	got := rec.snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 disconnected events, got %v", got)
	}
	seen := map[string]bool{}
	for _, e := range got {
		if e.status != MCPStatusDisconnected {
			t.Fatalf("expected disconnected, got %v for %q", e.status, e.name)
		}
		seen[e.name] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("expected events for both a and b, got %v", got)
	}
}

// 对未注册的 server 操作不应触发任何回调（避免侧边栏被无意义事件刷新）。
func TestStatusChangeHandler_UnknownServer_NoEvent(t *testing.T) {
	mgr := NewManager()
	rec := &statusRecorder{}
	mgr.SetStatusChangeHandler(rec.handler)

	_, _ = mgr.Connect(context.Background(), "ghost")
	_, _ = mgr.Disconnect("ghost")

	if got := rec.snapshot(); len(got) != 0 {
		t.Fatalf("expected no events for unknown server, got %v", got)
	}
}

// 没有注册 handler 时各状态迁移不应 panic。
func TestStatusChangeHandler_NilSafe(t *testing.T) {
	mgr := NewManager()
	mgr.RegisterServer("bad", &MCPServerConfig{Name: "bad", Type: "nope"})
	_, _ = mgr.Connect(context.Background(), "bad")
	if _, err := mgr.Disconnect("bad"); err != nil {
		t.Fatalf("disconnect failed: %v", err)
	}
	mgr.CloseAll()
}
