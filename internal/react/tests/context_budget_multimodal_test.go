package react_test

import (
	"context"
	"strings"
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

func TestBuildThinkActPrompt_VisionModelHint(t *testing.T) {
	visionClient := &multimodalStubClient{model: "gpt-4o", supportsVision: ai.BoolPtr(true)}
	agent, err := NewReActAgent("vision-prompt-test", visionClient, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	prompt := agent.BuildThinkActPrompt(context.Background(), "", nil)

	visionHints := []string{"多模态", "视觉", "图片", "image", "vision", "screenshot", "multimodal"}
	found := false
	for _, hint := range visionHints {
		if strings.Contains(strings.ToLower(prompt), hint) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("think_act prompt does not inform the AI about vision/multimodal capabilities — " +
			"the model supports vision but the prompt has no hint about it")
	}
}

func TestBuildThinkActPrompt_NoVisionModel_NoHint(t *testing.T) {
	client := &multimodalStubClient{model: "deepseek-chat"}
	agent, err := NewReActAgent("no-vision-prompt-test", client, WithEmitter(NewDummyEmitter()))
	if err != nil {
		t.Fatalf("new agent: %v", err)
	}

	prompt := agent.BuildThinkActPrompt(context.Background(), "", nil)

	if strings.Contains(prompt, "5.1d") || strings.Contains(prompt, "多模态能力") {
		t.Fatal("non-vision model should not have multimodal hint section in prompt")
	}
}
