package react

import "testing"

// TestDefaultAgentConfig_UnlimitedIterations 锁定默认无限制语义：
// 默认配置不再带 120 上限，MaxIterations==0 表示不限制迭代次数。
func TestDefaultAgentConfig_UnlimitedIterations(t *testing.T) {
	cfg := defaultAgentConfig(nil)
	if cfg.MaxIterations != 0 {
		t.Fatalf("expected default MaxIterations=0 (unlimited), got %d", cfg.MaxIterations)
	}
}

// TestDefaultSubAgentMaxIter_Unlimited 子 Agent 默认同样无限制。
func TestDefaultSubAgentMaxIter_Unlimited(t *testing.T) {
	if defaultSubAgentMaxIter != 0 {
		t.Fatalf("expected defaultSubAgentMaxIter=0 (unlimited), got %d", defaultSubAgentMaxIter)
	}
}

// TestWithMaxIterations_OptIn 保留 opt-in：正值生效，非正值忽略（不会把
// 默认的无限制覆盖成有限/异常值）。
func TestWithMaxIterations_OptIn(t *testing.T) {
	cfg := defaultAgentConfig(nil)

	WithMaxIterations(30)(cfg)
	if cfg.MaxIterations != 30 {
		t.Fatalf("expected MaxIterations=30 after opt-in, got %d", cfg.MaxIterations)
	}

	// 非正值应被忽略，保持先前设定的上限不变。
	WithMaxIterations(0)(cfg)
	if cfg.MaxIterations != 30 {
		t.Fatalf("WithMaxIterations(0) should be ignored, got %d", cfg.MaxIterations)
	}
	WithMaxIterations(-5)(cfg)
	if cfg.MaxIterations != 30 {
		t.Fatalf("WithMaxIterations(-5) should be ignored, got %d", cfg.MaxIterations)
	}
}

// TestIterationAllowed 表驱动核对循环放行条件：
// maxIterations <= 0 永远放行（无限制），> 0 时在 iter<=max 内放行。
func TestIterationAllowed(t *testing.T) {
	cases := []struct {
		name          string
		iter          int
		maxIterations int
		want          bool
	}{
		{"unlimited/zero allows iter 1", 1, 0, true},
		{"unlimited/zero allows large iter", 100000, 0, true},
		{"unlimited/negative allows iter", 9999, -1, true},
		{"limited/within bound", 5, 30, true},
		{"limited/at bound", 30, 30, true},
		{"limited/over bound", 31, 30, false},
		{"limited/one allows first only", 1, 1, true},
		{"limited/one rejects second", 2, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := iterationAllowed(tc.iter, tc.maxIterations); got != tc.want {
				t.Fatalf("iterationAllowed(%d, %d) = %v, want %v", tc.iter, tc.maxIterations, got, tc.want)
			}
		})
	}
}
