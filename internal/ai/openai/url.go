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

func appendAPIPath(raw string, apiPath string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		s := strings.TrimRight(raw, "/")
		if strings.HasSuffix(s, "/v1") {
			return s + apiPath
		}
		return s + "/v1" + apiPath
	}

	path := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(path, "/v1") {
		u.Path = path + apiPath
	} else if path == "" || path == "/" {
		u.Path = "/v1" + apiPath
	} else {
		u.Path = path + "/v1" + apiPath
	}
	return u.String()
}
