package anthropic_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"aster/internal/ai"
	"aster/internal/ai/anthropic"
)

func TestChatExWithOptions_BuildsAnthropicCacheableRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Fatalf("unexpected x-api-key: %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("unexpected anthropic-version: %q", got)
		}
		if got := r.Header.Get("X-Test-Trace"); got != "trace-1" {
			t.Fatalf("unexpected X-Test-Trace: %q", got)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request failed: %v", err)
		}

		system, ok := req["system"].([]any)
		if !ok || len(system) != 2 {
			t.Fatalf("unexpected system blocks: %#v", req["system"])
		}
		firstBlock := system[0].(map[string]any)
		if firstBlock["type"] != "text" {
			t.Fatalf("unexpected first system block: %#v", firstBlock)
		}
		cacheControl, ok := firstBlock["cache_control"].(map[string]any)
		if !ok || cacheControl["type"] != "ephemeral" || cacheControl["ttl"] != "5m" {
			t.Fatalf("unexpected cache_control: %#v", firstBlock["cache_control"])
		}

		tools, ok := req["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("unexpected tools: %#v", req["tools"])
		}
		tool := tools[0].(map[string]any)
		if tool["name"] != "search_repo" {
			t.Fatalf("unexpected tool name: %#v", tool["name"])
		}
		if toolCache, ok := tool["cache_control"].(map[string]any); !ok || toolCache["type"] != "ephemeral" {
			t.Fatalf("unexpected tool cache_control: %#v", tool["cache_control"])
		}

		messages, ok := req["messages"].([]any)
		if !ok || len(messages) != 2 {
			t.Fatalf("unexpected messages: %#v", req["messages"])
		}
		toolResultMsg := messages[1].(map[string]any)
		if toolResultMsg["role"] != "user" {
			t.Fatalf("unexpected tool result role: %#v", toolResultMsg["role"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_123",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "ok"},
			},
			"usage": map[string]any{
				"input_tokens":                120,
				"output_tokens":               30,
				"cache_creation_input_tokens": 40,
				"cache_read_input_tokens":     80,
			},
		})
	}))
	defer server.Close()

	client := anthropic.NewClient(
		anthropic.WithURL(server.URL),
		anthropic.WithAPIKey("test-key"),
		anthropic.WithModel("claude-sonnet"),
		anthropic.WithTimeout(5*time.Second),
		anthropic.WithHeaders(map[string]string{
			"X-Test-Trace": "trace-1",
		}),
	)

	choices, err := client.ChatExWithOptions(context.Background(), []*ai.MsgInfo{
		ai.NewSystemMsgInfo("static rules\n<CURRENT_STEP>\ndynamic state"),
		ai.NewUserMsgInfo("hello"),
		ai.NewToolCallResultMsgInfo("tool result", "toolu_1"),
	}, &ai.RequestOptions{
		PromptFamily:         "think_act",
		PromptCacheEnabled:   true,
		PromptCacheRetention: "5m",
	}, &ai.FunctionTool{
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:        "search_repo",
			Description: "search repository",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	})
	if err != nil {
		t.Fatalf("ChatExWithOptions failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Usage == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if choices[0].Usage.InputTokens != 120 || choices[0].Usage.OutputTokens != 30 {
		t.Fatalf("unexpected input/output usage: %#v", choices[0].Usage)
	}
	if choices[0].Usage.CacheReadTokens != 80 || choices[0].Usage.CacheWriteTokens != 40 {
		t.Fatalf("unexpected cache usage: %#v", choices[0].Usage)
	}
}

func TestChatExWithOptions_ParsesToolUseBlocks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_456",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "tool_use",
			"content": []map[string]any{
				{"type": "text", "text": "need tool"},
				{"type": "tool_use", "id": "toolu_2", "name": "rg", "input": map[string]any{"pattern": "Nonce"}},
			},
		})
	}))
	defer server.Close()

	client := anthropic.NewClient(
		anthropic.WithURL(server.URL),
		anthropic.WithModel("claude-sonnet"),
		anthropic.WithTimeout(5*time.Second),
	)

	choices, err := client.ChatEx(context.Background(), []*ai.MsgInfo{
		ai.NewUserMsgInfo("inspect"),
	})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}
	if len(choices) != 1 || choices[0] == nil || choices[0].Message == nil {
		t.Fatalf("unexpected choices: %#v", choices)
	}
	if got := choices[0].Message.Content; got != "need tool" {
		t.Fatalf("unexpected content: %#v", got)
	}
	if len(choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("unexpected tool calls: %#v", choices[0].Message.ToolCalls)
	}
	call := choices[0].Message.ToolCalls[0]
	if call.Id != "toolu_2" || call.Function == nil || call.Function.Name != "rg" {
		t.Fatalf("unexpected tool call: %#v", call)
	}
}

func TestChatExWithOptions_ImageContentConverted(t *testing.T) {
	var capturedMessages []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedMessages, _ = req["messages"].([]any)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_img",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "I see an image"}},
		})
	}))
	defer server.Close()

	client := anthropic.NewClient(
		anthropic.WithURL(server.URL),
		anthropic.WithModel("claude-sonnet"),
		anthropic.WithTimeout(5*time.Second),
	)

	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{
		{
			Role: "user",
			Content: []*ai.ChatContext{
				{Type: "text", Text: "what is in this image?"},
				{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,iVBORw0KGgo="}},
			},
		},
	})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}

	if len(capturedMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(capturedMessages))
	}
	msg := capturedMessages[0].(map[string]any)
	content, ok := msg["content"].([]any)
	if !ok {
		t.Fatalf("expected content array, got %T", msg["content"])
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}

	textBlock := content[0].(map[string]any)
	if textBlock["type"] != "text" || textBlock["text"] != "what is in this image?" {
		t.Fatalf("unexpected text block: %#v", textBlock)
	}

	imageBlock := content[1].(map[string]any)
	if imageBlock["type"] != "image" {
		t.Fatalf("expected image block type, got %v", imageBlock["type"])
	}
	source, ok := imageBlock["source"].(map[string]any)
	if !ok {
		t.Fatalf("expected source map, got %T", imageBlock["source"])
	}
	if source["type"] != "base64" {
		t.Fatalf("expected base64 source type, got %v", source["type"])
	}
	if source["media_type"] != "image/png" {
		t.Fatalf("expected image/png media_type, got %v", source["media_type"])
	}
	if source["data"] != "iVBORw0KGgo=" {
		t.Fatalf("expected base64 data, got %v", source["data"])
	}
}

