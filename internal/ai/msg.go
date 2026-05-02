package ai

import (
	"bytes"
	"encoding/json"
	"strings"
)

// InputAudio 音频输入
type InputAudio struct {
	Data   string `json:"data,omitempty"`
	Format string `json:"format,omitempty"`
}

// ChatContext 多模态内容上下文
type ChatContext struct {
	Type       string         `json:"type"`
	Text       string         `json:"text,omitempty"`
	ImageURL   map[string]any `json:"image_url,omitempty"`
	Detail     string         `json:"detail,omitempty"`
	InputAudio any            `json:"input_audio,omitempty"`
}

// TokenUsage 模型 token 使用统计
type TokenUsage struct {
	TotalTokens      int `json:"total_tokens,omitempty"`
	InputTokens      int `json:"input_tokens,omitempty"`
	OutputTokens     int `json:"output_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

func (u *TokenUsage) NormalizeInPlace() {
	if u == nil {
		return
	}
	if u.TotalTokens < 0 {
		u.TotalTokens = 0
	}
	if u.InputTokens < 0 {
		u.InputTokens = 0
	}
	if u.OutputTokens < 0 {
		u.OutputTokens = 0
	}
	if u.ReasoningTokens < 0 {
		u.ReasoningTokens = 0
	}
	if u.CacheReadTokens < 0 {
		u.CacheReadTokens = 0
	}
	if u.CacheWriteTokens < 0 {
		u.CacheWriteTokens = 0
	}
}

func (u *TokenUsage) ContextCountTokens() int {
	if u == nil {
		return 0
	}
	u.NormalizeInPlace()
	count := u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens + u.OutputTokens
	if count < 0 {
		return 0
	}
	return count
}

func (u *TokenUsage) IsZero() bool {
	if u == nil {
		return true
	}
	return u.TotalTokens == 0 && u.InputTokens == 0 && u.OutputTokens == 0 && u.ReasoningTokens == 0 && u.CacheReadTokens == 0 && u.CacheWriteTokens == 0
}

func NormalizeTokenUsagePtr(usage *TokenUsage) *TokenUsage {
	if usage == nil {
		return nil
	}
	usage.NormalizeInPlace()
	if usage.IsZero() {
		return nil
	}
	return usage
}

func NormalizeFunctionToolInPlace(tool *FunctionTool) *FunctionTool {
	if tool == nil {
		return nil
	}
	tool.Id = strings.TrimSpace(tool.Id)
	tool.Type = strings.TrimSpace(tool.Type)
	if tool.Function != nil {
		tool.Function.Name = strings.TrimSpace(tool.Function.Name)
	}
	return tool
}

func NormalizeFunctionToolSlice(items []*FunctionTool) []*FunctionTool {
	if len(items) == 0 {
		return nil
	}
	out := make([]*FunctionTool, 0, len(items))
	for _, item := range items {
		if normalized := NormalizeFunctionToolInPlace(item); normalized != nil {
			out = append(out, normalized)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func NormalizeMsgInfoInPlace(msg *MsgInfo) *MsgInfo {
	if msg == nil {
		return nil
	}
	msg.Role = strings.TrimSpace(msg.Role)
	msg.Type = strings.TrimSpace(msg.Type)
	msg.ToolCallID = strings.TrimSpace(msg.ToolCallID)
	msg.ReasoningOutput = strings.TrimSpace(msg.ReasoningOutput)
	msg.Usage = NormalizeTokenUsagePtr(msg.Usage)
	if len(msg.ToolCalls) == 0 {
		msg.ToolCalls = nil
		return msg
	}
	out := msg.ToolCalls[:0]
	for _, tool := range msg.ToolCalls {
		if normalized := NormalizeFunctionToolInPlace(tool); normalized != nil {
			out = append(out, normalized)
		}
	}
	if len(out) == 0 {
		msg.ToolCalls = nil
		return msg
	}
	msg.ToolCalls = out
	return msg
}

func NormalizeMsgInfoSlice(items []*MsgInfo) []*MsgInfo {
	if len(items) == 0 {
		return nil
	}
	out := make([]*MsgInfo, 0, len(items))
	for _, item := range items {
		if normalized := NormalizeMsgInfoInPlace(item); normalized != nil {
			out = append(out, normalized)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MsgInfo 消息信息
type MsgInfo struct {
	Role            string          `json:"role,omitempty"`
	Content         any             `json:"content,omitempty"`
	Type            string          `json:"type,omitempty"`
	ToolCallID      string          `json:"tool_call_id,omitempty"`
	ToolCalls       []*FunctionTool `json:"tool_calls,omitempty"`
	ReasoningOutput string          `json:"reasoning_content,omitempty"`
	Usage           *TokenUsage     `json:"usage,omitempty"`

	// OriginalInput/OriginalOutput 用于透传原始输入/输出，不参与序列化
	OriginalInput  any `json:"-"`
	OriginalOutput any `json:"-"`
}

// ChatMsg 聊天请求消息
type ChatMsg struct {
	Model       string          `json:"model"`
	Modalities  string          `json:"modalities,omitempty"`
	Messages    []*MsgInfo      `json:"messages,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Tools       []*FunctionTool `json:"tools,omitempty"`
	ExtraBody   map[string]any  `json:"-"`
	Temperature float32         `json:"temperature,omitempty"`
	TopP        float32         `json:"top_p,omitempty"`
}

