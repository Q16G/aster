package builtin_tools

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	bt "aster/internal/builtin_tools"
)

// graceBudget 给取消清理留出 graceKillDelay(3s) + 余量。
func graceBudget() time.Duration { return 6 * time.Second }

// assertProcessGone 轮询确认 pid 在宽限期内退出；超时则强杀并 fail。
func assertProcessGone(t *testing.T, label string, pid int) {
	t.Helper()
	deadline := time.Now().Add(graceBudget())
	for {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return // 进程已不存在，符合预期
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL) // 清理，避免测试泄漏
			t.Fatalf("%s (pid=%d) still alive after cancel — process group not cleaned up", label, pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// parseMarkerPID 从 stdout 中解析形如 "MARKER=12345" 的行。
func parseMarkerPID(t *testing.T, stdout, marker string) int {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, marker+"="); ok {
			pid, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				t.Fatalf("bad %s pid %q: %v", marker, v, err)
			}
			return pid
		}
	}
	t.Fatalf("marker %q not found in stdout %q", marker, stdout)
	return 0
}

// TestProcGroup_CancelKillsWholeGroup 验证 ctx 取消时，shell 之下后台 spawn 的
// 子进程（与 shell 同进程组）会被整组清理，而非孤儿残留。
func TestProcGroup_CancelKillsWholeGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash background syntax and POSIX signals")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// 后台启动 sleep 并打印其 PID，shell 自身 wait 保持存活直到被取消。
	script := `sleep 300 & echo CHILD=$!; wait`
	res := bt.RunCommandLimited(ctx, "", "bash", []string{"-c", script}, 1024, 1024, 10*time.Second)

	child := parseMarkerPID(t, res.Stdout, "CHILD")
	assertProcessGone(t, "child", child)
}

// TestProcGroup_CancelKillsGrandchild 忠实地 mock 真实场景：
//
//	sh -lc "semgrep ..."   (level1, 进程组组长)
//	  └─ semgrep (python)  (level2，此处用内层 bash 模拟)
//	       └─ semgrep-core (level3，此处用 sleep 模拟 — 真正需要被杀的孙子进程)
//
// 验证 ctx 取消时，即使中间进程不转发信号，整组 SIGTERM 也能直达孙子进程，
// 使 semgrep-core 类进程一并退出，不会孤儿残留。
func TestProcGroup_CancelKillsGrandchild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses bash background syntax and POSIX signals")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// level2 = 内层 bash（模拟 semgrep python 进程）：它后台拉起 sleep（模拟 semgrep-core），
	// 打印孙子 PID，然后 wait 保持存活。外层 level1 也 wait 保持存活，直到被取消。
	script := `bash -c 'sleep 300 & echo CORE=$!; wait' & echo PY=$!; wait`
	res := bt.RunCommandLimited(ctx, "", "bash", []string{"-c", script}, 4096, 4096, 10*time.Second)

	py := parseMarkerPID(t, res.Stdout, "PY")    // semgrep(python) 类中间进程
	core := parseMarkerPID(t, res.Stdout, "CORE") // semgrep-core 类孙子进程

	assertProcessGone(t, "semgrep(python)-analog", py)
	assertProcessGone(t, "semgrep-core-analog", core)
}
