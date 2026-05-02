package mcp

import (
	"context"
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
