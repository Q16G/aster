package react_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"aster/internal/ai"
)

// dumpingChatClient 包装一个真实 ChatClient，在每次模型调用前把出站消息
// 渲染成 OpenAI 风格紧凑视图并写出到 ZEN_LIVE_DUMP_FILE 指向的文件
// （默认 /tmp/zen_live_dump.log）。同时遵守 StreamingChatClient /
// ChatClientWithOptions / TokenUsageProvider 协议透传。
type dumpingChatClient struct {
	base    ai.ChatClient
	counter atomic.Int32
	maxBody int // 每条消息体最多 dump 多少 rune
	dump    func(string)
}

func newDumpingChatClient(base ai.ChatClient, maxBody int) *dumpingChatClient {
	if maxBody <= 0 {
		maxBody = 800
	}
	w, _ := openDumpWriter()
	return &dumpingChatClient{base: base, maxBody: maxBody, dump: w}
}

func openDumpWriter() (func(string), *os.File) {
	path := strings.TrimSpace(os.Getenv("ZEN_LIVE_DUMP_FILE"))
	if path == "" {
		path = "/tmp/zen_live_dump.log"
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return func(s string) { fmt.Fprint(os.Stderr, s) }, nil
	}
	return func(s string) {
		_, _ = f.WriteString(s)
	}, f
}

func (c *dumpingChatClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	c.dumpCall("Chat", []*ai.MsgInfo{info}, nil, tools)
	return c.base.Chat(ctx, info, tools...)
}

func (c *dumpingChatClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.dumpCall("ChatEx", infos, nil, tools)
	return c.base.ChatEx(ctx, infos, tools...)
}

func (c *dumpingChatClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	c.dumpCall("ChatText", []*ai.MsgInfo{ai.NewUserMsgInfo(text)}, nil, tools)
	return c.base.ChatText(ctx, text, tools...)
}

func (c *dumpingChatClient) ChatExWithOptions(ctx context.Context, infos []*ai.MsgInfo, options *ai.RequestOptions, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	c.dumpCall("ChatExWithOptions", infos, options, tools)
	if typed, ok := c.base.(ai.ChatClientWithOptions); ok {
		return typed.ChatExWithOptions(ctx, infos, options, tools...)
	}
	return c.base.ChatEx(ctx, infos, tools...)
}

func (c *dumpingChatClient) ChatTextWithOptions(ctx context.Context, text string, options *ai.RequestOptions, tools ...*ai.FunctionTool) (string, error) {
	c.dumpCall("ChatTextWithOptions", []*ai.MsgInfo{ai.NewUserMsgInfo(text)}, options, tools)
	if typed, ok := c.base.(ai.ChatClientWithOptions); ok {
		return typed.ChatTextWithOptions(ctx, text, options, tools...)
	}
	return c.base.ChatText(ctx, text, tools...)
}

func (c *dumpingChatClient) ChatStream(ctx context.Context, infos []*ai.MsgInfo, handler ai.StreamHandler, tools ...*ai.FunctionTool) error {
	c.dumpCall("ChatStream", infos, nil, tools)
	if typed, ok := c.base.(ai.StreamingChatClient); ok {
		return typed.ChatStream(ctx, infos, handler, tools...)
	}
	choices, err := c.base.ChatEx(ctx, infos, tools...)
	if err != nil {
		return err
	}
	if handler != nil {
		if len(choices) == 0 || choices[0] == nil {
			return handler(nil, true)
		}
		return handler(&ai.StreamDelta{}, true)
	}
	return nil
}

func (c *dumpingChatClient) ChatStreamWithOptions(ctx context.Context, infos []*ai.MsgInfo, options *ai.RequestOptions, handler ai.StreamHandler, tools ...*ai.FunctionTool) error {
	c.dumpCall("ChatStreamWithOptions", infos, options, tools)
	if typed, ok := c.base.(ai.StreamingChatClientWithOptions); ok {
		return typed.ChatStreamWithOptions(ctx, infos, options, handler, tools...)
	}
	return c.ChatStream(ctx, infos, handler, tools...)
}

func (c *dumpingChatClient) LastTokenUsage() *ai.TokenUsage {
	if typed, ok := c.base.(ai.TokenUsageProvider); ok {
		return typed.LastTokenUsage()
	}
	return nil
}

func (c *dumpingChatClient) UsagePricingModel() any {
	if provider, ok := c.base.(interface{ UsagePricingModel() any }); ok {
		return provider.UsagePricingModel()
	}
	return nil
}

