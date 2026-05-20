package builtin_tools

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestAgentBrowserBlock_OpenUnreachable(t *testing.T) {
	if _, err := exec.LookPath("agent-browser"); err != nil {
		t.Skip("agent-browser not found in PATH")
	}

	targets := []struct {
		name string
		url  string
	}{
		{"tcp_timeout", "http://10.255.255.1:9999/"},
		{"test_net", "http://192.0.2.1:9999/"},
		{"localhost_refused", "http://127.0.0.1:19999/"},
	}

	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			start := time.Now()
			cmd := exec.CommandContext(ctx, "agent-browser", "open", target.url, "--ignore-https-errors", "--json")
			out, err := cmd.CombinedOutput()
			elapsed := time.Since(start)

			t.Logf("[%s] elapsed=%v err=%v output=%s", target.name, elapsed, err, string(out))

			if elapsed > 30*time.Second {
				t.Errorf("agent-browser open blocked for %v on %s — this could cause agent hangs", elapsed, target.url)
			} else {
				t.Logf("OK: returned in %v (within timeout)", elapsed)
			}
		})
	}
}

func TestAgentBrowserBlock_SubCommands(t *testing.T) {
	if _, err := exec.LookPath("agent-browser"); err != nil {
		t.Skip("agent-browser not found in PATH")
	}

	commands := []struct {
		name string
		args []string
	}{
		{"snapshot", []string{"snapshot", "-i", "--urls", "--json"}},
		{"screenshot", []string{"screenshot", "--annotate", "--json"}},
		{"get_url", []string{"get", "url", "--json"}},
		{"get_title", []string{"get", "title", "--json"}},
		{"cookies", []string{"cookies", "--json"}},
	}

	for _, cmd := range commands {
		t.Run(cmd.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			start := time.Now()
			c := exec.CommandContext(ctx, "agent-browser", cmd.args...)
			out, err := c.CombinedOutput()
			elapsed := time.Since(start)

			t.Logf("[%s] elapsed=%v err=%v output=%.200s", cmd.name, elapsed, err, string(out))

			if elapsed > 10*time.Second {
				t.Errorf("agent-browser %s blocked for %v — potential hang source", cmd.name, elapsed)
			}
		})
	}
}

func TestAgentBrowserBlock_ContextCancelKillsProcess(t *testing.T) {
	if _, err := exec.LookPath("agent-browser"); err != nil {
		t.Skip("agent-browser not found in PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	start := time.Now()
	cmd := exec.CommandContext(ctx, "agent-browser", "open", "http://10.255.255.1:9999/", "--ignore-https-errors", "--json")
	_, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	t.Logf("elapsed=%v err=%v", elapsed, err)

	if ctx.Err() == context.DeadlineExceeded {
		t.Logf("Context timeout fired at %v — process was killed by context", elapsed)
		if elapsed > 5*time.Second {
			t.Error("Process was not killed promptly after context deadline")
		} else {
			t.Log("OK: exec.CommandContext properly kills agent-browser on timeout")
		}
	} else {
		t.Logf("Command finished before timeout (%v) — agent-browser has internal timeout", elapsed)
	}
}
