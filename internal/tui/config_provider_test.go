package tui

import (
	"os"
	"path/filepath"
	"testing"

	"aster/internal/provider"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	data, err := provider.LoadBundledSnapshot()
	require.NoError(t, err)
	reg := provider.NewRegistry("")
	reg.LoadFromModelsDevData(data)
	return reg
}

func TestResolveProviderState_ProtocolResolution(t *testing.T) {
	reg := newTestRegistry(t)

	writeCfg := func(t *testing.T, yaml string) *AppConfig {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))
		cfg, err := LoadConfig(path)
		require.NoError(t, err)
		return cfg
	}

	t.Run("explicit protocol anthropic overrides", func(t *testing.T) {
		cfg := writeCfg(t, `default_provider: openai
providers:
  openai:
    protocol: anthropic
    api_key: k
    default_model: m
`)
		state := cfg.ResolveProviderState("", "", "", "", reg, nil)
		require.NotNil(t, state)
		assert.Equal(t, "anthropic", state.Protocol)
	})

	t.Run("explicit protocol openai overrides registry anthropic", func(t *testing.T) {
		cfg := writeCfg(t, `default_provider: minimax-cn
providers:
  minimax-cn:
    protocol: openai
    api_key: k
    default_model: m
`)
		state := cfg.ResolveProviderState("", "", "", "", reg, nil)
		require.NotNil(t, state)
		assert.Equal(t, "openai-compatible", state.Protocol)
	})

	t.Run("omitted protocol falls back to registry inference", func(t *testing.T) {
		cfg := writeCfg(t, `default_provider: minimax-cn
providers:
  minimax-cn:
    api_key: k
    default_model: m
`)
		state := cfg.ResolveProviderState("", "", "", "", reg, nil)
		require.NotNil(t, state)
		assert.Equal(t, "anthropic", state.Protocol)
	})

	t.Run("explicit protocol anthropic on registry-unknown provider", func(t *testing.T) {
		cfg := writeCfg(t, `default_provider: my-anthropic-gw
providers:
  my-anthropic-gw:
    protocol: anthropic
    base_url: https://my-gw.example.com/anthropic/v1
    api_key: k
    default_model: m
`)
		state := cfg.ResolveProviderState("", "", "", "", reg, nil)
		require.NotNil(t, state)
		assert.Equal(t, "anthropic", state.Protocol)
		assert.Equal(t, "https://my-gw.example.com/anthropic/v1", state.BaseURL)
	})

	t.Run("omitted protocol unknown provider defaults empty (openai client)", func(t *testing.T) {
		cfg := writeCfg(t, `default_provider: my-custom
providers:
  my-custom:
    api_key: k
    default_model: m
`)
		state := cfg.ResolveProviderState("", "", "", "", reg, nil)
		require.NotNil(t, state)
		assert.Equal(t, "", state.Protocol)
	})
}

func TestLoadConfig_ProviderEnvExpandsAndDerivesProxy(t *testing.T) {
	t.Setenv("TEST_PROXY_HOST", "127.0.0.1")

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := `default_provider: openai
providers:
  openai:
    env:
      PROXY_HOST: ${TEST_PROXY_HOST}
      HTTPS_PROXY: http://${PROXY_HOST}:7890
      OPENAI_TOKEN: test-provider-key
      HEADER_TOKEN: env-header-token
    base_url: https://api.openai.com/v1
    api_key: ${OPENAI_TOKEN}
    default_model: gpt-4o-mini
    headers:
      X-Test-Header: Bearer ${HEADER_TOKEN}
`
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o644))

	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)

	state := cfg.ResolveProviderState("", "", "", "", nil, nil)
	require.NotNil(t, state)

	assert.Equal(t, "openai", state.Name)
	assert.Equal(t, "test-provider-key", state.APIKey)
	assert.Equal(t, "gpt-4o-mini", state.ModelID)
	assert.Equal(t, "http://127.0.0.1:7890", state.Proxy)
	assert.Equal(t, "127.0.0.1", state.Env["PROXY_HOST"])
	assert.Equal(t, "http://127.0.0.1:7890", state.Env["HTTPS_PROXY"])
	assert.Equal(t, "Bearer env-header-token", state.Headers["X-Test-Header"])
}

func TestSaveConfig_PreservesProviderEnvPlaceholders(t *testing.T) {
	t.Setenv("TEST_PROVIDER_PROXY", "http://127.0.0.1:7890")
	t.Setenv("TEST_PROVIDER_API_KEY", "sk-live-value")

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configYAML := `default_provider: openai
providers:
  openai:
    env:
      HTTPS_PROXY: ${TEST_PROVIDER_PROXY}
      OPENAI_TOKEN: ${TEST_PROVIDER_API_KEY}
    api_key: ${OPENAI_TOKEN}
    default_model: gpt-4o
`
	require.NoError(t, os.WriteFile(configPath, []byte(configYAML), 0o644))

	require.NoError(t, SaveConfig(configPath, func(cfg *AppConfig) {
		cfg.DefaultProvider = "openai"
	}))

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	text := string(data)

	assert.Contains(t, text, "HTTPS_PROXY: ${TEST_PROVIDER_PROXY}")
	assert.Contains(t, text, "OPENAI_TOKEN: ${TEST_PROVIDER_API_KEY}")
	assert.Contains(t, text, "api_key: ${OPENAI_TOKEN}")
	assert.NotContains(t, text, "http://127.0.0.1:7890")
	assert.NotContains(t, text, "sk-live-value")
}
