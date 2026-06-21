package tui

import (
	"testing"

	"aster/internal/provider"
)

func TestParseModelVariant(t *testing.T) {
	// registry：openai/gpt-4o 带一个已注册 variant "thinking"；ollama 不在 registry。
	reg := provider.NewRegistry("")
	reg.LoadFromModelsDevData(provider.ModelsDevData{
		"openai": &provider.ModelsDevProvider{
			ID: "openai",
			Models: map[string]*provider.ModelsDevModel{
				"gpt-4o": {
					ID:       "gpt-4o",
					Variants: map[string]map[string]any{"thinking": {"reasoning": true}},
				},
			},
		},
	})

	cfgThinking := map[string]map[string]any{"thinking": {"effort": "high"}}

	for _, tt := range []struct {
		name        string
		modelID     string
		reg         *provider.Registry
		providerID  string
		cfgVariants map[string]map[string]any
		wantBase    string
		wantVariant string
		wantOpts    bool
	}{
		{"ollama 单冒号原生名整名保留", "qwen2.5:latest", reg, "ollama", nil, "qwen2.5:latest", "", false},
		{"ollama 多冒号原生名整名保留(核心回归)", "qwen3.6:35b-a3b-coding-mxfp8", reg, "ollama", nil, "qwen3.6:35b-a3b-coding-mxfp8", "", false},
		{"三段斜杠+冒号整名保留", "registry.io/lib/model:tag", reg, "ollama", nil, "registry.io/lib/model:tag", "", false},
		{"config variant 命中", "gpt-4o:thinking", reg, "openai", cfgThinking, "gpt-4o", "thinking", true},
		{"registry variant 命中(cfg=nil)", "gpt-4o:thinking", reg, "openai", nil, "gpt-4o", "thinking", true},
		{"未知后缀防误切", "gpt-4o:unknownsuffix", reg, "openai", nil, "gpt-4o:unknownsuffix", "", false},
		{"无冒号整名", "claude-3-5-sonnet", reg, "anthropic", nil, "claude-3-5-sonnet", "", false},
		// registry 查找按 providerID 作用域:gpt-4o:thinking 的 thinking 只在 openai 下注册,
		// 用 providerID=ollama 解析时不应命中,整名保留(验证 provider 作用域隔离)。
		{"registry variant 不跨 provider 命中", "gpt-4o:thinking", reg, "ollama", nil, "gpt-4o:thinking", "", false},
		{"末尾空冒号整名保留", "gpt-4o:", reg, "openai", nil, "gpt-4o:", "", false},
		{"多连续冒号整名保留", "a::b", reg, "ollama", nil, "a::b", "", false},
		{"reg=nil + config 命中", "gpt-4o:thinking", nil, "openai", cfgThinking, "gpt-4o", "thinking", true},
		{"reg=nil + config 不命中整名保留", "qwen2.5:latest", nil, "ollama", nil, "qwen2.5:latest", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base, variant, opts := ParseModelVariant(tt.modelID, tt.reg, tt.providerID, tt.cfgVariants)
			if base != tt.wantBase {
				t.Errorf("base = %q, want %q", base, tt.wantBase)
			}
			if variant != tt.wantVariant {
				t.Errorf("variant = %q, want %q", variant, tt.wantVariant)
			}
			if (opts != nil) != tt.wantOpts {
				t.Errorf("opts non-nil = %v, want %v (opts=%v)", opts != nil, tt.wantOpts, opts)
			}
		})
	}
}

func TestBuildModelSelectOptionsGroupsCurrentAndRecent(t *testing.T) {
	options := buildModelSelectOptions(
		[]ModelOption{
			{ID: "gpt-4o", OwnedBy: "openai"},
			{ID: "gpt-4.1-mini", OwnedBy: "openai"},
			{ID: "claude-3-7-sonnet", OwnedBy: "anthropic"},
		},
		"gpt-4o",
		nil,
		[]string{"gpt-4.1-mini", "gpt-4o"},
	)

	if len(options) != 6 {
		t.Fatalf("expected 6 options, got %d", len(options))
	}
	if !options[0].Disabled || options[0].Label != "Current" {
		t.Fatalf("expected Current heading, got %+v", options[0])
	}
	if options[1].Value != "gpt-4o" {
		t.Fatalf("expected gpt-4o as current, got %+v", options[1])
	}
	if !options[2].Disabled || options[2].Label != "Recent" {
		t.Fatalf("expected Recent heading, got %+v", options[2])
	}
	if options[3].Value != "gpt-4.1-mini" {
		t.Fatalf("expected gpt-4.1-mini as recent, got %+v", options[3])
	}
	if !options[4].Disabled || options[4].Label != "All Models" {
		t.Fatalf("expected All Models heading, got %+v", options[4])
	}
	if options[5].Value != "claude-3-7-sonnet" {
		t.Fatalf("expected claude-3-7-sonnet in all, got %+v", options[5])
	}
}
