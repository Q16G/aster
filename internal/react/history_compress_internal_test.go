package react

import (
	"testing"

	"aster/internal/ai"
)

func TestFormatMsgContent_ChatContextTextAndImage(t *testing.T) {
	content := []*ai.ChatContext{
		{Type: "text", Text: "hello"},
		{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAA"}},
		{Type: "text", Text: "world"},
	}
	got := FormatMsgContent(content)
	if got != "hello\n[image]\nworld" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestFormatMsgContent_ChatContextImageOnly(t *testing.T) {
	content := []*ai.ChatContext{
		{Type: "image_url", ImageURL: map[string]any{"url": "data:image/png;base64,AAA"}},
	}
	got := FormatMsgContent(content)
	if got != "[image]" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestFormatMsgContent_ChatContextWithNil(t *testing.T) {
	content := []*ai.ChatContext{
		nil,
		{Type: "text", Text: "ok"},
		nil,
	}
	got := FormatMsgContent(content)
	if got != "ok" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestFormatMsgContent_ChatContextEmptyTextSkipped(t *testing.T) {
	content := []*ai.ChatContext{
		{Type: "text", Text: "   "},
		{Type: "text", Text: "valid"},
	}
	got := FormatMsgContent(content)
	if got != "valid" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestFormatMsgContent_ChatContextEmpty(t *testing.T) {
	content := []*ai.ChatContext{}
	got := FormatMsgContent(content)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestFormatMsgContent_String(t *testing.T) {
	got := FormatMsgContent("hello world")
	if got != "hello world" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestFormatMsgContent_Nil(t *testing.T) {
	got := FormatMsgContent(nil)
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}
