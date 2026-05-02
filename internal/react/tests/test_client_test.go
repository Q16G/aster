package react_test

import (
	"context"

	"aster/internal/ai"
)

type stubChatClient struct{}

func (s *stubChatClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (s *stubChatClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (s *stubChatClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}
