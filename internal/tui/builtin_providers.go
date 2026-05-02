package tui

import "os"

type BuiltinProvider struct {
	Name         string
	ID           string
	BaseURL      string
	APIKeyEnvVar string
	DefaultModel string
	Models       []string
}

var builtinProviders = []BuiltinProvider{
	{
		Name:         "OpenAI",
		ID:           "openai",
		BaseURL:      "https://api.openai.com/v1",
		APIKeyEnvVar: "OPENAI_API_KEY",
		DefaultModel: "gpt-4o",
		Models:       []string{"gpt-4o", "gpt-4o-mini", "gpt-4.1", "gpt-4.1-mini", "o3-mini"},
	},
	{
		Name:         "Anthropic",
		ID:           "anthropic",
		BaseURL:      "https://api.anthropic.com/v1",
		APIKeyEnvVar: "ANTHROPIC_API_KEY",
		DefaultModel: "claude-sonnet-4-20250514",
		Models:       []string{"claude-sonnet-4-20250514", "claude-opus-4-20250514", "claude-haiku-4-20250514"},
	},
	{
		Name:         "DeepSeek",
		ID:           "deepseek",
		BaseURL:      "https://api.deepseek.com/v1",
		APIKeyEnvVar: "DEEPSEEK_API_KEY",
		DefaultModel: "deepseek-chat",
		Models:       []string{"deepseek-chat", "deepseek-reasoner"},
	},
	{
		Name:         "Groq",
		ID:           "groq",
		BaseURL:      "https://api.groq.com/openai/v1",
		APIKeyEnvVar: "GROQ_API_KEY",
		DefaultModel: "llama-3.3-70b-versatile",
		Models:       []string{"llama-3.3-70b-versatile", "llama-3.1-8b-instant", "mixtral-8x7b-32768"},
	},
	{
		Name:         "OpenRouter",
		ID:           "openrouter",
		BaseURL:      "https://openrouter.ai/api/v1",
		APIKeyEnvVar: "OPENROUTER_API_KEY",
		DefaultModel: "anthropic/claude-sonnet-4",
		Models:       []string{"anthropic/claude-sonnet-4", "openai/gpt-4o", "google/gemini-2.5-pro"},
	},
	{
		Name:         "Together",
		ID:           "together",
		BaseURL:      "https://api.together.xyz/v1",
		APIKeyEnvVar: "TOGETHER_API_KEY",
		DefaultModel: "meta-llama/Llama-3-70b-chat-hf",
		Models:       []string{"meta-llama/Llama-3-70b-chat-hf", "meta-llama/Llama-3-8b-chat-hf"},
	},
	{
		Name:         "Ollama",
		ID:           "ollama",
		BaseURL:      "http://localhost:11434/v1",
		APIKeyEnvVar: "",
		DefaultModel: "qwen2.5:latest",
		Models:       []string{"qwen2.5:latest", "llama3:latest", "deepseek-r1:latest"},
	},
}

func GetBuiltinProvider(id string) (BuiltinProvider, bool) {
	for _, p := range builtinProviders {
		if p.ID == id {
			return p, true
		}
	}
	return BuiltinProvider{}, false
}

func isProviderAvailable(p BuiltinProvider) bool {
	if p.APIKeyEnvVar == "" {
		return true
	}
	return os.Getenv(p.APIKeyEnvVar) != ""
}

func resolveAPIKey(p BuiltinProvider, cfgKey string) string {
	if cfgKey != "" {
		return os.Expand(cfgKey, os.Getenv)
	}
	if p.APIKeyEnvVar != "" {
		return os.Getenv(p.APIKeyEnvVar)
	}
	return ""
}
