package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeYAMLAndLoad 写入 YAML 字符串到临时文件并 LoadConfig。
func writeYAMLAndLoad(t *testing.T, yaml string) *AppConfig {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))
	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	return cfg
}

func TestLoadConfig_ReactBlockMissing_ReactNil(t *testing.T) {
	cfg := writeYAMLAndLoad(t, `default_provider: openai
providers:
  openai:
    api_key: k
    default_model: m
`)
	if cfg.React != nil {
		t.Fatalf("expected cfg.React == nil when no react block, got %+v", cfg.React)
	}
}

func TestLoadConfig_ReactMaxParallelStepsParsed(t *testing.T) {
	cfg := writeYAMLAndLoad(t, `default_provider: openai
providers:
  openai:
    api_key: k
    default_model: m
react:
  max_parallel_steps: 3
`)
	require.NotNil(t, cfg.React)
	assert.Equal(t, 3, cfg.React.MaxParallelSteps)
}

func TestLoadConfig_ReactBlockEmpty_DefaultZero(t *testing.T) {
	cfg := writeYAMLAndLoad(t, `default_provider: openai
providers:
  openai:
    api_key: k
    default_model: m
react: {}
`)
	require.NotNil(t, cfg.React)
	assert.Equal(t, 0, cfg.React.MaxParallelSteps) // 零值默认（main helper 归一化为 1）
}
