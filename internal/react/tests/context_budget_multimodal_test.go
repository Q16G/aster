package react_test

import (
	"context"
	"testing"

	"aster/internal/ai"
	. "aster/internal/react"
)

type multimodalStubClient struct {
	model          string
	supportsVision *bool
	supportsAudio  *bool
}

func (c *multimodalStubClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *multimodalStubClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (c *multimodalStubClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *multimodalStubClient) ModelContextInfo() ai.ModelContextInfo {
	return ai.ModelContextInfo{
		ModelName:      c.model,
		SupportsVision: c.supportsVision,
		SupportsAudio:  c.supportsAudio,
	}
}

func TestModelSupportsVision_ExplicitTrue(t *testing.T) {
	client := &multimodalStubClient{model: "custom-model", supportsVision: ai.BoolPtr(true)}
	if !ModelSupportsVision(client) {
		t.Fatal("expected ModelSupportsVision to return true when explicitly set")
	}
}

func TestModelSupportsAudio_ExplicitTrue(t *testing.T) {
	client := &multimodalStubClient{model: "custom-model", supportsAudio: ai.BoolPtr(true)}
	if !ModelSupportsAudio(client) {
		t.Fatal("expected ModelSupportsAudio to return true when explicitly set")
	}
}

func TestModelSupportsVision_InferredFromKnownModel(t *testing.T) {
	client := &stubChatClient{}
	if ModelSupportsVision(client) {
		t.Fatal("stubChatClient has no model name, should not support vision")
	}
}

func TestModelSupportsVision_GPT4o_Inferred(t *testing.T) {
	client := &multimodalStubClient{model: "gpt-4o"}
	if !ModelSupportsVision(client) {
		t.Fatal("gpt-4o should infer SupportsVision=true from known profiles")
	}
	if !ModelSupportsAudio(client) {
		t.Fatal("gpt-4o should infer SupportsAudio=true from known profiles")
	}
}

func TestModelSupports_DeepSeek_NoMultimodal(t *testing.T) {
	client := &multimodalStubClient{model: "deepseek-chat"}
	if ModelSupportsVision(client) {
		t.Fatal("deepseek-chat should not support vision")
	}
	if ModelSupportsAudio(client) {
		t.Fatal("deepseek-chat should not support audio")
	}
}

func TestModelSupportsVision_GLM45V_Inferred(t *testing.T) {
	client := &multimodalStubClient{model: "glm-4.5v"}
	if !ModelSupportsVision(client) {
		t.Fatal("glm-4.5v should infer SupportsVision=true")
	}
	if ModelSupportsAudio(client) {
		t.Fatal("glm-4.5v should not support audio")
	}
}
