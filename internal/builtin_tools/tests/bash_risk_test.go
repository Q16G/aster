package builtin_tools_test

import (
	. "aster/internal/builtin_tools"
	"testing"
)

func TestParseModelRisk(t *testing.T) {
	tests := []struct {
		input    string
		expected RiskLevel
	}{
		{"low", RiskLevelLow},
		{"Low", RiskLevelLow},
		{"LOW", RiskLevelLow},
		{"high", RiskLevelHigh},
		{"High", RiskLevelHigh},
		{"uncertain", RiskLevelUncertain},
		{"Uncertain", RiskLevelUncertain},
		// 无效值一律 uncertain
		{"", RiskLevelUncertain},
		{"  ", RiskLevelUncertain},
		{"medium", RiskLevelUncertain},
		{"unknown", RiskLevelUncertain},
	}
	for _, tc := range tests {
		got := ParseModelRisk(tc.input)
		if got != tc.expected {
			t.Errorf("ParseModelRisk(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestResolvePermissionDecision_Allowlist(t *testing.T) {
	// allowlist 命中 → 所有模式均放行
	for _, mode := range []PermissionMode{PermissionModeYOLO, PermissionModeManual, PermissionModeAI} {
		for _, risk := range []RiskLevel{RiskLevelLow, RiskLevelHigh, RiskLevelUncertain} {
			if ResolvePermissionDecision(mode, risk, true) {
				t.Errorf("allowlistHit=true, mode=%s, risk=%s: expected no confirmation", mode, risk)
			}
		}
	}
}

func TestResolvePermissionDecision_YOLO(t *testing.T) {
	// YOLO 模式 → 无论风险等级均放行
	for _, risk := range []RiskLevel{RiskLevelLow, RiskLevelHigh, RiskLevelUncertain} {
		if ResolvePermissionDecision(PermissionModeYOLO, risk, false) {
			t.Errorf("YOLO mode, risk=%s: expected no confirmation", risk)
		}
	}
}

func TestResolvePermissionDecision_MANUAL(t *testing.T) {
	// MANUAL 模式 → 未命中 allowlist 时一律需要确认
	for _, risk := range []RiskLevel{RiskLevelLow, RiskLevelHigh, RiskLevelUncertain} {
		if !ResolvePermissionDecision(PermissionModeManual, risk, false) {
			t.Errorf("MANUAL mode, risk=%s: expected confirmation required", risk)
		}
	}
}

func TestResolvePermissionDecision_AI(t *testing.T) {
	tests := []struct {
		risk     RiskLevel
		wantConf bool
	}{
		{RiskLevelLow, false},      // low → 自动
		{RiskLevelHigh, true},      // high → 确认
		{RiskLevelUncertain, true}, // uncertain → 确认
	}
	for _, tc := range tests {
		got := ResolvePermissionDecision(PermissionModeAI, tc.risk, false)
		if got != tc.wantConf {
			t.Errorf("AI mode, risk=%s: got confirmation=%v, want %v", tc.risk, got, tc.wantConf)
		}
	}
}

func TestIsCompoundCommand(t *testing.T) {
	compound := []string{
		"echo hello && echo world",
		"cat file | grep pattern",
		"ls; rm -f /tmp/x",
		"echo $(whoami)",
		"echo `date`",
		"cat <<EOF\nhello\nEOF",
		"diff <(sort a) <(sort b)",
	}
	for _, cmd := range compound {
		if !IsCompoundCommand(cmd) {
			t.Errorf("expected compound for %q", cmd)
		}
	}

	simple := []string{
		"go test ./...",
		"git status",
		"npm run build",
		"echo 'hello && world'",
		`echo "hello | world"`,
	}
	for _, cmd := range simple {
		if IsCompoundCommand(cmd) {
			t.Errorf("expected not compound for %q", cmd)
		}
	}
}