func TestChatExWithOptions_URLImageContentConverted(t *testing.T) {
	var capturedMessages []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedMessages, _ = req["messages"].([]any)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_url",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer server.Close()

	client := anthropic.NewClient(
		anthropic.WithURL(server.URL),
		anthropic.WithModel("claude-sonnet"),
		anthropic.WithTimeout(5*time.Second),
	)

	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{
		{
			Role: "user",
			Content: []*ai.ChatContext{
				{Type: "image_url", ImageURL: map[string]any{"url": "https://example.com/image.png"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}

	if len(capturedMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(capturedMessages))
	}
	msg := capturedMessages[0].(map[string]any)
	content := msg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}

	imageBlock := content[0].(map[string]any)
	if imageBlock["type"] != "image" {
		t.Fatalf("expected image block, got %v", imageBlock["type"])
	}
	source := imageBlock["source"].(map[string]any)
	if source["type"] != "url" || source["url"] != "https://example.com/image.png" {
		t.Fatalf("unexpected source: %#v", source)
	}
}

func TestChatExWithOptions_ToolResultImageContentConverted(t *testing.T) {
	var capturedMessages []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedMessages, _ = req["messages"].([]any)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_tr",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer server.Close()

	client := anthropic.NewClient(
		anthropic.WithURL(server.URL),
		anthropic.WithModel("claude-sonnet"),
		anthropic.WithTimeout(5*time.Second),
	)

	toolCallMsg := ai.NewAIMsgInfo("")
	toolCallMsg.ToolCalls = []*ai.FunctionTool{{
		Id:   "toolu_img",
		Type: "function",
		Function: &ai.FunctionDetail{
			Name:      "screenshot",
			Arguments: "{}",
		},
	}}

	toolResultMsg := ai.NewToolCallResultMsgInfo([]*ai.ChatContext{
		{Type: "text", Text: "screenshot captured"},
		{Type: "image_url", ImageURL: map[string]any{"url": "data:image/jpeg;base64,/9j/4AAQ"}},
	}, "toolu_img")

	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{
		ai.NewUserMsgInfo("take a screenshot"),
		toolCallMsg,
		toolResultMsg,
	})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}

	// Find the tool_result message (role=user with tool_result content).
	var toolResultContent []any
	for _, m := range capturedMessages {
		msg := m.(map[string]any)
		content, ok := msg["content"].([]any)
		if !ok || len(content) == 0 {
			continue
		}
		block := content[0].(map[string]any)
		if block["type"] == "tool_result" {
			toolResultContent, _ = block["content"].([]any)
			break
		}
	}
	if toolResultContent == nil {
		t.Fatalf("tool_result content not found or not an array")
	}
	if len(toolResultContent) != 2 {
		t.Fatalf("expected 2 content blocks in tool_result, got %d", len(toolResultContent))
	}

	textBlock := toolResultContent[0].(map[string]any)
	if textBlock["type"] != "text" || textBlock["text"] != "screenshot captured" {
		t.Fatalf("unexpected text block: %#v", textBlock)
	}

	imageBlock := toolResultContent[1].(map[string]any)
	if imageBlock["type"] != "image" {
		t.Fatalf("expected image block, got %v", imageBlock["type"])
	}
	source := imageBlock["source"].(map[string]any)
	if source["type"] != "base64" || source["media_type"] != "image/jpeg" {
		t.Fatalf("unexpected source: %#v", source)
	}
}

func TestChatExWithOptions_Base64WithNewlines(t *testing.T) {
	var capturedMessages []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		capturedMessages, _ = req["messages"].([]any)

		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_nl",
			"type":        "message",
			"role":        "assistant",
			"stop_reason": "end_turn",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
		})
	}))
	defer server.Close()

	client := anthropic.NewClient(
		anthropic.WithURL(server.URL),
		anthropic.WithModel("claude-sonnet"),
		anthropic.WithTimeout(5*time.Second),
	)

	base64WithNewlines := "iVBORw0KGgo=\nAAAANSUhEUg==\nAAAADAAAAA=="
	_, err := client.ChatEx(context.Background(), []*ai.MsgInfo{
		{
			Role: "user",
			Content: []*ai.ChatContext{
				{Type: "image_url", ImageURL: map[string]any{
					"url": "data:image/png;base64," + base64WithNewlines,
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("ChatEx failed: %v", err)
	}

	if len(capturedMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(capturedMessages))
	}
	msg := capturedMessages[0].(map[string]any)
	content := msg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}

	imageBlock := content[0].(map[string]any)
	if imageBlock["type"] != "image" {
		t.Fatalf("expected image block, got %v", imageBlock["type"])
	}
	source := imageBlock["source"].(map[string]any)
	if source["type"] != "base64" {
		t.Fatalf("expected base64 source, got %v", source["type"])
	}
	if source["data"] != base64WithNewlines {
		t.Fatalf("expected base64 data with newlines preserved, got %v", source["data"])
	}
}
