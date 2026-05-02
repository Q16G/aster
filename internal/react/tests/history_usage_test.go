package react_test

import (
	. "aster/internal/react"
	"testing"

	"aster/internal/ai"
)

func TestIsOverflowWithUsage(t *testing.T) {
	budget := ContextBudget{
		ModelName:           "gpt-4o-mini",
		ContextWindowTokens: 128000,
		InputTokenLimit:     96000,
		OutputTokenLimit:    32000,
		OutputCapTokens:     32000,
		UsableInputTokens:   96000,
		TriggerTokens:       96000,
	}
	usage := &ai.TokenUsage{
		InputTokens:      70000,
		OutputTokens:     20000,
		CacheReadTokens:  10000,
		ReasoningTokens:  5000,
		CacheWriteTokens: 0,
	}
	if !IsOverflowWithUsage(usage, budget) {
		t.Fatalf("expected overflow when input+cache.read+output > usable_input")
	}

	usage = &ai.TokenUsage{
		InputTokens:     40000,
		OutputTokens:    10000,
		CacheReadTokens: 5000,
	}
	if IsOverflowWithUsage(usage, budget) {
		t.Fatalf("expected no overflow when usage within budget")
	}
}

func TestLatestAssistantUsageFromHistory(t *testing.T) {
	history := []*ai.MsgInfo{
		{Role: "user", Content: "Q1"},
		{Role: "assistant", Content: "A1", Usage: &ai.TokenUsage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 2}},
		{Role: "assistant", Content: "A2", Usage: &ai.TokenUsage{InputTokens: 20, OutputTokens: 7, CacheReadTokens: 3}},
	}
	usage, idx := LatestAssistantUsageFromHistory(history)
	if usage == nil {
		t.Fatalf("expected latest assistant usage")
	}
	if idx != 2 {
		t.Fatalf("expected latest usage index 2, got %d", idx)
	}
	if usage.InputTokens != 20 || usage.OutputTokens != 7 || usage.CacheReadTokens != 3 {
		t.Fatalf("unexpected usage: %#v", usage)
	}
}

func TestTotalAssistantUsageContextTokens(t *testing.T) {
	history := []*ai.MsgInfo{
		{Role: "user", Content: "Q1"},
		{Role: "assistant", Content: "A1", Usage: &ai.TokenUsage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 2}},
		{Role: "assistant", Content: "A2", Usage: &ai.TokenUsage{InputTokens: 20, OutputTokens: 7, CacheReadTokens: 3}},
		{Role: "assistant", Content: "A3"},
	}
	total, hasUsage := TotalAssistantUsageContextTokens(history)
	if !hasUsage {
		t.Fatalf("expected hasUsage=true")
	}
	// (10+2+5) + (20+3+7) = 47
	if total != 47 {
		t.Fatalf("unexpected total usage context tokens: %d", total)
	}
}

func TestTotalAssistantUsageContextTokens_NoUsage(t *testing.T) {
	history := []*ai.MsgInfo{
		{Role: "user", Content: "Q1"},
		{Role: "assistant", Content: "A1"},
		{Role: "tool", Content: "T1"},
	}
	total, hasUsage := TotalAssistantUsageContextTokens(history)
	if hasUsage {
		t.Fatalf("expected hasUsage=false")
	}
	if total != 0 {
		t.Fatalf("expected total=0, got %d", total)
	}
}
