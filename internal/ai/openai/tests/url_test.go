package openai_test

import (
	. "aster/internal/ai/openai"
	"testing"
)

func TestResolveChatCompletionsURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"https://api.openai.com", "https://api.openai.com/chat/completions"},
		{"https://api.openai.com/", "https://api.openai.com/chat/completions"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/chat/completions", "https://api.openai.com/chat/completions"},
		{"https://api.openai.com/v1/embeddings", "https://api.openai.com/v1/chat/completions"},
		{"https://example.com/mygateway", "https://example.com/mygateway/chat/completions"},
		{"https://example.com/mygateway/v1", "https://example.com/mygateway/v1/chat/completions"},
		{"https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{"https://open.bigmodel.cn/api/paas/v4/", "https://open.bigmodel.cn/api/paas/v4/chat/completions"},
		{"https://example.com/v2", "https://example.com/v2/chat/completions"},
	}
	for _, c := range cases {
		if got := ResolveChatCompletionsURL(c.in); got != c.want {
			t.Fatalf("ResolveChatCompletionsURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveEmbeddingsURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"https://api.openai.com", "https://api.openai.com/embeddings"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/embeddings"},
		{"https://api.openai.com/v1/embeddings", "https://api.openai.com/v1/embeddings"},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/embeddings"},
		{"https://example.com/mygateway/v1", "https://example.com/mygateway/v1/embeddings"},
		{"https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/embeddings"},
	}
	for _, c := range cases {
		if got := ResolveEmbeddingsURL(c.in); got != c.want {
			t.Fatalf("ResolveEmbeddingsURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveModelsURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/models"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/models"},
		{"https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4/models"},
		{"https://api.openai.com/v1/models", "https://api.openai.com/v1/models"},
		{"https://example.com/v2", "https://example.com/v2/models"},
	}
	for _, c := range cases {
		if got := ResolveModelsURL(c.in); got != c.want {
			t.Fatalf("ResolveModelsURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
