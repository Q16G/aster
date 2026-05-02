package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// TimelineItemType 时间线记忆项类型
type TimelineItemType string

const (
	TimelineItemTypeToolCall        TimelineItemType = "tool_call"
	TimelineItemTypeEnvironment     TimelineItemType = "environment"
	TimelineItemTypePerception      TimelineItemType = "perception"
	TimelineItemTypeThought         TimelineItemType = "thought"
	TimelineItemTypeUserInput       TimelineItemType = "user_input"
	TimelineItemTypeHandoff         TimelineItemType = "handoff"
	TimelineItemTypeAssistantOutput TimelineItemType = "assistant_output"
)

// TimelineItemValue 时间线记忆项值接口
type TimelineItemValue interface {
	String() string
	Type() TimelineItemType
}

// TimelineItem 时间线记忆项
type TimelineItem struct {
	CreateAt time.Time
	Value    TimelineItemValue
}

// ==================== TimelineItemValue 具体实现 ====================

// ToolCallItem 工具调用记录
type ToolCallItem struct {
	ToolCallID  string
	ToolName    string
	Description string
	Arguments   any
	Result      any
	Error       string
}

// NewToolCallItem 创建工具调用记录
func NewToolCallItem(toolCallID, toolName, description string, arguments, result any, err string) *ToolCallItem {
	return &ToolCallItem{
		ToolCallID:  toolCallID,
		ToolName:    toolName,
		Description: description,
		Arguments:   arguments,
		Result:      result,
		Error:       err,
	}
}

func (t *ToolCallItem) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tool Call: %s (ID: %s)\n", t.ToolName, t.ToolCallID))
	if t.Description != "" {
		sb.WriteString(fmt.Sprintf("Description: %s\n", t.Description))
	}
	if t.Arguments != nil {
		argBytes, _ := json.Marshal(t.Arguments)
		sb.WriteString(fmt.Sprintf("Arguments: %s\n", string(argBytes)))
	}
	if t.Result != nil {
		resultBytes, _ := json.Marshal(t.Result)
		sb.WriteString(fmt.Sprintf("Result: %s\n", string(resultBytes)))
	}
	if t.Error != "" {
		sb.WriteString(fmt.Sprintf("Error: %s\n", t.Error))
	}
	return sb.String()
}

func (t *ToolCallItem) Type() TimelineItemType {
	return TimelineItemTypeToolCall
}

// EnvironmentItem 环境内容
type EnvironmentItem struct {
	Content string
}

func NewEnvironmentItem(content string) *EnvironmentItem {
	return &EnvironmentItem{Content: content}
}

func (e *EnvironmentItem) String() string {
	return fmt.Sprintf("Environment: %s", e.Content)
}

func (e *EnvironmentItem) Type() TimelineItemType {
	return TimelineItemTypeEnvironment
}

// PerceptionItem 感知层内容
type PerceptionItem struct {
	Content string
}

func NewPerceptionItem(content string) *PerceptionItem {
	return &PerceptionItem{Content: content}
}

func (p *PerceptionItem) String() string {
	return fmt.Sprintf("Perception: %s", p.Content)
}

func (p *PerceptionItem) Type() TimelineItemType {
	return TimelineItemTypePerception
}

// ThoughtItem 思考层内容
type ThoughtItem struct {
	ThoughtChain map[string]string
}

func NewThoughtItem(thoughtChain map[string]string) *ThoughtItem {
	return &ThoughtItem{ThoughtChain: thoughtChain}
}

func (t *ThoughtItem) String() string {
	var sb strings.Builder
	sb.WriteString("Thought Chain:\n")
	keys := make([]string, 0, len(t.ThoughtChain))
	for key := range t.ThoughtChain {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		sb.WriteString(fmt.Sprintf("  %s: %s\n", key, t.ThoughtChain[key]))
	}
	return sb.String()
}

func (t *ThoughtItem) Type() TimelineItemType {
	return TimelineItemTypeThought
}

// UserInputItem 用户输入
type UserInputItem struct {
	Input string
}

func NewUserInputItem(input string) *UserInputItem {
	return &UserInputItem{Input: input}
}

func (u *UserInputItem) String() string {
	return fmt.Sprintf("User Input: %s", u.Input)
}

func (u *UserInputItem) Type() TimelineItemType {
	return TimelineItemTypeUserInput
}

// HandoffItem 父->子交接上下文
type HandoffItem struct {
	Content string
}

func NewHandoffItem(content string) *HandoffItem {
	return &HandoffItem{Content: strings.TrimSpace(content)}
}

func (h *HandoffItem) String() string {
	if h == nil {
		return ""
	}
	return fmt.Sprintf("Handoff: %s", strings.TrimSpace(h.Content))
}

func (h *HandoffItem) Type() TimelineItemType {
	return TimelineItemTypeHandoff
}

// AssistantOutputItem AI 输出
type AssistantOutputItem struct {
	Content string
}

func NewAssistantOutputItem(content string) *AssistantOutputItem {
	return &AssistantOutputItem{Content: strings.TrimSpace(content)}
}

func (a *AssistantOutputItem) String() string {
	return fmt.Sprintf("AI Output: %s", a.Content)
}

func (a *AssistantOutputItem) Type() TimelineItemType {
	return TimelineItemTypeAssistantOutput
}
