package anthropic

import (
	"strings"
	"testing"

	"aster/internal/ai"
)

func TestAnyToText_ChatContextTextAndImage(t *testing.T) {
	contexts := []*ai.ChatContext{
		{Type: "text", Text: "hello"},
		{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAA"}},
		{Type: "text", Text: "world"},
	}
	got := anyToText(contexts)
	if got != "hello\n[image]\nworld" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestAnyToText_ChatContextImageOnly(t *testing.T) {
	contexts := []*ai.ChatContext{
		{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAA"}},
	}
	got := anyToText(contexts)
	if got != "[image]" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestAnyToText_ChatContextEmptyText(t *testing.T) {
	contexts := []*ai.ChatContext{
		{Type: "text", Text: "  "},
		nil,
		{Type: "text", Text: "ok"},
	}
	got := anyToText(contexts)
	if got != "ok" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestAnyToText_String(t *testing.T) {
	got := anyToText("plain text")
	if got != "plain text" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestAnyToText_OtherType(t *testing.T) {
	got := anyToText(map[string]any{"key": "value"})
	if got != `{"key":"value"}` {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestExtractChatContexts_DirectSlice(t *testing.T) {
	input := []*ai.ChatContext{
		{Type: "text", Text: "hi"},
	}
	result := extractChatContexts(input)
	if len(result) != 1 || result[0].Type != "text" {
		t.Fatalf("unexpected: %+v", result)
	}
}

func TestExtractChatContexts_SliceAny(t *testing.T) {
	input := []any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "http://example.com/img.png"}},
	}
	result := extractChatContexts(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(result))
	}
	if result[0].Type != "text" || result[0].Text != "hello" {
		t.Fatalf("unexpected text context: %+v", result[0])
	}
	if result[1].Type != "image_url" {
		t.Fatalf("unexpected image context: %+v", result[1])
	}
	url, _ := result[1].ImageURL["url"].(string)
	if url != "http://example.com/img.png" {
		t.Fatalf("unexpected image url: %q", url)
	}
}

func TestExtractChatContexts_MapSingle(t *testing.T) {
	input := map[string]any{"type": "text", "text": "single"}
	result := extractChatContexts(input)
	if len(result) != 1 || result[0].Type != "text" || result[0].Text != "single" {
		t.Fatalf("unexpected: %+v", result)
	}
}

func TestExtractChatContexts_MapNoType(t *testing.T) {
	input := map[string]any{"foo": "bar"}
	result := extractChatContexts(input)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %+v", result)
	}
}

func TestExtractChatContexts_SliceAnyInvalid(t *testing.T) {
	input := []any{"not a map", 42}
	result := extractChatContexts(input)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %+v", result)
	}
}

func TestExtractChatContexts_Nil(t *testing.T) {
	result := extractChatContexts(nil)
	if result != nil {
		t.Fatalf("expected nil, got %+v", result)
	}
}

func TestExtractChatContexts_StringInput(t *testing.T) {
	result := extractChatContexts("just a string")
	if result != nil {
		t.Fatalf("expected nil, got %+v", result)
	}
}

func TestParseDataURI_Valid(t *testing.T) {
	media, data, ok := parseDataURI("data:image/png;base64,iVBOR")
	if !ok || media != "image/png" || data != "iVBOR" {
		t.Fatalf("unexpected: media=%q data=%q ok=%v", media, data, ok)
	}
}

func TestParseDataURI_WithNewlines(t *testing.T) {
	media, data, ok := parseDataURI("data:image/png;base64,iVBOR\nAAAA\nBBBB")
	if !ok || media != "image/png" {
		t.Fatalf("unexpected: media=%q ok=%v", media, ok)
	}
	if data != "iVBOR\nAAAA\nBBBB" {
		t.Fatalf("expected newlines preserved, got %q", data)
	}
}

func TestParseDataURI_NoBase64Marker(t *testing.T) {
	_, _, ok := parseDataURI("data:image/png,rawdata")
	if ok {
		t.Fatalf("expected false for missing ;base64, marker")
	}
}

func TestParseDataURI_EmptyMediaType(t *testing.T) {
	_, _, ok := parseDataURI("data:;base64,AAA")
	if ok {
		t.Fatalf("expected false for empty media type")
	}
}

func TestParseDataURI_EmptyData(t *testing.T) {
	_, _, ok := parseDataURI("data:image/png;base64,")
	if ok {
		t.Fatalf("expected false for empty data")
	}
}

func TestParseDataURI_NotDataURI(t *testing.T) {
	_, _, ok := parseDataURI("https://example.com/img.png")
	if ok {
		t.Fatalf("expected false for non-data URI")
	}
}

func TestParseDataURI_WithMediaParams(t *testing.T) {
	media, data, ok := parseDataURI("data:text/plain;charset=utf-8;base64,SGVsbG8=")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if media != "text/plain;charset=utf-8" {
		t.Fatalf("unexpected media: %q", media)
	}
	if data != "SGVsbG8=" {
		t.Fatalf("unexpected data: %q", data)
	}
}

func TestConvertImageURLToAnthropicSource_Base64(t *testing.T) {
	src := convertImageURLToAnthropicSource(map[string]any{
		"url": "data:image/jpeg;base64,/9j/4AAQ",
	})
	if src == nil {
		t.Fatalf("expected non-nil source")
	}
	if src["type"] != "base64" || src["media_type"] != "image/jpeg" || src["data"] != "/9j/4AAQ" {
		t.Fatalf("unexpected: %#v", src)
	}
}

func TestConvertImageURLToAnthropicSource_URL(t *testing.T) {
	src := convertImageURLToAnthropicSource(map[string]any{
		"url": "https://example.com/img.png",
	})
	if src == nil || src["type"] != "url" || src["url"] != "https://example.com/img.png" {
		t.Fatalf("unexpected: %#v", src)
	}
}

func TestConvertImageURLToAnthropicSource_NilMap(t *testing.T) {
	if src := convertImageURLToAnthropicSource(nil); src != nil {
		t.Fatalf("expected nil, got %#v", src)
	}
}

func TestConvertImageURLToAnthropicSource_EmptyURL(t *testing.T) {
	if src := convertImageURLToAnthropicSource(map[string]any{"url": ""}); src != nil {
		t.Fatalf("expected nil, got %#v", src)
	}
}

func TestConvertImageURLToAnthropicSource_UnknownScheme(t *testing.T) {
	if src := convertImageURLToAnthropicSource(map[string]any{"url": "ftp://files/img.png"}); src != nil {
		t.Fatalf("expected nil for ftp, got %#v", src)
	}
}

func TestConvertImageURLToAnthropicSource_InvalidDataURI(t *testing.T) {
	if src := convertImageURLToAnthropicSource(map[string]any{"url": "data:image/png,rawdata"}); src != nil {
		t.Fatalf("expected nil for non-base64 data URI, got %#v", src)
	}
}

func TestBuildToolResultContent_WithImages(t *testing.T) {
	info := &ai.MsgInfo{
		Role: "tool",
		Content: []*ai.ChatContext{
			{Type: "text", Text: "result text"},
			{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAA"}},
		},
	}
	result := buildToolResultContent(info)
	blocks, ok := result.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", result)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0]["type"] != "text" {
		t.Fatalf("expected text block, got %v", blocks[0]["type"])
	}
	if blocks[1]["type"] != "image" {
		t.Fatalf("expected image block, got %v", blocks[1]["type"])
	}
}

func TestBuildToolResultContent_StringFallback(t *testing.T) {
	info := &ai.MsgInfo{
		Role:    "tool",
		Content: "plain tool result",
	}
	result := buildToolResultContent(info)
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if s != "plain tool result" {
		t.Fatalf("unexpected: %q", s)
	}
}

func TestBuildToolResultContent_EmptyBlocksFallback(t *testing.T) {
	info := &ai.MsgInfo{
		Role: "tool",
		Content: []*ai.ChatContext{
			{Type: "text", Text: "  "},
			{Type: "image_url", ImageURL: map[string]any{"url": "invalid://nope"}},
		},
	}
	result := buildToolResultContent(info)
	s, ok := result.(string)
	if !ok {
		t.Fatalf("expected string fallback when all blocks invalid, got %T", result)
	}
	expected := anyToText(info.Content)
	if s != expected {
		t.Fatalf("expected fallback=%q, got %q", expected, s)
	}
}

func TestDefaultConfig_SupportsVision(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.SupportsVision == nil {
		t.Fatalf("expected SupportsVision to be set")
	}
	if !*cfg.SupportsVision {
		t.Fatalf("expected SupportsVision=true by default")
	}
}

func TestBuildAnthropicContent_NilInfo(t *testing.T) {
	result := buildAnthropicContent(nil)
	if result != nil {
		t.Fatalf("expected nil, got %+v", result)
	}
}

func TestBuildAnthropicContent_StringFallback(t *testing.T) {
	info := &ai.MsgInfo{
		Role:    "user",
		Content: "plain text fallback",
	}
	result := buildAnthropicContent(info)
	if len(result) != 1 || result[0]["type"] != "text" || result[0]["text"] != "plain text fallback" {
		t.Fatalf("unexpected: %+v", result)
	}
}

func TestBuildAnthropicContent_ToolCallsWithImage(t *testing.T) {
	info := &ai.MsgInfo{
		Role: "assistant",
		Content: []*ai.ChatContext{
			{Type: "text", Text: "here is the screenshot"},
		},
		ToolCalls: []*ai.FunctionTool{
			{
				Id:   "call-1",
				Type: "function",
				Function: &ai.FunctionDetail{
					Name:      "screenshot",
					Arguments: `{"url":"http://example.com"}`,
				},
			},
		},
	}
	result := buildAnthropicContent(info)
	if len(result) < 2 {
		t.Fatalf("expected >=2 blocks, got %d", len(result))
	}
	hasText, hasToolUse := false, false
	for _, b := range result {
		if b["type"] == "text" {
			hasText = true
		}
		if b["type"] == "tool_use" {
			hasToolUse = true
		}
	}
	if !hasText || !hasToolUse {
		t.Fatalf("expected text and tool_use blocks, got %+v", result)
	}
}

func TestSplitMessages_SystemImageDegradedToText(t *testing.T) {
	infos := []*ai.MsgInfo{
		{
			Role: "system",
			Content: []*ai.ChatContext{
				{Type: "text", Text: "you are a bot"},
				{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAA"}},
			},
		},
	}
	system, messages := splitMessages(infos, nil)
	if len(system) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(system))
	}
	if !strings.Contains(system[0].Text, "you are a bot") {
		t.Fatalf("expected system text, got %q", system[0].Text)
	}
	if !strings.Contains(system[0].Text, "[image]") {
		t.Fatalf("expected [image] placeholder in system text, got %q", system[0].Text)
	}
	if len(messages) != 0 {
		t.Fatalf("expected no messages, got %d", len(messages))
	}
}

func TestSplitMessages_StringUserMessage(t *testing.T) {
	infos := []*ai.MsgInfo{
		{Role: "user", Content: "hello"},
	}
	_, messages := splitMessages(infos, nil)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0]["role"] != "user" {
		t.Fatalf("expected role=user, got %v", messages[0]["role"])
	}
	content, ok := messages[0]["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected content blocks, got %T", messages[0]["content"])
	}
	if content[0]["type"] != "text" || content[0]["text"] != "hello" {
		t.Fatalf("expected text block with 'hello', got %+v", content[0])
	}
}

