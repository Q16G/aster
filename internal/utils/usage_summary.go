package utils

import (
	"aster/internal/ai"
	aiusage "aster/internal/ai/usage"
)

type UsageSummary struct {
	Usage   *ai.TokenUsage
	Values  map[string]int
	CostUSD float64
}

type usageModelProvider interface {
	UsagePricingModel() aiusage.PricingModel
}

func BuildUsageSummary(client ai.ChatClient, usage *ai.TokenUsage) UsageSummary {
	usage = ai.NormalizeTokenUsagePtr(usage)
	pricing := aiusage.PricingModel{}
	if provider, ok := client.(usageModelProvider); ok {
		pricing = provider.UsagePricingModel()
	}

	result := aiusage.Summarize(pricing, usage)
	values := map[string]int{}
	if result.Tokens.Total != nil && *result.Tokens.Total > 0 {
		values["total_tokens"] = *result.Tokens.Total
	}
	if result.Tokens.Input > 0 {
		values["input_tokens"] = result.Tokens.Input
	}
	if result.Tokens.Output > 0 {
		values["output_tokens"] = result.Tokens.Output
	}
	if result.Tokens.Reasoning > 0 {
		values["reasoning_tokens"] = result.Tokens.Reasoning
	}
	if result.Tokens.Cache.Read > 0 {
		values["cache_read_tokens"] = result.Tokens.Cache.Read
	}
	if result.Tokens.Cache.Write > 0 {
		values["cache_write_tokens"] = result.Tokens.Cache.Write
	}
	if len(values) == 0 {
		values = nil
	}

	return UsageSummary{
		Usage:   usage,
		Values:  values,
		CostUSD: result.Cost,
	}
}
