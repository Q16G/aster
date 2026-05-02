package memory_test

import (
	. "aster/internal/memory"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/runtimelog"
)

type failingTimelineMemoryChatClient struct {
	err error
}

func (c *failingTimelineMemoryChatClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", c.err
}

func (c *failingTimelineMemoryChatClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, c.err
}

func (c *failingTimelineMemoryChatClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "", c.err
}

func TestTimelineMemory_Compress_WritesFailureLog(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

	tm := NewTimeLine(context.Background(), &failingTimelineMemoryChatClient{err: errors.New("boom")}, nil, WithKeepLastItems(0))
	if err := tm.AddItem("1", NewEnvironmentItem(strings.Repeat("x", 64))); err != nil {
		t.Fatalf("AddItem failed: %v", err)
	}

	if err := tm.Compress(); err == nil {
		t.Fatalf("expected Compress error")
	}

	out := buf.String()
	if !strings.Contains(out, "\"event\":\"timeline_memory_compress_failed\"") {
		t.Fatalf("expected timeline_memory_compress_failed log, got %s", out)
	}
}

func TestTimelineMemory_CompressOldMemories_WritesFailureLog(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

	tm := NewTimeLine(context.Background(), &failingTimelineMemoryChatClient{err: errors.New("boom")}, nil, WithTriggerBytes(1), WithKeepLastItems(0))
	if err := tm.AddItem("1", NewEnvironmentItem(strings.Repeat("y", 64))); err != nil {
		t.Fatalf("AddItem failed: %v", err)
	}

	if err := tm.CompressOldMemories(); err == nil {
		t.Fatalf("expected CompressOldMemories error")
	}

	out := buf.String()
	if !strings.Contains(out, "\"event\":\"timeline_memory_compress_old_failed\"") {
		t.Fatalf("expected timeline_memory_compress_old_failed log, got %s", out)
	}
}