// dumpCall 把一次调用的出站消息 + 工具集 + 请求选项渲染成 OpenAI 风格紧凑视图。
func (c *dumpingChatClient) dumpCall(method string, infos []*ai.MsgInfo, options *ai.RequestOptions, tools []*ai.FunctionTool) {
	id := c.counter.Add(1)
	var b strings.Builder
	fmt.Fprintf(&b, "\n────────────────────────────────────────────────────────\n")
	fmt.Fprintf(&b, "▶ call#%03d  method=%s", id, method)
	if options != nil {
		fmt.Fprintf(&b, "  family=%s  cache=%v  cache_key_hash=%.16s", options.PromptFamily, options.PromptCacheEnabled, options.PromptCacheKeyHash)
	}
	fmt.Fprintf(&b, "\n────────────────────────────────────────────────────────\n")

	// 角色序列 + 各 block 长度（一行紧凑视图）。
	fmt.Fprintf(&b, "messages[%d]: ", len(infos))
	for i, m := range infos {
		if m == nil {
			fmt.Fprintf(&b, "[%d]nil ", i)
			continue
		}
		fmt.Fprintf(&b, "[%d]%s/%dB ", i, role(m), bodyLen(m))
	}
	fmt.Fprintln(&b)

	if len(tools) > 0 {
		names := make([]string, 0, len(tools))
		for _, tool := range tools {
			if tool == nil || tool.Function == nil {
				continue
			}
			names = append(names, tool.Function.Name)
		}
		fmt.Fprintf(&b, "tools[%d]: %s\n", len(tools), strings.Join(names, ", "))
	}
	fmt.Fprintln(&b)

	// 逐条详细 dump（按 OpenAI body 形态序列化）。
	for i, m := range infos {
		if m == nil {
			continue
		}
		fmt.Fprintf(&b, "── msg[%d] role=%s", i, role(m))
		if m.ToolCallID != "" {
			fmt.Fprintf(&b, " tool_call_id=%s", m.ToolCallID)
		}
		if len(m.ToolCalls) > 0 {
			fmt.Fprintf(&b, " tool_calls=%d", len(m.ToolCalls))
		}
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, c.renderContent(m))
		if len(m.ToolCalls) > 0 {
			for j, tc := range m.ToolCalls {
				if tc == nil || tc.Function == nil {
					continue
				}
				args := ""
				switch v := tc.Function.Arguments.(type) {
				case string:
					args = v
				default:
					raw, _ := json.Marshal(v)
					args = string(raw)
				}
				fmt.Fprintf(&b, "   ↳ tool_call[%d] id=%s name=%s args=%s\n", j, tc.Id, tc.Function.Name, truncate(args, c.maxBody))
			}
		}
		fmt.Fprintln(&b)
	}
	c.dump(b.String())
}

func role(m *ai.MsgInfo) string {
	if m == nil {
		return "nil"
	}
	if m.Role == "" {
		return "?"
	}
	return m.Role
}

func bodyLen(m *ai.MsgInfo) int {
	if m == nil {
		return 0
	}
	switch v := m.Content.(type) {
	case string:
		return len(v)
	case []*ai.ChatContext:
		total := 0
		for _, c := range v {
			if c != nil {
				total += len(c.Text)
			}
		}
		return total
	default:
		raw, _ := json.Marshal(v)
		return len(raw)
	}
}

func (c *dumpingChatClient) renderContent(m *ai.MsgInfo) string {
	switch v := m.Content.(type) {
	case string:
		return truncate(v, c.maxBody)
	case []*ai.ChatContext:
		var parts []string
		for _, ctx := range v {
			if ctx == nil {
				continue
			}
			switch ctx.Type {
			case "text":
				parts = append(parts, truncate(ctx.Text, c.maxBody))
			case "image_url":
				parts = append(parts, "[image]")
			default:
				parts = append(parts, fmt.Sprintf("[%s]", ctx.Type))
			}
		}
		return strings.Join(parts, "\n")
	default:
		raw, _ := json.Marshal(v)
		return truncate(string(raw), c.maxBody)
	}
}

// truncate 按 rune 截断，中间用 …(<剩余字节>B 略)… 提示，便于人工 review。
func truncate(s string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	half := maxRunes / 2
	rs := []rune(s)
	head := string(rs[:half])
	tail := string(rs[len(rs)-half:])
	skipped := len(s) - len(head) - len(tail)
	return fmt.Sprintf("%s\n…(omitted %dB)…\n%s", head, skipped, tail)
}
