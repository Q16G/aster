package builtin_tools

import "testing"

func TestResolveTimeout_Semgrep(t *testing.T) {
	bt := &BashTool{}

	cases := []struct {
		name    string
		args    map[string]any
		command string
		want    int64
	}{
		{"semgrep_default", map[string]any{}, "semgrep scan --config x .", bashSemgrepTimeoutMs},
		{"semgrep_with_env_prefix", map[string]any{}, "FOO=1 semgrep scan .", bashSemgrepTimeoutMs},
		{"semgrep_explicit_override", map[string]any{"timeout_ms": int64(120000)}, "semgrep scan .", 120000},
		{"plain_command", map[string]any{}, "ls -la", bashDefaultTimeoutMs},
		{"build_command", map[string]any{}, "go test ./...", bashBuildTestTimeoutMs},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bt.resolveTimeout(tc.args, tc.command); got != tc.want {
				t.Fatalf("resolveTimeout(%q) = %d, want %d", tc.command, got, tc.want)
			}
		})
	}
}
