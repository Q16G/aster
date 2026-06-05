package tui

import "testing"

// TestDefaultPolicies_UnlimitedIterations TUI Profile 默认策略不再带迭代上限：
// MaxIterations==0 经 agent_factory 的 ">0" 守卫不会覆盖 cfg 默认，保持无限制。
func TestDefaultPolicies_UnlimitedIterations(t *testing.T) {
	if got := defaultPolicies().MaxIterations; got != 0 {
		t.Fatalf("expected default policy MaxIterations=0 (unlimited), got %d", got)
	}
}
