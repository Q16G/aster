package react

import "testing"

func TestInferKnownModelContext_DeepSeekV4(t *testing.T) {
	cases := []struct {
		name              string
		model             string
		wantContextWindow int
		wantOutputLimit   int
	}{
		{name: "deepseek-v4-pro", model: "deepseek-v4-pro", wantContextWindow: 1000000, wantOutputLimit: 384000},
		{name: "deepseek-v4-flash", model: "deepseek-v4-flash", wantContextWindow: 1000000, wantOutputLimit: 384000},
		// Legacy aliases that DeepSeek routes to V4-Flash.
		{name: "deepseek-chat", model: "deepseek-chat", wantContextWindow: 1000000, wantOutputLimit: 384000},
		{name: "deepseek-reasoner", model: "deepseek-reasoner", wantContextWindow: 1000000, wantOutputLimit: 384000},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			info, ok := inferKnownModelContext(tc.model)
			if !ok {
				t.Fatalf("expected model %q to be inferred", tc.model)
			}
			if info.ContextWindowTokens != tc.wantContextWindow {
				t.Fatalf("unexpected context window: got=%d want=%d", info.ContextWindowTokens, tc.wantContextWindow)
			}
			if info.OutputTokenLimit != tc.wantOutputLimit {
				t.Fatalf("unexpected output limit: got=%d want=%d", info.OutputTokenLimit, tc.wantOutputLimit)
			}
		})
	}
}