// MarshalJSON 将 ExtraBody 合入最终请求负载
func (c *ChatMsg) MarshalJSON() ([]byte, error) {
	type alias ChatMsg
	temp := (*alias)(c)
	raw, err := json.Marshal(temp)
	if err != nil {
		return nil, err
	}
	if len(c.ExtraBody) == 0 {
		return raw, nil
	}
	var base map[string]any
	if err = json.Unmarshal(raw, &base); err != nil {
		return nil, err
	}
	for k, v := range c.ExtraBody {
		base[k] = v
	}
	return json.Marshal(base)
}

// ChatChoices 聊天响应选项
type ChatChoices struct {
	Index        int         `json:"index"`
	Message      *MsgInfo    `json:"message"`
	Usage        *TokenUsage `json:"usage,omitempty"`
	Text         string      `json:"text,omitempty"`
	FinishReason string      `json:"finish_reason"`
}

// StreamDelta 流式响应增量数据
type StreamDelta struct {
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	Content          string          `json:"content,omitempty"`
	ToolCalls        []*FunctionTool `json:"tool_calls,omitempty"`
	FinishReason     string          `json:"finish_reason,omitempty"`
}

// StreamChoice 流式响应选项
type StreamChoice struct {
	Index        int          `json:"index"`
	Delta        *StreamDelta `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason,omitempty"`
}

// Completions 完整的AI响应
type Completions struct {
	Id            string         `json:"id"`
	Object        string         `json:"object"`
	Model         string         `json:"model"`
	Created       any            `json:"created"`
	Choices       []*ChatChoices `json:"choices"`
	ReasonContent string         `json:"reason_content,omitempty"`
}

// NewChatMsg 创建聊天消息
func NewChatMsg(model string, messages []*MsgInfo, tools ...*FunctionTool) *ChatMsg {
	return &ChatMsg{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}
}

// NewUserMsgInfo 创建用户消息
func NewUserMsgInfo(content any) *MsgInfo {
	return &MsgInfo{Role: "user", Content: content}
}

// NewSystemMsgInfo 创建系统消息
func NewSystemMsgInfo(content any) *MsgInfo {
	return &MsgInfo{Role: "system", Content: content}
}

// NewAIMsgInfo 创建AI助手消息
func NewAIMsgInfo(content any) *MsgInfo {
	return &MsgInfo{Role: "assistant", Content: content}
}

// NewToolCallMsgInfo 创建工具调用消息
func NewToolCallMsgInfo(tool *FunctionTool) *MsgInfo {
	return &MsgInfo{Role: "assistant", ToolCalls: []*FunctionTool{tool}}
}

// NewToolCallResultMsgInfo 创建工具调用结果消息
func NewToolCallResultMsgInfo(content any, callID string) *MsgInfo {
	return &MsgInfo{Role: "tool", Content: content, ToolCallID: callID}
}

// NewBaseChat 创建文本聊天上下文
func NewBaseChat(text string) *ChatContext {
	return &ChatContext{
		Type: "text",
		Text: text,
	}
}

// NewImageChat 创建图片聊天上下文
func NewImageChat(url string) *ChatContext {
	return &ChatContext{
		Type: "image_url",
		ImageURL: map[string]any{
			"url": url,
		},
	}
}

// NewInputAudio 创建音频输入上下文
func NewInputAudio(data string) *ChatContext {
	return &ChatContext{
		Type: "input_audio",
		InputAudio: InputAudio{
			Data:   data,
			Format: "wav",
		},
	}
}

// ChatChoice2String 提取回复文本，支持多种消息内容结构。
func ChatChoice2String(choices []*ChatChoices) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = ""
		}
	}()
	buf := bytes.NewBuffer(nil)
	for _, choice := range choices {
		if choice == nil {
			continue
		}
		if choice.Message == nil {
			if strings.TrimSpace(choice.Text) != "" {
				buf.WriteString(choice.Text)
				buf.WriteByte('\n')
			}
			continue
		}
		switch ret := choice.Message.Content.(type) {
		case string:
			if ret == "" {
				continue
			}
			buf.WriteString(ret)
			buf.WriteByte('\n')
		case map[string]any:
			raw, err := json.Marshal(ret)
			if err != nil {
				continue
			}
			var ctx ChatContext
			if err = json.Unmarshal(raw, &ctx); err == nil {
				if ctx.Text != "" {
					buf.WriteString(ctx.Text)
					buf.WriteByte('\n')
				}
				continue
			}
			var ctxList []*ChatContext
			if err = json.Unmarshal(raw, &ctxList); err == nil {
				for _, item := range ctxList {
					if item != nil && item.Text != "" {
						buf.WriteString(item.Text)
						buf.WriteByte('\n')
					}
				}
			}
		case []any:
			for _, raw := range ret {
				content, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				rawBytes, err := json.Marshal(content)
				if err != nil {
					continue
				}
				var ctx ChatContext
				if err = json.Unmarshal(rawBytes, &ctx); err == nil && ctx.Text != "" {
					buf.WriteString(ctx.Text)
					buf.WriteByte('\n')
				}
			}
		}
	}
	return buf.String()
}
