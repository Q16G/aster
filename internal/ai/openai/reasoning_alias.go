package openai

import "aster/internal/ai"

func normalizedInboundReasoning(reasoningContent string, reasoning string) string {
	if reasoningContent != "" {
		return reasoningContent
	}
	return reasoning
}

type openAIChatResponse struct {
	Choices []*openAIChatChoice `json:"choices"`
	Usage   *openAIUsage        `json:"usage,omitempty"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type openAIChatChoice struct {
	Index        int                `json:"index"`
	Message      *openAIChatMessage `json:"message"`
	Usage        *openAIUsage       `json:"usage,omitempty"`
	Text         string             `json:"text,omitempty"`
	FinishReason string             `json:"finish_reason,omitempty"`
}

func (c *openAIChatChoice) toAIChoice() *ai.ChatChoices {
	if c == nil {
		return nil
	}
	out := &ai.ChatChoices{
		Index:        c.Index,
		Message:      c.Message.toMsgInfo(),
		Text:         c.Text,
		FinishReason: c.FinishReason,
	}
	if c.Usage != nil {
		out.Usage = normalizeTokenUsage(c.Usage.toTokenUsage())
	}
	return out
}

type openAIChatMessage struct {
	Role             string             `json:"role,omitempty"`
	Content          any                `json:"content,omitempty"`
	Type             string             `json:"type,omitempty"`
	ToolCallID       string             `json:"tool_call_id,omitempty"`
	ToolCalls        []*ai.FunctionTool `json:"tool_calls,omitempty"`
	ReasoningContent string             `json:"reasoning_content,omitempty"`
	Reasoning        string             `json:"reasoning,omitempty"`
}

func (m *openAIChatMessage) toMsgInfo() *ai.MsgInfo {
	if m == nil {
		return nil
	}
	return &ai.MsgInfo{
		Role:            m.Role,
		Content:         m.Content,
		Type:            m.Type,
		ToolCallID:      m.ToolCallID,
		ToolCalls:       ai.NormalizeFunctionToolSlice(m.ToolCalls),
		ReasoningOutput: normalizedInboundReasoning(m.ReasoningContent, m.Reasoning),
	}
}
