package usage

import (
	"math"

	"aster/internal/ai"
)

type CacheCost struct {
	Read  float64 `json:"read"`
	Write float64 `json:"write"`
}

type PricingCost struct {
	Input                float64      `json:"input"`
	Output               float64      `json:"output"`
	Cache                *CacheCost   `json:"cache,omitempty"`
	ExperimentalOver200K *PricingCost `json:"experimentalOver200K,omitempty"`
}

type PricingModel struct {
	Cost *PricingCost `json:"cost,omitempty"`
}

type CacheTokenCounts struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

type TokenCounts struct {
	Total     *int             `json:"total,omitempty"`
	Input     int              `json:"input"`
	Output    int              `json:"output"`
	Reasoning int              `json:"reasoning"`
	Cache     CacheTokenCounts `json:"cache"`
}

type Result struct {
	Cost   float64     `json:"cost"`
	Tokens TokenCounts `json:"tokens"`
}

// Summarize computes normalized token counts and cost from a pricing model.
//
// NOTE: In this codebase, ai.TokenUsage's InputTokens are already adjusted to exclude cached
// tokens for providers that include cached tokens in inputTokens. CacheReadTokens/CacheWriteTokens
// are kept as-is.
func Summarize(pricing PricingModel, u *ai.TokenUsage) Result {
	safe := func(v int) int {
		if v < 0 {
			return 0
		}
		return v
	}
	input := safe(0)
	output := safe(0)
	reasoning := safe(0)
	cacheRead := safe(0)
	cacheWrite := safe(0)
	total := 0
	hasTotal := false

	if u != nil {
		input = safe(u.InputTokens)
		output = safe(u.OutputTokens)
		reasoning = safe(u.ReasoningTokens)
		cacheRead = safe(u.CacheReadTokens)
		cacheWrite = safe(u.CacheWriteTokens)
		if u.TotalTokens > 0 {
			total = u.TotalTokens
			hasTotal = true
		}
	}

	if !hasTotal {
		total = input + output + cacheRead + cacheWrite
		hasTotal = true
	}

	var totalPtr *int
	if hasTotal {
		v := total
		totalPtr = &v
	}

	tokens := TokenCounts{
		Total:     totalPtr,
		Input:     input,
		Output:    output,
		Reasoning: reasoning,
		Cache: CacheTokenCounts{
			Read:  cacheRead,
			Write: cacheWrite,
		},
	}

	costInfo := pricing.Cost
	if costInfo != nil && costInfo.ExperimentalOver200K != nil && tokens.Input+tokens.Cache.Read > 200_000 {
		costInfo = costInfo.ExperimentalOver200K
	}

	cost := 0.0
	if costInfo != nil {
		cost += float64(tokens.Input) * safeRate(costInfo.Input) / 1_000_000.0
		cost += float64(tokens.Output) * safeRate(costInfo.Output) / 1_000_000.0
		if costInfo.Cache != nil {
			cost += float64(tokens.Cache.Read) * safeRate(costInfo.Cache.Read) / 1_000_000.0
			cost += float64(tokens.Cache.Write) * safeRate(costInfo.Cache.Write) / 1_000_000.0
		}
		// Charge reasoning tokens at the same rate as output tokens.
		cost += float64(tokens.Reasoning) * safeRate(costInfo.Output) / 1_000_000.0
	}

	if !isFinite(cost) {
		cost = 0
	}
	return Result{
		Cost:   cost,
		Tokens: tokens,
	}
}

func safeRate(v float64) float64 {
	if !isFinite(v) {
		return 0
	}
	return v
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
