package react

import (
	"strings"
	"testing"
)

func TestResolveSubAgentType(t *testing.T) {
	if got := resolveSubAgentType("explore"); got.Name != subAgentTypeExplore {
		t.Fatalf("explore → %q", got.Name)
	}
	if got := resolveSubAgentType("general-purpose"); got.Name != subAgentTypeGeneralPurpose {
		t.Fatalf("general-purpose → %q", got.Name)
	}
	// 未知 / 空名回退 general-purpose。
	for _, in := range []string{"", "  ", "unknown-type", "Explorer"} {
		if got := resolveSubAgentType(in); got.Name != subAgentTypeGeneralPurpose {
			t.Fatalf("resolveSubAgentType(%q) → %q, want general-purpose fallback", in, got.Name)
		}
	}
}

// TestSubAgentTypesDifferOnlyInDescription 锁 U1：两个内置类型只在描述文本上有差异，
// 各自描述非空且不同（工具集统一由 resolveChildToolNames 决定、不按类型分支）。
func TestSubAgentTypesDifferOnlyInDescription(t *testing.T) {
	explore := resolveSubAgentType(subAgentTypeExplore)
	general := resolveSubAgentType(subAgentTypeGeneralPurpose)

	if strings.TrimSpace(explore.SystemPromptExtra) == "" {
		t.Fatal("explore SystemPromptExtra empty")
	}
	if strings.TrimSpace(general.SystemPromptExtra) == "" {
		t.Fatal("general-purpose SystemPromptExtra empty")
	}
	if explore.SystemPromptExtra == general.SystemPromptExtra {
		t.Fatal("两类型描述应不同（类型只差在描述）")
	}
	// explore 的只读约束纯靠描述表达。
	if !strings.Contains(explore.SystemPromptExtra, "只读") {
		t.Errorf("explore 描述应表达只读约束: %q", explore.SystemPromptExtra)
	}
}
