package ai

import (
	"context"
	"strings"
)

type RequestOptions struct {
	PromptFamily         string
	PromptCacheEnabled   bool
	PromptCacheKey       string
	PromptCacheKeyHash   string
	PromptCacheRetention string
}

type ChatClientWithOptions interface {
	ChatClient
	ChatExWithOptions(ctx context.Context, infos []*MsgInfo, options *RequestOptions, tools ...*FunctionTool) ([]*ChatChoices, error)
	ChatTextWithOptions(ctx context.Context, text string, options *RequestOptions, tools ...*FunctionTool) (string, error)
}

type StreamingChatClientWithOptions interface {
	StreamingChatClient
	ChatStreamWithOptions(ctx context.Context, infos []*MsgInfo, options *RequestOptions, handler StreamHandler, tools ...*FunctionTool) error
}

func NormalizeRequestOptions(options *RequestOptions) *RequestOptions {
	if options == nil {
		return nil
	}
	normalized := *options
	normalized.PromptFamily = strings.TrimSpace(normalized.PromptFamily)
	normalized.PromptCacheKey = strings.TrimSpace(normalized.PromptCacheKey)
	normalized.PromptCacheKeyHash = strings.TrimSpace(normalized.PromptCacheKeyHash)
	normalized.PromptCacheRetention = strings.TrimSpace(normalized.PromptCacheRetention)
	if !normalized.PromptCacheEnabled {
		normalized.PromptCacheKey = ""
		normalized.PromptCacheKeyHash = ""
		normalized.PromptCacheRetention = ""
	}
	if normalized.PromptFamily == "" &&
		!normalized.PromptCacheEnabled &&
		normalized.PromptCacheKey == "" &&
		normalized.PromptCacheKeyHash == "" &&
		normalized.PromptCacheRetention == "" {
		return nil
	}
	return &normalized
}

func ChatExWithOptions(ctx context.Context, client ChatClient, infos []*MsgInfo, options *RequestOptions, tools ...*FunctionTool) ([]*ChatChoices, error) {
	if typed, ok := client.(ChatClientWithOptions); ok {
		return typed.ChatExWithOptions(ctx, infos, NormalizeRequestOptions(options), tools...)
	}
	return client.ChatEx(ctx, infos, tools...)
}

func ChatTextWithOptions(ctx context.Context, client ChatClient, text string, options *RequestOptions, tools ...*FunctionTool) (string, error) {
	if typed, ok := client.(ChatClientWithOptions); ok {
		return typed.ChatTextWithOptions(ctx, text, NormalizeRequestOptions(options), tools...)
	}
	return client.ChatText(ctx, text, tools...)
}

func ChatStreamWithOptions(ctx context.Context, client StreamingChatClient, infos []*MsgInfo, options *RequestOptions, handler StreamHandler, tools ...*FunctionTool) error {
	if typed, ok := client.(StreamingChatClientWithOptions); ok {
		return typed.ChatStreamWithOptions(ctx, infos, NormalizeRequestOptions(options), handler, tools...)
	}
	return client.ChatStream(ctx, infos, handler, tools...)
}
