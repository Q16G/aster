package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBundledSnapshot(t *testing.T) {
	data, err := LoadBundledSnapshot()
	require.NoError(t, err)
	assert.Greater(t, len(data), 80, "bundled snapshot should have 80+ providers")

	openai, ok := data["openai"]
	require.True(t, ok, "openai provider must exist")
	assert.Equal(t, "OpenAI", openai.Name)
	assert.Greater(t, len(openai.Models), 0)
}

func TestRegistryLoadFromSnapshot(t *testing.T) {
	data, err := LoadBundledSnapshot()
	require.NoError(t, err)

	reg := NewRegistry("")
	reg.LoadFromModelsDevData(data)

	providers := reg.ListProviders()
	assert.Greater(t, len(providers), 80)

	p, ok := reg.GetProvider("anthropic")
	require.True(t, ok)
	assert.Equal(t, "Anthropic", p.Name)
	assert.Equal(t, "anthropic", p.Protocol)
	assert.Contains(t, p.EnvVars, "ANTHROPIC_API_KEY")

	models := reg.ListModels("anthropic")
	assert.Greater(t, len(models), 0)
}

func TestRegistryProtocolInference(t *testing.T) {
	tests := []struct {
		npm      string
		expected string
	}{
		{"@ai-sdk/anthropic", "anthropic"},
		{"@ai-sdk/openai", "native-openai"},
		{"@ai-sdk/openai-compatible", "openai-compatible"},
		{"", "openai-compatible"},
		{"@ai-sdk/google-genai", "openai-compatible"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, inferProtocol(tt.npm), "npm=%q", tt.npm)
	}
}

func TestRegistryResolveContextBudget(t *testing.T) {
	data, err := LoadBundledSnapshot()
	require.NoError(t, err)

	reg := NewRegistry("")
	reg.LoadFromModelsDevData(data)

	ctx, out, found := reg.ResolveContextBudget("gpt-4o")
	assert.True(t, found)
	assert.Greater(t, ctx, 0)
	assert.Greater(t, out, 0)

	_, _, found = reg.ResolveContextBudget("nonexistent-model-xyz")
	assert.False(t, found)
}

func TestRegistryResolveModelCapabilities(t *testing.T) {
	data, err := LoadBundledSnapshot()
	require.NoError(t, err)

	reg := NewRegistry("")
	reg.LoadFromModelsDevData(data)

	vision, _, found := reg.ResolveModelCapabilities("gpt-4o")
	assert.True(t, found)
	assert.True(t, vision)
}

func TestRegistryResolveAPIKey(t *testing.T) {
	reg := NewRegistry("")
	reg.LoadFromModelsDevData(ModelsDevData{
		"test-provider": {
			ID:   "test-provider",
			Name: "Test",
			Env:  []string{"TEST_API_KEY"},
		},
	})

	assert.Equal(t, "explicit-key", reg.ResolveAPIKey("test-provider", "explicit-key"))
	assert.Equal(t, "", reg.ResolveAPIKey("test-provider", ""))
	assert.Equal(t, "", reg.ResolveAPIKey("unknown-provider", ""))
}

func TestRegistryIsProviderAvailable(t *testing.T) {
	reg := NewRegistry("")
	reg.LoadFromModelsDevData(ModelsDevData{
		"local": {ID: "local", Name: "Local", Env: nil},
		"cloud": {ID: "cloud", Name: "Cloud", Env: []string{"CLOUD_API_KEY"}},
	})

	assert.True(t, reg.IsProviderAvailable("local"))
	assert.False(t, reg.IsProviderAvailable("cloud"))
	assert.False(t, reg.IsProviderAvailable("nonexistent"))
}

func TestStatusRank(t *testing.T) {
	assert.Less(t, statusRank("active"), statusRank("beta"))
	assert.Less(t, statusRank("beta"), statusRank("alpha"))
	assert.Less(t, statusRank("alpha"), statusRank("deprecated"))
	assert.Equal(t, statusRank(""), statusRank("active"))
}
