package ai

import "context"

type ChatClient interface {
	Chat(ctx context.Context, info *MsgInfo, tools ...*FunctionTool) (string, error)
	ChatEx(ctx context.Context, infos []*MsgInfo, tools ...*FunctionTool) ([]*ChatChoices, error)
	ChatText(ctx context.Context, text string, tools ...*FunctionTool) (string, error)
}

type TokenUsageProvider interface {
	LastTokenUsage() *TokenUsage
}

type StreamHandler func(delta *StreamDelta, done bool) error

type StreamingChatClient interface {
	ChatClient
	ChatStream(ctx context.Context, infos []*MsgInfo, handler StreamHandler, tools ...*FunctionTool) error
}
