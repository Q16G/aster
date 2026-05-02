package react_test

import (
	. "aster/internal/react"
	"bytes"
	"context"
	"strings"
	"testing"

	"aster/internal/ai"
	"aster/internal/runtimelog"
)

type historyCompressLogClient struct{}

func (c *historyCompressLogClient) Chat(_ context.Context, _ *ai.MsgInfo, _ ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *historyCompressLogClient) ChatEx(_ context.Context, _ []*ai.MsgInfo, _ ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return []*ai.ChatChoices{{
		Message: ai.NewAIMsgInfo("compressed-summary"),
	}}, nil
}

func (c *historyCompressLogClient) ChatText(_ context.Context, _ string, _ ...*ai.FunctionTool) (string, error) {
	return "compressed-summary", nil
}

func TestAIHistoryCompressor_WritesCompactionLogs(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := runtimelog.SetOutput(&buf)
	t.Cleanup(func() {
		runtimelog.SetOutput(prevWriter)
	})

	// 阈值需要大于压缩后 summary 的 token 数（~21 tokens by rune/2），
	// 但小于原始 history 的 token 数（~127 tokens），以触发压缩且能收敛。
	compressor := NewAIHistoryCompressorWithTokenBudget(30, 0)
	history := []*ai.MsgInfo{
		ai.NewUserMsgInfo("first user question with enough words to exceed budget"),
		ai.NewAIMsgInfo("first assistant answer with enough words to exceed budget"),
		ai.NewUserMsgInfo("second user question with enough words to exceed budget"),
		ai.NewAIMsgInfo("second assistant answer with enough words to exceed budget"),
	}

	result, err := compressor.Compress(context.Background(), &historyCompressLogClient{}, "summarize", history)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if result == nil || !result.DidCompact {
		t.Fatalf("expected history compaction to happen, got %#v", result)
	}

	out := buf.String()
	if !strings.Contains(out, "\"event\":\"history_compaction_triggered\"") {
		t.Fatalf("expected history_compaction_triggered log, got %s", out)
	}
	if !strings.Contains(out, "\"event\":\"history_compaction_completed\"") {
		t.Fatalf("expected history_compaction_completed log, got %s", out)
	}
}
