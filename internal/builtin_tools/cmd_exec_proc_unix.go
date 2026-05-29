//go:build !windows

package builtin_tools

import (
	"os/exec"
	"syscall"
)

// setProcGroup 让子进程成为独立进程组的组长（pgid == pid），
// 这样后续可以对整组发信号，覆盖 shell 之下 spawn 的所有孙子进程。
func setProcGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killGroup 对命令所在进程组发信号。pid 取负即整组（组长 pid == pgid）。
func killGroup(cmd *exec.Cmd, sig procSignal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	var s syscall.Signal
	switch sig {
	case killSignal:
		s = syscall.SIGKILL
	default:
		s = syscall.SIGTERM
	}
	_ = syscall.Kill(-cmd.Process.Pid, s)
}
