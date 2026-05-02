package usage_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"aster/internal/ai"
	. "aster/internal/ai/usage"
)

func TestSummarize_CostAndTokens(t *testing.T) {
	model := PricingModel{
		Cost: &PricingCost{
			Input:  1.0,
			Output: 2.0,
			Cache: &CacheCost{
				Read:  0.5,
				Write: 0.25,
			},
		},
	}
	u := &ai.TokenUsage{
		InputTokens:      1000,
		OutputTokens:     2000,
		ReasoningTokens:  300,
		CacheReadTokens:  400,
		CacheWriteTokens: 500,
	}

	res := Summarize(model, u)
	require.NotNil(t, res.Tokens.Total)
	require.Equal(t, 1000+2000+400+500, *res.Tokens.Total)

	expected := float64(1000)*1.0/1_000_000.0 +
		float64(2000)*2.0/1_000_000.0 +
		float64(400)*0.5/1_000_000.0 +
		float64(500)*0.25/1_000_000.0 +
		float64(300)*2.0/1_000_000.0
	require.InEpsilon(t, expected, res.Cost, 1e-12)
}

func TestSummarize_ExperimentalOver200K(t *testing.T) {
	model := PricingModel{
		Cost: &PricingCost{
			Input:  1.0,
			Output: 1.0,
			ExperimentalOver200K: &PricingCost{
				Input:  10.0,
				Output: 10.0,
			},
		},
	}
	u := &ai.TokenUsage{
		InputTokens:  200001,
		OutputTokens: 1,
	}

	res := Summarize(model, u)
	expected := float64(200001)*10.0/1_000_000.0 + float64(1)*10.0/1_000_000.0
	require.InEpsilon(t, expected, res.Cost, 1e-12)
}
