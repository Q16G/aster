package utils_test

import (
	"context"
	"testing"

	"aster/internal/ai"
	aiusage "aster/internal/ai/usage"
	. "aster/internal/utils"
)

type usageSummaryTestClient struct {
	usagePricing aiusage.PricingModel
}

func (c *usageSummaryTestClient) Chat(ctx context.Context, info *ai.MsgInfo, tools ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *usageSummaryTestClient) ChatEx(ctx context.Context, infos []*ai.MsgInfo, tools ...*ai.FunctionTool) ([]*ai.ChatChoices, error) {
	return nil, nil
}

func (c *usageSummaryTestClient) ChatText(ctx context.Context, text string, tools ...*ai.FunctionTool) (string, error) {
	return "", nil
}

func (c *usageSummaryTestClient) UsagePricingModel() aiusage.PricingModel {
	return c.usagePricing
}

func TestBuildUsageSummary_CalculatesTotalAndCost(t *testing.T) {
	client := &usageSummaryTestClient{
		usagePricing: aiusage.PricingModel{
			Cost: &aiusage.PricingCost{
				Input:  1.0,
				Output: 2.0,
				Cache: &aiusage.CacheCost{
					Read:  0.5,
					Write: 0.25,
				},
			},
		},
	}
	usage := &ai.TokenUsage{
		InputTokens:      1000,
		OutputTokens:     2000,
		ReasoningTokens:  300,
		CacheReadTokens:  400,
		CacheWriteTokens: 500,
	}

	summary := BuildUsageSummary(client, usage)

	if summary.Values["total_tokens"] != 3900 {
		t.Fatalf("expected total_tokens=3900, got %#v", summary.Values)
	}
	if summary.Values["input_tokens"] != 1000 || summary.Values["cache_write_tokens"] != 500 {
		t.Fatalf("unexpected usage payload: %#v", summary.Values)
	}

	expected := float64(1000)*1.0/1_000_000.0 +
		float64(2000)*2.0/1_000_000.0 +
		float64(400)*0.5/1_000_000.0 +
		float64(500)*0.25/1_000_000.0 +
		float64(300)*2.0/1_000_000.0
	if summary.CostUSD < expected*0.999999 || summary.CostUSD > expected*1.000001 {
		t.Fatalf("expected cost %.12f, got %.12f", expected, summary.CostUSD)
	}
}