func TestSplitMessages_ToolWithImage(t *testing.T) {
	infos := []*ai.MsgInfo{
		{
			Role:       "tool",
			ToolCallID: "call-1",
			Content: []*ai.ChatContext{
				{Type: "text", Text: "result"},
				{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAA"}},
			},
		},
	}
	_, messages := splitMessages(infos, nil)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	content := messages[0]["content"].([]map[string]any)
	toolResult := content[0]
	blocks, ok := toolResult["content"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any for tool_result content, got %T", toolResult["content"])
	}
	hasImage := false
	for _, b := range blocks {
		if b["type"] == "image" {
			hasImage = true
		}
	}
	if !hasImage {
		t.Fatalf("expected image block in tool_result, got %+v", blocks)
	}
}

func TestBuildAnthropicContent_WithImage(t *testing.T) {
	info := &ai.MsgInfo{
		Role: "user",
		Content: []*ai.ChatContext{
			{Type: "text", Text: "look at this"},
			{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAA"}},
		},
	}
	result := buildAnthropicContent(info)
	if len(result) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(result))
	}
	if result[0]["type"] != "text" || result[0]["text"] != "look at this" {
		t.Fatalf("expected text block, got %+v", result[0])
	}
	if result[1]["type"] != "image" {
		t.Fatalf("expected image block, got %+v", result[1])
	}
	source, ok := result[1]["source"].(map[string]any)
	if !ok {
		t.Fatalf("expected source map, got %T", result[1]["source"])
	}
	if source["type"] != "base64" || source["media_type"] != "image/png" {
		t.Fatalf("unexpected source: %+v", source)
	}
}

func TestAnyToText_EmptyChatContextSlice(t *testing.T) {
	got := anyToText([]*ai.ChatContext{})
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestConvertImageURLToAnthropicSource_URLNonString(t *testing.T) {
	if src := convertImageURLToAnthropicSource(map[string]any{"url": 12345}); src != nil {
		t.Fatalf("expected nil for non-string url, got %#v", src)
	}
}

func TestConvertImageURLToAnthropicSource_HTTPUrl(t *testing.T) {
	src := convertImageURLToAnthropicSource(map[string]any{
		"url": "http://example.com/img.png",
	})
	if src == nil || src["type"] != "url" || src["url"] != "http://example.com/img.png" {
		t.Fatalf("unexpected: %#v", src)
	}
}
