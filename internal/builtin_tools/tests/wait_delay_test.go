package builtin_tools

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	bt "aster/internal/builtin_tools"
)

func TestWaitDelay_DaemonHoldsPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon pipe test uses bash background process syntax")
	}

	script := `echo "hello from parent"; (sleep 300 &); sleep 0.2`
	defer func() { _ = exec.Command("pkill", "-f", "sleep 300").Run() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	res := bt.RunCommandLimited(ctx, "", "bash", []string{"-c", script}, 1024, 1024, 3*time.Second)
	elapsed := time.Since(start)

	t.Logf("elapsed=%v exitCode=%d stdout=%q err=%v", elapsed, res.ExitCode, res.Stdout, res.RunErr)

	if elapsed > 8*time.Second {
		t.Fatalf("RunCommandLimited blocked for %v — WaitDelay(3s) did not fire in time", elapsed)
	}
	if elapsed < 2*time.Second {
		t.Fatalf("elapsed %v too short — WaitDelay should have waited ~3s for pipe", elapsed)
	}

	if !strings.Contains(res.Stdout, "hello from parent") {
		t.Errorf("expected captured stdout to contain 'hello from parent', got %q", res.Stdout)
	}
}

func TestWaitDelay_ParameterAffectsWaitTime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon pipe test uses bash background process syntax")
	}

	script := `echo "data"; (sleep 300 &); sleep 0.2`
	defer func() { _ = exec.Command("pkill", "-f", "sleep 300").Run() }()

	cases := []struct {
		name      string
		delay     time.Duration
		expectMin time.Duration
		expectMax time.Duration
	}{
		{"2s_delay", 2 * time.Second, 1500 * time.Millisecond, 5 * time.Second},
		{"5s_delay", 5 * time.Second, 4 * time.Second, 8 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			start := time.Now()
			res := bt.RunCommandLimited(ctx, "", "bash", []string{"-c", script}, 1024, 1024, tc.delay)
			elapsed := time.Since(start)

			t.Logf("delay=%v elapsed=%v stdout=%q", tc.delay, elapsed, res.Stdout)

			if elapsed < tc.expectMin || elapsed > tc.expectMax {
				t.Fatalf("expected elapsed in [%v, %v], got %v", tc.expectMin, tc.expectMax, elapsed)
			}
			if !strings.Contains(res.Stdout, "data") {
				t.Errorf("expected 'data' in stdout, got %q", res.Stdout)
			}
		})
	}
}

func TestWaitDelay_ZeroDisablesWaitDelay(t *testing.T) {
	exe := "echo"
	args := []string{"zero-test"}
	if runtime.GOOS == "windows" {
		exe = "cmd.exe"
		args = []string{"/c", "echo", "zero-test"}
	}

	ctx := context.Background()
	start := time.Now()
	res := bt.RunCommandLimited(ctx, "", exe, args, 1024, 1024, 0)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("normal command with waitDelay=0 took %v", elapsed)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d (err=%v)", res.ExitCode, res.RunErr)
	}
	if !strings.Contains(res.Stdout, "zero-test") {
		t.Fatalf("expected 'zero-test' in stdout, got %q", res.Stdout)
	}
}

func TestWaitDelay_NormalCommandUnaffected(t *testing.T) {
	exe := "echo"
	args := []string{"normal"}
	if runtime.GOOS == "windows" {
		exe = "cmd.exe"
		args = []string{"/c", "echo", "normal"}
	}

	ctx := context.Background()
	start := time.Now()
	res := bt.RunCommandLimited(ctx, "", exe, args, 1024, 1024, 10*time.Second)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Fatalf("normal command took %v — WaitDelay should not affect fast commands", elapsed)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d (err=%v)", res.ExitCode, res.RunErr)
	}
	if !strings.Contains(res.Stdout, "normal") {
		t.Fatalf("expected 'normal' in stdout, got %q", res.Stdout)
	}
}

func TestWaitDelay_ContextCancelWithOrphanPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon pipe test uses bash background process syntax")
	}

	defer func() { _ = exec.Command("pkill", "-f", "sleep 300").Run() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	res := bt.RunCommandLimited(ctx, "", "bash", []string{"-c", `echo "before"; (sleep 300 &); sleep 60`}, 1024, 1024, 10*time.Second)
	elapsed := time.Since(start)

	t.Logf("elapsed=%v exitCode=%d stdout=%q err=%v", elapsed, res.ExitCode, res.Stdout, res.RunErr)

	// context fires at 2s, WaitDelay adds up to 10s → should return within ~12s
	if elapsed > 15*time.Second {
		t.Fatalf("blocked for %v after context cancel — WaitDelay not working with context", elapsed)
	}

	if !strings.Contains(res.Stdout, "before") {
		t.Errorf("expected 'before' in captured stdout, got %q", res.Stdout)
	}
}
