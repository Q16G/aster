package anthropic

import (
	"net/url"
	"strings"
)

// ResolveMessagesURL 将用户传入的 base_url 规范化为 Anthropic Messages 接口的最终请求地址。
// 与 OpenAI 客户端的 ResolveChatCompletionsURL 行为一致：若 URL 已包含 /messages 则原样返回，
// 否则在末尾补全 /messages。例如 https://api.minimaxi.com/anthropic/v1 ->
// https://api.minimaxi.com/anthropic/v1/messages。
func ResolveMessagesURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return value
	}
	if strings.Contains(value, "/messages") {
		return value
	}
	return appendAPIPath(value, "/messages")
}

func appendAPIPath(raw string, apiPath string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(raw, "/") + apiPath
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + apiPath
	return u.String()
}
