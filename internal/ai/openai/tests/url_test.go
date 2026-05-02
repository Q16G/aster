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
		{"https://api.openai.com", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/chat/completions", "https://api.openai.com/chat/completions"},
		{"https://api.openai.com/v1/embeddings", "https://api.openai.com/v1/chat/completions"},
		{"https://example.com/mygateway", "https://example.com/mygateway/v1/chat/completions"},
		{"https://example.com/mygateway/v1", "https://example.com/mygateway/v1/chat/completions"},
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
		{"https://api.openai.com", "https://api.openai.com/v1/embeddings"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/embeddings"},
		{"https://api.openai.com/v1/embeddings", "https://api.openai.com/v1/embeddings"},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1/embeddings"},
		{"https://example.com/mygateway/v1", "https://example.com/mygateway/v1/embeddings"},
	}
	for _, c := range cases {
		if got := ResolveEmbeddingsURL(c.in); got != c.want {
			t.Fatalf("ResolveEmbeddingsURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
