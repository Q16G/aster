package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	content := `
mcp_servers:
  sqlcheck:
    description: "SQL injection checker"
    type: stdio
    command: sqlcheck-mcp
    args: ["--mode", "taint"]
    resident: false
  semgrep:
    description: "SAST engine"
    type: streamable-http
    url: https://semgrep.internal/mcp
    resident: true
  codeql:
    description: "CodeQL service"
    type: sse
    url: https://codeql.internal:8080/mcp/sse
    headers:
      Authorization: "Bearer test-token"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if len(cfg.MCPServers) != 3 {
		t.Fatalf("expected 3 servers, got %d", len(cfg.MCPServers))
	}

	sqlcheck := cfg.MCPServers["sqlcheck"]
	if sqlcheck.Name != "sqlcheck" {
		t.Fatalf("expected name 'sqlcheck', got %q", sqlcheck.Name)
	}
	if sqlcheck.Type != "stdio" {
		t.Fatalf("expected type 'stdio', got %q", sqlcheck.Type)
	}
	if sqlcheck.Resident {
		t.Fatal("expected resident=false for sqlcheck")
	}

	semgrep := cfg.MCPServers["semgrep"]
	if !semgrep.Resident {
		t.Fatal("expected resident=true for semgrep")
	}
	if semgrep.URL != "https://semgrep.internal/mcp" {
		t.Fatalf("unexpected url: %q", semgrep.URL)
	}
}

func TestLoadConfig_EnvExpansion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	t.Setenv("TEST_MCP_TOKEN", "secret123")

	content := `
mcp_servers:
  test:
    description: "test server"
    type: sse
    url: https://test.internal/mcp
    headers:
      Authorization: "Bearer ${TEST_MCP_TOKEN}"
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	testServer := cfg.MCPServers["test"]
	if testServer.Headers["Authorization"] != "Bearer secret123" {
		t.Fatalf("expected expanded token, got %q", testServer.Headers["Authorization"])
	}
}

func TestMCPServerConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     MCPServerConfig
		wantErr bool
	}{
		{"valid stdio", MCPServerConfig{Name: "a", Type: "stdio", Command: "cmd"}, false},
		{"valid sse", MCPServerConfig{Name: "b", Type: "sse", URL: "http://x"}, false},
		{"valid streamable-http", MCPServerConfig{Name: "c", Type: "streamable-http", URL: "http://x"}, false},
		{"missing type", MCPServerConfig{Name: "d"}, true},
		{"stdio no command", MCPServerConfig{Name: "e", Type: "stdio"}, true},
		{"sse no url", MCPServerConfig{Name: "f", Type: "sse"}, true},
		{"unknown type", MCPServerConfig{Name: "g", Type: "grpc"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
