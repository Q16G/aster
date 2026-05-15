package openai

import (
	"net/url"
	"strings"
)

// ResolveChatCompletionsURL 将用户传入的 URL 规范化为 chat/completions 的最终请求地址。
func ResolveChatCompletionsURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return value
	}
	if strings.Contains(value, "/chat/completions") {
		return value
	}
	if strings.Contains(value, "/embeddings") {
		return strings.Replace(value, "/embeddings", "/chat/completions", 1)
	}
	return appendAPIPath(value, "/chat/completions")
}

// ResolveEmbeddingsURL 将用户传入的 URL 规范化为 embeddings 的最终请求地址。
func ResolveEmbeddingsURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return value
	}
	if strings.Contains(value, "/embeddings") {
		return value
	}
	if strings.Contains(value, "/chat/completions") {
		return strings.Replace(value, "/chat/completions", "/embeddings", 1)
	}
	return appendAPIPath(value, "/embeddings")
}

// ResolveModelsURL 将用户传入的 URL 规范化为 models 的最终请求地址。
func ResolveModelsURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return value
	}
	if strings.Contains(value, "/models") {
		return value
	}
	return appendAPIPath(value, "/models")
}

func appendAPIPath(raw string, apiPath string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(raw, "/") + apiPath
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + apiPath
	return u.String()
}
